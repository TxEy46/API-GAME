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
func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			utils.JSONError(w, "Authorization header required", http.StatusUnauthorized)
			return
		}

		// Extract token from "Bearer <token>"
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			utils.JSONError(w, "Invalid authorization format", http.StatusUnauthorized)
			return
		}

		tokenString := parts[1]
		fmt.Printf("🔐 Token received: %s...\n", tokenString[:20])

		// Validate JWT token
		claims, err := auth.ValidateToken(tokenString)
		if err != nil {
			fmt.Printf("❌ Token validation failed: %v\n", err)
			utils.JSONError(w, "Invalid token: "+err.Error(), http.StatusUnauthorized)
			return
		}

		fmt.Printf("✅ Token valid: UserID=%d, Username=%s, Role=%s\n",
			claims.UserID, claims.Username, claims.Role)

		// Add user info to headers
		r.Header.Set("User-ID", strconv.Itoa(claims.UserID))
		r.Header.Set("Username", claims.Username)
		r.Header.Set("Role", claims.Role)

		next.ServeHTTP(w, r)
	})
}

// AdminOnly middleware restricts access to admin users
func AdminOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role := r.Header.Get("Role")
		if role != "admin" {
			utils.JSONError(w, "Admin access required", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
