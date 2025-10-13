package utils

import (
	"encoding/json"
	"net/http"
)

// JSONResponse sends a JSON response
// ฟังก์ชันสำหรับส่ง response แบบ JSON
func JSONResponse(w http.ResponseWriter, data interface{}, statusCode int) {
	// ตั้งค่า Header ให้เป็น application/json
	w.Header().Set("Content-Type", "application/json")

	// ตั้งค่า HTTP Status Code
	w.WriteHeader(statusCode)

	// แปลง data เป็น JSON และส่ง response
	json.NewEncoder(w).Encode(data)
}

// JSONError sends a JSON error response
// ฟังก์ชันสำหรับส่ง error response แบบ JSON
func JSONError(w http.ResponseWriter, message string, statusCode int) {
	// เรียกใช้ JSONResponse ด้วยรูปแบบ error มาตรฐาน
	JSONResponse(w, map[string]string{"error": message}, statusCode)
}
