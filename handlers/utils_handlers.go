package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"go-api-game/utils"
	"net/http"
)

var db *sql.DB

// InitDB initializes the database connection
func InitDB(database *sql.DB) {
	db = database
	fmt.Println("✅ Database connection initialized in handlers")
}

// RootHandler handles the root endpoint
func RootHandler(w http.ResponseWriter, r *http.Request) {
	utils.JSONResponse(w, map[string]string{
		"message": "Game Store API",
		"version": "1.0",
	}, http.StatusOK)
}

// JSONResponse sends a JSON response
func JSONResponse(w http.ResponseWriter, data interface{}, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}

// JSONError sends a JSON error response
func JSONError(w http.ResponseWriter, message string, statusCode int) {
	JSONResponse(w, map[string]interface{}{
		"error":   true,
		"message": message,
	}, statusCode)
}
