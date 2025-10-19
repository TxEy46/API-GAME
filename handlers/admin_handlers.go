package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"go-api-game/config"
	"go-api-game/utils"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// saveImage handles image upload to Cloudinary with fallback to local storage
func saveImage(file io.Reader, header *multipart.FileHeader) (string, error) {
	// Read file bytes
	fileBytes, err := io.ReadAll(file)
	if err != nil {
		return "", fmt.Errorf("error reading image file: %v", err)
	}

	// Check file type
	allowedTypes := map[string]bool{
		".jpg": true, ".jpeg": true, ".png": true, ".gif": true,
		".webp": true, ".avif": true,
	}
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if !allowedTypes[ext] {
		return "", fmt.Errorf("invalid file type. Allowed: jpg, jpeg, png, gif, webp, avif")
	}

	// Generate unique filename
	filename := fmt.Sprintf("game_%d%s", time.Now().UnixNano(), ext)

	// Try Cloudinary first
	if config.Cld != nil {
		imageURL, err := config.UploadImageFromBytes(fileBytes, filename)
		if err != nil {
			fmt.Printf("❌ Cloudinary upload failed, using local storage: %v\n", err)
			// Fallback to local storage
			return saveToLocalStorage(fileBytes, filename)
		}
		fmt.Printf("✅ Image uploaded to Cloudinary: %s\n", imageURL)
		return imageURL, nil
	}

	// Use local storage if Cloudinary not configured
	return saveToLocalStorage(fileBytes, filename)
}

// saveToLocalStorage saves image to local file system
func saveToLocalStorage(fileBytes []byte, filename string) (string, error) {
	// Create uploads directory if not exists
	if _, err := os.Stat("uploads"); os.IsNotExist(err) {
		os.Mkdir("uploads", 0755)
	}

	filePath := filepath.Join("uploads", filename)

	err := os.WriteFile(filePath, fileBytes, 0644)
	if err != nil {
		return "", fmt.Errorf("error saving image locally: %v", err)
	}

	localURL := "/uploads/" + filename
	fmt.Printf("✅ Image saved locally: %s\n", localURL)
	return localURL, nil
}

// deleteImage handles image deletion from both Cloudinary and local storage
func deleteImage(imageURL string) error {
	if imageURL == "" {
		return nil
	}

	// Check if it's a Cloudinary URL
	if strings.Contains(imageURL, "cloudinary.com") {
		// Delete from Cloudinary
		err := config.DeleteImage(imageURL)
		if err != nil {
			return fmt.Errorf("error deleting Cloudinary image: %v", err)
		}
		fmt.Printf("🗑️ Deleted Cloudinary image: %s\n", imageURL)
	} else {
		// Delete from local storage
		filePath := strings.TrimPrefix(imageURL, "/")
		if _, err := os.Stat(filePath); err == nil {
			err := os.Remove(filePath)
			if err != nil {
				return fmt.Errorf("error deleting local image: %v", err)
			}
			fmt.Printf("🗑️ Deleted local image: %s\n", filePath)
		}
	}
	return nil
}

// AdminAddGameHandler handles adding new games
// ฟังก์ชันสำหรับผู้ดูแลระบบเพิ่มเกมใหม่เข้าสู่ระบบ
func AdminAddGameHandler(w http.ResponseWriter, r *http.Request) {
	// ตรวจสอบว่าเป็นเมธอด POST หรือไม่
	if r.Method != "POST" {
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// ตรวจสอบประเภทของข้อมูลที่ส่งมา (JSON หรือ Form-data)
	contentType := r.Header.Get("Content-Type")

	// โครงสร้างสำหรับเก็บข้อมูลจาก request
	var req struct {
		Name        string  `json:"name"`         // ชื่อเกม (จำเป็น)
		Price       float64 `json:"price"`        // ราคาเกม (จำเป็น)
		CategoryID  int     `json:"category_id"`  // ID หมวดหมู่ (จำเป็น)
		Description string  `json:"description"`  // คำอธิบายเกม
		ReleaseDate string  `json:"release_date"` // วันที่วางจำหน่าย (ถ้าไม่ส่งจะใช้วันที่ปัจจุบัน)
	}

	var imageURL string // ตัวแปรเก็บ URL ของภาพเกม

	// กรณีส่งข้อมูลแบบ Form-data (มีการอัพโหลดไฟล์ภาพ)
	if strings.Contains(contentType, "multipart/form-data") {
		// แยกวิเคราะห์ form data ขนาดสูงสุด 10MB
		err := r.ParseMultipartForm(10 << 20) // 10 MB limit
		if err != nil {
			utils.JSONError(w, "Error parsing form data", http.StatusBadRequest)
			return
		}

		// ดึงค่าจากฟอร์ม
		req.Name = r.FormValue("name")
		priceStr := r.FormValue("price")
		categoryIDStr := r.FormValue("category_id")
		req.Description = r.FormValue("description")
		req.ReleaseDate = r.FormValue("release_date") // Optional

		// แปลงสตริงเป็นตัวเลข
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

		// จัดการกับการอัพโหลดไฟล์ภาพ
		file, header, err := r.FormFile("image")
		if err == nil {
			defer file.Close()

			// ใช้ฟังก์ชันใหม่สำหรับอัพโหลดภาพ
			imageURL, err = saveImage(file, header)
			if err != nil {
				utils.JSONError(w, "Error uploading image: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
	} else {
		// กรณีส่งข้อมูลแบบ JSON (ไม่มีไฟล์ภาพ)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			utils.JSONError(w, "Invalid request body", http.StatusBadRequest)
			return
		}
	}

	// ตรวจสอบความถูกต้องของข้อมูลที่จำเป็น
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

	// จัดการวันที่วางจำหน่าย
	var releaseDate interface{}
	if req.ReleaseDate != "" {
		// ถ้ารับ release_date มา ให้แปลงเป็นรูปแบบวันที่และใช้ค่าที่ส่งมา
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

	// เพิ่มเกมลงฐานข้อมูล
	var result sql.Result
	var err error

	// สร้างคำสั่ง SQL สำหรับเพิ่มเกม โดยตรวจสอบว่ามี release_date หรือไม่
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
		// ลบไฟล์ที่อัพโหลดไว้ถ้าเพิ่มข้อมูลในฐานข้อมูลล้มเหลว
		if imageURL != "" {
			deleteImage(imageURL)
		}
		utils.JSONError(w, "Error adding game: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// ดึง ID ของเกมที่เพิ่งเพิ่ม
	gameID, _ := result.LastInsertId()

	// เริ่มต้นระบบจัดอันดับด้วยยอดขาย 0
	_, err = db.Exec("INSERT INTO ranking (game_id, sales_count) VALUES (?, 0)", gameID)
	if err != nil {
		fmt.Printf("⚠️ Error initializing ranking: %v\n", err)
		// ดำเนินการต่อแม้ว่าการเริ่มต้นระบบจัดอันดับจะล้มเหลว
	}

	fmt.Printf("✅ Game added successfully: ID=%d, Name=%s\n", gameID, req.Name)

	// ส่ง response กลับไปยัง client
	utils.JSONResponse(w, map[string]interface{}{
		"message": "Game added successfully",
		"game_id": gameID,
		"release_date": func() string {
			// แปลง releaseDate ให้เป็นสตริงรูปแบบ YYYY-MM-DD
			if date, ok := releaseDate.(time.Time); ok {
				return date.Format("2006-01-02")
			}
			return releaseDate.(string)
		}(),
	}, http.StatusCreated)
}

// AdminUpdateGameHandler handles updating games
// ฟังก์ชันสำหรับผู้ดูแลระบบอัพเดทข้อมูลเกมที่มีอยู่
func AdminUpdateGameHandler(w http.ResponseWriter, r *http.Request) {
	// ตรวจสอบว่าเป็นเมธอด PUT หรือ PATCH
	if r.Method != "PUT" && r.Method != "PATCH" {
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// ดึง game_id จาก URL path
	// ตัวอย่าง URL: /admin/games/123 → gameID = 123
	pathParts := strings.Split(r.URL.Path, "/")
	gameIDStr := pathParts[len(pathParts)-1]
	gameID, err := strconv.Atoi(gameIDStr)
	if err != nil {
		utils.JSONError(w, "Invalid game ID", http.StatusBadRequest)
		return
	}

	fmt.Printf("🔍 Admin updating game ID: %d\n", gameID)

	// ตรวจสอบประเภทของข้อมูลที่ส่งมา
	contentType := r.Header.Get("Content-Type")
	var req struct {
		Name        string  `json:"name"`
		Price       float64 `json:"price"`
		CategoryID  int     `json:"category_id"`
		Description string  `json:"description"`
		ReleaseDate string  `json:"release_date"`
	}

	var imageURL string

	// กรณีส่งข้อมูลแบบ Form-data
	if strings.Contains(contentType, "multipart/form-data") {
		err = r.ParseMultipartForm(10 << 20)
		if err != nil {
			utils.JSONError(w, "Error parsing form data", http.StatusBadRequest)
			return
		}

		// ดึงค่าจากฟอร์ม
		req.Name = r.FormValue("name")
		priceStr := r.FormValue("price")
		categoryIDStr := r.FormValue("category_id")
		req.Description = r.FormValue("description")
		req.ReleaseDate = r.FormValue("release_date")

		// แปลงสตริงเป็นตัวเลข
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

		// จัดการกับการอัพโหลดไฟล์ภาพใหม่
		file, header, err := r.FormFile("image")
		if err == nil {
			defer file.Close()

			// ใช้ฟังก์ชันใหม่สำหรับอัพโหลดภาพ
			imageURL, err = saveImage(file, header)
			if err != nil {
				utils.JSONError(w, "Error uploading image: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
	} else {
		// กรณีส่งข้อมูลแบบ JSON
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			utils.JSONError(w, "Invalid request body", http.StatusBadRequest)
			return
		}
	}

	// ดึง URL ภาพเก่าเพื่อลบในภายหลัง (ถ้ามีการอัพโหลดภาพใหม่)
	var oldImageURL sql.NullString
	if imageURL != "" {
		db.QueryRow("SELECT image_url FROM games WHERE id = ?", gameID).Scan(&oldImageURL)
	}

	// สร้างคำสั่งอัพเดทแบบไดนามิกตามฟิลด์ที่มีการส่งมา
	updateFields := []string{} // เก็บชื่อฟิลด์ที่ต้องการอัพเดท
	args := []interface{}{}    // เก็บค่าที่จะใช้ในคำสั่ง SQL

	// ตรวจสอบแต่ละฟิลด์และเพิ่มลงใน query ถ้ามีค่า
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
		updateFields = append(updateFields, "image_url = ?")
		args = append(args, imageURL)
	}

	// ตรวจสอบว่ามีฟิลด์ที่จะอัพเดทหรือไม่
	if len(updateFields) == 0 {
		utils.JSONError(w, "No fields to update", http.StatusBadRequest)
		return
	}

	// เพิ่ม game ID ไปยัง args สำหรับเงื่อนไข WHERE
	args = append(args, gameID)

	// สร้างและ execute คำสั่ง UPDATE
	query := fmt.Sprintf("UPDATE games SET %s WHERE id = ?", strings.Join(updateFields, ", "))
	result, err := db.Exec(query, args...)
	if err != nil {
		fmt.Printf("❌ Error updating game: %v\n", err)
		// ลบไฟล์ภาพใหม่ถ้าอัพเดทฐานข้อมูลล้มเหลว
		if imageURL != "" {
			deleteImage(imageURL)
		}
		utils.JSONError(w, "Error updating game: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// ตรวจสอบว่ามีแถวถูกอัพเดทจริงหรือไม่
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		if imageURL != "" {
			deleteImage(imageURL)
		}
		utils.JSONError(w, "Game not found", http.StatusNotFound)
		return
	}

	// ลบไฟล์ภาพเก่าถ้ามีการอัพโหลดภาพใหม่
	if imageURL != "" && oldImageURL.Valid && oldImageURL.String != "" {
		err := deleteImage(oldImageURL.String)
		if err != nil {
			fmt.Printf("⚠️ Error deleting old image: %v\n", err)
		} else {
			fmt.Printf("🗑️ Deleted old image: %s\n", oldImageURL.String)
		}
	}

	fmt.Printf("✅ Game updated successfully: ID=%d\n", gameID)

	// ส่ง response สำเร็จกลับไป
	utils.JSONResponse(w, map[string]interface{}{
		"message": "Game updated successfully",
		"game_id": gameID,
	}, http.StatusOK)
}

// AdminDeleteGameHandler handles deleting games
// ฟังก์ชันสำหรับผู้ดูแลระบบลบเกมออกจากระบบ
func AdminDeleteGameHandler(w http.ResponseWriter, r *http.Request) {
	// ตรวจสอบว่าเป็นเมธอด DELETE หรือไม่
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

	// ดึง URL ภาพก่อนลบ (เพื่อลบไฟล์ภาพออกจากระบบไฟล์)
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

	// เริ่มต้น transaction เพื่อความปลอดภัยของข้อมูล
	tx, err := db.Begin()
	if err != nil {
		utils.JSONError(w, "Error starting transaction", http.StatusInternalServerError)
		return
	}

	// ลบข้อมูลที่เกี่ยวข้องตามลำดับเพื่อป้องกัน foreign key constraint violations

	// 1. ลบจากตาราง ranking (ข้อมูลการจัดอันดับ)
	_, err = tx.Exec("DELETE FROM ranking WHERE game_id = ?", gameID)
	if err != nil {
		tx.Rollback() // ยกเลิก transaction ถ้าล้มเหลว
		utils.JSONError(w, "Error deleting game ranking", http.StatusInternalServerError)
		return
	}

	// 2. ลบจากตาราง cart_items (เกมในตะกร้าสินค้าของผู้ใช้)
	_, err = tx.Exec("DELETE FROM cart_items WHERE game_id = ?", gameID)
	if err != nil {
		tx.Rollback()
		utils.JSONError(w, "Error deleting game from carts", http.StatusInternalServerError)
		return
	}

	// 3. ลบจากตาราง purchase_items (รายการเกมในการซื้อ)
	_, err = tx.Exec("DELETE pi FROM purchase_items pi WHERE pi.game_id = ?", gameID)
	if err != nil {
		tx.Rollback()
		utils.JSONError(w, "Error deleting game purchase records", http.StatusInternalServerError)
		return
	}

	// 4. ลบจากตาราง purchased_games (เกมในคลังเกมของผู้ใช้)
	_, err = tx.Exec("DELETE FROM purchased_games WHERE game_id = ?", gameID)
	if err != nil {
		tx.Rollback()
		utils.JSONError(w, "Error deleting game from user libraries", http.StatusInternalServerError)
		return
	}

	// 5. ลบเกมจากตาราง games (ลบข้อมูลหลัก)
	result, err := tx.Exec("DELETE FROM games WHERE id = ?", gameID)
	if err != nil {
		tx.Rollback()
		utils.JSONError(w, "Error deleting game", http.StatusInternalServerError)
		return
	}

	// ตรวจสอบว่ามีเกมถูกลบจริงหรือไม่
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		tx.Rollback()
		utils.JSONError(w, "Game not found", http.StatusNotFound)
		return
	}

	// ยืนยัน transaction
	if err := tx.Commit(); err != nil {
		utils.JSONError(w, "Error committing transaction", http.StatusInternalServerError)
		return
	}

	// ลบไฟล์ภาพถ้ามี
	if imageURL.Valid && imageURL.String != "" {
		err := deleteImage(imageURL.String)
		if err != nil {
			fmt.Printf("⚠️ Error deleting game image: %v\n", err)
		} else {
			fmt.Printf("🗑️ Deleted game image: %s\n", imageURL.String)
		}
	}

	fmt.Printf("✅ Game deleted successfully: ID=%d\n", gameID)

	// ส่ง response สำเร็จกลับไป
	utils.JSONResponse(w, map[string]interface{}{
		"message": "Game deleted successfully",
		"game_id": gameID,
	}, http.StatusOK)
}

// AdminUsersHandler handles admin user management
// ฟังก์ชันสำหรับผู้ดูแลระบบดึงรายการผู้ใช้ทั้งหมด (ไม่รวม admin)
func AdminUsersHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	fmt.Printf("🔍 Admin fetching all users (excluding admins)\n")

	// ดึงข้อมูลผู้ใช้ทั้งหมดที่ไม่ใช่ admin เรียงตามวันที่สร้างล่าสุด
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

	// อ่านข้อมูลผู้ใช้ทีละแถว
	for rows.Next() {
		var id int
		var username, email, role string
		var createdDate string
		var walletBalance float64

		if err := rows.Scan(&id, &username, &email, &role, &createdDate, &walletBalance); err != nil {
			fmt.Printf("❌ Error scanning user row: %v\n", err)
			continue
		}

		// สร้าง object ผู้ใช้
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

	// ตรวจสอบข้อผิดพลาดระหว่างการอ่านข้อมูล
	if err = rows.Err(); err != nil {
		fmt.Printf("❌ Error during users rows iteration: %v\n", err)
		utils.JSONError(w, "Error processing users", http.StatusInternalServerError)
		return
	}

	fmt.Printf("✅ Total users found (excluding admins): %d\n", count)

	// ตรวจสอบว่า users ไม่เป็น nil
	if users == nil {
		users = []map[string]interface{}{}
	}

	// ส่ง response กลับไป
	utils.JSONResponse(w, users, http.StatusOK)
}

// AdminStatsHandler handles admin statistics
// ฟังก์ชันสำหรับผู้ดูแลระบบดึงสถิติรวมของระบบ
func AdminStatsHandler(w http.ResponseWriter, r *http.Request) {
	// โครงสร้างสำหรับเก็บสถิติ
	var stats struct {
		TotalUsers     int     `json:"total_users"`     // จำนวนผู้ใช้ทั้งหมด
		TotalGames     int     `json:"total_games"`     // จำนวนเกมทั้งหมด
		TotalSales     float64 `json:"total_sales"`     // ยอดขายรวมทั้งหมด
		TotalPurchases int     `json:"total_purchases"` // จำนวนการซื้อทั้งหมด
	}

	// ดึงจำนวนผู้ใช้ทั้งหมด
	db.QueryRow("SELECT COUNT(*) FROM users").Scan(&stats.TotalUsers)

	// ดึงจำนวนเกมทั้งหมด
	db.QueryRow("SELECT COUNT(*) FROM games").Scan(&stats.TotalGames)

	// ดึงยอดขายรวมทั้งหมด (ใช้ COALESCE เพื่อป้องกัน NULL)
	db.QueryRow("SELECT COALESCE(SUM(final_amount), 0) FROM purchases").Scan(&stats.TotalSales)

	// ดึงจำนวนการซื้อทั้งหมด
	db.QueryRow("SELECT COUNT(*) FROM purchases").Scan(&stats.TotalPurchases)

	// ส่งสถิติกลับไป
	utils.JSONResponse(w, stats, http.StatusOK)
}

// AdminTransactionsHandler handles admin transaction management
// ฟังก์ชันหลักสำหรับจัดการธุรกรรมโดยผู้ดูแลระบบ
func AdminTransactionsHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("💰 AdminTransactionsHandler: %s %s\n", r.Method, r.URL.Path)

	// ตรวจสอบเมธอดและเรียกฟังก์ชันที่เหมาะสม
	switch r.Method {
	case "GET":
		getAllTransactions(w, r) // ดึงธุรกรรมทั้งหมด
	default:
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// AdminUserTransactionsHandler handles user-specific transaction management for admin
// ฟังก์ชันสำหรับจัดการธุรกรรมเฉพาะผู้ใช้โดยผู้ดูแลระบบ
func AdminUserTransactionsHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("💰 AdminUserTransactionsHandler: %s %s\n", r.Method, r.URL.Path)

	// แยก user ID จาก URL path
	// ตัวอย่าง URL: /admin/transactions/user/123 → userID = 123
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

	// ตรวจสอบเมธอดและเรียกฟังก์ชันที่เหมาะสม
	switch r.Method {
	case "GET":
		getUserTransactions(w, r, userID) // ดึงธุรกรรมของผู้ใช้เฉพาะคน
	default:
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// GET /admin/transactions - ดึงประวัติธุรกรรมทั้งหมด
// ฟังก์ชันสำหรับดึงประวัติธุรกรรมทั้งหมดในระบบ (มี pagination และ filtering)
func getAllTransactions(w http.ResponseWriter, r *http.Request) {
	fmt.Println("🔍 Fetching all transactions")

	// รับ query parameters สำหรับ filtering และ pagination
	query := r.URL.Query()
	transactionType := query.Get("type") // ประเภทธุรกรรม (ฝากเงิน, ถอนเงิน, ซื้อเกม)
	limitStr := query.Get("limit")       // จำนวนรายการต่อหน้า
	offsetStr := query.Get("offset")     // ตำแหน่งเริ่มต้น

	// ตั้งค่า default values
	limit := 100
	offset := 0

	// แปลงค่า limit และ offset เป็นตัวเลข
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

	// สร้างคำสั่ง SQL พื้นฐาน
	baseQuery := `
		SELECT 
			t.id, t.user_id, u.username, t.type, t.amount, 
			t.description, DATE_FORMAT(t.created_at, '%Y-%m-%d %H:%i:%s') as created_at
		FROM user_transactions t
		LEFT JOIN users u ON t.user_id = u.id
	`
	var args []interface{}
	whereClauses := []string{}

	// เพิ่มเงื่อนไข WHERE ถ้ามีการกรองประเภทธุรกรรม
	if transactionType != "" {
		whereClauses = append(whereClauses, "t.type = ?")
		args = append(args, transactionType)
	}

	// รวมเงื่อนไข WHERE ถ้ามี
	if len(whereClauses) > 0 {
		baseQuery += " WHERE " + strings.Join(whereClauses, " AND ")
	}

	// เพิ่มการเรียงลำดับและ pagination
	baseQuery += " ORDER BY t.created_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	// Execute query
	rows, err := db.Query(baseQuery, args...)
	if err != nil {
		fmt.Printf("❌ Error fetching transactions: %v\n", err)
		utils.JSONError(w, "Error fetching transactions", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var transactions []map[string]interface{}
	count := 0

	// อ่านข้อมูลธุรกรรมทีละแถว
	for rows.Next() {
		var id, userID int
		var username, transactionType, description, createdAt string
		var amount float64

		err := rows.Scan(&id, &userID, &username, &transactionType, &amount, &description, &createdAt)
		if err != nil {
			fmt.Printf("❌ Error scanning transaction row: %v\n", err)
			continue
		}

		// สร้าง object ธุรกรรม
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

	// ตรวจสอบข้อผิดพลาดระหว่างการอ่านข้อมูล
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

	// ส่ง response กลับไปพร้อมข้อมูลธุรกรรมและข้อมูล pagination
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
// ฟังก์ชันสำหรับดึงประวัติธุรกรรมของผู้ใช้เฉพาะคน (มี pagination และ filtering)
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

	// สร้างคำสั่ง SQL
	baseQuery := `
		SELECT 
			t.id, t.type, t.amount, t.description, 
			DATE_FORMAT(t.created_at, '%Y-%m-%d %H:%i:%s') as created_at
		FROM user_transactions t
		WHERE t.user_id = ?
	`
	var args []interface{}
	args = append(args, userID)

	// เพิ่มเงื่อนไขประเภทธุรกรรมถ้ามี
	if transactionType != "" {
		baseQuery += " AND t.type = ?"
		args = append(args, transactionType)
	}

	// เพิ่มการเรียงลำดับและ pagination
	baseQuery += " ORDER BY t.created_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	// Execute query
	rows, err := db.Query(baseQuery, args...)
	if err != nil {
		fmt.Printf("❌ Error fetching user transactions: %v\n", err)
		utils.JSONError(w, "Error fetching user transactions", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var transactions []map[string]interface{}
	count := 0

	// อ่านข้อมูลธุรกรรมทีละแถว
	for rows.Next() {
		var id int
		var transactionType, description, createdAt string
		var amount float64

		err := rows.Scan(&id, &transactionType, &amount, &description, &createdAt)
		if err != nil {
			fmt.Printf("❌ Error scanning user transaction row: %v\n", err)
			continue
		}

		// สร้าง object ธุรกรรม
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

	// ตรวจสอบข้อผิดพลาดระหว่างการอ่านข้อมูล
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

	// ส่ง response กลับไปพร้อมข้อมูลธุรกรรมและข้อมูลผู้ใช้
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
