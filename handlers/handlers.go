package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"go-api-game/auth"
	"go-api-game/utils"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var db *sql.DB

// InitDB initializes the database connection
func InitDB(database *sql.DB) {
	db = database
}

// RootHandler handles the root endpoint
func RootHandler(w http.ResponseWriter, r *http.Request) {
	utils.JSONResponse(w, map[string]string{
		"message": "Game Store API",
		"version": "1.0",
	}, http.StatusOK)
}

// RegisterHandler handles user registration
func RegisterHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("🔍 Register Handler - Method: %s, Content-Type: %s\n", r.Method, r.Header.Get("Content-Type"))

	if r.Method != "POST" {
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	var avatarURL string

	contentType := r.Header.Get("Content-Type")

	if strings.Contains(contentType, "multipart/form-data") {
		// Handle multipart form (มีไฟล์ avatar)
		fmt.Printf("📝 Processing as multipart/form-data\n")

		err := r.ParseMultipartForm(10 << 20) // 10 MB limit
		if err != nil {
			utils.JSONError(w, "Error parsing form data", http.StatusBadRequest)
			return
		}

		// Get form values
		req.Username = r.FormValue("username")
		req.Email = r.FormValue("email")
		req.Password = r.FormValue("password")

		// Handle avatar file upload
		file, header, err := r.FormFile("avatar")
		if err == nil {
			defer file.Close()

			// ✅ ลบการตรวจสอบประเภทไฟล์ออก - อนุญาตทุกไฟล์
			// ไม่มีการตรวจสอบนามสกุลไฟล์อีกต่อไป

			// Create unique filename
			ext := strings.ToLower(filepath.Ext(header.Filename))
			if ext == "" {
				// ถ้าไฟล์ไม่มีนามสกุล ให้ใช้ .dat เป็น default
				ext = ".dat"
			}
			filename := fmt.Sprintf("avatar_%d%s", time.Now().UnixNano(), ext)
			filePath := filepath.Join("uploads", filename)

			// Save file
			dst, err := os.Create(filePath)
			if err != nil {
				utils.JSONError(w, "Error saving avatar", http.StatusInternalServerError)
				return
			}
			defer dst.Close()

			if _, err := io.Copy(dst, file); err != nil {
				utils.JSONError(w, "Error copying avatar", http.StatusInternalServerError)
				return
			}

			avatarURL = "/uploads/" + filename
			fmt.Printf("✅ Avatar uploaded: %s\n", avatarURL)
		} else {
			// ไม่มีไฟล์ avatar ส่งมา → ใช้ default avatar
			avatarURL = "/uploads/default-avatar.png"
			fmt.Printf("📝 No avatar uploaded, using default: %s\n", avatarURL)
		}

		fmt.Printf("🔍 Form data - Username: %s, Email: %s, Password: %s, Avatar: %s\n",
			req.Username, req.Email, "***", avatarURL)

	} else if strings.Contains(contentType, "application/json") {
		// Handle JSON
		fmt.Printf("📝 Processing as JSON\n")

		body, err := io.ReadAll(r.Body)
		if err != nil {
			fmt.Printf("❌ Error reading body: %v\n", err)
			utils.JSONError(w, "Error reading request body", http.StatusBadRequest)
			return
		}

		fmt.Printf("🔍 Raw request body: %s\n", string(body))
		r.Body = io.NopCloser(bytes.NewBuffer(body))

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			fmt.Printf("❌ JSON decode error: %v\n", err)
			utils.JSONError(w, "Invalid JSON format: "+err.Error(), http.StatusBadRequest)
			return
		}

		// สำหรับ JSON request → ใช้ default avatar
		avatarURL = "/uploads/default-avatar.png"
		fmt.Printf("🔍 JSON data - Username: %s, Email: %s, Password: %s, Avatar: %s\n",
			req.Username, req.Email, "***", avatarURL)
	} else {
		utils.JSONError(w, "Content-Type must be application/json or multipart/form-data", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.Username == "" || req.Email == "" || req.Password == "" {
		utils.JSONError(w, "Username, email and password are required", http.StatusBadRequest)
		return
	}

	// Validate email format
	if !isValidEmail(req.Email) {
		utils.JSONError(w, "Invalid email format", http.StatusBadRequest)
		return
	}

	// Validate password strength
	if len(req.Password) < 6 {
		utils.JSONError(w, "Password must be at least 6 characters", http.StatusBadRequest)
		return
	}

	// Check if username or email already exists
	var count int
	err := db.QueryRow(`
        SELECT COUNT(*) 
        FROM users 
        WHERE username = ? OR email = ?
    `, req.Username, req.Email).Scan(&count)

	if err != nil {
		utils.JSONError(w, "Error checking user existence", http.StatusInternalServerError)
		return
	}

	if count > 0 {
		// Check which field is duplicate
		var existingUsername, existingEmail string
		db.QueryRow(`
            SELECT username, email 
            FROM users 
            WHERE username = ? OR email = ?
            LIMIT 1
        `, req.Username, req.Email).Scan(&existingUsername, &existingEmail)

		if existingUsername == req.Username {
			utils.JSONError(w, "Username already exists", http.StatusBadRequest)
			return
		}
		if existingEmail == req.Email {
			utils.JSONError(w, "Email already exists", http.StatusBadRequest)
			return
		}
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		utils.JSONError(w, "Error processing password", http.StatusInternalServerError)
		return
	}

	// Insert user with avatar_url (ตอนนี้จะมี avatar_url เสมอ)
	result, err := db.Exec(`
        INSERT INTO users (username, email, password_hash, role, avatar_url) 
        VALUES (?, ?, ?, 'user', ?)
    `, req.Username, req.Email, string(hashedPassword), avatarURL)

	if err != nil {
		// Delete uploaded file if database insert fails (เฉพาะไฟล์ที่อัปโหลดใหม่)
		if avatarURL != "" && avatarURL != "/uploads/default-avatar.png" {
			os.Remove(strings.TrimPrefix(avatarURL, "/"))
		}
		utils.JSONError(w, "Error creating user: "+err.Error(), http.StatusInternalServerError)
		return
	}

	userID, _ := result.LastInsertId()

	// Create cart for user
	_, err = db.Exec("INSERT INTO carts (user_id) VALUES (?)", userID)
	if err != nil {
		// Delete uploaded file if cart creation fails (เฉพาะไฟล์ที่อัปโหลดใหม่)
		if avatarURL != "" && avatarURL != "/uploads/default-avatar.png" {
			os.Remove(strings.TrimPrefix(avatarURL, "/"))
		}
		utils.JSONError(w, "Error creating cart", http.StatusInternalServerError)
		return
	}

	fmt.Printf("✅ User registered successfully: ID=%d, Username=%s, Avatar: %s\n",
		userID, req.Username, avatarURL)

	// Return response with avatar_url
	response := map[string]interface{}{
		"message":    "User registered successfully",
		"user_id":    userID,
		"username":   req.Username,
		"email":      req.Email,
		"avatar_url": avatarURL, // ส่ง avatar_url ตลอด
	}

	utils.JSONResponse(w, response, http.StatusCreated)
}

// isValidEmail checks if email format is valid
func isValidEmail(email string) bool {
	// Simple email validation
	if len(email) < 3 || len(email) > 254 {
		return false
	}

	// Check for @ symbol
	at := strings.Index(email, "@")
	if at == -1 || at == 0 || at == len(email)-1 {
		return false
	}

	// Check for dot after @
	dot := strings.LastIndex(email[at:], ".")
	if dot == -1 || dot == 0 || dot == len(email[at:])-1 {
		return false
	}

	return true
}

// LoginHandler handles user login with identifier (username or email)
func LoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Identifier string `json:"identifier"`
		Password   string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.JSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	fmt.Printf("🔍 Login attempt: identifier='%s'\n", req.Identifier)

	if req.Identifier == "" || req.Password == "" {
		utils.JSONError(w, "Identifier and password are required", http.StatusBadRequest)
		return
	}

	var userID int
	var username, email, passwordHash, role string

	// วิธีง่าย: ไม่ต้อง select avatar_url
	err := db.QueryRow(`
		SELECT id, username, email, password_hash, role 
		FROM users 
		WHERE username = ? OR email = ?
	`, req.Identifier, req.Identifier).Scan(
		&userID, &username, &email, &passwordHash, &role,
	)

	if err != nil {
		fmt.Printf("❌ Database error: %v\n", err)
		if err == sql.ErrNoRows {
			utils.JSONError(w, "Invalid identifier or password", http.StatusUnauthorized)
		} else {
			utils.JSONError(w, "Error during login: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}

	fmt.Printf("✅ User found: ID=%d, Username=%s, Email=%s, Role=%s\n", userID, username, email, role)
	fmt.Printf("🔑 Password hash: %s...\n", passwordHash[:20])

	// Check password
	err = bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password))
	if err != nil {
		fmt.Printf("❌ Password mismatch: %v\n", err)
		utils.JSONError(w, "Invalid identifier or password", http.StatusUnauthorized)
		return
	}

	fmt.Printf("✅ Password correct!\n")

	// Generate JWT token
	token, err := auth.GenerateToken(userID, username, email, role)
	if err != nil {
		utils.JSONError(w, "Error generating token", http.StatusInternalServerError)
		return
	}

	fmt.Printf("🎉 Login successful for user: %s, role: %s\n", username, role)

	utils.JSONResponse(w, map[string]interface{}{
		"message":  "Login successful",
		"user_id":  userID,
		"username": username,
		"email":    email,
		"role":     role,
		"token":    token,
	}, http.StatusOK)
}

// GamesHandler returns all games
func GamesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	fmt.Printf("🔍 Fetching all games\n")

	// ใช้ DATE_FORMAT เพื่อแปลง DATE เป็น string โดยตรง
	rows, err := db.Query(`
		SELECT g.id, g.name, g.price, c.name as category, g.image_url, 
		       g.description, 
		       DATE_FORMAT(g.release_date, '%Y-%m-%d') as release_date,
		       r.rank_position
		FROM games g
		LEFT JOIN categories c ON g.category_id = c.id
		LEFT JOIN ranking r ON g.id = r.game_id
		ORDER BY g.id
	`)
	if err != nil {
		fmt.Printf("❌ Error fetching games: %v\n", err)
		utils.JSONError(w, "Error fetching games: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var games []map[string]interface{}
	count := 0

	for rows.Next() {
		var id int
		var name string
		var price float64
		var category string
		var imageURL, description sql.NullString
		var releaseDate sql.NullString // เปลี่ยนเป็น string
		var rank sql.NullInt64

		err := rows.Scan(&id, &name, &price, &category, &imageURL, &description, &releaseDate, &rank)
		if err != nil {
			fmt.Printf("❌ Error scanning game row: %v\n", err)
			continue
		}

		game := map[string]interface{}{
			"id":          id,
			"name":        name,
			"price":       price,
			"category":    category,
			"image_url":   imageURL.String,
			"description": description.String,
			"rank":        rank.Int64,
		}

		// Handle release date
		if releaseDate.Valid && releaseDate.String != "" {
			game["release_date"] = releaseDate.String
		} else {
			game["release_date"] = nil
		}

		games = append(games, game)
		count++

		fmt.Printf("✅ Game found: ID=%d, Name=%s, Price=%.2f\n", id, name, price)
	}

	if err = rows.Err(); err != nil {
		fmt.Printf("❌ Error during rows iteration: %v\n", err)
		utils.JSONError(w, "Error processing games", http.StatusInternalServerError)
		return
	}

	fmt.Printf("✅ Total games found: %d\n", count)

	if games == nil {
		games = []map[string]interface{}{}
	}

	utils.JSONResponse(w, games, http.StatusOK)
}

// GameByIDHandler returns a specific game by ID
func GameByIDHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	pathParts := strings.Split(r.URL.Path, "/")
	idStr := pathParts[len(pathParts)-1]
	gameID, err := strconv.Atoi(idStr)
	if err != nil {
		utils.JSONError(w, "Invalid game ID", http.StatusBadRequest)
		return
	}

	fmt.Printf("🔍 Fetching game by ID: %d\n", gameID)

	// ใช้ DATE_FORMAT เพื่อแปลง DATE เป็น string โดยตรง
	var game struct {
		ID          int
		Name        string
		Price       float64
		Category    string
		ImageURL    sql.NullString
		Description sql.NullString
		ReleaseDate sql.NullString
		Rank        sql.NullInt64
	}

	err = db.QueryRow(`
		SELECT g.id, g.name, g.price, c.name as category, g.image_url, 
		       g.description, 
		       DATE_FORMAT(g.release_date, '%Y-%m-%d') as release_date,
		       r.rank_position
		FROM games g
		LEFT JOIN categories c ON g.category_id = c.id
		LEFT JOIN ranking r ON g.id = r.game_id
		WHERE g.id = ?
	`, gameID).Scan(&game.ID, &game.Name, &game.Price, &game.Category,
		&game.ImageURL, &game.Description, &game.ReleaseDate, &game.Rank)

	if err != nil {
		fmt.Printf("❌ Error fetching game ID %d: %v\n", gameID, err)
		if err == sql.ErrNoRows {
			utils.JSONError(w, "Game not found", http.StatusNotFound)
		} else {
			utils.JSONError(w, "Error fetching game: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}

	fmt.Printf("✅ Game found: ID=%d, Name=%s\n", game.ID, game.Name)

	gameMap := map[string]interface{}{
		"id":          game.ID,
		"name":        game.Name,
		"price":       game.Price,
		"category":    game.Category,
		"image_url":   game.ImageURL.String,
		"description": game.Description.String,
		"rank":        game.Rank.Int64,
	}

	if game.ReleaseDate.Valid && game.ReleaseDate.String != "" {
		gameMap["release_date"] = game.ReleaseDate.String
	} else {
		gameMap["release_date"] = nil
	}

	utils.JSONResponse(w, gameMap, http.StatusOK)
}

// CategoriesHandler returns all categories
func CategoriesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rows, err := db.Query("SELECT id, name FROM categories")
	if err != nil {
		utils.JSONError(w, "Error fetching categories", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var categories []map[string]interface{}
	for rows.Next() {
		var id int
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			continue
		}
		categories = append(categories, map[string]interface{}{
			"id":   id,
			"name": name,
		})
	}

	utils.JSONResponse(w, categories, http.StatusOK)
}

// SearchHandler handles game search
func SearchHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query().Get("q")
	category := r.URL.Query().Get("category")

	fmt.Printf("🔍 Search request - Query: '%s', Category: '%s'\n", query, category)

	sqlQuery := `
		SELECT g.id, g.name, g.price, c.name as category, g.image_url, 
		       g.description, 
		       DATE_FORMAT(g.release_date, '%Y-%m-%d') as release_date,
		       r.rank_position
		FROM games g
		LEFT JOIN categories c ON g.category_id = c.id
		LEFT JOIN ranking r ON g.id = r.game_id
		WHERE 1=1
	`
	args := []interface{}{}

	if query != "" {
		sqlQuery += " AND (g.name LIKE ? OR g.description LIKE ?)"
		searchTerm := "%" + query + "%"
		args = append(args, searchTerm, searchTerm)
	}

	if category != "" {
		sqlQuery += " AND c.name = ?"
		args = append(args, category)
	}

	sqlQuery += " ORDER BY g.id"

	fmt.Printf("🔍 Executing search query: %s\n", sqlQuery)
	fmt.Printf("🔍 Query parameters: %v\n", args)

	rows, err := db.Query(sqlQuery, args...)
	if err != nil {
		fmt.Printf("❌ Error searching games: %v\n", err)
		utils.JSONError(w, "Error searching games: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var games []map[string]interface{}
	count := 0

	for rows.Next() {
		var id int
		var name string
		var price float64
		var category string
		var imageURL, description sql.NullString
		var releaseDate sql.NullString
		var rank sql.NullInt64

		err := rows.Scan(&id, &name, &price, &category, &imageURL, &description, &releaseDate, &rank)
		if err != nil {
			fmt.Printf("❌ Error scanning search result row: %v\n", err)
			continue
		}

		game := map[string]interface{}{
			"id":          id,
			"name":        name,
			"price":       price,
			"category":    category,
			"image_url":   imageURL.String,
			"description": description.String,
			"rank":        rank.Int64,
		}

		if releaseDate.Valid && releaseDate.String != "" {
			game["release_date"] = releaseDate.String
		} else {
			game["release_date"] = nil
		}

		games = append(games, game)
		count++
		fmt.Printf("✅ Search result: ID=%d, Name=%s\n", id, name)
	}

	if err = rows.Err(); err != nil {
		fmt.Printf("❌ Error during search rows iteration: %v\n", err)
		utils.JSONError(w, "Error processing search results", http.StatusInternalServerError)
		return
	}

	fmt.Printf("✅ Search completed: found %d games\n", count)

	if games == nil {
		games = []map[string]interface{}{}
	}

	utils.JSONResponse(w, games, http.StatusOK)
}

// RankingHandler returns game rankings
func RankingHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	fmt.Printf("🔍 Fetching game rankings\n")

	// ใช้ sql.NullInt64 สำหรับ rank_position
	rows, err := db.Query(`
		SELECT g.id, g.name, g.price, c.name as category, g.image_url, 
		       r.sales_count, r.rank_position,
		       DATE_FORMAT(g.release_date, '%Y-%m-%d') as release_date
		FROM ranking r
		JOIN games g ON r.game_id = g.id
		JOIN categories c ON g.category_id = c.id
		ORDER BY COALESCE(r.rank_position, 999), r.sales_count DESC
		LIMIT 5
	`)
	if err != nil {
		fmt.Printf("❌ Error fetching rankings: %v\n", err)
		utils.JSONError(w, "Error fetching rankings: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var rankings []map[string]interface{}
	count := 0

	for rows.Next() {
		var id int
		var name string
		var price float64
		var category string
		var imageURL sql.NullString
		var salesCount int
		var rank sql.NullInt64 // เปลี่ยนเป็น sql.NullInt64
		var releaseDate sql.NullString

		err := rows.Scan(&id, &name, &price, &category, &imageURL, &salesCount, &rank, &releaseDate)
		if err != nil {
			fmt.Printf("❌ Error scanning ranking row: %v\n", err)
			continue
		}

		// Handle NULL rank_position
		rankValue := 0
		if rank.Valid {
			rankValue = int(rank.Int64)
		}

		ranking := map[string]interface{}{
			"id":            id,
			"name":          name,
			"price":         price,
			"category":      category,
			"image_url":     imageURL.String,
			"sales_count":   salesCount,
			"rank_position": rankValue,
		}

		if releaseDate.Valid && releaseDate.String != "" {
			ranking["release_date"] = releaseDate.String
		} else {
			ranking["release_date"] = nil
		}

		rankings = append(rankings, ranking)
		count++
		fmt.Printf("✅ Ranking: Position=%d, Game=%s, Sales=%d\n", rankValue, name, salesCount)
	}

	if err = rows.Err(); err != nil {
		fmt.Printf("❌ Error during ranking rows iteration: %v\n", err)
		utils.JSONError(w, "Error processing rankings", http.StatusInternalServerError)
		return
	}

	fmt.Printf("✅ Total rankings found: %d\n", count)

	if rankings == nil {
		rankings = []map[string]interface{}{}
	}

	utils.JSONResponse(w, rankings, http.StatusOK)
}

// User profile handlers
func ProfileHandler(w http.ResponseWriter, r *http.Request) {
	userIDStr := r.Header.Get("User-ID")

	fmt.Printf("🔍 Profile request - User-ID header: '%s'\n", userIDStr)

	if userIDStr == "" {
		utils.JSONError(w, "User ID not found in headers", http.StatusUnauthorized)
		return
	}

	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		fmt.Printf("❌ Invalid User-ID format: %s\n", userIDStr)
		utils.JSONError(w, "Invalid user ID format", http.StatusBadRequest)
		return
	}

	fmt.Printf("🔍 Querying database for user ID: %d\n", userID)

	var id int
	var username, email string
	var avatarURL sql.NullString
	var walletBalance float64

	err = db.QueryRow(`
		SELECT id, username, email, avatar_url, wallet_balance 
		FROM users 
		WHERE id = ?
	`, userID).Scan(&id, &username, &email, &avatarURL, &walletBalance)

	if err != nil {
		fmt.Printf("❌ Database error in ProfileHandler: %v\n", err)
		fmt.Printf("❌ SQL Error details: %v\n", err)

		if err == sql.ErrNoRows {
			utils.JSONError(w, "User not found in database", http.StatusNotFound)
		} else {
			utils.JSONError(w, "Database error: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}

	fmt.Printf("✅ Database result - ID: %d, Username: %s, Email: %s, Balance: %.2f\n",
		id, username, email, walletBalance)

	// Build response
	profile := map[string]interface{}{
		"id":             id,
		"username":       username,
		"email":          email,
		"wallet_balance": walletBalance,
		"avatar_url":     "",
	}

	if avatarURL.Valid {
		profile["avatar_url"] = avatarURL.String
	}

	fmt.Printf("🎉 Sending profile response\n")
	utils.JSONResponse(w, profile, http.StatusOK)
}

func WalletHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("User-ID")

	var balance float64
	err := db.QueryRow("SELECT wallet_balance FROM users WHERE id = ?", userID).Scan(&balance)
	if err != nil {
		utils.JSONError(w, "Error fetching wallet", http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, map[string]interface{}{
		"balance": balance,
	}, http.StatusOK)
}

func DepositHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := r.Header.Get("User-ID")

	var req struct {
		Amount float64 `json:"amount"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.JSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Amount <= 0 {
		utils.JSONError(w, "Amount must be positive", http.StatusBadRequest)
		return
	}

	// Start transaction
	tx, err := db.Begin()
	if err != nil {
		utils.JSONError(w, "Error starting transaction", http.StatusInternalServerError)
		return
	}

	// Update wallet balance
	_, err = tx.Exec("UPDATE users SET wallet_balance = wallet_balance + ? WHERE id = ?",
		req.Amount, userID)
	if err != nil {
		tx.Rollback()
		utils.JSONError(w, "Error updating wallet", http.StatusInternalServerError)
		return
	}

	// Record transaction
	_, err = tx.Exec(`
		INSERT INTO user_transactions (user_id, type, amount, description) 
		VALUES (?, 'deposit', ?, ?)
	`, userID, req.Amount, fmt.Sprintf("Deposit: $%.2f", req.Amount))
	if err != nil {
		tx.Rollback()
		utils.JSONError(w, "Error recording transaction", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		utils.JSONError(w, "Error committing transaction", http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, map[string]interface{}{
		"message": "Deposit successful",
		"amount":  req.Amount,
	}, http.StatusOK)
}

func TransactionsHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("User-ID")

	fmt.Printf("🔍 Transactions request for user ID: %s\n", userID)

	if userID == "" {
		utils.JSONError(w, "User ID not found", http.StatusUnauthorized)
		return
	}

	userIDInt, err := strconv.Atoi(userID)
	if err != nil {
		utils.JSONError(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	// ใช้ DATE_FORMAT เพื่อได้ string โดยตรงจาก MySQL
	rows, err := db.Query(`
		SELECT type, amount, description, 
		       DATE_FORMAT(created_at, '%Y-%m-%d %H:%i:%s') as created_date
		FROM user_transactions 
		WHERE user_id = ? 
		ORDER BY created_at DESC
	`, userIDInt)

	if err != nil {
		fmt.Printf("❌ Error executing transactions query: %v\n", err)
		utils.JSONError(w, "Error fetching transactions", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var transactions []map[string]interface{}

	for rows.Next() {
		var txType string
		var amount float64
		var description string
		var createdAt string // ใช้ string ธรรมดา

		if err := rows.Scan(&txType, &amount, &description, &createdAt); err != nil {
			fmt.Printf("❌ Error scanning transaction row: %v\n", err)
			continue
		}

		fmt.Printf("✅ Transaction found: Type=%s, Amount=%.2f\n", txType, amount)

		transactions = append(transactions, map[string]interface{}{
			"type":        txType,
			"amount":      amount,
			"description": description,
			"date":        createdAt,
		})
	}

	if transactions == nil {
		transactions = []map[string]interface{}{}
	}

	fmt.Printf("✅ Returning %d transactions\n", len(transactions))
	utils.JSONResponse(w, transactions, http.StatusOK)
}

func LibraryHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("User-ID")

	fmt.Printf("🔍 Library request for user ID: %s\n", userID)

	if userID == "" {
		utils.JSONError(w, "User ID not found", http.StatusUnauthorized)
		return
	}

	userIDInt, err := strconv.Atoi(userID)
	if err != nil {
		utils.JSONError(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	fmt.Printf("🔍 Querying library for user ID: %d\n", userIDInt)

	// ใช้ DATE_FORMAT เพื่อแปลง DATE เป็น string โดยตรง
	rows, err := db.Query(`
		SELECT g.id, g.name, g.price, c.name as category, g.image_url, 
		       g.description, 
		       DATE_FORMAT(g.release_date, '%Y-%m-%d') as release_date,
		       DATE_FORMAT(pg.purchased_at, '%Y-%m-%d %H:%i:%s') as purchased_date
		FROM purchased_games pg
		JOIN games g ON pg.game_id = g.id
		JOIN categories c ON g.category_id = c.id
		WHERE pg.user_id = ?
		ORDER BY pg.purchased_at DESC
	`, userIDInt)

	if err != nil {
		fmt.Printf("❌ Error fetching library: %v\n", err)
		utils.JSONError(w, "Error fetching library: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var games []map[string]interface{}
	count := 0

	for rows.Next() {
		var id int
		var name string
		var price float64
		var category string
		var imageURL, description sql.NullString
		var releaseDate sql.NullString
		var purchasedDate string

		err := rows.Scan(&id, &name, &price, &category, &imageURL, &description, &releaseDate, &purchasedDate)
		if err != nil {
			fmt.Printf("❌ Error scanning library row: %v\n", err)
			continue
		}

		game := map[string]interface{}{
			"id":           id,
			"name":         name,
			"price":        price,
			"category":     category,
			"image_url":    imageURL.String,
			"description":  description.String,
			"purchased_at": purchasedDate,
		}

		if releaseDate.Valid && releaseDate.String != "" {
			game["release_date"] = releaseDate.String
		} else {
			game["release_date"] = nil
		}

		games = append(games, game)
		count++
		fmt.Printf("✅ Library game: ID=%d, Name=%s, Purchased=%s\n", id, name, purchasedDate)
	}

	if err = rows.Err(); err != nil {
		fmt.Printf("❌ Error during library rows iteration: %v\n", err)
		utils.JSONError(w, "Error processing library", http.StatusInternalServerError)
		return
	}

	fmt.Printf("✅ Total library games found: %d\n", count)

	// Always return games array, even if empty
	if games == nil {
		games = []map[string]interface{}{}
	}

	utils.JSONResponse(w, map[string]interface{}{
		"total_games": count,
		"games":       games,
	}, http.StatusOK)
}

// Cart handlers
func CartHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("User-ID")

	rows, err := db.Query(`
		SELECT g.id, g.name, g.price, c.name as category, g.image_url, ci.quantity
		FROM cart_items ci
		JOIN games g ON ci.game_id = g.id
		JOIN categories c ON g.category_id = c.id
		JOIN carts ca ON ci.cart_id = ca.id
		WHERE ca.user_id = ?
	`, userID)
	if err != nil {
		utils.JSONError(w, "Error fetching cart", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var cartItems []map[string]interface{}
	total := 0.0

	for rows.Next() {
		var item struct {
			ID       int     `json:"id"`
			Name     string  `json:"name"`
			Price    float64 `json:"price"`
			Category string  `json:"category"`
			ImageURL string  `json:"image_url"`
			Quantity int     `json:"quantity"`
		}

		if err := rows.Scan(&item.ID, &item.Name, &item.Price, &item.Category, &item.ImageURL, &item.Quantity); err != nil {
			continue
		}

		itemTotal := item.Price * float64(item.Quantity)
		total += itemTotal

		cartItems = append(cartItems, map[string]interface{}{
			"game_id":   item.ID,
			"name":      item.Name,
			"price":     item.Price,
			"category":  item.Category,
			"image_url": item.ImageURL,
			"quantity":  item.Quantity,
			"subtotal":  itemTotal,
		})
	}

	utils.JSONResponse(w, map[string]interface{}{
		"items":      cartItems,
		"total":      total,
		"item_count": len(cartItems),
	}, http.StatusOK)
}

func AddToCartHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := r.Header.Get("User-ID")

	var req struct {
		GameID int `json:"game_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.JSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Check if user already owns the game
	var owned bool
	err := db.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM purchased_games WHERE user_id = ? AND game_id = ?
		)
	`, userID, req.GameID).Scan(&owned)
	if err != nil {
		utils.JSONError(w, "Error checking ownership", http.StatusInternalServerError)
		return
	}

	if owned {
		utils.JSONError(w, "You already own this game", http.StatusBadRequest)
		return
	}

	// Get user's cart ID
	var cartID int
	err = db.QueryRow("SELECT id FROM carts WHERE user_id = ?", userID).Scan(&cartID)
	if err != nil {
		utils.JSONError(w, "Error finding cart", http.StatusInternalServerError)
		return
	}

	// Add to cart
	_, err = db.Exec(`
		INSERT INTO cart_items (cart_id, game_id, quantity) 
		VALUES (?, ?, 1)
		ON DUPLICATE KEY UPDATE quantity = quantity + 1
	`, cartID, req.GameID)
	if err != nil {
		utils.JSONError(w, "Error adding to cart", http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, map[string]string{
		"message": "Game added to cart",
	}, http.StatusOK)
}

func RemoveFromCartHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := r.Header.Get("User-ID")

	var req struct {
		GameID int `json:"game_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.JSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Get user's cart ID
	var cartID int
	err := db.QueryRow("SELECT id FROM carts WHERE user_id = ?", userID).Scan(&cartID)
	if err != nil {
		utils.JSONError(w, "Error finding cart", http.StatusInternalServerError)
		return
	}

	// Remove from cart
	_, err = db.Exec("DELETE FROM cart_items WHERE cart_id = ? AND game_id = ?", cartID, req.GameID)
	if err != nil {
		utils.JSONError(w, "Error removing from cart", http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, map[string]string{
		"message": "Game removed from cart",
	}, http.StatusOK)
}

func CheckoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userIDStr := r.Header.Get("User-ID")
	userID, _ := strconv.Atoi(userIDStr)

	var req struct {
		DiscountCode string `json:"discount_code"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.JSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Start transaction
	tx, err := db.Begin()
	if err != nil {
		utils.JSONError(w, "Error starting transaction", http.StatusInternalServerError)
		return
	}

	// Get cart items and total
	rows, err := tx.Query(`
		SELECT g.id, g.name, g.price, ci.quantity
		FROM cart_items ci
		JOIN games g ON ci.game_id = g.id
		JOIN carts ca ON ci.cart_id = ca.id
		WHERE ca.user_id = ?
	`, userID)
	if err != nil {
		tx.Rollback()
		utils.JSONError(w, "Error fetching cart items", http.StatusInternalServerError)
		return
	}
	defer rows.Close() // ✅ ใช้ defer เพื่อปิด rows

	var cartItems []struct {
		GameID   int
		Name     string
		Price    float64
		Quantity int
	}
	total := 0.0

	for rows.Next() {
		var item struct {
			GameID   int
			Name     string
			Price    float64
			Quantity int
		}
		if err := rows.Scan(&item.GameID, &item.Name, &item.Price, &item.Quantity); err != nil {
			tx.Rollback()
			utils.JSONError(w, "Error scanning cart items", http.StatusInternalServerError)
			return
		}
		cartItems = append(cartItems, item)
		total += item.Price * float64(item.Quantity)
	}

	if err := rows.Err(); err != nil {
		tx.Rollback()
		utils.JSONError(w, "Error reading cart items", http.StatusInternalServerError)
		return
	}

	if len(cartItems) == 0 {
		tx.Rollback()
		utils.JSONError(w, "Cart is empty", http.StatusBadRequest)
		return
	}

	// Check for duplicate games in library
	for _, item := range cartItems {
		var owned bool
		err := tx.QueryRow(`
			SELECT EXISTS(
				SELECT 1 FROM purchased_games WHERE user_id = ? AND game_id = ?
			)
		`, userID, item.GameID).Scan(&owned)
		if err != nil {
			tx.Rollback()
			utils.JSONError(w, "Error checking game ownership", http.StatusInternalServerError)
			return
		}
		if owned {
			tx.Rollback()
			utils.JSONError(w, fmt.Sprintf("You already own: %s", item.Name), http.StatusBadRequest)
			return
		}
	}

	// Apply discount if provided
	var discountCodeID *int
	var discountValue float64
	finalAmount := total

	if req.DiscountCode != "" {
		var discount struct {
			ID               int
			Type             string
			Value            float64
			MinTotal         float64
			UsageLimit       *int
			SingleUsePerUser bool
			Active           bool
		}

		// ✅ ใช้ sql.NullString สำหรับรับค่า date จาก database
		var startDateStr, endDateStr sql.NullString

		err := tx.QueryRow(`
			SELECT id, type, value, min_total, usage_limit, single_use_per_user, 
			       active, start_date, end_date
			FROM discount_codes 
			WHERE code = ? AND active = 1
		`, req.DiscountCode).Scan(
			&discount.ID, &discount.Type, &discount.Value, &discount.MinTotal,
			&discount.UsageLimit, &discount.SingleUsePerUser, &discount.Active,
			&startDateStr, &endDateStr, // ✅ รับเป็น string ก่อน
		)

		if err == nil {
			// ✅ Convert string date to time.Time
			var startDate, endDate *time.Time

			if startDateStr.Valid && startDateStr.String != "" {
				parsedStart, err := time.Parse("2006-01-02", startDateStr.String)
				if err == nil {
					startDate = &parsedStart
				}
			}

			if endDateStr.Valid && endDateStr.String != "" {
				parsedEnd, err := time.Parse("2006-01-02", endDateStr.String)
				if err == nil {
					endDate = &parsedEnd
				}
			}

			// Check discount validity
			now := time.Now()
			if startDate != nil && now.Before(*startDate) {
				tx.Rollback()
				utils.JSONError(w, "Discount code not yet valid", http.StatusBadRequest)
				return
			}
			if endDate != nil && now.After(*endDate) {
				tx.Rollback()
				utils.JSONError(w, "Discount code has expired", http.StatusBadRequest)
				return
			}
			if discount.MinTotal > 0 && total < discount.MinTotal {
				tx.Rollback()
				utils.JSONError(w, fmt.Sprintf("Minimum purchase of $%.2f required", discount.MinTotal), http.StatusBadRequest)
				return
			}

			// Check usage limit
			if discount.UsageLimit != nil {
				var usageCount int
				err := tx.QueryRow(`
                SELECT COUNT(*) 
                FROM user_discount_codes 
                WHERE discount_code_id = ?
            `, discount.ID).Scan(&usageCount)

				if err == nil && usageCount >= *discount.UsageLimit {
					// ❌ ตั้งค่า active = 0 เมื่อใช้ครบจำนวน
					tx.Exec("UPDATE discount_codes SET active = 0 WHERE id = ?", discount.ID)
					fmt.Printf("🚫 Discount code deactivated: ID=%d, usage reached limit\n", discount.ID)

					tx.Rollback()
					utils.JSONError(w, "Discount code usage limit reached", http.StatusBadRequest)
					return
				}
			}

			// Check if user already used this code
			if discount.SingleUsePerUser {
				var used bool
				err := tx.QueryRow(`
					SELECT EXISTS(
						SELECT 1 FROM user_discount_codes 
						WHERE user_id = ? AND discount_code_id = ?
					)
				`, userID, discount.ID).Scan(&used)
				if err != nil {
					tx.Rollback()
					utils.JSONError(w, "Error checking discount usage", http.StatusInternalServerError)
					return
				}
				if used {
					tx.Rollback()
					utils.JSONError(w, "Discount code already used", http.StatusBadRequest)
					return
				}
			}

			// Apply discount
			if discount.Type == "percent" {
				discountValue = total * (discount.Value / 100)
			} else {
				discountValue = discount.Value
			}

			finalAmount = total - discountValue
			if finalAmount < 0 {
				finalAmount = 0
			}

			discountCodeID = &discount.ID

			fmt.Printf("✅ Discount applied in checkout: Code=%s, Discount=%.2f, Final=%.2f\n",
				req.DiscountCode, discountValue, finalAmount)
		} else if err != sql.ErrNoRows {
			// ❌ Database error (ไม่ใช่แค่หาไม่เจอ)
			tx.Rollback()
			utils.JSONError(w, "Error checking discount code", http.StatusInternalServerError)
			return
		}
		// ถ้า err == sql.ErrNoRows ก็แค่ไม่ใช้ส่วนลด (ไม่ต้องทำอะไร)
	}

	// Check wallet balance
	var walletBalance float64
	err = tx.QueryRow("SELECT wallet_balance FROM users WHERE id = ?", userID).Scan(&walletBalance)
	if err != nil {
		tx.Rollback()
		utils.JSONError(w, "Error checking wallet balance", http.StatusInternalServerError)
		return
	}

	if walletBalance < finalAmount {
		tx.Rollback()
		utils.JSONError(w, "Insufficient wallet balance", http.StatusBadRequest)
		return
	}

	// Create purchase record
	result, err := tx.Exec(`
		INSERT INTO purchases (user_id, total_amount, discount_code_id, final_amount)
		VALUES (?, ?, ?, ?)
	`, userID, total, discountCodeID, finalAmount)
	if err != nil {
		tx.Rollback()
		utils.JSONError(w, "Error creating purchase record", http.StatusInternalServerError)
		return
	}

	purchaseID, _ := result.LastInsertId()

	// Add purchase items and mark games as purchased
	for _, item := range cartItems {
		// Add to purchase items
		_, err := tx.Exec(`
			INSERT INTO purchase_items (purchase_id, game_id, price_at_purchase)
			VALUES (?, ?, ?)
		`, purchaseID, item.GameID, item.Price)
		if err != nil {
			tx.Rollback()
			utils.JSONError(w, "Error recording purchase items", http.StatusInternalServerError)
			return
		}

		// Add to purchased games
		_, err = tx.Exec(`
			INSERT INTO purchased_games (user_id, game_id) 
			VALUES (?, ?)
		`, userID, item.GameID)
		if err != nil {
			tx.Rollback()
			utils.JSONError(w, "Error adding to library", http.StatusInternalServerError)
			return
		}

		// Update ranking sales count
		_, err = tx.Exec(`
			INSERT INTO ranking (game_id, sales_count) 
			VALUES (?, 1)
			ON DUPLICATE KEY UPDATE sales_count = sales_count + 1
		`, item.GameID)
		if err != nil {
			tx.Rollback()
			utils.JSONError(w, "Error updating rankings", http.StatusInternalServerError)
			return
		}
	}

	// Update rankings order
	_, err = tx.Exec(`
		UPDATE ranking 
		SET rank_position = (
			SELECT rnk FROM (
				SELECT game_id, RANK() OVER (ORDER BY sales_count DESC) as rnk
				FROM ranking
			) r WHERE r.game_id = ranking.game_id
		)
	`)
	if err != nil {
		tx.Rollback()
		utils.JSONError(w, "Error updating rank positions", http.StatusInternalServerError)
		return
	}

	// Record discount usage
	if discountCodeID != nil {
		_, err = tx.Exec(`
            INSERT INTO user_discount_codes (user_id, discount_code_id)
            VALUES (?, ?)
        `, userID, *discountCodeID)
		if err != nil {
			tx.Rollback()
			utils.JSONError(w, "Error recording discount usage", http.StatusInternalServerError)
			return
		}

		// ✅ ตรวจสอบว่าถึงขีดจำกัดการใช้งานแล้วหรือไม่
		var usageCount int
		var usageLimit *int
		err = tx.QueryRow(`
            SELECT usage_limit FROM discount_codes WHERE id = ?
        `, *discountCodeID).Scan(&usageLimit)

		if err == nil && usageLimit != nil {
			err = tx.QueryRow(`
                SELECT COUNT(*) FROM user_discount_codes WHERE discount_code_id = ?
            `, *discountCodeID).Scan(&usageCount)

			if err == nil && usageCount >= *usageLimit {
				// 🚫 ตั้งค่า active = 0 เมื่อใช้ครบจำนวน
				_, err = tx.Exec("UPDATE discount_codes SET active = 0 WHERE id = ?", *discountCodeID)
				if err == nil {
					fmt.Printf("🚫 Discount code auto-deactivated: ID=%d, usage reached limit (%d/%d)\n",
						*discountCodeID, usageCount, *usageLimit)
				}
			}
		}
	}

	// Update wallet balance
	_, err = tx.Exec("UPDATE users SET wallet_balance = wallet_balance - ? WHERE id = ?",
		finalAmount, userID)
	if err != nil {
		tx.Rollback()
		utils.JSONError(w, "Error updating wallet", http.StatusInternalServerError)
		return
	}

	// Record transaction
	_, err = tx.Exec(`
		INSERT INTO user_transactions (user_id, type, amount, description)
		VALUES (?, 'purchase', ?, ?)
	`, userID, finalAmount, fmt.Sprintf("Purchase #%d", purchaseID))
	if err != nil {
		tx.Rollback()
		utils.JSONError(w, "Error recording transaction", http.StatusInternalServerError)
		return
	}

	// Clear cart
	_, err = tx.Exec("DELETE FROM cart_items WHERE cart_id = (SELECT id FROM carts WHERE user_id = ?)", userID)
	if err != nil {
		tx.Rollback()
		utils.JSONError(w, "Error clearing cart", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		utils.JSONError(w, "Error completing purchase", http.StatusInternalServerError)
		return
	}

	fmt.Printf("✅ Checkout completed: user_id=%d, purchase_id=%d, total=%.2f, final=%.2f\n",
		userID, purchaseID, total, finalAmount)

	utils.JSONResponse(w, map[string]interface{}{
		"message":      "Purchase completed successfully",
		"purchase_id":  purchaseID,
		"total":        total,
		"discount":     discountValue,
		"final_amount": finalAmount,
		"games_count":  len(cartItems),
	}, http.StatusOK)
}

func PurchaseHistoryHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("User-ID")

	fmt.Printf("🔍 Purchase history request for user ID: %s\n", userID)

	if userID == "" {
		utils.JSONError(w, "User ID not found", http.StatusUnauthorized)
		return
	}

	userIDInt, err := strconv.Atoi(userID)
	if err != nil {
		utils.JSONError(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	fmt.Printf("🔍 Querying purchase history for user ID: %d\n", userIDInt)

	// ใช้ DATE_FORMAT เพื่อแปลง DATETIME เป็น string โดยตรง
	rows, err := db.Query(`
		SELECT p.id, p.total_amount, p.final_amount, 
		       DATE_FORMAT(p.purchase_date, '%Y-%m-%d %H:%i:%s') as purchase_date,
		       dc.code as discount_code
		FROM purchases p
		LEFT JOIN discount_codes dc ON p.discount_code_id = dc.id
		WHERE p.user_id = ?
		ORDER BY p.purchase_date DESC
	`, userIDInt)

	if err != nil {
		fmt.Printf("❌ Error fetching purchase history: %v\n", err)
		utils.JSONError(w, "Error fetching purchase history: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var purchases []map[string]interface{}
	count := 0

	for rows.Next() {
		var id int
		var totalAmount, finalAmount float64
		var purchaseDate string
		var discountCode sql.NullString

		if err := rows.Scan(&id, &totalAmount, &finalAmount, &purchaseDate, &discountCode); err != nil {
			fmt.Printf("❌ Error scanning purchase history row: %v\n", err)
			continue
		}

		purchase := map[string]interface{}{
			"id":             id,
			"total_amount":   totalAmount,
			"final_amount":   finalAmount,
			"purchase_date":  purchaseDate,
			"discount_saved": totalAmount - finalAmount,
		}

		if discountCode.Valid {
			purchase["discount_code"] = discountCode.String
		} else {
			purchase["discount_code"] = nil
		}

		purchases = append(purchases, purchase)
		count++
		fmt.Printf("✅ Purchase found: ID=%d, Total=%.2f, Final=%.2f\n", id, totalAmount, finalAmount)
	}

	if err = rows.Err(); err != nil {
		fmt.Printf("❌ Error during purchase history rows iteration: %v\n", err)
		utils.JSONError(w, "Error processing purchase history", http.StatusInternalServerError)
		return
	}

	fmt.Printf("✅ Total purchases found: %d\n", count)

	// Always return an array, even if empty
	if purchases == nil {
		purchases = []map[string]interface{}{}
	}

	utils.JSONResponse(w, purchases, http.StatusOK)
}

// Admin handlers
func AdminAddGameHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request based on Content-Type
	contentType := r.Header.Get("Content-Type")
	var req struct {
		Name        string  `json:"name"`
		Price       float64 `json:"price"`
		CategoryID  int     `json:"category_id"`
		Description string  `json:"description"`
		ReleaseDate string  `json:"release_date"` // Optional - ถ้าไม่ส่งจะใช้วันที่ปัจจุบัน
	}

	var imageURL string

	if strings.Contains(contentType, "multipart/form-data") {
		// Handle multipart form (มีไฟล์ภาพ)
		err := r.ParseMultipartForm(10 << 20) // 10 MB limit
		if err != nil {
			utils.JSONError(w, "Error parsing form data", http.StatusBadRequest)
			return
		}

		// Get form values
		req.Name = r.FormValue("name")
		priceStr := r.FormValue("price")
		categoryIDStr := r.FormValue("category_id")
		req.Description = r.FormValue("description")
		req.ReleaseDate = r.FormValue("release_date") // Optional

		// Convert string to numbers
		if priceStr != "" {
			req.Price, err = strconv.ParseFloat(priceStr, 64)
			if err != nil {
				utils.JSONError(w, "Invalid price format", http.StatusBadRequest)
				return
			}
		}

		if categoryIDStr != "" {
			req.CategoryID, err = strconv.Atoi(categoryIDStr)
			if err != nil {
				utils.JSONError(w, "Invalid category ID", http.StatusBadRequest)
				return
			}
		}

		// Handle image file upload
		file, header, err := r.FormFile("image")
		if err == nil {
			defer file.Close()

			// Validate file type
			allowedTypes := map[string]bool{
				".jpg": true, ".jpeg": true, ".png": true, ".gif": true,
				".webp": true, ".avif": true,
			}
			ext := strings.ToLower(filepath.Ext(header.Filename))
			if !allowedTypes[ext] {
				utils.JSONError(w, "Invalid file type. Allowed: jpg, jpeg, png, gif, webp, avif", http.StatusBadRequest)
				return
			}

			// Create unique filename
			filename := fmt.Sprintf("game_%d%s", time.Now().UnixNano(), ext)
			filePath := filepath.Join("uploads", filename)

			// Save file
			dst, err := os.Create(filePath)
			if err != nil {
				utils.JSONError(w, "Error saving image", http.StatusInternalServerError)
				return
			}
			defer dst.Close()

			if _, err := io.Copy(dst, file); err != nil {
				utils.JSONError(w, "Error copying image", http.StatusInternalServerError)
				return
			}

			imageURL = "/uploads/" + filename
			fmt.Printf("✅ Image uploaded: %s\n", imageURL)
		}
	} else {
		// Handle JSON data (ไม่มีไฟล์ภาพ)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			utils.JSONError(w, "Invalid request body", http.StatusBadRequest)
			return
		}
	}

	// Validate required fields
	if req.Name == "" {
		utils.JSONError(w, "Game name is required", http.StatusBadRequest)
		return
	}

	if req.Price <= 0 {
		utils.JSONError(w, "Price must be greater than 0", http.StatusBadRequest)
		return
	}

	if req.CategoryID <= 0 {
		utils.JSONError(w, "Valid category ID is required", http.StatusBadRequest)
		return
	}

	// ตั้งค่า release_date อัตโนมัติเป็นวันที่ปัจจุบันถ้าไม่ได้รับมา
	var releaseDate interface{}
	if req.ReleaseDate != "" {
		// ถ้ารับ release_date มา ให้ใช้ค่าที่ส่งมา
		date, err := time.Parse("2006-01-02", req.ReleaseDate)
		if err != nil {
			utils.JSONError(w, "Invalid release date format. Use YYYY-MM-DD", http.StatusBadRequest)
			return
		}
		releaseDate = date
		fmt.Printf("📅 Using provided release date: %s\n", req.ReleaseDate)
	} else {
		// ถ้าไม่ได้รับ release_date มา ให้ใช้วันที่ปัจจุบัน
		currentDate := time.Now().Format("2006-01-02")
		releaseDate = currentDate
		fmt.Printf("📅 Using current date as release date: %s\n", currentDate)
	}

	// Insert game
	var result sql.Result
	var err error

	if releaseDate != nil {
		result, err = db.Exec(`
			INSERT INTO games (name, price, category_id, image_url, description, release_date)
			VALUES (?, ?, ?, ?, ?, ?)
		`, req.Name, req.Price, req.CategoryID, imageURL, req.Description, releaseDate)
	} else {
		result, err = db.Exec(`
			INSERT INTO games (name, price, category_id, image_url, description)
			VALUES (?, ?, ?, ?, ?)
		`, req.Name, req.Price, req.CategoryID, imageURL, req.Description)
	}

	if err != nil {
		fmt.Printf("❌ Error adding game: %v\n", err)
		// Delete uploaded file if database insert fails
		if imageURL != "" {
			os.Remove(strings.TrimPrefix(imageURL, "/"))
		}
		utils.JSONError(w, "Error adding game: "+err.Error(), http.StatusInternalServerError)
		return
	}

	gameID, _ := result.LastInsertId()

	// Initialize ranking with 0 sales
	_, err = db.Exec("INSERT INTO ranking (game_id, sales_count) VALUES (?, 0)", gameID)
	if err != nil {
		fmt.Printf("⚠️ Error initializing ranking: %v\n", err)
		// Continue even if ranking fails
	}

	fmt.Printf("✅ Game added successfully: ID=%d, Name=%s\n", gameID, req.Name)

	utils.JSONResponse(w, map[string]interface{}{
		"message": "Game added successfully",
		"game_id": gameID,
		"release_date": func() string {
			if date, ok := releaseDate.(time.Time); ok {
				return date.Format("2006-01-02")
			}
			return releaseDate.(string)
		}(),
	}, http.StatusCreated)
}

func AdminDiscountHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("🎯 AdminDiscountHandler: %s %s\n", r.Method, r.URL.Path)

	// Extract ID จาก URL ถ้ามี
	var id int
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) >= 3 {
		if parsedID, err := strconv.Atoi(pathParts[2]); err == nil {
			id = parsedID
		}
	}

	switch r.Method {
	case "GET":
		if id > 0 {
			getDiscountByID(w, r, id)
		} else {
			getAllDiscounts(w, r)
		}
	case "POST":
		createDiscount(w, r)
	case "PUT":
		if id > 0 {
			updateDiscountWithReset(w, r, id) // เปลี่ยนเป็นฟังก์ชันใหม่
		} else {
			utils.JSONError(w, "Discount ID required", http.StatusBadRequest)
		}
	case "DELETE":
		if id > 0 {
			deleteDiscountWithCleanup(w, r, id) // เปลี่ยนเป็นฟังก์ชันใหม่
		} else {
			utils.JSONError(w, "Discount ID required", http.StatusBadRequest)
		}
	default:
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// PUT /admin/discounts/{id} - อัพเดต + รีเซ็ตการใช้งานเมื่อเปิดใช้งานใหม่
func updateDiscountWithReset(w http.ResponseWriter, r *http.Request, id int) {
	fmt.Printf("✏️ Updating discount code with reset: ID=%d\n", id)

	var req struct {
		Code             string  `json:"code"`
		Type             string  `json:"type"`
		Value            float64 `json:"value"`
		MinTotal         float64 `json:"min_total"`
		StartDate        *string `json:"start_date"`
		EndDate          *string `json:"end_date"`
		UsageLimit       *int    `json:"usage_limit"`
		SingleUsePerUser bool    `json:"single_use_per_user"`
		Active           bool    `json:"active"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.JSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validation
	if req.Code == "" {
		utils.JSONError(w, "Discount code is required", http.StatusBadRequest)
		return
	}
	if req.Value <= 0 {
		utils.JSONError(w, "Discount value must be greater than 0", http.StatusBadRequest)
		return
	}
	if req.Type != "percent" && req.Type != "fixed" {
		utils.JSONError(w, "Discount type must be 'percent' or 'fixed'", http.StatusBadRequest)
		return
	}

	// เริ่ม transaction
	tx, err := db.Begin()
	if err != nil {
		utils.JSONError(w, "Error starting transaction", http.StatusInternalServerError)
		return
	}

	// ตรวจสอบสถานะ active ก่อนหน้า
	var currentActive bool
	err = tx.QueryRow("SELECT active FROM discount_codes WHERE id = ?", id).Scan(&currentActive)
	if err != nil {
		tx.Rollback()
		if err == sql.ErrNoRows {
			utils.JSONError(w, "Discount code not found", http.StatusNotFound)
		} else {
			utils.JSONError(w, "Error checking current discount status", http.StatusInternalServerError)
		}
		return
	}

	// ถ้ากำลังเปลี่ยนจาก inactive (false) เป็น active (true) -> ลบประวัติการใช้งาน
	resetUsage := false
	if !currentActive && req.Active {
		_, err = tx.Exec("DELETE FROM user_discount_codes WHERE discount_code_id = ?", id)
		if err != nil {
			tx.Rollback()
			utils.JSONError(w, "Error resetting discount usage history", http.StatusInternalServerError)
			return
		}
		resetUsage = true
		fmt.Printf("✅ Reset usage history for discount ID: %d (reactivated)\n", id)
	}

	// Parse dates
	var startDate, endDate interface{}
	if req.StartDate != nil && *req.StartDate != "" {
		if date, err := time.Parse("2006-01-02", *req.StartDate); err == nil {
			startDate = date
		} else {
			tx.Rollback()
			utils.JSONError(w, "Invalid start date format. Use YYYY-MM-DD", http.StatusBadRequest)
			return
		}
	}
	if req.EndDate != nil && *req.EndDate != "" {
		if date, err := time.Parse("2006-01-02", *req.EndDate); err == nil {
			endDate = date
		} else {
			tx.Rollback()
			utils.JSONError(w, "Invalid end date format. Use YYYY-MM-DD", http.StatusBadRequest)
			return
		}
	}

	// ตรวจสอบว่า code ซ้ำหรือไม่ (ไม่รวมตัวเอง)
	var existingCode string
	var existingID int
	err = tx.QueryRow("SELECT id, code FROM discount_codes WHERE code = ? AND id != ?", req.Code, id).Scan(&existingID, &existingCode)
	if err == nil {
		tx.Rollback()
		utils.JSONError(w, "Discount code already exists", http.StatusConflict)
		return
	} else if err != sql.ErrNoRows {
		tx.Rollback()
		utils.JSONError(w, "Error checking discount code", http.StatusInternalServerError)
		return
	}

	// อัพเดต discount code
	result, err := tx.Exec(`
		UPDATE discount_codes 
		SET code = ?, type = ?, value = ?, min_total = ?, start_date = ?, end_date = ?, 
		    usage_limit = ?, single_use_per_user = ?, active = ?
		WHERE id = ?
	`, req.Code, req.Type, req.Value, req.MinTotal, startDate, endDate, req.UsageLimit, req.SingleUsePerUser, req.Active, id)

	if err != nil {
		tx.Rollback()
		fmt.Printf("❌ Error updating discount code: %v\n", err)
		utils.JSONError(w, "Error updating discount code", http.StatusInternalServerError)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		tx.Rollback()
		utils.JSONError(w, "Discount code not found", http.StatusNotFound)
		return
	}

	if err := tx.Commit(); err != nil {
		utils.JSONError(w, "Error completing update", http.StatusInternalServerError)
		return
	}

	fmt.Printf("✅ Discount code updated: ID=%d, Code=%s, Active=%t\n", id, req.Code, req.Active)

	utils.JSONResponse(w, map[string]interface{}{
		"message":     "Discount code updated successfully",
		"id":          id,
		"active":      req.Active,
		"reset_usage": resetUsage, // บอกว่าทำการรีเซ็ตการใช้งานหรือไม่
	}, http.StatusOK)
}

// DELETE /admin/discounts/{id} - ลบ + ลบประวัติการใช้งาน
func deleteDiscountWithCleanup(w http.ResponseWriter, r *http.Request, id int) {
	fmt.Printf("🗑️ Deleting discount code with cleanup: ID=%d\n", id)

	// เริ่ม transaction
	tx, err := db.Begin()
	if err != nil {
		utils.JSONError(w, "Error starting transaction", http.StatusInternalServerError)
		return
	}

	// 1. ลบประวัติการใช้งานใน user_discount_codes ก่อน
	_, err = tx.Exec("DELETE FROM user_discount_codes WHERE discount_code_id = ?", id)
	if err != nil {
		tx.Rollback()
		fmt.Printf("❌ Error deleting discount usage history: %v\n", err)
		utils.JSONError(w, "Error deleting discount usage history", http.StatusInternalServerError)
		return
	}
	fmt.Printf("✅ Deleted usage history for discount ID: %d\n", id)

	// 2. ลบ discount code
	result, err := tx.Exec("DELETE FROM discount_codes WHERE id = ?", id)
	if err != nil {
		tx.Rollback()
		fmt.Printf("❌ Error deleting discount code: %v\n", err)
		utils.JSONError(w, "Error deleting discount code", http.StatusInternalServerError)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		tx.Rollback()
		utils.JSONError(w, "Discount code not found", http.StatusNotFound)
		return
	}

	if err := tx.Commit(); err != nil {
		utils.JSONError(w, "Error completing deletion", http.StatusInternalServerError)
		return
	}

	fmt.Printf("✅ Discount code deleted: ID=%d\n", id)

	utils.JSONResponse(w, map[string]interface{}{
		"message":      "Discount code deleted successfully",
		"id":           id,
		"cleanup_done": true, // บอกว่าทำการลบประวัติการใช้งานแล้ว
	}, http.StatusOK)
}

// GET /admin/discounts - ดึงทั้งหมด
func getAllDiscounts(w http.ResponseWriter, r *http.Request) {
	// เรียกตรวจสอบอัตโนมัติก่อนดึงข้อมูล
	go autoDeactivateDiscounts()
	fmt.Println("🔍 Fetching all discount codes")

	rows, err := db.Query(`
		SELECT 
			dc.id, dc.code, dc.type, dc.value, dc.min_total, 
			DATE_FORMAT(dc.start_date, '%Y-%m-%d') as start_date,
			DATE_FORMAT(dc.end_date, '%Y-%m-%d') as end_date,
			dc.usage_limit, dc.single_use_per_user, dc.active,
			dc.created_at,
			COUNT(udc.id) as usage_count
		FROM discount_codes dc
		LEFT JOIN user_discount_codes udc ON dc.id = udc.discount_code_id
		GROUP BY dc.id
		ORDER BY dc.created_at DESC
	`)
	if err != nil {
		fmt.Printf("❌ Error fetching discount codes: %v\n", err)
		utils.JSONError(w, "Error fetching discount codes", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var discounts []map[string]interface{}
	count := 0

	for rows.Next() {
		var id int
		var code, discountType string
		var value, minTotal float64
		var startDate, endDate, createdAt sql.NullString
		var usageLimit sql.NullInt64
		var singleUsePerUser, active bool
		var usageCount int

		err := rows.Scan(&id, &code, &discountType, &value, &minTotal, &startDate, &endDate, &usageLimit, &singleUsePerUser, &active, &createdAt, &usageCount)
		if err != nil {
			fmt.Printf("❌ Error scanning discount row: %v\n", err)
			continue
		}

		discount := map[string]interface{}{
			"id":                  id,
			"code":                code,
			"type":                discountType,
			"value":               value,
			"min_total":           minTotal,
			"usage_limit":         usageLimit.Int64,
			"single_use_per_user": singleUsePerUser,
			"active":              active,
			"created_at":          createdAt.String,
			"usage_count":         usageCount, // เพิ่มจำนวนการใช้งาน
		}

		if startDate.Valid {
			discount["start_date"] = startDate.String
		}
		if endDate.Valid {
			discount["end_date"] = endDate.String
		}

		discounts = append(discounts, discount)
		count++
	}

	if err = rows.Err(); err != nil {
		fmt.Printf("❌ Error during rows iteration: %v\n", err)
		utils.JSONError(w, "Error processing discount codes", http.StatusInternalServerError)
		return
	}

	fmt.Printf("✅ Total discount codes found: %d\n", count)

	utils.JSONResponse(w, map[string]interface{}{
		"discounts": discounts,
		"total":     count,
	}, http.StatusOK)
}

// GET /admin/discounts/{id} - ดึงโดย ID
func getDiscountByID(w http.ResponseWriter, r *http.Request, id int) {
	fmt.Printf("🔍 Fetching discount code: ID=%d\n", id)

	var code, discountType string
	var value, minTotal float64
	var startDate, endDate, createdAt sql.NullString
	var usageLimit sql.NullInt64
	var singleUsePerUser, active bool
	var usageCount int

	err := db.QueryRow(`
		SELECT 
			dc.code, dc.type, dc.value, dc.min_total, 
			DATE_FORMAT(dc.start_date, '%Y-%m-%d') as start_date,
			DATE_FORMAT(dc.end_date, '%Y-%m-%d') as end_date,
			dc.usage_limit, dc.single_use_per_user, dc.active, dc.created_at,
			COUNT(udc.id) as usage_count
		FROM discount_codes dc
		LEFT JOIN user_discount_codes udc ON dc.id = udc.discount_code_id
		WHERE dc.id = ?
		GROUP BY dc.id
	`, id).Scan(&code, &discountType, &value, &minTotal, &startDate, &endDate, &usageLimit, &singleUsePerUser, &active, &createdAt, &usageCount)

	if err != nil {
		if err == sql.ErrNoRows {
			utils.JSONError(w, "Discount code not found", http.StatusNotFound)
		} else {
			utils.JSONError(w, "Error fetching discount code", http.StatusInternalServerError)
		}
		return
	}

	discount := map[string]interface{}{
		"id":                  id,
		"code":                code,
		"type":                discountType,
		"value":               value,
		"min_total":           minTotal,
		"usage_limit":         usageLimit.Int64,
		"single_use_per_user": singleUsePerUser,
		"active":              active,
		"created_at":          createdAt.String,
		"usage_count":         usageCount, // เพิ่มจำนวนการใช้งาน
	}

	if startDate.Valid {
		discount["start_date"] = startDate.String
	}
	if endDate.Valid {
		discount["end_date"] = endDate.String
	}

	fmt.Printf("✅ Discount code found: ID=%d, Code=%s, Usage Count=%d\n", id, code, usageCount)
	utils.JSONResponse(w, discount, http.StatusOK)
}

// POST /admin/discounts - สร้างใหม่ (คงเดิม)
func createDiscount(w http.ResponseWriter, r *http.Request) {
	fmt.Println("➕ Creating new discount code")

	var req struct {
		Code             string  `json:"code"`
		Type             string  `json:"type"`
		Value            float64 `json:"value"`
		MinTotal         float64 `json:"min_total"`
		StartDate        *string `json:"start_date"`
		EndDate          *string `json:"end_date"`
		UsageLimit       *int    `json:"usage_limit"`
		SingleUsePerUser bool    `json:"single_use_per_user"`
		Active           bool    `json:"active"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.JSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validation
	if req.Code == "" {
		utils.JSONError(w, "Discount code is required", http.StatusBadRequest)
		return
	}
	if req.Value <= 0 {
		utils.JSONError(w, "Discount value must be greater than 0", http.StatusBadRequest)
		return
	}
	if req.Type != "percent" && req.Type != "fixed" {
		utils.JSONError(w, "Discount type must be 'percent' or 'fixed'", http.StatusBadRequest)
		return
	}

	// Parse dates
	var startDate, endDate interface{}
	if req.StartDate != nil && *req.StartDate != "" {
		if date, err := time.Parse("2006-01-02", *req.StartDate); err == nil {
			startDate = date
		} else {
			utils.JSONError(w, "Invalid start date format. Use YYYY-MM-DD", http.StatusBadRequest)
			return
		}
	}
	if req.EndDate != nil && *req.EndDate != "" {
		if date, err := time.Parse("2006-01-02", *req.EndDate); err == nil {
			endDate = date
		} else {
			utils.JSONError(w, "Invalid end date format. Use YYYY-MM-DD", http.StatusBadRequest)
			return
		}
	}

	// ตรวจสอบว่า code ซ้ำหรือไม่
	var existingCode string
	err := db.QueryRow("SELECT code FROM discount_codes WHERE code = ?", req.Code).Scan(&existingCode)
	if err == nil {
		utils.JSONError(w, "Discount code already exists", http.StatusConflict)
		return
	} else if err != sql.ErrNoRows {
		utils.JSONError(w, "Error checking discount code", http.StatusInternalServerError)
		return
	}

	// สร้าง discount code ใหม่
	result, err := db.Exec(`
		INSERT INTO discount_codes 
		(code, type, value, min_total, start_date, end_date, usage_limit, single_use_per_user, active)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, req.Code, req.Type, req.Value, req.MinTotal, startDate, endDate, req.UsageLimit, req.SingleUsePerUser, req.Active)

	if err != nil {
		fmt.Printf("❌ Error creating discount code: %v\n", err)
		utils.JSONError(w, "Error creating discount code", http.StatusInternalServerError)
		return
	}

	id, _ := result.LastInsertId()
	fmt.Printf("✅ Discount code created: ID=%d, Code=%s\n", id, req.Code)

	utils.JSONResponse(w, map[string]interface{}{
		"message": "Discount code created successfully",
		"id":      id,
	}, http.StatusCreated)
}

func AdminUsersHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	fmt.Printf("🔍 Admin fetching all users (excluding admins)\n")

	// ไม่รวม admin users ในผลลัพธ์
	rows, err := db.Query(`
		SELECT id, username, email, role, 
		       DATE_FORMAT(created_at, '%Y-%m-%d %H:%i:%s') as created_date,
		       wallet_balance
		FROM users
		WHERE role != 'admin'
		ORDER BY created_at DESC
	`)
	if err != nil {
		fmt.Printf("❌ Error fetching users: %v\n", err)
		utils.JSONError(w, "Error fetching users: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var users []map[string]interface{}
	count := 0

	for rows.Next() {
		var id int
		var username, email, role string
		var createdDate string
		var walletBalance float64

		if err := rows.Scan(&id, &username, &email, &role, &createdDate, &walletBalance); err != nil {
			fmt.Printf("❌ Error scanning user row: %v\n", err)
			continue
		}

		user := map[string]interface{}{
			"id":             id,
			"username":       username,
			"email":          email,
			"role":           role,
			"created_at":     createdDate,
			"wallet_balance": walletBalance,
		}

		users = append(users, user)
		count++
		fmt.Printf("✅ User: ID=%d, Username=%s, Role=%s\n", id, username, role)
	}

	if err = rows.Err(); err != nil {
		fmt.Printf("❌ Error during users rows iteration: %v\n", err)
		utils.JSONError(w, "Error processing users", http.StatusInternalServerError)
		return
	}

	fmt.Printf("✅ Total users found (excluding admins): %d\n", count)

	if users == nil {
		users = []map[string]interface{}{}
	}

	utils.JSONResponse(w, users, http.StatusOK)
}

// AdminUpdateGameHandler แก้ไขเกม
func AdminUpdateGameHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "PUT" && r.Method != "PATCH" {
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// ดึง game_id จาก URL path
	pathParts := strings.Split(r.URL.Path, "/")
	gameIDStr := pathParts[len(pathParts)-1]
	gameID, err := strconv.Atoi(gameIDStr)
	if err != nil {
		utils.JSONError(w, "Invalid game ID", http.StatusBadRequest)
		return
	}

	fmt.Printf("🔍 Admin updating game ID: %d\n", gameID)

	// Parse request based on Content-Type
	contentType := r.Header.Get("Content-Type")
	var req struct {
		Name        string  `json:"name"`
		Price       float64 `json:"price"`
		CategoryID  int     `json:"category_id"`
		Description string  `json:"description"`
		ReleaseDate string  `json:"release_date"`
	}

	var imageURL string

	if strings.Contains(contentType, "multipart/form-data") {
		// Handle multipart form
		err = r.ParseMultipartForm(10 << 20)
		if err != nil {
			utils.JSONError(w, "Error parsing form data", http.StatusBadRequest)
			return
		}

		// Get form values
		req.Name = r.FormValue("name")
		priceStr := r.FormValue("price")
		categoryIDStr := r.FormValue("category_id")
		req.Description = r.FormValue("description")
		req.ReleaseDate = r.FormValue("release_date")

		// Convert string to numbers
		if priceStr != "" {
			req.Price, err = strconv.ParseFloat(priceStr, 64)
			if err != nil {
				utils.JSONError(w, "Invalid price format", http.StatusBadRequest)
				return
			}
		}

		if categoryIDStr != "" {
			req.CategoryID, err = strconv.Atoi(categoryIDStr)
			if err != nil {
				utils.JSONError(w, "Invalid category ID", http.StatusBadRequest)
				return
			}
		}

		// Handle image file upload
		file, header, err := r.FormFile("image")
		if err == nil {
			defer file.Close()

			// Validate file type
			allowedTypes := map[string]bool{
				".jpg": true, ".jpeg": true, ".png": true, ".gif": true,
				".webp": true, ".avif": true,
			}
			ext := strings.ToLower(filepath.Ext(header.Filename))
			if !allowedTypes[ext] {
				utils.JSONError(w, "Invalid file type. Allowed: jpg, jpeg, png, gif, webp, avif", http.StatusBadRequest)
				return
			}

			// Create unique filename
			filename := fmt.Sprintf("game_%d%s", time.Now().UnixNano(), ext)
			filePath := filepath.Join("uploads", filename)

			// Save file
			dst, err := os.Create(filePath)
			if err != nil {
				utils.JSONError(w, "Error saving image", http.StatusInternalServerError)
				return
			}
			defer dst.Close()

			if _, err := io.Copy(dst, file); err != nil {
				utils.JSONError(w, "Error copying image", http.StatusInternalServerError)
				return
			}

			imageURL = "/uploads/" + filename
			fmt.Printf("✅ New image uploaded: %s\n", imageURL)
		}
	} else {
		// Handle JSON data
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			utils.JSONError(w, "Invalid request body", http.StatusBadRequest)
			return
		}
	}

	// Build update query dynamically
	updateFields := []string{}
	args := []interface{}{}

	if req.Name != "" {
		updateFields = append(updateFields, "name = ?")
		args = append(args, req.Name)
	}

	if req.Price > 0 {
		updateFields = append(updateFields, "price = ?")
		args = append(args, req.Price)
	}

	if req.CategoryID > 0 {
		updateFields = append(updateFields, "category_id = ?")
		args = append(args, req.CategoryID)
	}

	if req.Description != "" {
		updateFields = append(updateFields, "description = ?")
		args = append(args, req.Description)
	}

	if req.ReleaseDate != "" {
		date, err := time.Parse("2006-01-02", req.ReleaseDate)
		if err != nil {
			utils.JSONError(w, "Invalid release date format. Use YYYY-MM-DD", http.StatusBadRequest)
			return
		}
		updateFields = append(updateFields, "release_date = ?")
		args = append(args, date)
	}

	if imageURL != "" {
		// Get old image URL to delete later
		var oldImageURL sql.NullString
		db.QueryRow("SELECT image_url FROM games WHERE id = ?", gameID).Scan(&oldImageURL)

		updateFields = append(updateFields, "image_url = ?")
		args = append(args, imageURL)
	}

	if len(updateFields) == 0 {
		utils.JSONError(w, "No fields to update", http.StatusBadRequest)
		return
	}

	// Add game ID to args
	args = append(args, gameID)

	// Execute update
	query := fmt.Sprintf("UPDATE games SET %s WHERE id = ?", strings.Join(updateFields, ", "))
	result, err := db.Exec(query, args...)
	if err != nil {
		fmt.Printf("❌ Error updating game: %v\n", err)
		if imageURL != "" {
			os.Remove(strings.TrimPrefix(imageURL, "/"))
		}
		utils.JSONError(w, "Error updating game: "+err.Error(), http.StatusInternalServerError)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		if imageURL != "" {
			os.Remove(strings.TrimPrefix(imageURL, "/"))
		}
		utils.JSONError(w, "Game not found", http.StatusNotFound)
		return
	}

	// Delete old image file if new image was uploaded
	if imageURL != "" {
		// Delete old image file
		var oldImageURL sql.NullString
		db.QueryRow("SELECT image_url FROM games WHERE id = ?", gameID).Scan(&oldImageURL)
		if oldImageURL.Valid && oldImageURL.String != "" {
			oldFilePath := strings.TrimPrefix(oldImageURL.String, "/")
			if _, err := os.Stat(oldFilePath); err == nil {
				os.Remove(oldFilePath)
				fmt.Printf("🗑️ Deleted old image: %s\n", oldFilePath)
			}
		}
	}

	fmt.Printf("✅ Game updated successfully: ID=%d\n", gameID)

	utils.JSONResponse(w, map[string]interface{}{
		"message": "Game updated successfully",
		"game_id": gameID,
	}, http.StatusOK)
}

// AdminDeleteGameHandler ลบเกม
func AdminDeleteGameHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "DELETE" {
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// ดึง game_id จาก URL path
	pathParts := strings.Split(r.URL.Path, "/")
	gameIDStr := pathParts[len(pathParts)-1]
	gameID, err := strconv.Atoi(gameIDStr)
	if err != nil {
		utils.JSONError(w, "Invalid game ID", http.StatusBadRequest)
		return
	}

	fmt.Printf("🔍 Admin deleting game ID: %d\n", gameID)

	// Get image URL before deletion (เพื่อลบไฟล์ภาพ)
	var imageURL sql.NullString
	err = db.QueryRow("SELECT image_url FROM games WHERE id = ?", gameID).Scan(&imageURL)
	if err != nil {
		if err == sql.ErrNoRows {
			utils.JSONError(w, "Game not found", http.StatusNotFound)
		} else {
			utils.JSONError(w, "Error fetching game", http.StatusInternalServerError)
		}
		return
	}

	// Start transaction
	tx, err := db.Begin()
	if err != nil {
		utils.JSONError(w, "Error starting transaction", http.StatusInternalServerError)
		return
	}

	// Delete from related tables first
	_, err = tx.Exec("DELETE FROM ranking WHERE game_id = ?", gameID)
	if err != nil {
		tx.Rollback()
		utils.JSONError(w, "Error deleting game ranking", http.StatusInternalServerError)
		return
	}

	// Delete from cart_items
	_, err = tx.Exec("DELETE FROM cart_items WHERE game_id = ?", gameID)
	if err != nil {
		tx.Rollback()
		utils.JSONError(w, "Error deleting game from carts", http.StatusInternalServerError)
		return
	}

	// Delete from purchase_items (ต้องลบผ่าน purchase_items ก่อน)
	_, err = tx.Exec("DELETE pi FROM purchase_items pi WHERE pi.game_id = ?", gameID)
	if err != nil {
		tx.Rollback()
		utils.JSONError(w, "Error deleting game purchase records", http.StatusInternalServerError)
		return
	}

	// Delete from purchased_games
	_, err = tx.Exec("DELETE FROM purchased_games WHERE game_id = ?", gameID)
	if err != nil {
		tx.Rollback()
		utils.JSONError(w, "Error deleting game from user libraries", http.StatusInternalServerError)
		return
	}

	// Finally delete the game
	result, err := tx.Exec("DELETE FROM games WHERE id = ?", gameID)
	if err != nil {
		tx.Rollback()
		utils.JSONError(w, "Error deleting game", http.StatusInternalServerError)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		tx.Rollback()
		utils.JSONError(w, "Game not found", http.StatusNotFound)
		return
	}

	if err := tx.Commit(); err != nil {
		utils.JSONError(w, "Error committing transaction", http.StatusInternalServerError)
		return
	}

	// Delete image file if exists
	if imageURL.Valid && imageURL.String != "" {
		filePath := strings.TrimPrefix(imageURL.String, "/")
		if _, err := os.Stat(filePath); err == nil {
			os.Remove(filePath)
			fmt.Printf("🗑️ Deleted game image: %s\n", filePath)
		}
	}

	fmt.Printf("✅ Game deleted successfully: ID=%d\n", gameID)

	utils.JSONResponse(w, map[string]interface{}{
		"message": "Game deleted successfully",
		"game_id": gameID,
	}, http.StatusOK)
}

func AdminStatsHandler(w http.ResponseWriter, r *http.Request) {
	var stats struct {
		TotalUsers     int     `json:"total_users"`
		TotalGames     int     `json:"total_games"`
		TotalSales     float64 `json:"total_sales"`
		TotalPurchases int     `json:"total_purchases"`
	}

	// Get total users
	db.QueryRow("SELECT COUNT(*) FROM users").Scan(&stats.TotalUsers)

	// Get total games
	db.QueryRow("SELECT COUNT(*) FROM games").Scan(&stats.TotalGames)

	// Get total sales
	db.QueryRow("SELECT COALESCE(SUM(final_amount), 0) FROM purchases").Scan(&stats.TotalSales)

	// Get total purchases
	db.QueryRow("SELECT COUNT(*) FROM purchases").Scan(&stats.TotalPurchases)

	utils.JSONResponse(w, stats, http.StatusOK)
}

func AdminTransactionsHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("💰 AdminTransactionsHandler: %s %s\n", r.Method, r.URL.Path)

	switch r.Method {
	case "GET":
		getAllTransactions(w, r)
	default:
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// AdminUserTransactionsHandler จัดการดูประวัติธุรกรรมของผู้ใช้เฉพาะคน
func AdminUserTransactionsHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("💰 AdminUserTransactionsHandler: %s %s\n", r.Method, r.URL.Path)

	// Extract user ID จาก URL
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 4 {
		utils.JSONError(w, "User ID required", http.StatusBadRequest)
		return
	}

	userID, err := strconv.Atoi(pathParts[3])
	if err != nil {
		utils.JSONError(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case "GET":
		getUserTransactions(w, r, userID)
	default:
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// GET /admin/transactions - ดึงประวัติธุรกรรมทั้งหมด
func getAllTransactions(w http.ResponseWriter, r *http.Request) {
	fmt.Println("🔍 Fetching all transactions")

	// รับ query parameters สำหรับ filtering
	query := r.URL.Query()
	transactionType := query.Get("type")
	limitStr := query.Get("limit")
	offsetStr := query.Get("offset")

	// ตั้งค่า default values
	limit := 100
	offset := 0

	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}
	if offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	// Build query - แก้ไขตามโครงสร้างตารางจริง
	baseQuery := `
		SELECT 
			t.id, t.user_id, u.username, t.type, t.amount, 
			t.description, DATE_FORMAT(t.created_at, '%Y-%m-%d %H:%i:%s') as created_at
		FROM user_transactions t
		LEFT JOIN users u ON t.user_id = u.id
	`
	var args []interface{}
	whereClauses := []string{}

	if transactionType != "" {
		whereClauses = append(whereClauses, "t.type = ?")
		args = append(args, transactionType)
	}

	if len(whereClauses) > 0 {
		baseQuery += " WHERE " + strings.Join(whereClauses, " AND ")
	}

	baseQuery += " ORDER BY t.created_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := db.Query(baseQuery, args...)
	if err != nil {
		fmt.Printf("❌ Error fetching transactions: %v\n", err)
		utils.JSONError(w, "Error fetching transactions", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var transactions []map[string]interface{}
	count := 0

	for rows.Next() {
		var id, userID int
		var username, transactionType, description, createdAt string
		var amount float64

		err := rows.Scan(&id, &userID, &username, &transactionType, &amount, &description, &createdAt)
		if err != nil {
			fmt.Printf("❌ Error scanning transaction row: %v\n", err)
			continue
		}

		transaction := map[string]interface{}{
			"id":          id,
			"user_id":     userID,
			"user_name":   username,
			"type":        transactionType,
			"amount":      amount,
			"description": description,
			"created_at":  createdAt,
		}

		transactions = append(transactions, transaction)
		count++
	}

	if err = rows.Err(); err != nil {
		fmt.Printf("❌ Error during rows iteration: %v\n", err)
		utils.JSONError(w, "Error processing transactions", http.StatusInternalServerError)
		return
	}

	// ดึงจำนวน total สำหรับ pagination
	var totalCount int
	countQuery := `
		SELECT COUNT(*) 
		FROM user_transactions t
		LEFT JOIN users u ON t.user_id = u.id
	`
	if len(whereClauses) > 0 {
		countQuery += " WHERE " + strings.Join(whereClauses, " AND ")
	}
	err = db.QueryRow(countQuery, args[:len(args)-2]...).Scan(&totalCount)
	if err != nil {
		fmt.Printf("❌ Error counting transactions: %v\n", err)
		totalCount = count
	}

	fmt.Printf("✅ Total transactions found: %d (showing %d)\n", totalCount, count)

	utils.JSONResponse(w, map[string]interface{}{
		"transactions": transactions,
		"total":        totalCount,
		"limit":        limit,
		"offset":       offset,
		"count":        count,
		"success":      true,
	}, http.StatusOK)
}

// GET /admin/transactions/user/{userID} - ดึงประวัติธุรกรรมของผู้ใช้เฉพาะคน
func getUserTransactions(w http.ResponseWriter, r *http.Request, userID int) {
	fmt.Printf("🔍 Fetching transactions for user: ID=%d\n", userID)

	// ตรวจสอบว่าผู้ใช้มีอยู่จริง
	var username string
	err := db.QueryRow("SELECT username FROM users WHERE id = ?", userID).Scan(&username)
	if err != nil {
		if err == sql.ErrNoRows {
			utils.JSONError(w, "User not found", http.StatusNotFound)
		} else {
			utils.JSONError(w, "Error checking user", http.StatusInternalServerError)
		}
		return
	}

	// รับ query parameters
	query := r.URL.Query()
	transactionType := query.Get("type")
	limitStr := query.Get("limit")
	offsetStr := query.Get("offset")

	// ตั้งค่า default values
	limit := 50
	offset := 0

	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}
	if offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	// Build query - แก้ไขตามโครงสร้างตารางจริง
	baseQuery := `
		SELECT 
			t.id, t.type, t.amount, t.description, 
			DATE_FORMAT(t.created_at, '%Y-%m-%d %H:%i:%s') as created_at
		FROM user_transactions t
		WHERE t.user_id = ?
	`
	var args []interface{}
	args = append(args, userID)

	if transactionType != "" {
		baseQuery += " AND t.type = ?"
		args = append(args, transactionType)
	}

	baseQuery += " ORDER BY t.created_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := db.Query(baseQuery, args...)
	if err != nil {
		fmt.Printf("❌ Error fetching user transactions: %v\n", err)
		utils.JSONError(w, "Error fetching user transactions", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var transactions []map[string]interface{}
	count := 0

	for rows.Next() {
		var id int
		var transactionType, description, createdAt string
		var amount float64

		err := rows.Scan(&id, &transactionType, &amount, &description, &createdAt)
		if err != nil {
			fmt.Printf("❌ Error scanning user transaction row: %v\n", err)
			continue
		}

		transaction := map[string]interface{}{
			"id":          id,
			"user_id":     userID,
			"user_name":   username,
			"type":        transactionType,
			"amount":      amount,
			"description": description,
			"created_at":  createdAt,
		}

		transactions = append(transactions, transaction)
		count++
	}

	if err = rows.Err(); err != nil {
		fmt.Printf("❌ Error during rows iteration: %v\n", err)
		utils.JSONError(w, "Error processing user transactions", http.StatusInternalServerError)
		return
	}

	// ดึงจำนวน total สำหรับ pagination
	var totalCount int
	countQuery := "SELECT COUNT(*) FROM user_transactions WHERE user_id = ?"
	if transactionType != "" {
		countQuery += " AND type = ?"
		err = db.QueryRow(countQuery, userID, transactionType).Scan(&totalCount)
	} else {
		err = db.QueryRow(countQuery, userID).Scan(&totalCount)
	}
	if err != nil {
		fmt.Printf("❌ Error counting user transactions: %v\n", err)
		totalCount = count
	}

	// ดึงข้อมูลผู้ใช้เพิ่มเติม
	var userUsername, userEmail, userCreatedAt string
	var userWalletBalance float64

	err = db.QueryRow(`
		SELECT username, email, wallet_balance, DATE_FORMAT(created_at, '%Y-%m-%d %H:%i:%s') as created_at 
		FROM users WHERE id = ?
	`, userID).Scan(&userUsername, &userEmail, &userWalletBalance, &userCreatedAt)

	userData := make(map[string]interface{})
	if err != nil {
		fmt.Printf("❌ Error fetching user data: %v\n", err)
		userData = map[string]interface{}{
			"username": username,
			"error":    "Could not fetch full user details",
		}
	} else {
		userData = map[string]interface{}{
			"username":       userUsername,
			"email":          userEmail,
			"wallet_balance": userWalletBalance,
			"created_at":     userCreatedAt,
		}
	}

	fmt.Printf("✅ Transactions found for user %s: %d (showing %d)\n", username, totalCount, count)

	utils.JSONResponse(w, map[string]interface{}{
		"transactions": transactions,
		"user":         userData,
		"total":        totalCount,
		"limit":        limit,
		"offset":       offset,
		"count":        count,
		"success":      true,
	}, http.StatusOK)
}

// TransactionStatsHandler ดึงสถิติธุรกรรม
func TransactionStatsHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Println("📊 Fetching transaction statistics")

	stats := make(map[string]interface{})

	// ยอดรวมทั้งหมด
	var totalDeposit, totalPurchase float64
	err := db.QueryRow("SELECT COALESCE(SUM(amount), 0) FROM user_transactions WHERE type = 'deposit'").Scan(&totalDeposit)
	if err != nil {
		fmt.Printf("❌ Error getting deposit total: %v\n", err)
	}
	err = db.QueryRow("SELECT COALESCE(SUM(amount), 0) FROM user_transactions WHERE type = 'purchase'").Scan(&totalPurchase)
	if err != nil {
		fmt.Printf("❌ Error getting purchase total: %v\n", err)
	}

	// จำนวนธุรกรรมแยกตามประเภท
	var depositCount, purchaseCount int
	err = db.QueryRow("SELECT COUNT(*) FROM user_transactions WHERE type = 'deposit'").Scan(&depositCount)
	if err != nil {
		fmt.Printf("❌ Error counting deposits: %v\n", err)
	}
	err = db.QueryRow("SELECT COUNT(*) FROM user_transactions WHERE type = 'purchase'").Scan(&purchaseCount)
	if err != nil {
		fmt.Printf("❌ Error counting purchases: %v\n", err)
	}

	// ธุรกรรมล่าสุด
	var latestTransaction string
	err = db.QueryRow("SELECT DATE_FORMAT(created_at, '%Y-%m-%d %H:%i:%s') FROM user_transactions ORDER BY created_at DESC LIMIT 1").Scan(&latestTransaction)
	if err != nil && err != sql.ErrNoRows {
		fmt.Printf("❌ Error getting latest transaction: %v\n", err)
	}

	// ยอดรวมรายวัน (7 วันที่ผ่านมา)
	dailyStats := make([]map[string]interface{}, 0)
	rows, err := db.Query(`
		SELECT 
			DATE(created_at) as date,
			COUNT(*) as count,
			COALESCE(SUM(CASE WHEN type = 'deposit' THEN amount ELSE 0 END), 0) as deposit_total,
			COALESCE(SUM(CASE WHEN type = 'purchase' THEN amount ELSE 0 END), 0) as purchase_total
		FROM user_transactions 
		WHERE created_at >= DATE_SUB(NOW(), INTERVAL 7 DAY)
		GROUP BY DATE(created_at)
		ORDER BY date DESC
	`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var date string
			var count int
			var depositTotal, purchaseTotal float64
			if err := rows.Scan(&date, &count, &depositTotal, &purchaseTotal); err == nil {
				dailyStats = append(dailyStats, map[string]interface{}{
					"date":           date,
					"count":          count,
					"deposit_total":  depositTotal,
					"purchase_total": purchaseTotal,
				})
			}
		}
	}

	stats["total_deposit"] = totalDeposit
	stats["total_purchase"] = totalPurchase
	stats["deposit_count"] = depositCount
	stats["purchase_count"] = purchaseCount
	stats["latest_transaction"] = latestTransaction
	stats["total_transactions"] = depositCount + purchaseCount
	stats["daily_stats"] = dailyStats

	fmt.Printf("✅ Transaction statistics loaded\n")

	utils.JSONResponse(w, map[string]interface{}{
		"stats":   stats,
		"success": true,
	}, http.StatusOK)
}

// UpdateProfileHandler อัพเดตข้อมูลผู้ใช้ (รวม avatar และเปลี่ยนรหัสผ่าน)
func UpdateProfileHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "PUT" && r.Method != "PATCH" {
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := r.Header.Get("User-ID")

	fmt.Printf("🔍 Update profile request for user ID: %s\n", userID)

	if userID == "" {
		utils.JSONError(w, "User ID not found", http.StatusUnauthorized)
		return
	}

	userIDInt, err := strconv.Atoi(userID)
	if err != nil {
		utils.JSONError(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	// ตรวจสอบ Content-Type
	contentType := r.Header.Get("Content-Type")
	var req struct {
		Username        string `json:"username"`
		Email           string `json:"email"`
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
		ConfirmPassword string `json:"confirm_password"`
	}
	var avatarURL string

	if strings.Contains(contentType, "multipart/form-data") {
		// Handle multipart form (มีไฟล์ avatar)
		err = r.ParseMultipartForm(10 << 20) // 10 MB limit
		if err != nil {
			utils.JSONError(w, "Error parsing form data", http.StatusBadRequest)
			return
		}

		// Get form values
		req.Username = r.FormValue("username")
		req.Email = r.FormValue("email")
		req.CurrentPassword = r.FormValue("current_password")
		req.NewPassword = r.FormValue("new_password")
		req.ConfirmPassword = r.FormValue("confirm_password")

		// Handle avatar file upload
		file, header, err := r.FormFile("avatar")
		if err == nil {
			defer file.Close()

			// ✅ ลบการตรวจสอบประเภทไฟล์ออก - อนุญาตทุกไฟล์
			// ไม่มีการตรวจสอบนามสกุลไฟล์อีกต่อไป

			// Create unique filename
			ext := strings.ToLower(filepath.Ext(header.Filename))
			if ext == "" {
				// ถ้าไฟล์ไม่มีนามสกุล ให้ใช้ .dat เป็น default
				ext = ".dat"
			}
			filename := fmt.Sprintf("avatar_%d_%d%s", userIDInt, time.Now().UnixNano(), ext)
			filePath := filepath.Join("uploads", filename)

			// Save file
			dst, err := os.Create(filePath)
			if err != nil {
				utils.JSONError(w, "Error saving avatar", http.StatusInternalServerError)
				return
			}
			defer dst.Close()

			if _, err := io.Copy(dst, file); err != nil {
				utils.JSONError(w, "Error copying avatar", http.StatusInternalServerError)
				return
			}

			avatarURL = "/uploads/" + filename
			fmt.Printf("✅ Avatar uploaded: %s\n", avatarURL)
		}
	} else {
		// Handle JSON data (ไม่มีไฟล์ avatar)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			utils.JSONError(w, "Invalid request body", http.StatusBadRequest)
			return
		}
	}

	// Validate input - ตรวจสอบว่ามี field ใดๆ ที่จะอัพเดตหรือไม่
	if req.Username == "" && req.Email == "" && avatarURL == "" && req.NewPassword == "" {
		utils.JSONError(w, "No fields to update", http.StatusBadRequest)
		return
	}

	// Validate email if provided
	if req.Email != "" && !isValidEmail(req.Email) {
		utils.JSONError(w, "Invalid email format", http.StatusBadRequest)
		return
	}

	// Validate password change if new password is provided
	if req.NewPassword != "" {
		if req.CurrentPassword == "" {
			utils.JSONError(w, "Current password is required to change password", http.StatusBadRequest)
			return
		}

		if req.ConfirmPassword == "" {
			utils.JSONError(w, "Confirm password is required", http.StatusBadRequest)
			return
		}

		if req.NewPassword != req.ConfirmPassword {
			utils.JSONError(w, "New password and confirm password do not match", http.StatusBadRequest)
			return
		}

		if len(req.NewPassword) < 6 {
			utils.JSONError(w, "New password must be at least 6 characters", http.StatusBadRequest)
			return
		}

		if req.CurrentPassword == req.NewPassword {
			utils.JSONError(w, "New password must be different from current password", http.StatusBadRequest)
			return
		}
	}

	// Check if new username or email already exists (if provided)
	if req.Username != "" || req.Email != "" {
		var existingUser string
		checkQuery := `
			SELECT 
				CASE 
					WHEN username = ? AND id != ? THEN 'username'
					WHEN email = ? AND id != ? THEN 'email'
				END as existing_field
			FROM users 
			WHERE (username = ? OR email = ?) AND id != ?
		`
		err := db.QueryRow(checkQuery, req.Username, userIDInt, req.Email, userIDInt, req.Username, req.Email, userIDInt).Scan(&existingUser)

		if err == nil && existingUser != "" {
			utils.JSONError(w, fmt.Sprintf("%s already exists", existingUser), http.StatusBadRequest)
			return
		} else if err != nil && err != sql.ErrNoRows {
			utils.JSONError(w, "Error checking user existence", http.StatusInternalServerError)
			return
		}
	}

	// ถ้ามีการเปลี่ยนรหัสผ่าน ต้องตรวจสอบรหัสผ่านปัจจุบัน
	var newPasswordHash string
	if req.NewPassword != "" {
		// Get current password hash from database
		var currentPasswordHash string
		err = db.QueryRow("SELECT password_hash FROM users WHERE id = ?", userIDInt).Scan(&currentPasswordHash)
		if err != nil {
			if err == sql.ErrNoRows {
				utils.JSONError(w, "User not found", http.StatusNotFound)
			} else {
				utils.JSONError(w, "Error fetching user data", http.StatusInternalServerError)
			}
			return
		}

		// Verify current password
		err = bcrypt.CompareHashAndPassword([]byte(currentPasswordHash), []byte(req.CurrentPassword))
		if err != nil {
			fmt.Printf("❌ Current password mismatch for user ID: %d\n", userIDInt)
			utils.JSONError(w, "Current password is incorrect", http.StatusUnauthorized)
			return
		}

		// Hash new password
		hashedBytes, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
		if err != nil {
			utils.JSONError(w, "Error processing new password", http.StatusInternalServerError)
			return
		}
		newPasswordHash = string(hashedBytes)
	}

	// Build update query dynamically based on provided fields
	updateFields := []string{}
	args := []interface{}{}

	if req.Username != "" {
		updateFields = append(updateFields, "username = ?")
		args = append(args, req.Username)
	}

	if req.Email != "" {
		updateFields = append(updateFields, "email = ?")
		args = append(args, req.Email)
	}

	if avatarURL != "" {
		updateFields = append(updateFields, "avatar_url = ?")
		args = append(args, avatarURL)
	}

	if newPasswordHash != "" {
		updateFields = append(updateFields, "password_hash = ?")
		args = append(args, newPasswordHash)
	}

	if len(updateFields) == 0 {
		utils.JSONError(w, "No fields to update", http.StatusBadRequest)
		return
	}

	// Add user ID to args
	args = append(args, userIDInt)

	// Execute update
	query := fmt.Sprintf("UPDATE users SET %s WHERE id = ?", strings.Join(updateFields, ", "))
	result, err := db.Exec(query, args...)
	if err != nil {
		fmt.Printf("❌ Error updating profile: %v\n", err)
		// Delete uploaded file if database update fails
		if avatarURL != "" {
			os.Remove(strings.TrimPrefix(avatarURL, "/"))
		}
		utils.JSONError(w, "Error updating profile: "+err.Error(), http.StatusInternalServerError)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		// Delete uploaded file if no rows affected
		if avatarURL != "" {
			os.Remove(strings.TrimPrefix(avatarURL, "/"))
		}
		utils.JSONError(w, "User not found or no changes made", http.StatusNotFound)
		return
	}

	fmt.Printf("✅ Profile updated successfully for user ID: %d\n", userIDInt)

	// Return updated user data
	var updatedUser struct {
		ID       int     `json:"id"`
		Username string  `json:"username"`
		Email    string  `json:"email"`
		Avatar   string  `json:"avatar_url"`
		Balance  float64 `json:"wallet_balance"`
	}
	var avatarDB sql.NullString

	err = db.QueryRow(`
		SELECT id, username, email, avatar_url, wallet_balance 
		FROM users 
		WHERE id = ?
	`, userIDInt).Scan(&updatedUser.ID, &updatedUser.Username, &updatedUser.Email, &avatarDB, &updatedUser.Balance)

	if err != nil {
		utils.JSONError(w, "Error fetching updated profile", http.StatusInternalServerError)
		return
	}

	if avatarDB.Valid {
		updatedUser.Avatar = avatarDB.String
	} else {
		updatedUser.Avatar = ""
	}

	response := map[string]interface{}{
		"message": "Profile updated successfully",
		"user":    updatedUser,
	}

	// Add password change notice if password was changed
	if newPasswordHash != "" {
		response["password_changed"] = true
	}

	utils.JSONResponse(w, response, http.StatusOK)
}

// ApplyDiscountHandler handles discount code validation and application
func ApplyDiscountHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Code        string  `json:"code"`
		TotalAmount float64 `json:"total_amount"`
		UserID      int     `json:"user_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.JSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	fmt.Printf("🔍 Applying discount code: %s for user %d, total: %.2f\n", req.Code, req.UserID, req.TotalAmount)

	// Check if discount code exists and is valid
	var discount struct {
		ID               int
		Type             string
		Value            float64
		MinTotal         float64
		UsageLimit       *int
		SingleUsePerUser bool
		Active           bool
		StartDate        *time.Time
		EndDate          *time.Time
	}

	// ใช้ sql.NullString เพื่อรับค่า date จาก database
	var startDateStr, endDateStr sql.NullString

	err := db.QueryRow(`
        SELECT id, type, value, min_total, usage_limit, single_use_per_user, 
               active, start_date, end_date
        FROM discount_codes 
        WHERE code = ? AND active = 1
    `, req.Code).Scan(
		&discount.ID, &discount.Type, &discount.Value, &discount.MinTotal,
		&discount.UsageLimit, &discount.SingleUsePerUser, &discount.Active,
		&startDateStr, &endDateStr, // รับเป็น string ก่อน
	)

	if err != nil {
		fmt.Printf("❌ Database error: %v\n", err)
		if err == sql.ErrNoRows {
			utils.JSONError(w, "Discount code not found or inactive", http.StatusBadRequest)
		} else {
			utils.JSONError(w, "Error checking discount code", http.StatusInternalServerError)
		}
		return
	}

	// Convert string date to time.Time
	if startDateStr.Valid && startDateStr.String != "" {
		startDate, err := time.Parse("2006-01-02", startDateStr.String)
		if err != nil {
			fmt.Printf("❌ Error parsing start date: %v\n", err)
		} else {
			discount.StartDate = &startDate
		}
	}

	if endDateStr.Valid && endDateStr.String != "" {
		endDate, err := time.Parse("2006-01-02", endDateStr.String)
		if err != nil {
			fmt.Printf("❌ Error parsing end date: %v\n", err)
		} else {
			discount.EndDate = &endDate
		}
	}

	fmt.Printf("✅ Discount found: ID=%d, StartDate=%v, EndDate=%v\n",
		discount.ID, discount.StartDate, discount.EndDate)

	// Validate discount code
	now := time.Now()

	// Check date validity
	if discount.StartDate != nil && now.Before(*discount.StartDate) {
		utils.JSONError(w, "Discount code not yet valid", http.StatusBadRequest)
		return
	}
	if discount.EndDate != nil && now.After(*discount.EndDate) {
		utils.JSONError(w, "Discount code has expired", http.StatusBadRequest)
		return
	}

	// Check minimum total
	if discount.MinTotal > 0 && req.TotalAmount < discount.MinTotal {
		utils.JSONError(w, fmt.Sprintf("Minimum purchase of $%.2f required", discount.MinTotal), http.StatusBadRequest)
		return
	}

	// Check usage limit
	if discount.UsageLimit != nil {
		var usageCount int
		err := db.QueryRow(`
            SELECT COUNT(*) 
            FROM user_discount_codes 
            WHERE discount_code_id = ?
        `, discount.ID).Scan(&usageCount)

		if err == nil && usageCount >= *discount.UsageLimit {
			// ❌ ตั้งค่า active = 0 เมื่อใช้ครบจำนวน
			db.Exec("UPDATE discount_codes SET active = 0 WHERE id = ?", discount.ID)
			fmt.Printf("🚫 Discount code deactivated: ID=%d, usage reached limit\n", discount.ID)

			utils.JSONError(w, "Discount code usage limit reached", http.StatusBadRequest)
			return
		}
	}

	// Check if user already used this code (for single-use codes)
	if discount.SingleUsePerUser {
		var used bool
		err := db.QueryRow(`
            SELECT EXISTS(
                SELECT 1 FROM user_discount_codes 
                WHERE user_id = ? AND discount_code_id = ?
            )
        `, req.UserID, discount.ID).Scan(&used)

		if err != nil {
			fmt.Printf("❌ Error checking single use: %v\n", err)
		} else if used {
			utils.JSONError(w, "Discount code already used", http.StatusBadRequest)
			return
		}
	}

	// Calculate discount amount
	var discountAmount float64
	var finalAmount float64

	if discount.Type == "percent" {
		discountAmount = req.TotalAmount * (discount.Value / 100)
	} else {
		discountAmount = discount.Value
	}

	finalAmount = req.TotalAmount - discountAmount
	if finalAmount < 0 {
		finalAmount = 0
	}

	fmt.Printf("✅ Discount applied: Code=%s, Type=%s, Value=%.2f, Discount=%.2f, Final=%.2f\n",
		req.Code, discount.Type, discount.Value, discountAmount, finalAmount)

	// Return successful response
	utils.JSONResponse(w, map[string]interface{}{
		"valid":           true,
		"discount_id":     discount.ID,
		"code":            req.Code,
		"type":            discount.Type,
		"value":           discount.Value,
		"min_total":       discount.MinTotal,
		"discount_amount": discountAmount,
		"final_amount":    finalAmount,
		"original_amount": req.TotalAmount,
		"message":         "Discount applied successfully",
	}, http.StatusOK)
}

// ฟังก์ชันสำหรับตรวจสอบและปิดใช้งานส่วนลดที่ครบจำนวน
func autoDeactivateDiscounts() {
	fmt.Println("🔄 Checking for discount codes to deactivate...")

	rows, err := db.Query(`
        SELECT dc.id, dc.usage_limit, COUNT(udc.id) as usage_count
        FROM discount_codes dc
        LEFT JOIN user_discount_codes udc ON dc.id = udc.discount_code_id
        WHERE dc.active = 1 AND dc.usage_limit IS NOT NULL
        GROUP BY dc.id
        HAVING usage_count >= dc.usage_limit
    `)
	if err != nil {
		fmt.Printf("❌ Error checking discount deactivation: %v\n", err)
		return
	}
	defer rows.Close()

	var deactivatedCount int
	for rows.Next() {
		var discountID int
		var usageLimit, usageCount int
		err := rows.Scan(&discountID, &usageLimit, &usageCount)
		if err != nil {
			continue
		}

		// ปิดใช้งานส่วนลด
		_, err = db.Exec("UPDATE discount_codes SET active = 0 WHERE id = ?", discountID)
		if err == nil {
			fmt.Printf("🚫 Auto-deactivated discount: ID=%d, usage %d/%d\n",
				discountID, usageCount, usageLimit)
			deactivatedCount++
		}
	}

	if deactivatedCount > 0 {
		fmt.Printf("✅ Auto-deactivated %d discount codes\n", deactivatedCount)
	}
}
