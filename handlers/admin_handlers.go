package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"go-api-game/utils"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// AdminAddGameHandler handles adding new games
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

// AdminUpdateGameHandler handles updating games
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

// AdminDeleteGameHandler handles deleting games
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

// AdminUsersHandler handles admin user management
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

// AdminStatsHandler handles admin statistics
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

// AdminTransactionsHandler handles admin transaction management
func AdminTransactionsHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("💰 AdminTransactionsHandler: %s %s\n", r.Method, r.URL.Path)

	switch r.Method {
	case "GET":
		getAllTransactions(w, r)
	default:
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// AdminUserTransactionsHandler handles user-specific transaction management for admin
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
