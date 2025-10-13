package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"go-api-game/utils"
	"net/http"
)

// ตัวแปร global สำหรับเก็บ connection ไปยังฐานข้อมูล
var db *sql.DB

// InitDB initializes the database connection
// ฟังก์ชันสำหรับกำหนดค่า connection ฐานข้อมูลให้กับ package handlers
func InitDB(database *sql.DB) {
	db = database
	fmt.Println("✅ Database connection initialized in handlers")
}

// RootHandler handles the root endpoint
// ฟังก์ชันสำหรับจัดการ endpoint หลัก (root) ของ API
func RootHandler(w http.ResponseWriter, r *http.Request) {
	// ส่ง response พื้นฐานกลับไป
	utils.JSONResponse(w, map[string]string{
		"message": "Game Store API",
		"version": "1.0",
	}, http.StatusOK)
}

// JSONResponse sends a JSON response
// ฟังก์ชันสำหรับส่ง response แบบ JSON (อาจซ้ำซ้อนกับ utils.JSONResponse)
func JSONResponse(w http.ResponseWriter, data interface{}, statusCode int) {
	// ตั้งค่า header ให้เป็น JSON
	w.Header().Set("Content-Type", "application/json")
	// ตั้งค่า status code
	w.WriteHeader(statusCode)
	// แปลง data เป็น JSON และส่ง response
	json.NewEncoder(w).Encode(data)
}

// JSONError sends a JSON error response
// ฟังก์ชันสำหรับส่ง error response แบบ JSON
func JSONError(w http.ResponseWriter, message string, statusCode int) {
	// ส่ง response error ในรูปแบบมาตรฐาน
	JSONResponse(w, map[string]interface{}{
		"error":   true,    // บ่งชี้ว่าเป็น error
		"message": message, // ข้อความ error
	}, statusCode)
}
