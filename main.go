package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"golang.org/x/crypto/bcrypt"
)

var db *sql.DB
var sessions = map[string]int{} // sessionID -> userID

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

	// Routes with CORS
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

	// Serve uploads folder (no CORS needed)
	http.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir("uploads"))))

	ip := getLocalIP()
	fmt.Println("🚀 Server started at http://" + ip + ":8080")

	log.Fatal(http.ListenAndServe("0.0.0.0:8080", nil))
}

// ================== CORS Middleware ==================
func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// ระบุ frontend origin
		w.Header().Set("Access-Control-Allow-Origin", "http://localhost:4200")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Credentials", "true") // ต้องมี

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	}
}

// ================== IP Finder ==================
func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ip4 := ipnet.IP.To4(); ip4 != nil && !ip4.IsLinkLocalUnicast() {
				return ip4.String()
			}
		}
	}
	return "127.0.0.1"
}

// ================== Root Handler ==================
func rootHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Game Shop API"})
}

// ================== Game Handler ==================
func gameHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// ดึงเกมทั้งหมด
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

	case http.MethodPost:
		// สร้างเกมใหม่ (ต้องเป็น admin)
		userID := r.Header.Get("X-User-ID")
		var role string
		err := db.QueryRow("SELECT role FROM users WHERE id=?", userID).Scan(&role)
		if err != nil || role != "admin" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		// รับข้อมูลจาก FormData
		name := r.FormValue("name")
		price := r.FormValue("price")
		categoryID := r.FormValue("category_id")
		description := r.FormValue("description")

		if name == "" || price == "" || categoryID == "" {
			http.Error(w, "Missing required fields", http.StatusBadRequest)
			return
		}

		// อัพโหลดรูปภาพ (ถ้ามี)
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

		// บันทึกลง database
		result, err := db.Exec(
			"INSERT INTO games (name, price, category_id, image_url, description) VALUES (?, ?, ?, ?, ?)",
			name, price, categoryID, imageURL, description,
		)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		id, _ := result.LastInsertId()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      id,
			"message": "Game created successfully",
		})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func gameByIDHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Path[len("/game/"):]

	switch r.Method {
	case http.MethodGet:
		// ดึงเกมโดย ID
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

	case http.MethodPut:
		// อัพเดทเกม (ต้องเป็น admin)
		userID := r.Header.Get("X-User-ID")
		var role string
		err := db.QueryRow("SELECT role FROM users WHERE id=?", userID).Scan(&role)
		if err != nil || role != "admin" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		// รับข้อมูลจาก FormData
		name := r.FormValue("name")
		price := r.FormValue("price")
		categoryID := r.FormValue("category_id")
		description := r.FormValue("description")

		if name == "" || price == "" || categoryID == "" {
			http.Error(w, "Missing required fields", http.StatusBadRequest)
			return
		}

		// อัพโหลดรูปภาพ (ถ้ามี)
		file, header, err := r.FormFile("image")
		if err == nil {
			defer file.Close()
			ext := filepath.Ext(header.Filename)
			filename := fmt.Sprintf("game_%s%s", idStr, ext)
			out, err := os.Create(filepath.Join("uploads", filename))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			defer out.Close()
			io.Copy(out, file)
			imageURL := "/uploads/" + filename

			// อัพเดทรูปภาพ
			_, err = db.Exec(
				"UPDATE games SET name=?, price=?, category_id=?, image_url=?, description=? WHERE id=?",
				name, price, categoryID, imageURL, description, idStr,
			)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		} else {
			// อัพเดทโดยไม่เปลี่ยนรูปภาพ
			_, err = db.Exec(
				"UPDATE games SET name=?, price=?, category_id=?, description=? WHERE id=?",
				name, price, categoryID, description, idStr,
			)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		json.NewEncoder(w).Encode(map[string]string{
			"message": "Game updated successfully",
		})

	case http.MethodDelete:
		// ลบเกม (ต้องเป็น admin)
		userID := r.Header.Get("X-User-ID")
		var role string
		err := db.QueryRow("SELECT role FROM users WHERE id=?", userID).Scan(&role)
		if err != nil || role != "admin" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		// ตรวจสอบว่ามีคนซื้อเกมนี้แล้วหรือยัง
		var purchaseCount int
		err = db.QueryRow("SELECT COUNT(*) FROM purchased_games WHERE game_id = ?", idStr).Scan(&purchaseCount)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if purchaseCount > 0 {
			http.Error(w, "Cannot delete game that has been purchased", http.StatusBadRequest)
			return
		}

		_, err = db.Exec("DELETE FROM games WHERE id=?", idStr)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]string{
			"message": "Game deleted successfully",
		})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// ================== Auth Middleware ==================
func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session_id")
		if err != nil {
			fmt.Println("❌ No session cookie found")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		userID, ok := sessions[cookie.Value]
		if !ok {
			fmt.Printf("❌ Session not found: %s\n", cookie.Value)
			fmt.Printf("📋 All sessions: %+v\n", sessions)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		fmt.Printf("🔍 Session found: %s -> UserID: %d\n", cookie.Value, userID)

		var role string
		err = db.QueryRow("SELECT role FROM users WHERE id=?", userID).Scan(&role)
		if err != nil {
			fmt.Printf("❌ User not found in DB: %d, error: %v\n", userID, err)
			http.Error(w, "User not found", http.StatusUnauthorized)
			return
		}

		fmt.Printf("🎯 User %d role: %s\n", userID, role)

		if role != "admin" {
			fmt.Printf("🚫 Access denied - User %d is not admin (role: %s)\n", userID, role)
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		fmt.Printf("✅ User %d (admin) authorized for: %s %s\n", userID, r.Method, r.URL.Path)
		r.Header.Set("X-User-ID", fmt.Sprintf("%d", userID))
		next.ServeHTTP(w, r)
	}
}

// ================== Register ==================
func registerHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// รองรับ JSON หรือ FormData
	var input struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	contentType := r.Header.Get("Content-Type")
	if contentType == "application/json" || contentType == "application/json; charset=utf-8" {
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	} else {
		input.Username = r.FormValue("username")
		input.Email = r.FormValue("email")
		input.Password = r.FormValue("password")
	}

	var exists int
	err := db.QueryRow("SELECT COUNT(*) FROM users WHERE email = ?", input.Email).Scan(&exists)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if exists > 0 {
		http.Error(w, "Email already exists", http.StatusBadRequest)
		return
	}

	hashed, _ := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)

	_, err = db.Exec("INSERT INTO users (username, email, password_hash, role) VALUES (?, ?, ?, 'user')",
		input.Username, input.Email, string(hashed))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"message": "User registered successfully"})
}

// ================== Login ==================
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
	err := db.QueryRow(
		"SELECT id, password_hash, role FROM users WHERE email = ? OR username = ?",
		input.Identifier, input.Identifier,
	).Scan(&id, &hashed, &role)
	if err != nil {
		http.Error(w, "Username or email incorrect", http.StatusUnauthorized)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hashed), []byte(input.Password)); err != nil {
		http.Error(w, "Password incorrect", http.StatusUnauthorized)
		return
	}

	// สร้าง session
	sessionID := fmt.Sprintf("%d_%d", id, time.Now().UnixNano())
	sessions[sessionID] = id

	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    sessionID,
		Path:     "/",
		Expires:  time.Now().Add(24 * time.Hour),
		HttpOnly: true,
	})

	json.NewEncoder(w).Encode(map[string]string{
		"message": "Login successful",
		"role":    role,
	})
}

// ================== Profile ==================
func meHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID := r.Header.Get("X-User-ID")
	var u User
	var avatar sql.NullString
	err := db.QueryRow("SELECT id, username, email, avatar_url, role, wallet_balance FROM users WHERE id = ?", userID).
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

// ================== Update User ==================
func updateMeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID := r.Header.Get("X-User-ID")
	var input struct {
		Username string `json:"username"`
		Email    string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	_, err := db.Exec("UPDATE users SET username=?, email=? WHERE id=?", input.Username, input.Email, userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"message": "Profile updated successfully"})
}

// ================== Upload Avatar ==================
func uploadAvatarHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID := r.Header.Get("X-User-ID")

	file, header, err := r.FormFile("avatar")
	if err != nil {
		http.Error(w, "No file uploaded", http.StatusBadRequest)
		return
	}
	defer file.Close()

	ext := filepath.Ext(header.Filename)
	filename := fmt.Sprintf("user_%s%s", userID, ext)
	out, err := os.Create(filepath.Join("uploads", filename))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer out.Close()
	io.Copy(out, file)

	avatarURL := "/uploads/" + filename
	_, err = db.Exec("UPDATE users SET avatar_url=? WHERE id=?", avatarURL, userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"message": "Avatar uploaded", "avatar_url": avatarURL})
}

// ================== Admin Users ==================
func adminUsersHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	var role string
	err := db.QueryRow("SELECT role FROM users WHERE id=?", userID).Scan(&role)
	if err != nil || role != "admin" {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// รับ query parameter
	includeAdmin := r.URL.Query().Get("includeAdmin")

	var query string
	if includeAdmin == "true" {
		query = "SELECT id, username, email, avatar_url, role, wallet_balance FROM users"
	} else {
		query = "SELECT id, username, email, avatar_url, role, wallet_balance FROM users WHERE role != 'admin'"
	}

	rows, err := db.Query(query)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		var avatar sql.NullString
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &avatar, &u.Role, &u.WalletBalance); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if avatar.Valid {
			u.AvatarURL = avatar.String
		} else {
			u.AvatarURL = ""
		}
		users = append(users, u)
	}

	json.NewEncoder(w).Encode(users)
}

func adminUploadGameImageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := r.Header.Get("X-User-ID")

	// ตรวจสอบว่าเป็น admin
	var role string
	err := db.QueryRow("SELECT role FROM users WHERE id=?", userID).Scan(&role)
	if err != nil || role != "admin" {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// ดึง game_id จาก FormValue
	gameID := r.FormValue("game_id")
	if gameID == "" {
		http.Error(w, "Missing game_id", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "No file uploaded", http.StatusBadRequest)
		return
	}
	defer file.Close()

	ext := filepath.Ext(header.Filename)
	filename := fmt.Sprintf("game_%s%s", gameID, ext)
	out, err := os.Create(filepath.Join("uploads", filename))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer out.Close()
	io.Copy(out, file)

	imageURL := "/uploads/" + filename
	_, err = db.Exec("UPDATE games SET image_url=? WHERE id=?", imageURL, gameID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{
		"message":   "Game image uploaded",
		"image_url": imageURL,
	})
}

// ================== Categories Handler ==================
func categoriesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rows, err := db.Query("SELECT id, name FROM categories")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var categories []Category
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.ID, &c.Name); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		categories = append(categories, c)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(categories)
}

// ================== Admin Discount Codes Handler ==================
func adminDiscountCodesHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	var role string
	err := db.QueryRow("SELECT role FROM users WHERE id=?", userID).Scan(&role)
	if err != nil || role != "admin" {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	switch r.Method {
	case http.MethodGet:
		rows, err := db.Query(`
            SELECT id, code, type, value, usage_limit, min_total, 
                   start_date, end_date, single_use_per_user, active 
            FROM discount_codes
        `)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var codes []DiscountCode
		for rows.Next() {
			var dc DiscountCode
			var startDate, endDate sql.NullString
			var singleUsePerUser, active int
			err := rows.Scan(&dc.ID, &dc.Code, &dc.Type, &dc.Value, &dc.UsageLimit,
				&dc.MinTotal, &startDate, &endDate, &singleUsePerUser, &active)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if startDate.Valid {
				dc.StartDate = startDate.String
			}
			if endDate.Valid {
				dc.EndDate = endDate.String
			}
			dc.SingleUsePerUser = singleUsePerUser == 1
			dc.Active = active
			codes = append(codes, dc)
		}
		json.NewEncoder(w).Encode(codes)

	case http.MethodPost:
		var input struct {
			Code             string  `json:"code"`
			Type             string  `json:"type"`
			Value            float64 `json:"value"`
			UsageLimit       int     `json:"usage_limit"`
			MinTotal         float64 `json:"min_total"`
			StartDate        string  `json:"start_date"`
			EndDate          string  `json:"end_date"`
			SingleUsePerUser bool    `json:"single_use_per_user"`
			Active           int     `json:"active"`
		}

		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		singleUse := 0
		if input.SingleUsePerUser {
			singleUse = 1
		}

		result, err := db.Exec(`
            INSERT INTO discount_codes 
            (code, type, value, usage_limit, min_total, start_date, end_date, single_use_per_user, active)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			input.Code, input.Type, input.Value, input.UsageLimit, input.MinTotal,
			input.StartDate, input.EndDate, singleUse, input.Active)

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		id, _ := result.LastInsertId()
		json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "message": "Discount code created"})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// ================== Admin Discount Code By ID Handler ==================
func adminDiscountCodeByIDHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	var role string
	err := db.QueryRow("SELECT role FROM users WHERE id=?", userID).Scan(&role)
	if err != nil || role != "admin" {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	idStr := r.URL.Path[len("/admin/discount-codes/"):]

	switch r.Method {
	case http.MethodPut:
		var input struct {
			Code             string  `json:"code"`
			Type             string  `json:"type"`
			Value            float64 `json:"value"`
			UsageLimit       int     `json:"usage_limit"`
			MinTotal         float64 `json:"min_total"`
			StartDate        string  `json:"start_date"`
			EndDate          string  `json:"end_date"`
			SingleUsePerUser bool    `json:"single_use_per_user"`
			Active           int     `json:"active"`
		}

		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		singleUse := 0
		if input.SingleUsePerUser {
			singleUse = 1
		}

		_, err := db.Exec(`
            UPDATE discount_codes 
            SET code=?, type=?, value=?, usage_limit=?, min_total=?, 
                start_date=?, end_date=?, single_use_per_user=?, active=?
            WHERE id=?`,
			input.Code, input.Type, input.Value, input.UsageLimit, input.MinTotal,
			input.StartDate, input.EndDate, singleUse, input.Active, idStr)

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]string{"message": "Discount code updated"})

	case http.MethodDelete:
		_, err := db.Exec("DELETE FROM discount_codes WHERE id=?", idStr)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"message": "Discount code deleted"})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// ================== Admin Transactions Handler ==================
func adminTransactionsHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	var role string
	err := db.QueryRow("SELECT role FROM users WHERE id=?", userID).Scan(&role)
	if err != nil || role != "admin" {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rows, err := db.Query(`
        SELECT t.id, t.user_id, u.username, t.game_id, g.name, t.amount, 
               t.transaction_date, t.status, t.payment_method
        FROM transactions t
        JOIN users u ON t.user_id = u.id
        JOIN games g ON t.game_id = g.id
        ORDER BY t.transaction_date DESC
    `)
	if err != nil {
		// ถ้าไม่มีตาราง transactions ให้ใช้ user_transactions แทน
		rows, err = db.Query(`
            SELECT ut.id, ut.user_id, u.username, 0 as game_id, 
                   ut.description as game_name, ut.amount, 
                   ut.created_at as transaction_date, 
                   'completed' as status, 
                   ut.type as payment_method
            FROM user_transactions ut
            JOIN users u ON ut.user_id = u.id
            ORDER BY ut.created_at DESC
        `)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	defer rows.Close()

	var transactions []Transaction
	for rows.Next() {
		var t Transaction
		var transactionDate sql.NullString
		err := rows.Scan(&t.ID, &t.UserID, &t.Username, &t.GameID, &t.GameName,
			&t.Amount, &transactionDate, &t.Status, &t.PaymentMethod)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if transactionDate.Valid {
			t.TransactionDate = transactionDate.String
		} else {
			t.TransactionDate = ""
		}
		transactions = append(transactions, t)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(transactions)
}
