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
var jwtSecret = []byte("your_secret_key_here") // เปลี่ยนเป็น secret ของคุณเอง

// ================== Data Structures ==================
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

type Category struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type DiscountCode struct {
	ID               int     `json:"id"`
	Code             string  `json:"code"`
	Type             string  `json:"type"` // percent, fixed
	Value            float64 `json:"value"`
	UsageLimit       int     `json:"usage_limit"`
	MinTotal         float64 `json:"min_total"`
	StartDate        string  `json:"start_date"`
	EndDate          string  `json:"end_date"`
	SingleUsePerUser bool    `json:"single_use_per_user"`
	Active           int     `json:"active"` // 0 or 1
}

type Cart struct {
	ID        int        `json:"id"`
	UserID    int        `json:"user_id"`
	CreatedAt string     `json:"created_at"`
	Items     []CartItem `json:"items"`
}

type CartItem struct {
	ID       int `json:"id"`
	CartID   int `json:"cart_id"`
	GameID   int `json:"game_id"`
	Quantity int `json:"quantity"`
}

type Purchase struct {
	ID             int     `json:"id"`
	UserID         int     `json:"user_id"`
	PurchaseDate   string  `json:"purchase_date"`
	TotalAmount    float64 `json:"total_amount"`
	DiscountCodeID *int    `json:"discount_code_id"`
	FinalAmount    float64 `json:"final_amount"`
}

type PurchaseItem struct {
	ID              int     `json:"id"`
	PurchaseID      int     `json:"purchase_id"`
	GameID          int     `json:"game_id"`
	PriceAtPurchase float64 `json:"price_at_purchase"`
}

type PurchasedGame struct {
	ID          int    `json:"id"`
	UserID      int    `json:"user_id"`
	GameID      int    `json:"game_id"`
	PurchasedAt string `json:"purchased_at"`
}

type Transaction struct {
	ID              int     `json:"id"`
	UserID          int     `json:"user_id"`
	Username        string  `json:"username"`
	GameID          int     `json:"game_id"`
	GameName        string  `json:"game_name"`
	Amount          float64 `json:"amount"`
	TransactionDate string  `json:"transaction_date"`
	Status          string  `json:"status"`
	PaymentMethod   string  `json:"payment_method"`
}

type UserTransaction struct {
	ID          int     `json:"id"`
	UserID      int     `json:"user_id"`
	Type        string  `json:"type"` // deposit, purchase
	Amount      float64 `json:"amount"`
	Description string  `json:"description"`
	CreatedAt   string  `json:"created_at"`
}

type Ranking struct {
	ID           int `json:"id"`
	GameID       int `json:"game_id"`
	SalesCount   int `json:"sales_count"`
	RankPosition int `json:"rank_position"`
}

type UserDiscountCode struct {
	ID             int    `json:"id"`
	UserID         int    `json:"user_id"`
	DiscountCodeID int    `json:"discount_code_id"`
	UsedAt         string `json:"used_at"`
}

// ================== JWT Claims ==================
type Claims struct {
	UserID int    `json:"user_id"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

// ================== Main ==================
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

	// Routes
	http.HandleFunc("/", corsMiddleware(rootHandler))
	http.HandleFunc("/game", corsMiddleware(gameHandler))
	http.HandleFunc("/game/", corsMiddleware(gameByIDHandler))
	http.HandleFunc("/register", corsMiddleware(registerHandler))
	http.HandleFunc("/login", corsMiddleware(loginHandler))
	http.HandleFunc("/me", corsMiddleware(authMiddleware(meHandler)))
	http.HandleFunc("/me/update", corsMiddleware(authMiddleware(updateMeHandler)))
	http.HandleFunc("/me/upload", corsMiddleware(authMiddleware(uploadAvatarHandler)))
	http.HandleFunc("/admin/users", corsMiddleware(authMiddleware(adminUsersHandler)))
	http.HandleFunc("/admin/game/upload", corsMiddleware(authMiddleware(adminUploadGameImageHandler)))
	http.HandleFunc("/categories", corsMiddleware(categoriesHandler))
	http.HandleFunc("/admin/discount-codes", corsMiddleware(authMiddleware(adminDiscountCodesHandler)))
	http.HandleFunc("/admin/discount-codes/", corsMiddleware(authMiddleware(adminDiscountCodeByIDHandler)))
	http.HandleFunc("/admin/transactions", corsMiddleware(authMiddleware(adminTransactionsHandler)))
	http.HandleFunc("/game/admin", corsMiddleware(authMiddleware(gameAdminHandler)))
	http.HandleFunc("/game/admin/", corsMiddleware(authMiddleware(gameAdminByIDHandler)))
	http.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir("uploads"))))
	http.HandleFunc("/me/update-with-avatar", corsMiddleware(authMiddleware(updateProfileHandler)))

	ip := getLocalIP()
	fmt.Println("🚀 Server started at http://" + ip + ":8080")
	log.Fatal(http.ListenAndServe("0.0.0.0:8080", nil))
}

// ================== CORS Middleware ==================
func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// อนุญาตเฉพาะ Angular dev server
		w.Header().Set("Access-Control-Allow-Origin", "http://localhost:4200")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		// Preflight request
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	}
}

// ================== JWT Auth Middleware ==================
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

// ================== Helper ==================
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

func getClaims(r *http.Request) *Claims {
	if claims, ok := r.Context().Value("claims").(*Claims); ok {
		return claims
	}
	return nil
}

// ================== Handlers ==================
// Root
func rootHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Game Shop API"})
}

// ================== Game ==================
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
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(g)
}

func gameAdminHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	claims := getClaims(r)
	if claims.Role != "admin" {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	name := r.FormValue("name")
	price := r.FormValue("price")
	categoryID := r.FormValue("category_id")
	description := r.FormValue("description")

	if name == "" || price == "" || categoryID == "" {
		http.Error(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	var imageURL string
	file, header, err := r.FormFile("image")
	if err == nil {
		defer file.Close()
		ext := filepath.Ext(header.Filename)
		filename := fmt.Sprintf("game_%d%s", time.Now().UnixNano(), ext)
		out, err := os.Create(filepath.Join("uploads", filename))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer out.Close()
		io.Copy(out, file)
		imageURL = "/uploads/" + filename
	}

	// ================== แก้ตรงนี้ ==================
	// ใส่ release_date เป็นวันที่ปัจจุบันเอง
	result, err := db.Exec(
		"INSERT INTO games (name, price, category_id, image_url, description, release_date) VALUES (?, ?, ?, ?, ?, ?)",
		name, price, categoryID, imageURL, description, time.Now().Format("2006-01-02"),
	)
	// =============================================

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	id, _ := result.LastInsertId()
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":      id,
		"message": "Game created successfully",
	})
}

// ================== Admin Game By ID ==================
func gameAdminByIDHandler(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	if claims.Role != "admin" {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	idStr := strings.Trim(r.URL.Path[len("/game/admin/"):], "/") // trim slash

	switch r.Method {
	case http.MethodPut:
		// ปรับ: ไม่อัพเดต release_date
		var input struct {
			Name        string  `json:"name"`
			Price       float64 `json:"price"`
			CategoryID  int     `json:"category_id"`
			Description string  `json:"description"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		_, err := db.Exec(
			"UPDATE games SET name=?, price=?, category_id=?, description=? WHERE id=?",
			input.Name, input.Price, input.CategoryID, input.Description, idStr,
		)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"message": "Game updated successfully"})

	case http.MethodDelete:
		var purchaseCount int
		db.QueryRow("SELECT COUNT(*) FROM purchased_games WHERE game_id = ?", idStr).Scan(&purchaseCount)
		if purchaseCount > 0 {
			http.Error(w, "Cannot delete game that has been purchased", http.StatusBadRequest)
			return
		}
		_, err := db.Exec("DELETE FROM games WHERE id=?", idStr)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"message": "Game deleted successfully"})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// ================== Register & Login ==================
func registerHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var exists int
	db.QueryRow("SELECT COUNT(*) FROM users WHERE email = ?", input.Email).Scan(&exists)
	if exists > 0 {
		http.Error(w, "Email already exists", http.StatusBadRequest)
		return
	}

	hashed, _ := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	db.Exec("INSERT INTO users (username, email, password_hash, role) VALUES (?, ?, ?, 'user')",
		input.Username, input.Email, string(hashed))
	json.NewEncoder(w).Encode(map[string]string{"message": "User registered successfully"})
}

func loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		Identifier string `json:"identifier"` // username หรือ email
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

// ================== Profile ==================
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
	if avatar.Valid {
		u.AvatarURL = avatar.String
	} else {
		u.AvatarURL = ""
	}
	json.NewEncoder(w).Encode(u)
}

func updateMeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	claims := getClaims(r)
	var input struct {
		Username string `json:"username"`
		Email    string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	db.Exec("UPDATE users SET username=?, email=? WHERE id=?", input.Username, input.Email, claims.UserID)
	json.NewEncoder(w).Encode(map[string]string{"message": "Profile updated successfully"})
}

func uploadAvatarHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	claims := getClaims(r)

	file, header, err := r.FormFile("avatar")
	if err != nil {
		http.Error(w, "No file uploaded", http.StatusBadRequest)
		return
	}
	defer file.Close()

	ext := filepath.Ext(header.Filename)
	filename := fmt.Sprintf("user_%d%s", claims.UserID, ext)
	out, _ := os.Create(filepath.Join("uploads", filename))
	defer out.Close()
	io.Copy(out, file)

	avatarURL := "/uploads/" + filename
	db.Exec("UPDATE users SET avatar_url=? WHERE id=?", avatarURL, claims.UserID)
	json.NewEncoder(w).Encode(map[string]string{"message": "Avatar uploaded", "avatar_url": avatarURL})
}

// ================== Admin Users ==================
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

// ================== Admin Upload Game Image ==================
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

	json.NewEncoder(w).Encode(map[string]string{"message": "Game image uploaded", "image_url": "/uploads/" + filename})
}

// ================== Categories ==================
func categoriesHandler(w http.ResponseWriter, r *http.Request) {
	rows, _ := db.Query("SELECT id, name FROM categories")
	defer rows.Close()
	var cats []Category
	for rows.Next() {
		var c Category
		rows.Scan(&c.ID, &c.Name)
		cats = append(cats, c)
	}
	json.NewEncoder(w).Encode(cats)
}

// ================== Admin Discount Codes ==================
func adminDiscountCodesHandler(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	if claims.Role != "admin" {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	rows, _ := db.Query("SELECT id, code, type, value, usage_limit, min_total, start_date, end_date, single_use_per_user, active FROM discount_codes")
	defer rows.Close()
	var codes []DiscountCode
	for rows.Next() {
		var d DiscountCode
		var startDate, endDate sql.NullString
		rows.Scan(&d.ID, &d.Code, &d.Type, &d.Value, &d.UsageLimit, &d.MinTotal, &startDate, &endDate, &d.SingleUsePerUser, &d.Active)
		if startDate.Valid {
			d.StartDate = startDate.String
		}
		if endDate.Valid {
			d.EndDate = endDate.String
		}
		codes = append(codes, d)
	}
	json.NewEncoder(w).Encode(codes)
}

func adminDiscountCodeByIDHandler(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	if claims.Role != "admin" {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	// ตัวอย่าง: /admin/discount-codes/1
	idStr := r.URL.Path[len("/admin/discount-codes/"):]
	var d DiscountCode
	var startDate, endDate sql.NullString
	err := db.QueryRow("SELECT id, code, type, value, usage_limit, min_total, start_date, end_date, single_use_per_user, active FROM discount_codes WHERE id=?", idStr).
		Scan(&d.ID, &d.Code, &d.Type, &d.Value, &d.UsageLimit, &d.MinTotal, &startDate, &endDate, &d.SingleUsePerUser, &d.Active)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if startDate.Valid {
		d.StartDate = startDate.String
	}
	if endDate.Valid {
		d.EndDate = endDate.String
	}
	json.NewEncoder(w).Encode(d)
}

// ================== Admin Transactions ==================
func adminTransactionsHandler(w http.ResponseWriter, r *http.Request) {
	claims := getClaims(r)
	if claims.Role != "admin" {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	rows, _ := db.Query("SELECT t.id, t.user_id, u.username, t.game_id, g.name, t.amount, t.transaction_date, t.status, t.payment_method FROM transactions t JOIN users u ON t.user_id=u.id JOIN games g ON t.game_id=g.id")
	defer rows.Close()
	var txs []Transaction
	for rows.Next() {
		var t Transaction
		rows.Scan(&t.ID, &t.UserID, &t.Username, &t.GameID, &t.GameName, &t.Amount, &t.TransactionDate, &t.Status, &t.PaymentMethod)
		txs = append(txs, t)
	}
	json.NewEncoder(w).Encode(txs)
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

	// อัปเดต avatar ถ้ามี
	file, header, err := r.FormFile("avatar")
	avatarURL := ""
	if err == nil {
		defer file.Close()
		ext := filepath.Ext(header.Filename)
		filename := fmt.Sprintf("user_%d%s", claims.UserID, ext)
		out, _ := os.Create(filepath.Join("uploads", filename))
		defer out.Close()
		io.Copy(out, file)
		avatarURL = "/uploads/" + filename
	}

	// อัปเดตใน DB
	if avatarURL != "" && password != "" {
		hashed, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		db.Exec("UPDATE users SET username=?, email=?, avatar_url=?, password_hash=? WHERE id=?",
			username, email, avatarURL, string(hashed), claims.UserID)
	} else if avatarURL != "" {
		db.Exec("UPDATE users SET username=?, email=?, avatar_url=? WHERE id=?",
			username, email, avatarURL, claims.UserID)
	} else if password != "" {
		hashed, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		db.Exec("UPDATE users SET username=?, email=?, password_hash=? WHERE id=?",
			username, email, string(hashed), claims.UserID)
	} else {
		db.Exec("UPDATE users SET username=?, email=? WHERE id=?", username, email, claims.UserID)
	}

	json.NewEncoder(w).Encode(map[string]string{"message": "Profile updated successfully"})
}
