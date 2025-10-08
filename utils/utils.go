package utils

import (
	"encoding/json"
	"net/http"
)

// JSONResponse sends a JSON response
func JSONResponse(w http.ResponseWriter, data interface{}, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}

// JSONError sends a JSON error response
func JSONError(w http.ResponseWriter, message string, statusCode int) {
	JSONResponse(w, map[string]string{"error": message}, statusCode)
}
