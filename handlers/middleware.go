// handlers/middleware.go
package handlers

import (
	"fmt"
	"go-api-game/auth"
	"go-api-game/utils"
	"net/http"
	"strconv"
	"strings"
)

// AuthMiddleware verifies user authentication using JWT
// Middleware สำหรับตรวจสอบการยืนยันตัวตนของผู้ใช้โดยใช้ JWT
func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// ดึง Authorization header จาก request
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			utils.JSONError(w, "Authorization header required", http.StatusUnauthorized)
			return
		}

		// แยก token จากรูปแบบ "Bearer <token>"
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			utils.JSONError(w, "Invalid authorization format", http.StatusUnauthorized)
			return
		}

		tokenString := parts[1]
		fmt.Printf("🔐 Token received: %s...\n", tokenString[:20])

		// ตรวจสอบความถูกต้องของ JWT token
		claims, err := auth.ValidateToken(tokenString)
		if err != nil {
			fmt.Printf("❌ Token validation failed: %v\n", err)
			utils.JSONError(w, "Invalid token: "+err.Error(), http.StatusUnauthorized)
			return
		}

		fmt.Printf("✅ Token valid: UserID=%d, Username=%s, Role=%s\n",
			claims.UserID, claims.Username, claims.Role)

		// เพิ่มข้อมูลผู้ใช้ลงใน headers เพื่อให้ handler ต่อไปใช้ได้
		r.Header.Set("User-ID", strconv.Itoa(claims.UserID))
		r.Header.Set("Username", claims.Username)
		r.Header.Set("Role", claims.Role)

		// เรียก handler ต่อไปใน chain
		next.ServeHTTP(w, r)
	})
}

// AdminOnly middleware restricts access to admin users
// Middleware สำหรับจำกัดการเข้าถึงเฉพาะผู้ใช้ที่เป็น admin
func AdminOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// ดึง Role จาก header (ถูกตั้งค่าโดย AuthMiddleware)
		role := r.Header.Get("Role")
		if role != "admin" {
			utils.JSONError(w, "Admin access required", http.StatusForbidden)
			return
		}

		// เรียก handler ต่อไปใน chain (เฉพาะ admin)
		next.ServeHTTP(w, r)
	})
}
