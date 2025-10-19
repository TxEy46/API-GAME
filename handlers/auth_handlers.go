package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"go-api-game/auth"
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

	"golang.org/x/crypto/bcrypt"
)

// saveAvatar handles avatar upload to Cloudinary with fallback to local storage
func saveAvatar(file io.Reader, header *multipart.FileHeader, userID int) (string, error) {
	// Read file bytes
	fileBytes, err := io.ReadAll(file)
	if err != nil {
		return "", fmt.Errorf("error reading avatar file: %v", err)
	}

	// Generate unique filename with user ID
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext == "" {
		ext = ".dat"
	}
	filename := fmt.Sprintf("avatar_%d_%d%s", userID, time.Now().UnixNano(), ext)

	// Try Cloudinary first
	if config.Cld != nil {
		imageURL, err := config.UploadImageFromBytes(fileBytes, filename)
		if err != nil {
			fmt.Printf("❌ Cloudinary upload failed, using local storage: %v\n", err)
			// Fallback to local storage
			return saveAvatarToLocalStorage(fileBytes, filename)
		}
		fmt.Printf("✅ Avatar uploaded to Cloudinary: %s\n", imageURL)
		return imageURL, nil
	}

	// Use local storage if Cloudinary not configured
	return saveAvatarToLocalStorage(fileBytes, filename)
}

// saveAvatarToLocalStorage saves avatar to local file system
func saveAvatarToLocalStorage(fileBytes []byte, filename string) (string, error) {
	// Create uploads directory if not exists
	if _, err := os.Stat("uploads"); os.IsNotExist(err) {
		os.Mkdir("uploads", 0755)
	}

	filePath := filepath.Join("uploads", filename)

	err := os.WriteFile(filePath, fileBytes, 0644)
	if err != nil {
		return "", fmt.Errorf("error saving avatar locally: %v", err)
	}

	localURL := "/uploads/" + filename
	fmt.Printf("✅ Avatar saved locally: %s\n", localURL)
	return localURL, nil
}

// deleteAvatar handles avatar deletion from both Cloudinary and local storage
func deleteAvatar(avatarURL string) error {
	if avatarURL == "" {
		return nil
	}

	// Check if it's a Cloudinary URL
	if strings.Contains(avatarURL, "cloudinary.com") {
		// Delete from Cloudinary
		err := config.DeleteImage(avatarURL)
		if err != nil {
			return fmt.Errorf("error deleting Cloudinary avatar: %v", err)
		}
		fmt.Printf("🗑️ Deleted Cloudinary avatar: %s\n", avatarURL)
	} else {
		// Delete from local storage
		filePath := strings.TrimPrefix(avatarURL, "/")
		if _, err := os.Stat(filePath); err == nil {
			err := os.Remove(filePath)
			if err != nil {
				return fmt.Errorf("error deleting local avatar: %v", err)
			}
			fmt.Printf("🗑️ Deleted local avatar: %s\n", filePath)
		}
	}
	return nil
}

// RegisterHandler handles user registration
// ฟังก์ชันสำหรับการลงทะเบียนผู้ใช้ใหม่
func RegisterHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("🔍 Register Handler - Method: %s, Content-Type: %s\n", r.Method, r.Header.Get("Content-Type"))

	// ตรวจสอบว่าเป็นเมธอด POST หรือไม่
	if r.Method != "POST" {
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// โครงสร้างสำหรับเก็บข้อมูลจาก request
	var req struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	var avatarURL string // ตัวแปรเก็บ URL ของภาพ avatar

	// ตรวจสอบประเภทของข้อมูลที่ส่งมา
	contentType := r.Header.Get("Content-Type")

	// กรณีส่งข้อมูลแบบ Form-data (มีการอัพโหลดไฟล์ avatar)
	if strings.Contains(contentType, "multipart/form-data") {
		fmt.Printf("📝 Processing as multipart/form-data\n")

		// แยกวิเคราะห์ form data ขนาดสูงสุด 10MB
		err := r.ParseMultipartForm(10 << 20) // 10 MB limit
		if err != nil {
			utils.JSONError(w, "Error parsing form data", http.StatusBadRequest)
			return
		}

		// ดึงค่าจากฟอร์ม
		req.Username = r.FormValue("username")
		req.Email = r.FormValue("email")
		req.Password = r.FormValue("password")

		// จัดการกับการอัพโหลดไฟล์ avatar
		file, header, err := r.FormFile("avatar")
		if err == nil {
			defer file.Close()

			// ใช้ฟังก์ชันใหม่สำหรับอัพโหลด avatar (userID จะถูกกำหนดภายหลัง)
			// ใช้ 0 เป็น temporary userID
			avatarURL, err = saveAvatar(file, header, 0)
			if err != nil {
				utils.JSONError(w, "Error uploading avatar: "+err.Error(), http.StatusInternalServerError)
				return
			}
		} else {
			// ไม่มีไฟล์ avatar ส่งมา → ใช้ default avatar
			avatarURL = "/uploads/default-avatar.png"
			fmt.Printf("📝 No avatar uploaded, using default: %s\n", avatarURL)
		}

		fmt.Printf("🔍 Form data - Username: %s, Email: %s, Password: %s, Avatar: %s\n",
			req.Username, req.Email, "***", avatarURL)

	} else if strings.Contains(contentType, "application/json") {
		// กรณีส่งข้อมูลแบบ JSON
		fmt.Printf("📝 Processing as JSON\n")

		// อ่าน body ของ request
		body, err := io.ReadAll(r.Body)
		if err != nil {
			fmt.Printf("❌ Error reading body: %v\n", err)
			utils.JSONError(w, "Error reading request body", http.StatusBadRequest)
			return
		}

		fmt.Printf("🔍 Raw request body: %s\n", string(body))
		// สร้าง new reader สำหรับ JSON decoder
		r.Body = io.NopCloser(bytes.NewBuffer(body))

		// แปลง JSON เป็น struct
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

	// ตรวจสอบความถูกต้องของข้อมูลที่จำเป็น
	if req.Username == "" || req.Email == "" || req.Password == "" {
		// ลบไฟล์ avatar ที่อัพโหลดไว้ถ้าข้อมูลไม่ครบ
		if avatarURL != "" && avatarURL != "/uploads/default-avatar.png" {
			deleteAvatar(avatarURL)
		}
		utils.JSONError(w, "Username, email and password are required", http.StatusBadRequest)
		return
	}

	// ตรวจสอบรูปแบบอีเมล
	if !isValidEmail(req.Email) {
		// ลบไฟล์ avatar ที่อัพโหลดไว้ถ้าอีเมลไม่ถูกต้อง
		if avatarURL != "" && avatarURL != "/uploads/default-avatar.png" {
			deleteAvatar(avatarURL)
		}
		utils.JSONError(w, "Invalid email format", http.StatusBadRequest)
		return
	}

	// ตรวจสอบความแข็งแรงของรหัสผ่าน
	if len(req.Password) < 6 {
		// ลบไฟล์ avatar ที่อัพโหลดไว้ถ้ารหัสผ่านสั้นเกินไป
		if avatarURL != "" && avatarURL != "/uploads/default-avatar.png" {
			deleteAvatar(avatarURL)
		}
		utils.JSONError(w, "Password must be at least 6 characters", http.StatusBadRequest)
		return
	}

	// ตรวจสอบว่าชื่อผู้ใช้หรืออีเมลมีอยู่แล้วหรือไม่
	var count int
	err := db.QueryRow(`
        SELECT COUNT(*) 
        FROM users 
        WHERE username = ? OR email = ?
    `, req.Username, req.Email).Scan(&count)

	if err != nil {
		// ลบไฟล์ avatar ที่อัพโหลดไว้ถ้ามีข้อผิดพลาด
		if avatarURL != "" && avatarURL != "/uploads/default-avatar.png" {
			deleteAvatar(avatarURL)
		}
		utils.JSONError(w, "Error checking user existence", http.StatusInternalServerError)
		return
	}

	if count > 0 {
		// ตรวจสอบว่าฟิลด์ใดซ้ำ
		var existingUsername, existingEmail string
		db.QueryRow(`
            SELECT username, email 
            FROM users 
            WHERE username = ? OR email = ?
            LIMIT 1
        `, req.Username, req.Email).Scan(&existingUsername, &existingEmail)

		// ลบไฟล์ avatar ที่อัพโหลดไว้ถ้าชื่อผู้ใช้หรืออีเมลซ้ำ
		if avatarURL != "" && avatarURL != "/uploads/default-avatar.png" {
			deleteAvatar(avatarURL)
		}

		if existingUsername == req.Username {
			utils.JSONError(w, "Username already exists", http.StatusBadRequest)
			return
		}
		if existingEmail == req.Email {
			utils.JSONError(w, "Email already exists", http.StatusBadRequest)
			return
		}
	}

	// Hash รหัสผ่าน
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		// ลบไฟล์ avatar ที่อัพโหลดไว้ถ้า hash รหัสผ่านล้มเหลว
		if avatarURL != "" && avatarURL != "/uploads/default-avatar.png" {
			deleteAvatar(avatarURL)
		}
		utils.JSONError(w, "Error processing password", http.StatusInternalServerError)
		return
	}

	// เพิ่มผู้ใช้ใหม่ลงฐานข้อมูล พร้อม avatar_url
	result, err := db.Exec(`
        INSERT INTO users (username, email, password_hash, role, avatar_url) 
        VALUES (?, ?, ?, 'user', ?)
    `, req.Username, req.Email, string(hashedPassword), avatarURL)

	if err != nil {
		// ลบไฟล์ที่อัพโหลดไว้ถ้าเพิ่มข้อมูลในฐานข้อมูลล้มเหลว (เฉพาะไฟล์ที่อัปโหลดใหม่)
		if avatarURL != "" && avatarURL != "/uploads/default-avatar.png" {
			deleteAvatar(avatarURL)
		}
		utils.JSONError(w, "Error creating user: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// ดึง ID ของผู้ใช้ที่เพิ่งเพิ่ม
	userID, _ := result.LastInsertId()

	// ถ้า avatar ถูกอัพโหลดและ userID ถูกกำหนดแล้ว ให้อัพเดทชื่อไฟล์
	if avatarURL != "" && avatarURL != "/uploads/default-avatar.png" && strings.Contains(avatarURL, "avatar_0_") {
		// สร้างชื่อไฟล์ใหม่ด้วย userID ที่ถูกต้อง
		newFilename := fmt.Sprintf("avatar_%d_%s", userID, strings.Split(filepath.Base(avatarURL), "_")[2])
		newAvatarURL := "/uploads/" + newFilename

		// ถ้าเป็นไฟล์ local ให้เปลี่ยนชื่อไฟล์
		if !strings.Contains(avatarURL, "cloudinary.com") {
			oldPath := strings.TrimPrefix(avatarURL, "/")
			newPath := filepath.Join("uploads", newFilename)
			if err := os.Rename(oldPath, newPath); err == nil {
				// อัพเดท avatar_url ในฐานข้อมูล
				db.Exec("UPDATE users SET avatar_url = ? WHERE id = ?", newAvatarURL, userID)
				avatarURL = newAvatarURL
				fmt.Printf("✅ Renamed avatar file to: %s\n", newAvatarURL)
			}
		}
	}

	// สร้างตะกร้าสินค้าสำหรับผู้ใช้
	_, err = db.Exec("INSERT INTO carts (user_id) VALUES (?)", userID)
	if err != nil {
		// ลบไฟล์ที่อัพโหลดไว้ถ้าสร้างตะกร้าล้มเหลว (เฉพาะไฟล์ที่อัปโหลดใหม่)
		if avatarURL != "" && avatarURL != "/uploads/default-avatar.png" {
			deleteAvatar(avatarURL)
		}
		utils.JSONError(w, "Error creating cart", http.StatusInternalServerError)
		return
	}

	fmt.Printf("✅ User registered successfully: ID=%d, Username=%s, Avatar: %s\n",
		userID, req.Username, avatarURL)

	// ส่ง response กลับไปพร้อม avatar_url
	response := map[string]interface{}{
		"message":    "User registered successfully",
		"user_id":    userID,
		"username":   req.Username,
		"email":      req.Email,
		"avatar_url": avatarURL, // ส่ง avatar_url ตลอด
	}

	utils.JSONResponse(w, response, http.StatusCreated)
}

// LoginHandler handles user login with identifier (username or email)
// ฟังก์ชันสำหรับการเข้าสู่ระบบด้วยชื่อผู้ใช้หรืออีเมล
func LoginHandler(w http.ResponseWriter, r *http.Request) {
	// ตรวจสอบว่าเป็นเมธอด POST หรือไม่
	if r.Method != "POST" {
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// โครงสร้างสำหรับเก็บข้อมูลการเข้าสู่ระบบ
	var req struct {
		Identifier string `json:"identifier"` // ชื่อผู้ใช้หรืออีเมล
		Password   string `json:"password"`   // รหัสผ่าน
	}

	// แปลง JSON request body เป็น struct
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.JSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	fmt.Printf("🔍 Login attempt: identifier='%s'\n", req.Identifier)

	// ตรวจสอบข้อมูลที่จำเป็น
	if req.Identifier == "" || req.Password == "" {
		utils.JSONError(w, "Identifier and password are required", http.StatusBadRequest)
		return
	}

	// ตัวแปรสำหรับเก็บข้อมูลผู้ใช้จากฐานข้อมูล
	var userID int
	var username, email, passwordHash, role, avatarURL string

	// ค้นหาผู้ใช้ด้วยชื่อผู้ใช้หรืออีเมล
	err := db.QueryRow(`
		SELECT id, username, email, password_hash, role, COALESCE(avatar_url, '') 
		FROM users 
		WHERE username = ? OR email = ?
	`, req.Identifier, req.Identifier).Scan(
		&userID, &username, &email, &passwordHash, &role, &avatarURL,
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

	// ตรวจสอบรหัสผ่าน
	err = bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password))
	if err != nil {
		fmt.Printf("❌ Password mismatch: %v\n", err)
		utils.JSONError(w, "Invalid identifier or password", http.StatusUnauthorized)
		return
	}

	fmt.Printf("✅ Password correct!\n")

	// สร้าง JWT token
	token, err := auth.GenerateToken(userID, username, email, role)
	if err != nil {
		utils.JSONError(w, "Error generating token", http.StatusInternalServerError)
		return
	}

	fmt.Printf("🎉 Login successful for user: %s, role: %s\n", username, role)

	// ส่ง response การเข้าสู่ระบบสำเร็จ
	utils.JSONResponse(w, map[string]interface{}{
		"message":    "Login successful",
		"user_id":    userID,
		"username":   username,
		"email":      email,
		"role":       role,
		"avatar_url": avatarURL,
		"token":      token,
	}, http.StatusOK)
}

// ProfileHandler handles user profile
// ฟังก์ชันสำหรับดึงข้อมูลโปรไฟล์ผู้ใช้
func ProfileHandler(w http.ResponseWriter, r *http.Request) {
	// ดึง User-ID จาก header (ถูกตั้งค่าโดย middleware การยืนยันตัวตน)
	userIDStr := r.Header.Get("User-ID")

	fmt.Printf("🔍 Profile request - User-ID header: '%s'\n", userIDStr)

	// ตรวจสอบว่ามี User-ID หรือไม่
	if userIDStr == "" {
		utils.JSONError(w, "User ID not found in headers", http.StatusUnauthorized)
		return
	}

	// แปลง User-ID เป็นตัวเลข
	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		fmt.Printf("❌ Invalid User-ID format: %s\n", userIDStr)
		utils.JSONError(w, "Invalid user ID format", http.StatusBadRequest)
		return
	}

	fmt.Printf("🔍 Querying database for user ID: %d\n", userID)

	// ตัวแปรสำหรับเก็บข้อมูลโปรไฟล์
	var id int
	var username, email string
	var avatarURL sql.NullString // ใช้ NullString เพราะ avatar_url อาจเป็น NULL
	var walletBalance float64

	// ดึงข้อมูลผู้ใช้จากฐานข้อมูล
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

	// สร้าง response object
	profile := map[string]interface{}{
		"id":             id,
		"username":       username,
		"email":          email,
		"wallet_balance": walletBalance,
		"avatar_url":     "", // ค่า default ถ้าไม่มี avatar
	}

	// ตั้งค่า avatar_url ถ้ามีค่า
	if avatarURL.Valid {
		profile["avatar_url"] = avatarURL.String
	}

	fmt.Printf("🎉 Sending profile response\n")
	utils.JSONResponse(w, profile, http.StatusOK)
}

// UpdateProfileHandler updates user profile (including avatar and password change)
// ฟังก์ชันสำหรับอัพเดทโปรไฟล์ผู้ใช้ (รวมถึงการเปลี่ยน avatar และรหัสผ่าน)
func UpdateProfileHandler(w http.ResponseWriter, r *http.Request) {
	// ตรวจสอบว่าเป็นเมธอด PUT หรือ PATCH
	if r.Method != "PUT" && r.Method != "PATCH" {
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// ดึง User-ID จาก header
	userID := r.Header.Get("User-ID")

	fmt.Printf("🔍 Update profile request for user ID: %s\n", userID)

	// ตรวจสอบว่ามี User-ID หรือไม่
	if userID == "" {
		utils.JSONError(w, "User ID not found", http.StatusUnauthorized)
		return
	}

	// แปลง User-ID เป็นตัวเลข
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
		CurrentPassword string `json:"current_password"` // รหัสผ่านปัจจุบัน (สำหรับการเปลี่ยนรหัสผ่าน)
		NewPassword     string `json:"new_password"`     // รหัสผ่านใหม่
		ConfirmPassword string `json:"confirm_password"` // ยืนยันรหัสผ่านใหม่
	}
	var avatarURL string

	// ดึง avatar URL เดิมก่อนทำการอัพเดท
	var oldAvatarURL sql.NullString
	db.QueryRow("SELECT avatar_url FROM users WHERE id = ?", userIDInt).Scan(&oldAvatarURL)

	// กรณีส่งข้อมูลแบบ Form-data (มีการอัพโหลดไฟล์ avatar)
	if strings.Contains(contentType, "multipart/form-data") {
		err = r.ParseMultipartForm(10 << 20) // 10 MB limit
		if err != nil {
			utils.JSONError(w, "Error parsing form data", http.StatusBadRequest)
			return
		}

		// ดึงค่าจากฟอร์ม
		req.Username = r.FormValue("username")
		req.Email = r.FormValue("email")
		req.CurrentPassword = r.FormValue("current_password")
		req.NewPassword = r.FormValue("new_password")
		req.ConfirmPassword = r.FormValue("confirm_password")

		// จัดการกับการอัพโหลดไฟล์ avatar
		file, header, err := r.FormFile("avatar")
		if err == nil {
			defer file.Close()

			// ใช้ฟังก์ชันใหม่สำหรับอัพโหลด avatar
			avatarURL, err = saveAvatar(file, header, userIDInt)
			if err != nil {
				utils.JSONError(w, "Error uploading avatar: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
	} else {
		// กรณีส่งข้อมูลแบบ JSON (ไม่มีไฟล์ avatar)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			utils.JSONError(w, "Invalid request body", http.StatusBadRequest)
			return
		}
	}

	// Validate input - ตรวจสอบว่ามี field ใดๆ ที่จะอัพเดตหรือไม่
	if req.Username == "" && req.Email == "" && avatarURL == "" && req.NewPassword == "" {
		// ลบไฟล์ avatar ใหม่ถ้าไม่มี field ใดๆ ที่จะอัพเดท
		if avatarURL != "" {
			deleteAvatar(avatarURL)
		}
		utils.JSONError(w, "No fields to update", http.StatusBadRequest)
		return
	}

	// ตรวจสอบรูปแบบอีเมลถ้ามีการส่งมา
	if req.Email != "" && !isValidEmail(req.Email) {
		// ลบไฟล์ avatar ใหม่ถ้าอีเมลไม่ถูกต้อง
		if avatarURL != "" {
			deleteAvatar(avatarURL)
		}
		utils.JSONError(w, "Invalid email format", http.StatusBadRequest)
		return
	}

	// ตรวจสอบการเปลี่ยนรหัสผ่านถ้ามีการส่งรหัสผ่านใหม่มา
	if req.NewPassword != "" {
		if req.CurrentPassword == "" {
			// ลบไฟล์ avatar ใหม่ถ้าข้อมูลรหัสผ่านไม่ครบ
			if avatarURL != "" {
				deleteAvatar(avatarURL)
			}
			utils.JSONError(w, "Current password is required to change password", http.StatusBadRequest)
			return
		}

		if req.ConfirmPassword == "" {
			// ลบไฟล์ avatar ใหม่ถ้าข้อมูลรหัสผ่านไม่ครบ
			if avatarURL != "" {
				deleteAvatar(avatarURL)
			}
			utils.JSONError(w, "Confirm password is required", http.StatusBadRequest)
			return
		}

		if req.NewPassword != req.ConfirmPassword {
			// ลบไฟล์ avatar ใหม่ถ้ารหัสผ่านไม่ตรงกัน
			if avatarURL != "" {
				deleteAvatar(avatarURL)
			}
			utils.JSONError(w, "New password and confirm password do not match", http.StatusBadRequest)
			return
		}

		if len(req.NewPassword) < 6 {
			// ลบไฟล์ avatar ใหม่ถ้ารหัสผ่านสั้นเกินไป
			if avatarURL != "" {
				deleteAvatar(avatarURL)
			}
			utils.JSONError(w, "New password must be at least 6 characters", http.StatusBadRequest)
			return
		}

		if req.CurrentPassword == req.NewPassword {
			// ลบไฟล์ avatar ใหม่ถ้ารหัสผ่านใหม่เหมือนรหัสผ่านเก่า
			if avatarURL != "" {
				deleteAvatar(avatarURL)
			}
			utils.JSONError(w, "New password must be different from current password", http.StatusBadRequest)
			return
		}
	}

	// ตรวจสอบว่าชื่อผู้ใช้หรืออีเมลใหม่มีอยู่แล้วหรือไม่ (ถ้ามีการส่งมา)
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
			// ลบไฟล์ avatar ใหม่ถ้าชื่อผู้ใช้หรืออีเมลซ้ำ
			if avatarURL != "" {
				deleteAvatar(avatarURL)
			}
			utils.JSONError(w, fmt.Sprintf("%s already exists", existingUser), http.StatusBadRequest)
			return
		} else if err != nil && err != sql.ErrNoRows {
			// ลบไฟล์ avatar ใหม่ถ้ามีข้อผิดพลาด
			if avatarURL != "" {
				deleteAvatar(avatarURL)
			}
			utils.JSONError(w, "Error checking user existence", http.StatusInternalServerError)
			return
		}
	}

	// ถ้ามีการเปลี่ยนรหัสผ่าน ต้องตรวจสอบรหัสผ่านปัจจุบัน
	var newPasswordHash string
	if req.NewPassword != "" {
		// ดึงรหัสผ่านปัจจุบันจากฐานข้อมูล
		var currentPasswordHash string
		err = db.QueryRow("SELECT password_hash FROM users WHERE id = ?", userIDInt).Scan(&currentPasswordHash)
		if err != nil {
			if err == sql.ErrNoRows {
				// ลบไฟล์ avatar ใหม่ถ้าผู้ใช้ไม่พบ
				if avatarURL != "" {
					deleteAvatar(avatarURL)
				}
				utils.JSONError(w, "User not found", http.StatusNotFound)
			} else {
				// ลบไฟล์ avatar ใหม่ถ้ามีข้อผิดพลาด
				if avatarURL != "" {
					deleteAvatar(avatarURL)
				}
				utils.JSONError(w, "Error fetching user data", http.StatusInternalServerError)
			}
			return
		}

		// ตรวจสอบรหัสผ่านปัจจุบัน
		err = bcrypt.CompareHashAndPassword([]byte(currentPasswordHash), []byte(req.CurrentPassword))
		if err != nil {
			fmt.Printf("❌ Current password mismatch for user ID: %d\n", userIDInt)
			// ลบไฟล์ avatar ใหม่ถ้ารหัสผ่านปัจจุบันไม่ถูกต้อง
			if avatarURL != "" {
				deleteAvatar(avatarURL)
			}
			utils.JSONError(w, "Current password is incorrect", http.StatusUnauthorized)
			return
		}

		// Hash รหัสผ่านใหม่
		hashedBytes, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
		if err != nil {
			// ลบไฟล์ avatar ใหม่ถ้า hash รหัสผ่านล้มเหลว
			if avatarURL != "" {
				deleteAvatar(avatarURL)
			}
			utils.JSONError(w, "Error processing new password", http.StatusInternalServerError)
			return
		}
		newPasswordHash = string(hashedBytes)
	}

	// สร้างคำสั่งอัพเดทแบบไดนามิกตามฟิลด์ที่มีการส่งมา
	updateFields := []string{} // เก็บชื่อฟิลด์ที่ต้องการอัพเดท
	args := []interface{}{}    // เก็บค่าที่จะใช้ในคำสั่ง SQL

	// ตรวจสอบแต่ละฟิลด์และเพิ่มลงใน query ถ้ามีค่า
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

	// ตรวจสอบว่ามีฟิลด์ที่จะอัพเดทหรือไม่
	if len(updateFields) == 0 {
		// ลบไฟล์ avatar ใหม่ถ้าไม่มี field ที่จะอัพเดท
		if avatarURL != "" {
			deleteAvatar(avatarURL)
		}
		utils.JSONError(w, "No fields to update", http.StatusBadRequest)
		return
	}

	// เพิ่ม user ID ไปยัง args สำหรับเงื่อนไข WHERE
	args = append(args, userIDInt)

	// สร้างและ execute คำสั่ง UPDATE
	query := fmt.Sprintf("UPDATE users SET %s WHERE id = ?", strings.Join(updateFields, ", "))
	result, err := db.Exec(query, args...)
	if err != nil {
		fmt.Printf("❌ Error updating profile: %v\n", err)
		// ลบไฟล์ที่อัพโหลดไว้ถ้าอัพเดทฐานข้อมูลล้มเหลว
		if avatarURL != "" {
			deleteAvatar(avatarURL)
		}
		utils.JSONError(w, "Error updating profile: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// ตรวจสอบว่ามีแถวถูกอัพเดทจริงหรือไม่
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		// ลบไฟล์ที่อัพโหลดไว้ถ้าไม่มีแถวถูกอัพเดท
		if avatarURL != "" {
			deleteAvatar(avatarURL)
		}
		utils.JSONError(w, "User not found or no changes made", http.StatusNotFound)
		return
	}

	// ลบไฟล์ avatar เก่าถ้ามีการอัพโหลด avatar ใหม่
	if avatarURL != "" && oldAvatarURL.Valid && oldAvatarURL.String != "" && oldAvatarURL.String != "/uploads/default-avatar.png" {
		err := deleteAvatar(oldAvatarURL.String)
		if err != nil {
			fmt.Printf("⚠️ Error deleting old avatar: %v\n", err)
		} else {
			fmt.Printf("🗑️ Deleted old avatar: %s\n", oldAvatarURL.String)
		}
	}

	fmt.Printf("✅ Profile updated successfully for user ID: %d\n", userIDInt)

	// ดึงข้อมูลผู้ใช้ที่อัพเดทแล้วเพื่อส่งกลับ
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

	// ตั้งค่า avatar URL
	if avatarDB.Valid {
		updatedUser.Avatar = avatarDB.String
	} else {
		updatedUser.Avatar = ""
	}

	// สร้าง response
	response := map[string]interface{}{
		"message": "Profile updated successfully",
		"user":    updatedUser,
	}

	// เพิ่มข้อความแจ้งการเปลี่ยนรหัสผ่านถ้ามีการเปลี่ยนรหัสผ่าน
	if newPasswordHash != "" {
		response["password_changed"] = true
	}

	utils.JSONResponse(w, response, http.StatusOK)
}

// isValidEmail checks if email format is valid
// ฟังก์ชันสำหรับตรวจสอบความถูกต้องของรูปแบบอีเมล
func isValidEmail(email string) bool {
	// การตรวจสอบอีเมลอย่างง่าย
	if len(email) < 3 || len(email) > 254 {
		return false
	}

	// ตรวจสอบว่ามี @ หรือไม่
	at := strings.Index(email, "@")
	if at == -1 || at == 0 || at == len(email)-1 {
		return false
	}

	// ตรวจสอบว่ามี . หลัง @ หรือไม่
	dot := strings.LastIndex(email[at:], ".")
	if dot == -1 || dot == 0 || dot == len(email[at:])-1 {
		return false
	}

	return true
}
