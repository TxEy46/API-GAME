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

// ProfileHandler handles user profile
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

// UpdateProfileHandler updates user profile (including avatar and password change)
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
