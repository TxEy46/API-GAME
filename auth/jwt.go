package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

// jwtSecret คือคีย์ลับสำหรับการเข้ารหัส JWT
// ควรเปลี่ยนค่าใน production environment
var jwtSecret = []byte("your-secret-key-change-in-production")

// Claims โครงสร้างสำหรับเก็บข้อมูลใน JWT token
type Claims struct {
	UserID               int    `json:"user_id"`  // ID ผู้ใช้
	Username             string `json:"username"` // ชื่อผู้ใช้
	Email                string `json:"email"`    // อีเมลผู้ใช้
	Role                 string `json:"role"`     // บทบาทผู้ใช้ (user, admin)
	jwt.RegisteredClaims        // ข้อมูลมาตรฐานของ JWT
}

// GenerateToken สร้าง JWT token
// ฟังก์ชันสำหรับสร้าง JWT token ใหม่สำหรับผู้ใช้
func GenerateToken(userID int, username, email, role string) (string, error) {
	// ตั้งค่าเวลาหมดอายุของ token (24 ชั่วโมง)
	expirationTime := time.Now().Add(24 * time.Hour)

	// สร้าง claims (ข้อมูลที่อยู่ใน token)
	claims := &Claims{
		UserID:   userID,
		Username: username,
		Email:    email,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expirationTime), // เวลาหมดอายุ
			IssuedAt:  jwt.NewNumericDate(time.Now()),     // เวลาที่สร้าง
			NotBefore: jwt.NewNumericDate(time.Now()),     // เวลาที่เริ่มใช้งานได้
			Issuer:    "game-store-api",                   // ผู้สร้าง token
		},
	}

	// สร้าง token ใหม่ด้วยการเซ็นด้วยวิธี HS256
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	// เซ็น token ด้วยคีย์ลับและได้ token string
	return token.SignedString(jwtSecret)
}

// ValidateToken ตรวจสอบและดึงข้อมูลจาก JWT token
// ฟังก์ชันสำหรับตรวจสอบความถูกต้องของ JWT token และดึงข้อมูลจาก claims
func ValidateToken(tokenString string) (*Claims, error) {
	// แยกวิเคราะห์ token และตรวจสอบความถูกต้อง
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		// ตรวจสอบวิธีการเซ็น token
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		// คืนค่าคีย์ลับสำหรับการตรวจสอบ
		return jwtSecret, nil
	})

	// ตรวจสอบข้อผิดพลาดในการแยกวิเคราะห์ token
	if err != nil {
		return nil, err
	}

	// ตรวจสอบว่า token ถูกต้องและได้ claims
	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	// คืนค่าข้อผิดพลาดถ้า token ไม่ถูกต้อง
	return nil, errors.New("invalid token")
}
