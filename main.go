package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

var db *sql.DB
var jwtSecret = []byte("your_secret_key_here")

type User struct {
	ID            int     `json:"id"`
	Username      string  `json:"username"`
	Email         string  `json:"email"`
	AvatarURL     string  `json:"avatar_url"`
	Role          string  `json:"role"`
	WalletBalance float64 `json:"wallet_balance"`
}

type Game struct {
	ID          int     `json:"id"`
	Name        string  `json:"name"`
	Price       float64 `json:"price"`
	CategoryID  int     `json:"category_id"`
	ImageURL    string  `json:"image_url"`
	Description string  `json:"description"`
	ReleaseDate string  `json:"release_date"`
}

type Claims struct {
	UserID int    `json:"user_id"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

func main() {
	var err error
	dsn := "65011212151:TxEy2003122@tcp(202.28.34.210:3309)/db65011212151"
	db, err = sql.Open("mysql", dsn)
	if err != nil {
		log.Fatal("Cannot connect to database:", err)
	}
	defer db.Close()

	if err = db.Ping(); err != nil {
		log.Fatal("Cannot ping database:", err)
	}
	fmt.Println("✅ Connected to database successfully")

	if _, err := os.Stat("uploads"); os.IsNotExist(err) {
		os.Mkdir("uploads", 0755)
	}

	// ================== Routes ==================
	http.HandleFunc("/", corsMiddleware(rootHandler))
	http.HandleFunc("/game", corsMiddleware(gameHandler))
	http.HandleFunc("/game/", corsMiddleware(gameByIDHandler))
	http.HandleFunc("/register", corsMiddleware(registerHandler))
	http.HandleFunc("/login", corsMiddleware(loginHandler))
	http.HandleFunc("/me", corsMiddleware(authMiddleware(meHandler)))
	http.HandleFunc("/me/update", corsMiddleware(authMiddleware(updateProfileHandler)))
	http.HandleFunc("/admin/users", corsMiddleware(authMiddleware(adminUsersHandler)))
	http.HandleFunc("/admin/game/upload", corsMiddleware(authMiddleware(adminUploadGameImageHandler)))
	http.HandleFunc("/categories", corsMiddleware(categoriesHandler))
	http.HandleFunc("/wallet/", corsMiddleware(authMiddleware(walletHandler)))
	http.HandleFunc("/cart/", corsMiddleware(authMiddleware(cartHandler)))
	http.HandleFunc("/cart/add", corsMiddleware(authMiddleware(addToCartHandler)))
	http.HandleFunc("/cart/remove", corsMiddleware(authMiddleware(removeFromCartHandler)))
	http.HandleFunc("/cart/clear", corsMiddleware(authMiddleware(clearCartHandler)))
	http.HandleFunc("/game/admin", corsMiddleware(authMiddleware(adminGameHandler)))
	http.HandleFunc("/game/admin/", corsMiddleware(authMiddleware(adminGameHandler)))

	// Serve uploads folder
	// Serve uploads folder with no-cache headers
	http.Handle("/uploads/", http.StripPrefix("/uploads/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// ป้องกัน cache
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, proxy-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		w.Header().Set("Surrogate-Control", "no-store")

		http.FileServer(http.Dir("uploads")).ServeHTTP(w, r)
	})))

	ip := getLocalIP()
	fmt.Println("🚀 Server started at http://" + ip + ":8080")
	log.Fatal(http.ListenAndServe("0.0.0.0:8080", nil))
}

// ================== Helpers ==================
func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "http://localhost:4200")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	}
}

func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tokenStr := ""
		authHeader := r.Header.Get("Authorization")
		if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
			tokenStr = authHeader[7:]
		} else {
			http.Error(w, "Missing or invalid Authorization header", http.StatusUnauthorized)
			return
		}

		token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(token *jwt.Token) (interface{}, error) {
			return jwtSecret, nil
		})
		if err != nil || !token.Valid {
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		claims, ok := token.Claims.(*Claims)
		if !ok {
			http.Error(w, "Invalid token claims", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), "claims", claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ip4 := ipnet.IP.To4(); ip4 != nil {
				return ip4.String()
			}
		}
	}
	return "127.0.0.1"
}

func getFullURL(path string) string {
	if path == "" {
		return ""
	}
	return fmt.Sprintf("http://%s:8080%s", getLocalIP(), path)
}

func getClaims(r *http.Request) *Claims {
	if claims, ok := r.Context().Value("claims").(*Claims); ok {
		return claims
	}
	return nil
}

// ================== Handlers ==================
func rootHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Game Shop API"})
}

func gameHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rows, err := db.Query("SELECT id, name, price, category_id, image_url, description, release_date FROM games")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var games []Game
	for rows.Next() {
		var g Game
		var releaseDate sql.NullString
		if err := rows.Scan(&g.ID, &g.Name, &g.Price, &g.CategoryID, &g.ImageURL, &g.Description, &releaseDate); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if releaseDate.Valid {
			g.ReleaseDate = releaseDate.String
		}
		g.ImageURL = getFullURL(g.ImageURL)
		games = append(games, g)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(games)
}

func gameByIDHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	idStr := r.URL.Path[len("/game/"):]
	var g Game
	var releaseDate sql.NullString
	err := db.QueryRow("SELECT id, name, price, category_id, image_url, description, release_date FROM games WHERE id=?", idStr).
		Scan(&g.ID, &g.Name, &g.Price, &g.CategoryID, &g.ImageURL, &g.Description, &releaseDate)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Game not found", http.StatusNotFound)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	if releaseDate.Valid {
		g.ReleaseDate = releaseDate.String
	}
	g.ImageURL = getFullURL(g.ImageURL)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(g)
}

func registerHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	username := r.FormValue("username")
	email := r.FormValue("email")
	password := r.FormValue("password")

	if username == "" || email == "" || password == "" {
		http.Error(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	var exists int
	db.QueryRow("SELECT COUNT(*) FROM users WHERE email = ?", email).Scan(&exists)
	if exists > 0 {
		http.Error(w, "Email already exists", http.StatusBadRequest)
		return
	}

	avatarURL := ""
	file, header, err := r.FormFile("avatar")
	if err == nil {
		defer file.Close()
		ext := filepath.Ext(header.Filename)
		filename := fmt.Sprintf("user_%d%s", time.Now().UnixNano(), ext)
		out, err := os.Create(filepath.Join("uploads", filename))
		if err != nil {
			http.Error(w, "Failed to save avatar", http.StatusInternalServerError)
			return
		}
		defer out.Close()
		io.Copy(out, file)
		avatarURL = "/uploads/" + filename
	}

	hashed, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)

	_, err = db.Exec("INSERT INTO users (username, email, password_hash, role, avatar_url) VALUES (?, ?, ?, 'user', ?)",
		username, email, string(hashed), avatarURL)
	if err != nil {
		http.Error(w, "Failed to create user", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{
		"message":    "User registered successfully",
		"avatar_url": getFullURL(avatarURL),
	})
}

func loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		Identifier string `json:"identifier"`
		Password   string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var id int
	var hashed, role string
	err := db.QueryRow("SELECT id, password_hash, role FROM users WHERE email=? OR username=?", input.Identifier, input.Identifier).Scan(&id, &hashed, &role)
	if err != nil {
		http.Error(w, "Username or email incorrect", http.StatusUnauthorized)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hashed), []byte(input.Password)); err != nil {
		http.Error(w, "Password incorrect", http.StatusUnauthorized)
		return
	}

	expirationTime := time.Now().Add(24 * time.Hour)
	claims := &Claims{
		UserID: id,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expirationTime),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, _ := token.SignedString(jwtSecret)
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Login successful",
		"token":   tokenStr,
		"role":    role,
	})
}

func meHandler(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	var u User
	var avatar sql.NullString
	err := db.QueryRow("SELECT id, username, email, avatar_url, role, wallet_balance FROM users WHERE id=?", claims.UserID).
		Scan(&u.ID, &u.Username, &u.Email, &avatar, &u.Role, &u.WalletBalance)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if avatar.Valid && avatar.String != "" {
		u.AvatarURL = getFullURL(avatar.String)
	} else {
		u.AvatarURL = ""
	}
	json.NewEncoder(w).Encode(u)
}

func updateProfileHandler(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	username := r.FormValue("username")
	email := r.FormValue("email")
	password := r.FormValue("password")

	var avatarURL string

	// ตรวจสอบและบันทึกไฟล์ avatar
	file, header, err := r.FormFile("avatar")
	if err == nil {
		defer file.Close()
		ext := filepath.Ext(header.Filename)
		filename := fmt.Sprintf("user_%d_%d%s", claims.UserID, time.Now().UnixNano(), ext)
		filepath := filepath.Join("uploads", filename)

		out, err := os.Create(filepath)
		if err != nil {
			log.Println("Failed to create file:", err)
			http.Error(w, "Failed to save avatar", http.StatusInternalServerError)
			return
		}
		defer out.Close()

		n, err := io.Copy(out, file)
		if err != nil || n == 0 {
			log.Println("Failed to write file:", err)
			http.Error(w, "Failed to save avatar", http.StatusInternalServerError)
			return
		}

		avatarURL = "/uploads/" + filename
		log.Println("Avatar saved:", filepath)
	} else {
		log.Println("No avatar uploaded:", err)
	}

	// เตรียม query update
	queryParts := []string{"username=?", "email=?"}
	args := []interface{}{username, email}

	if avatarURL != "" {
		queryParts = append(queryParts, "avatar_url=?")
		args = append(args, avatarURL)
	}

	if password != "" {
		hashed, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		queryParts = append(queryParts, "password_hash=?")
		args = append(args, string(hashed))
	}

	args = append(args, claims.UserID)
	query := fmt.Sprintf("UPDATE users SET %s WHERE id=?", strings.Join(queryParts, ", "))

	_, err = db.Exec(query, args...)
	if err != nil {
		log.Println("Failed to update profile:", err)
		http.Error(w, "Failed to update profile", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{
		"message":    "Profile updated successfully",
		"avatar_url": getFullURL(avatarURL),
	})
}

func adminUsersHandler(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	if claims.Role != "admin" {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	rows, _ := db.Query("SELECT id, username, email, role, wallet_balance FROM users")
	defer rows.Close()
	var users []User
	for rows.Next() {
		var u User
		rows.Scan(&u.ID, &u.Username, &u.Email, &u.Role, &u.WalletBalance)
		users = append(users, u)
	}
	json.NewEncoder(w).Encode(users)
}

func adminUploadGameImageHandler(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	if claims.Role != "admin" {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	file, header, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "No file uploaded", http.StatusBadRequest)
		return
	}
	defer file.Close()

	ext := filepath.Ext(header.Filename)
	filename := fmt.Sprintf("game_%d%s", time.Now().UnixNano(), ext)
	out, _ := os.Create(filepath.Join("uploads", filename))
	defer out.Close()
	io.Copy(out, file)

	json.NewEncoder(w).Encode(map[string]string{"message": "Game image uploaded", "image_url": getFullURL("/uploads/" + filename)})
}

func categoriesHandler(w http.ResponseWriter, r *http.Request) {
	rows, _ := db.Query("SELECT id, name FROM categories")
	defer rows.Close()
	var cats []map[string]interface{}
	for rows.Next() {
		var id int
		var name string
		rows.Scan(&id, &name)
		cats = append(cats, map[string]interface{}{"id": id, "name": name})
	}
	json.NewEncoder(w).Encode(cats)
}

// ================== Wallet Handler ==================
func walletHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	claims := getClaims(r)
	userID := claims.UserID

	// ตรวจสอบว่า user พยายามเข้าถึง wallet ของตัวเองหรือไม่
	pathUserID := r.URL.Path[len("/wallet/"):]
	if pathUserID != fmt.Sprintf("%d", userID) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	var walletBalance float64
	err := db.QueryRow("SELECT wallet_balance FROM users WHERE id = ?", userID).Scan(&walletBalance)
	if err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"wallet_balance": walletBalance,
	})
}

// ================== Cart Handlers ==================
func cartHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	claims := getClaims(r)
	userID := claims.UserID

	// ตรวจสอบว่า user พยายามเข้าถึง cart ของตัวเองหรือไม่
	pathUserID := r.URL.Path[len("/cart/"):]
	if pathUserID != fmt.Sprintf("%d", userID) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	rows, err := db.Query(`
		SELECT c.game_id, g.name, g.price, g.image_url, c.quantity 
		FROM cart c 
		JOIN games g ON c.game_id = g.id 
		WHERE c.user_id = ?
	`, userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var cartItems []map[string]interface{}
	for rows.Next() {
		var gameID, quantity int
		var name string
		var price float64
		var imageURL sql.NullString

		err := rows.Scan(&gameID, &name, &price, &imageURL, &quantity)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		item := map[string]interface{}{
			"game_id":  gameID,
			"name":     name,
			"price":    price,
			"quantity": quantity,
		}

		if imageURL.Valid {
			item["image_url"] = getFullURL(imageURL.String)
		} else {
			item["image_url"] = ""
		}

		cartItems = append(cartItems, item)
	}

	json.NewEncoder(w).Encode(cartItems)
}

func addToCartHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	claims := getClaims(r)
	var input struct {
		UserID   int `json:"user_id"`
		GameID   int `json:"game_id"`
		Quantity int `json:"quantity"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// ตรวจสอบว่า user พยายามเพิ่มสินค้าใน cart ของตัวเอง
	if input.UserID != claims.UserID {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// ตรวจสอบว่ามีเกมนี้อยู่หรือไม่
	var gameExists bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM games WHERE id = ?)", input.GameID).Scan(&gameExists)
	if err != nil || !gameExists {
		http.Error(w, "Game not found", http.StatusNotFound)
		return
	}

	// ตรวจสอบว่ามีใน cart แล้วหรือไม่
	var existingQuantity int
	err = db.QueryRow("SELECT quantity FROM cart WHERE user_id = ? AND game_id = ?", input.UserID, input.GameID).Scan(&existingQuantity)

	if err == sql.ErrNoRows {
		// ยังไม่มีใน cart -> insert ใหม่
		_, err = db.Exec("INSERT INTO cart (user_id, game_id, quantity) VALUES (?, ?, ?)",
			input.UserID, input.GameID, input.Quantity)
	} else if err == nil {
		// มีอยู่แล้ว -> update quantity
		_, err = db.Exec("UPDATE cart SET quantity = quantity + ? WHERE user_id = ? AND game_id = ?",
			input.Quantity, input.UserID, input.GameID)
	}

	if err != nil {
		http.Error(w, "Failed to add to cart", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"message": "Added to cart successfully"})
}

func removeFromCartHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	claims := getClaims(r)
	var input struct {
		UserID int `json:"user_id"`
		GameID int `json:"game_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// ตรวจสอบว่า user พยาย่างลบสินค้าใน cart ของตัวเอง
	if input.UserID != claims.UserID {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	_, err := db.Exec("DELETE FROM cart WHERE user_id = ? AND game_id = ?", input.UserID, input.GameID)
	if err != nil {
		http.Error(w, "Failed to remove from cart", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"message": "Removed from cart successfully"})
}

func clearCartHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	claims := getClaims(r)
	var input struct {
		UserID int `json:"user_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// ตรวจสอบว่า user พยาย่างล้าง cart ของตัวเอง
	if input.UserID != claims.UserID {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	_, err := db.Exec("DELETE FROM cart WHERE user_id = ?", input.UserID)
	if err != nil {
		http.Error(w, "Failed to clear cart", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"message": "Cart cleared successfully"})
}

func adminGameHandler(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	if claims.Role != "admin" {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	var id int
	idStr := strings.TrimPrefix(r.URL.Path, "/game/admin/")
	if idStr != "" && idStr != "/game/admin" {
		fmt.Sscanf(idStr, "%d", &id)
	}

	var err error

	switch r.Method {
	case http.MethodPost:
		// เพิ่มเกมใหม่
		name := r.FormValue("name")
		price := r.FormValue("price")
		categoryID := r.FormValue("category_id")
		description := r.FormValue("description")

		var imageURL string
		file, header, err := r.FormFile("image")
		if err == nil {
			defer file.Close()
			ext := filepath.Ext(header.Filename)
			filename := fmt.Sprintf("game_%d%s", time.Now().UnixNano(), ext)
			out, _ := os.Create(filepath.Join("uploads", filename))
			defer out.Close()
			io.Copy(out, file)
			imageURL = "/uploads/" + filename
		}

		_, err = db.Exec(`INSERT INTO games (name, price, category_id, description, image_url) VALUES (?, ?, ?, ?, ?)`,
			name, price, categoryID, description, imageURL)

	case http.MethodPut:
		// แก้ไขเกม
		if id <= 0 {
			http.Error(w, "Missing game ID", http.StatusBadRequest)
			return
		}

		name := r.FormValue("name")
		price := r.FormValue("price")
		categoryID := r.FormValue("category_id")
		description := r.FormValue("description")

		var imageURL string
		file, header, err := r.FormFile("image")
		if err == nil {
			defer file.Close()
			ext := filepath.Ext(header.Filename)
			filename := fmt.Sprintf("game_%d%s", time.Now().UnixNano(), ext)
			out, _ := os.Create(filepath.Join("uploads", filename))
			defer out.Close()
			io.Copy(out, file)
			imageURL = "/uploads/" + filename
		}

		if imageURL != "" {
			_, err = db.Exec(`UPDATE games SET name=?, price=?, category_id=?, description=?, image_url=? WHERE id=?`,
				name, price, categoryID, description, imageURL, id)
		} else {
			_, err = db.Exec(`UPDATE games SET name=?, price=?, category_id=?, description=? WHERE id=?`,
				name, price, categoryID, description, id)
		}

	case http.MethodDelete:
		// ลบเกม
		if id <= 0 {
			http.Error(w, "Missing game ID", http.StatusBadRequest)
			return
		}

		_, err = db.Exec("DELETE FROM games WHERE id=?", id)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var message string
	switch r.Method {
	case http.MethodPost:
		message = "Game added successfully"
	case http.MethodPut:
		message = "Game updated successfully"
	case http.MethodDelete:
		message = "Game deleted successfully"
	}

	json.NewEncoder(w).Encode(map[string]string{"message": message})
}
