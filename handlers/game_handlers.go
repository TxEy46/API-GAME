package handlers

import (
	"database/sql"
	"fmt"
	"go-api-game/utils"
	"net/http"
	"strconv"
	"strings"
)

// GamesHandler returns all games
func GamesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	fmt.Printf("🔍 Fetching all games\n")

	// ใช้ DATE_FORMAT เพื่อแปลง DATE เป็น string โดยตรง
	rows, err := db.Query(`
		SELECT g.id, g.name, g.price, c.name as category, g.image_url, 
		       g.description, 
		       DATE_FORMAT(g.release_date, '%Y-%m-%d') as release_date,
		       r.rank_position
		FROM games g
		LEFT JOIN categories c ON g.category_id = c.id
		LEFT JOIN ranking r ON g.id = r.game_id
		ORDER BY g.id
	`)
	if err != nil {
		fmt.Printf("❌ Error fetching games: %v\n", err)
		utils.JSONError(w, "Error fetching games: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var games []map[string]interface{}
	count := 0

	for rows.Next() {
		var id int
		var name string
		var price float64
		var category string
		var imageURL, description sql.NullString
		var releaseDate sql.NullString // เปลี่ยนเป็น string
		var rank sql.NullInt64

		err := rows.Scan(&id, &name, &price, &category, &imageURL, &description, &releaseDate, &rank)
		if err != nil {
			fmt.Printf("❌ Error scanning game row: %v\n", err)
			continue
		}

		game := map[string]interface{}{
			"id":          id,
			"name":        name,
			"price":       price,
			"category":    category,
			"image_url":   imageURL.String,
			"description": description.String,
			"rank":        rank.Int64,
		}

		// Handle release date
		if releaseDate.Valid && releaseDate.String != "" {
			game["release_date"] = releaseDate.String
		} else {
			game["release_date"] = nil
		}

		games = append(games, game)
		count++

		fmt.Printf("✅ Game found: ID=%d, Name=%s, Price=%.2f\n", id, name, price)
	}

	if err = rows.Err(); err != nil {
		fmt.Printf("❌ Error during rows iteration: %v\n", err)
		utils.JSONError(w, "Error processing games", http.StatusInternalServerError)
		return
	}

	fmt.Printf("✅ Total games found: %d\n", count)

	if games == nil {
		games = []map[string]interface{}{}
	}

	utils.JSONResponse(w, games, http.StatusOK)
}

// GameByIDHandler returns a specific game by ID
func GameByIDHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	pathParts := strings.Split(r.URL.Path, "/")
	idStr := pathParts[len(pathParts)-1]
	gameID, err := strconv.Atoi(idStr)
	if err != nil {
		utils.JSONError(w, "Invalid game ID", http.StatusBadRequest)
		return
	}

	fmt.Printf("🔍 Fetching game by ID: %d\n", gameID)

	// ใช้ DATE_FORMAT เพื่อแปลง DATE เป็น string โดยตรง
	var game struct {
		ID          int
		Name        string
		Price       float64
		Category    string
		ImageURL    sql.NullString
		Description sql.NullString
		ReleaseDate sql.NullString
		Rank        sql.NullInt64
	}

	err = db.QueryRow(`
		SELECT g.id, g.name, g.price, c.name as category, g.image_url, 
		       g.description, 
		       DATE_FORMAT(g.release_date, '%Y-%m-%d') as release_date,
		       r.rank_position
		FROM games g
		LEFT JOIN categories c ON g.category_id = c.id
		LEFT JOIN ranking r ON g.id = r.game_id
		WHERE g.id = ?
	`, gameID).Scan(&game.ID, &game.Name, &game.Price, &game.Category,
		&game.ImageURL, &game.Description, &game.ReleaseDate, &game.Rank)

	if err != nil {
		fmt.Printf("❌ Error fetching game ID %d: %v\n", gameID, err)
		if err == sql.ErrNoRows {
			utils.JSONError(w, "Game not found", http.StatusNotFound)
		} else {
			utils.JSONError(w, "Error fetching game: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}

	fmt.Printf("✅ Game found: ID=%d, Name=%s\n", game.ID, game.Name)

	gameMap := map[string]interface{}{
		"id":          game.ID,
		"name":        game.Name,
		"price":       game.Price,
		"category":    game.Category,
		"image_url":   game.ImageURL.String,
		"description": game.Description.String,
		"rank":        game.Rank.Int64,
	}

	if game.ReleaseDate.Valid && game.ReleaseDate.String != "" {
		gameMap["release_date"] = game.ReleaseDate.String
	} else {
		gameMap["release_date"] = nil
	}

	utils.JSONResponse(w, gameMap, http.StatusOK)
}

// CategoriesHandler returns all categories
func CategoriesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rows, err := db.Query("SELECT id, name FROM categories")
	if err != nil {
		utils.JSONError(w, "Error fetching categories", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var categories []map[string]interface{}
	for rows.Next() {
		var id int
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			continue
		}
		categories = append(categories, map[string]interface{}{
			"id":   id,
			"name": name,
		})
	}

	utils.JSONResponse(w, categories, http.StatusOK)
}

// SearchHandler handles game search
func SearchHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query().Get("q")
	category := r.URL.Query().Get("category")

	fmt.Printf("🔍 Search request - Query: '%s', Category: '%s'\n", query, category)

	sqlQuery := `
		SELECT g.id, g.name, g.price, c.name as category, g.image_url, 
		       g.description, 
		       DATE_FORMAT(g.release_date, '%Y-%m-%d') as release_date,
		       r.rank_position
		FROM games g
		LEFT JOIN categories c ON g.category_id = c.id
		LEFT JOIN ranking r ON g.id = r.game_id
		WHERE 1=1
	`
	args := []interface{}{}

	if query != "" {
		sqlQuery += " AND (g.name LIKE ? OR g.description LIKE ?)"
		searchTerm := "%" + query + "%"
		args = append(args, searchTerm, searchTerm)
	}

	if category != "" {
		sqlQuery += " AND c.name = ?"
		args = append(args, category)
	}

	sqlQuery += " ORDER BY g.id"

	fmt.Printf("🔍 Executing search query: %s\n", sqlQuery)
	fmt.Printf("🔍 Query parameters: %v\n", args)

	rows, err := db.Query(sqlQuery, args...)
	if err != nil {
		fmt.Printf("❌ Error searching games: %v\n", err)
		utils.JSONError(w, "Error searching games: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var games []map[string]interface{}
	count := 0

	for rows.Next() {
		var id int
		var name string
		var price float64
		var category string
		var imageURL, description sql.NullString
		var releaseDate sql.NullString
		var rank sql.NullInt64

		err := rows.Scan(&id, &name, &price, &category, &imageURL, &description, &releaseDate, &rank)
		if err != nil {
			fmt.Printf("❌ Error scanning search result row: %v\n", err)
			continue
		}

		game := map[string]interface{}{
			"id":          id,
			"name":        name,
			"price":       price,
			"category":    category,
			"image_url":   imageURL.String,
			"description": description.String,
			"rank":        rank.Int64,
		}

		if releaseDate.Valid && releaseDate.String != "" {
			game["release_date"] = releaseDate.String
		} else {
			game["release_date"] = nil
		}

		games = append(games, game)
		count++
		fmt.Printf("✅ Search result: ID=%d, Name=%s\n", id, name)
	}

	if err = rows.Err(); err != nil {
		fmt.Printf("❌ Error during search rows iteration: %v\n", err)
		utils.JSONError(w, "Error processing search results", http.StatusInternalServerError)
		return
	}

	fmt.Printf("✅ Search completed: found %d games\n", count)

	if games == nil {
		games = []map[string]interface{}{}
	}

	utils.JSONResponse(w, games, http.StatusOK)
}

// RankingHandler returns game rankings
func RankingHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	fmt.Printf("🔍 Fetching game rankings\n")

	// ใช้ sql.NullInt64 สำหรับ rank_position
	rows, err := db.Query(`
		SELECT g.id, g.name, g.price, c.name as category, g.image_url, 
		       r.sales_count, r.rank_position,
		       DATE_FORMAT(g.release_date, '%Y-%m-%d') as release_date
		FROM ranking r
		JOIN games g ON r.game_id = g.id
		JOIN categories c ON g.category_id = c.id
		ORDER BY COALESCE(r.rank_position, 999), r.sales_count DESC
		LIMIT 5
	`)
	if err != nil {
		fmt.Printf("❌ Error fetching rankings: %v\n", err)
		utils.JSONError(w, "Error fetching rankings: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var rankings []map[string]interface{}
	count := 0

	for rows.Next() {
		var id int
		var name string
		var price float64
		var category string
		var imageURL sql.NullString
		var salesCount int
		var rank sql.NullInt64 // เปลี่ยนเป็น sql.NullInt64
		var releaseDate sql.NullString

		err := rows.Scan(&id, &name, &price, &category, &imageURL, &salesCount, &rank, &releaseDate)
		if err != nil {
			fmt.Printf("❌ Error scanning ranking row: %v\n", err)
			continue
		}

		// Handle NULL rank_position
		rankValue := 0
		if rank.Valid {
			rankValue = int(rank.Int64)
		}

		ranking := map[string]interface{}{
			"id":            id,
			"name":          name,
			"price":         price,
			"category":      category,
			"image_url":     imageURL.String,
			"sales_count":   salesCount,
			"rank_position": rankValue,
		}

		if releaseDate.Valid && releaseDate.String != "" {
			ranking["release_date"] = releaseDate.String
		} else {
			ranking["release_date"] = nil
		}

		rankings = append(rankings, ranking)
		count++
		fmt.Printf("✅ Ranking: Position=%d, Game=%s, Sales=%d\n", rankValue, name, salesCount)
	}

	if err = rows.Err(); err != nil {
		fmt.Printf("❌ Error during ranking rows iteration: %v\n", err)
		utils.JSONError(w, "Error processing rankings", http.StatusInternalServerError)
		return
	}

	fmt.Printf("✅ Total rankings found: %d\n", count)

	if rankings == nil {
		rankings = []map[string]interface{}{}
	}

	utils.JSONResponse(w, rankings, http.StatusOK)
}

// LibraryHandler handles user game library
func LibraryHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("User-ID")

	fmt.Printf("🔍 Library request for user ID: %s\n", userID)

	if userID == "" {
		utils.JSONError(w, "User ID not found", http.StatusUnauthorized)
		return
	}

	userIDInt, err := strconv.Atoi(userID)
	if err != nil {
		utils.JSONError(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	fmt.Printf("🔍 Querying library for user ID: %d\n", userIDInt)

	// ใช้ DATE_FORMAT เพื่อแปลง DATE เป็น string โดยตรง
	rows, err := db.Query(`
		SELECT g.id, g.name, g.price, c.name as category, g.image_url, 
		       g.description, 
		       DATE_FORMAT(g.release_date, '%Y-%m-%d') as release_date,
		       DATE_FORMAT(pg.purchased_at, '%Y-%m-%d %H:%i:%s') as purchased_date
		FROM purchased_games pg
		JOIN games g ON pg.game_id = g.id
		JOIN categories c ON g.category_id = c.id
		WHERE pg.user_id = ?
		ORDER BY pg.purchased_at DESC
	`, userIDInt)

	if err != nil {
		fmt.Printf("❌ Error fetching library: %v\n", err)
		utils.JSONError(w, "Error fetching library: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var games []map[string]interface{}
	count := 0

	for rows.Next() {
		var id int
		var name string
		var price float64
		var category string
		var imageURL, description sql.NullString
		var releaseDate sql.NullString
		var purchasedDate string

		err := rows.Scan(&id, &name, &price, &category, &imageURL, &description, &releaseDate, &purchasedDate)
		if err != nil {
			fmt.Printf("❌ Error scanning library row: %v\n", err)
			continue
		}

		game := map[string]interface{}{
			"id":           id,
			"name":         name,
			"price":        price,
			"category":     category,
			"image_url":    imageURL.String,
			"description":  description.String,
			"purchased_at": purchasedDate,
		}

		if releaseDate.Valid && releaseDate.String != "" {
			game["release_date"] = releaseDate.String
		} else {
			game["release_date"] = nil
		}

		games = append(games, game)
		count++
		fmt.Printf("✅ Library game: ID=%d, Name=%s, Purchased=%s\n", id, name, purchasedDate)
	}

	if err = rows.Err(); err != nil {
		fmt.Printf("❌ Error during library rows iteration: %v\n", err)
		utils.JSONError(w, "Error processing library", http.StatusInternalServerError)
		return
	}

	fmt.Printf("✅ Total library games found: %d\n", count)

	// Always return games array, even if empty
	if games == nil {
		games = []map[string]interface{}{}
	}

	utils.JSONResponse(w, map[string]interface{}{
		"total_games": count,
		"games":       games,
	}, http.StatusOK)
}
