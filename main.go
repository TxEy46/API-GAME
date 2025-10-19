package main

import (
	"database/sql"
	"fmt"
	"go-api-game/handlers"
	"log"
	"net/http"
	"os"

	"go-api-game/config"

	_ "github.com/go-sql-driver/mysql"
	"github.com/rs/cors"
)

var db *sql.DB

func main() {
	// --------------------------
	// Connect Database
	// --------------------------
	var err error
	// ข้อมูลการเชื่อมต่อฐานข้อมูล MySQL
	dsn := "65011212151:TxEy2003122@tcp(202.28.34.210:3309)/db65011212151"
	db, err = sql.Open("mysql", dsn)
	if err != nil {
		log.Fatal("Cannot connect to database:", err)
	}
	defer db.Close()

	// ทดสอบการเชื่อมต่อฐานข้อมูล
	if err = db.Ping(); err != nil {
		log.Fatal("Cannot ping database:", err)
	}
	fmt.Println("✅ Connected to database successfully")

	// Initialize handlers with database
	handlers.InitDB(db)

	// Create uploads folder if not exists
	// สร้างโฟลเดอร์ uploads หากยังไม่มี (สำหรับเก็บไฟล์ภาพ)
	if _, err := os.Stat("uploads"); os.IsNotExist(err) {
		os.Mkdir("uploads", 0755)
	}

	// --------------------------
	// Initialize Cloudinary
	// --------------------------
	config.InitCloudinary()

	// --------------------------
	// Public Routes
	// เส้นทางที่ไม่ต้องยืนยันตัวตน
	// --------------------------
	http.HandleFunc("/", handlers.RootHandler)                 // หน้าแรก
	http.HandleFunc("/register", handlers.RegisterHandler)     // ลงทะเบียน
	http.HandleFunc("/login", handlers.LoginHandler)           // เข้าสู่ระบบ
	http.HandleFunc("/games", handlers.GamesHandler)           // รายการเกมทั้งหมด
	http.HandleFunc("/games/", handlers.GameByIDHandler)       // ข้อมูลเกมตาม ID
	http.HandleFunc("/categories", handlers.CategoriesHandler) // รายการหมวดหมู่
	http.HandleFunc("/search", handlers.SearchHandler)         // ค้นหาเกม
	http.HandleFunc("/ranking", handlers.RankingHandler)       // อันดับเกม

	// --------------------------
	// User Routes (Protected)
	// เส้นทางที่ต้องยืนยันตัวตน (ผู้ใช้ทั่วไป)
	// --------------------------
	http.Handle("/profile", handlers.AuthMiddleware(http.HandlerFunc(handlers.ProfileHandler)))
	http.Handle("/wallet", handlers.AuthMiddleware(http.HandlerFunc(handlers.WalletHandler)))
	http.Handle("/deposit", handlers.AuthMiddleware(http.HandlerFunc(handlers.DepositHandler)))
	http.Handle("/transactions", handlers.AuthMiddleware(http.HandlerFunc(handlers.TransactionsHandler)))
	http.Handle("/library", handlers.AuthMiddleware(http.HandlerFunc(handlers.LibraryHandler)))
	http.Handle("/cart", handlers.AuthMiddleware(http.HandlerFunc(handlers.CartHandler)))
	http.Handle("/cart/add", handlers.AuthMiddleware(http.HandlerFunc(handlers.AddToCartHandler)))
	http.Handle("/cart/remove", handlers.AuthMiddleware(http.HandlerFunc(handlers.RemoveFromCartHandler)))
	http.Handle("/checkout", handlers.AuthMiddleware(http.HandlerFunc(handlers.CheckoutHandler)))
	http.Handle("/purchases", handlers.AuthMiddleware(http.HandlerFunc(handlers.PurchaseHistoryHandler)))
	http.Handle("/profile/update", handlers.AuthMiddleware(http.HandlerFunc(handlers.UpdateProfileHandler)))
	http.Handle("/discounts/apply", handlers.AuthMiddleware(http.HandlerFunc(handlers.ApplyDiscountHandler)))

	// --------------------------
	// Admin Routes (Protected + Admin only)
	// เส้นทางสำหรับผู้ดูแลระบบเท่านั้น
	// --------------------------
	http.Handle("/admin/games", handlers.AuthMiddleware(handlers.AdminOnly(http.HandlerFunc(handlers.AdminAddGameHandler))))
	http.Handle("/admin/games/", handlers.AuthMiddleware(handlers.AdminOnly(http.HandlerFunc(handlers.AdminUpdateGameHandler))))
	http.Handle("/admin/games/delete/", handlers.AuthMiddleware(handlers.AdminOnly(http.HandlerFunc(handlers.AdminDeleteGameHandler))))
	http.Handle("/admin/discounts", handlers.AuthMiddleware(handlers.AdminOnly(http.HandlerFunc(handlers.AdminDiscountHandler))))
	http.Handle("/admin/discounts/", handlers.AuthMiddleware(handlers.AdminOnly(http.HandlerFunc(handlers.AdminDiscountHandler))))
	http.Handle("/admin/users", handlers.AuthMiddleware(handlers.AdminOnly(http.HandlerFunc(handlers.AdminUsersHandler))))
	http.Handle("/admin/stats", handlers.AuthMiddleware(handlers.AdminOnly(http.HandlerFunc(handlers.AdminStatsHandler))))
	http.Handle("/admin/transactions", handlers.AuthMiddleware(handlers.AdminOnly(http.HandlerFunc(handlers.AdminTransactionsHandler))))
	http.Handle("/admin/transactions/user/", handlers.AuthMiddleware(handlers.AdminOnly(http.HandlerFunc(handlers.AdminUserTransactionsHandler))))
	http.Handle("/admin/transactions/stats", handlers.AuthMiddleware(handlers.AdminOnly(http.HandlerFunc(handlers.TransactionStatsHandler))))

	// --------------------------
	// Serve static files
	// ให้บริการไฟล์ static (ภาพ)
	// --------------------------
	http.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir("uploads"))))

	// --------------------------
	// Configure CORS
	// ตั้งค่า CORS สำหรับการเรียกข้าม domain
	// --------------------------
	c := cors.New(cors.Options{
		AllowedOrigins: []string{
			"http://localhost:4200", // อนุญาตให้เรียกจาก Angular development server
		},
		AllowedMethods: []string{
			"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH", // อนุญาต methods ที่ใช้
		},
		AllowedHeaders: []string{
			"*", // อนุญาตทุก headers หรือระบุเฉพาะเช่น "Content-Type", "Authorization"
		},
		AllowCredentials: true, // อนุญาตการส่ง credentials (cookies, authentication)
		Debug:            true, // ตั้งเป็น false ใน production
	})

	// Wrap the default handler with CORS
	handler := c.Handler(http.DefaultServeMux)

	// --------------------------
	// Start Server
	// เริ่มต้นเซิร์ฟเวอร์
	// --------------------------
	ip := "192.168.56.1" // ใช้ IP แบบ fix
	fmt.Printf("🌐 Server IP: %s\n", ip)
	fmt.Printf("🚀 Server started at http://%s:8080\n", ip)
	fmt.Printf("🚀 Also available at http://localhost:8080\n")
	fmt.Println("✅ CORS enabled for: http://localhost:4200")
	fmt.Println("📚 Available endpoints:")
	fmt.Println("   PUBLIC:")
	fmt.Println("   GET  /                 - Home page")
	fmt.Println("   POST /register         - Register user")
	fmt.Println("   POST /login            - Login")
	fmt.Println("   GET  /games            - List all games")
	fmt.Println("   GET  /games/{id}       - Get game details")
	fmt.Println("   GET  /categories       - List categories")
	fmt.Println("   GET  /search           - Search games")
	fmt.Println("   GET  /ranking          - Game rankings")
	fmt.Println("   USER:")
	fmt.Println("   GET  /profile          - User profile")
	fmt.Println("   GET  /wallet           - Wallet balance")
	fmt.Println("   POST /deposit          - Deposit money")
	fmt.Println("   GET  /transactions     - Transaction history")
	fmt.Println("   GET  /library          - User game library")
	fmt.Println("   GET  /cart             - Get cart")
	fmt.Println("   POST /cart/add         - Add to cart")
	fmt.Println("   POST /cart/remove      - Remove from cart")
	fmt.Println("   POST /checkout         - Checkout cart")
	fmt.Println("   GET  /purchases        - Purchase history")
	fmt.Println("   ADMIN:")
	fmt.Println("   POST /admin/games      - Add new game")
	fmt.Println("   POST /admin/discounts  - Add discount code")
	fmt.Println("   GET  /admin/users      - List users")
	fmt.Println("   GET  /admin/stats      - Statistics")

	// ใช้ handler ที่มี CORS
	log.Fatal(http.ListenAndServe(":8080", handler))
}
