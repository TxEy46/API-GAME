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

	// Serve uploads folder (no CORS needed)
	http.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir("uploads"))))

	ip := getLocalIP()
	fmt.Println("🚀 Server started at http://" + ip + ":8080")

	log.Fatal(http.ListenAndServe("0.0.0.0:8080", nil))
}

// ================== CORS Middleware ==================
func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*") // Dev: allow all, production: restrict domain
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

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

	// ดึง id จาก URL
	idStr := r.URL.Path[len("/game/"):] // "/game/16" -> "16"
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

// ================== Auth Middleware ==================
func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session_id")
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		userID, ok := sessions[cookie.Value]
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
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

	rows, err := db.Query("SELECT id, username, email, avatar_url, role, wallet_balance FROM users")
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
