package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"go-api-game/utils"
	"net/http"
	"strconv"
	"time"
)

// CartHandler handles cart retrieval
func CartHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("User-ID")

	rows, err := db.Query(`
		SELECT g.id, g.name, g.price, c.name as category, g.image_url, ci.quantity
		FROM cart_items ci
		JOIN games g ON ci.game_id = g.id
		JOIN categories c ON g.category_id = c.id
		JOIN carts ca ON ci.cart_id = ca.id
		WHERE ca.user_id = ?
	`, userID)
	if err != nil {
		utils.JSONError(w, "Error fetching cart", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var cartItems []map[string]interface{}
	total := 0.0

	for rows.Next() {
		var item struct {
			ID       int     `json:"id"`
			Name     string  `json:"name"`
			Price    float64 `json:"price"`
			Category string  `json:"category"`
			ImageURL string  `json:"image_url"`
			Quantity int     `json:"quantity"`
		}

		if err := rows.Scan(&item.ID, &item.Name, &item.Price, &item.Category, &item.ImageURL, &item.Quantity); err != nil {
			continue
		}

		itemTotal := item.Price * float64(item.Quantity)
		total += itemTotal

		cartItems = append(cartItems, map[string]interface{}{
			"game_id":   item.ID,
			"name":      item.Name,
			"price":     item.Price,
			"category":  item.Category,
			"image_url": item.ImageURL,
			"quantity":  item.Quantity,
			"subtotal":  itemTotal,
		})
	}

	utils.JSONResponse(w, map[string]interface{}{
		"items":      cartItems,
		"total":      total,
		"item_count": len(cartItems),
	}, http.StatusOK)
}

// AddToCartHandler handles adding games to cart
func AddToCartHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := r.Header.Get("User-ID")

	var req struct {
		GameID int `json:"game_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.JSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Check if user already owns the game
	var owned bool
	err := db.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM purchased_games WHERE user_id = ? AND game_id = ?
		)
	`, userID, req.GameID).Scan(&owned)
	if err != nil {
		utils.JSONError(w, "Error checking ownership", http.StatusInternalServerError)
		return
	}

	if owned {
		utils.JSONError(w, "You already own this game", http.StatusBadRequest)
		return
	}

	// Get user's cart ID
	var cartID int
	err = db.QueryRow("SELECT id FROM carts WHERE user_id = ?", userID).Scan(&cartID)
	if err != nil {
		utils.JSONError(w, "Error finding cart", http.StatusInternalServerError)
		return
	}

	// Add to cart
	_, err = db.Exec(`
		INSERT INTO cart_items (cart_id, game_id, quantity) 
		VALUES (?, ?, 1)
		ON DUPLICATE KEY UPDATE quantity = quantity + 1
	`, cartID, req.GameID)
	if err != nil {
		utils.JSONError(w, "Error adding to cart", http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, map[string]string{
		"message": "Game added to cart",
	}, http.StatusOK)
}

// RemoveFromCartHandler handles removing games from cart
func RemoveFromCartHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := r.Header.Get("User-ID")

	var req struct {
		GameID int `json:"game_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.JSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Get user's cart ID
	var cartID int
	err := db.QueryRow("SELECT id FROM carts WHERE user_id = ?", userID).Scan(&cartID)
	if err != nil {
		utils.JSONError(w, "Error finding cart", http.StatusInternalServerError)
		return
	}

	// Remove from cart
	_, err = db.Exec("DELETE FROM cart_items WHERE cart_id = ? AND game_id = ?", cartID, req.GameID)
	if err != nil {
		utils.JSONError(w, "Error removing from cart", http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, map[string]string{
		"message": "Game removed from cart",
	}, http.StatusOK)
}

// CheckoutHandler handles cart checkout and purchase
func CheckoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userIDStr := r.Header.Get("User-ID")
	userID, _ := strconv.Atoi(userIDStr)

	var req struct {
		DiscountCode string `json:"discount_code"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.JSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Start transaction
	tx, err := db.Begin()
	if err != nil {
		utils.JSONError(w, "Error starting transaction", http.StatusInternalServerError)
		return
	}

	// Get cart items and total
	rows, err := tx.Query(`
		SELECT g.id, g.name, g.price, ci.quantity
		FROM cart_items ci
		JOIN games g ON ci.game_id = g.id
		JOIN carts ca ON ci.cart_id = ca.id
		WHERE ca.user_id = ?
	`, userID)
	if err != nil {
		tx.Rollback()
		utils.JSONError(w, "Error fetching cart items", http.StatusInternalServerError)
		return
	}
	defer rows.Close() // ✅ ใช้ defer เพื่อปิด rows

	var cartItems []struct {
		GameID   int
		Name     string
		Price    float64
		Quantity int
	}
	total := 0.0

	for rows.Next() {
		var item struct {
			GameID   int
			Name     string
			Price    float64
			Quantity int
		}
		if err := rows.Scan(&item.GameID, &item.Name, &item.Price, &item.Quantity); err != nil {
			tx.Rollback()
			utils.JSONError(w, "Error scanning cart items", http.StatusInternalServerError)
			return
		}
		cartItems = append(cartItems, item)
		total += item.Price * float64(item.Quantity)
	}

	if err := rows.Err(); err != nil {
		tx.Rollback()
		utils.JSONError(w, "Error reading cart items", http.StatusInternalServerError)
		return
	}

	if len(cartItems) == 0 {
		tx.Rollback()
		utils.JSONError(w, "Cart is empty", http.StatusBadRequest)
		return
	}

	// Check for duplicate games in library
	for _, item := range cartItems {
		var owned bool
		err := tx.QueryRow(`
			SELECT EXISTS(
				SELECT 1 FROM purchased_games WHERE user_id = ? AND game_id = ?
			)
		`, userID, item.GameID).Scan(&owned)
		if err != nil {
			tx.Rollback()
			utils.JSONError(w, "Error checking game ownership", http.StatusInternalServerError)
			return
		}
		if owned {
			tx.Rollback()
			utils.JSONError(w, fmt.Sprintf("You already own: %s", item.Name), http.StatusBadRequest)
			return
		}
	}

	// Apply discount if provided
	var discountCodeID *int
	var discountValue float64
	finalAmount := total

	if req.DiscountCode != "" {
		var discount struct {
			ID               int
			Type             string
			Value            float64
			MinTotal         float64
			UsageLimit       *int
			SingleUsePerUser bool
			Active           bool
		}

		// ✅ ใช้ sql.NullString สำหรับรับค่า date จาก database
		var startDateStr, endDateStr sql.NullString

		err := tx.QueryRow(`
			SELECT id, type, value, min_total, usage_limit, single_use_per_user, 
			       active, start_date, end_date
			FROM discount_codes 
			WHERE code = ? AND active = 1
		`, req.DiscountCode).Scan(
			&discount.ID, &discount.Type, &discount.Value, &discount.MinTotal,
			&discount.UsageLimit, &discount.SingleUsePerUser, &discount.Active,
			&startDateStr, &endDateStr, // ✅ รับเป็น string ก่อน
		)

		if err == nil {
			// ✅ Convert string date to time.Time
			var startDate, endDate *time.Time

			if startDateStr.Valid && startDateStr.String != "" {
				parsedStart, err := time.Parse("2006-01-02", startDateStr.String)
				if err == nil {
					startDate = &parsedStart
				}
			}

			if endDateStr.Valid && endDateStr.String != "" {
				parsedEnd, err := time.Parse("2006-01-02", endDateStr.String)
				if err == nil {
					endDate = &parsedEnd
				}
			}

			// Check discount validity
			now := time.Now()
			if startDate != nil && now.Before(*startDate) {
				tx.Rollback()
				utils.JSONError(w, "Discount code not yet valid", http.StatusBadRequest)
				return
			}
			if endDate != nil && now.After(*endDate) {
				tx.Rollback()
				utils.JSONError(w, "Discount code has expired", http.StatusBadRequest)
				return
			}
			if discount.MinTotal > 0 && total < discount.MinTotal {
				tx.Rollback()
				utils.JSONError(w, fmt.Sprintf("Minimum purchase of $%.2f required", discount.MinTotal), http.StatusBadRequest)
				return
			}

			// Check usage limit
			if discount.UsageLimit != nil {
				var usageCount int
				err := tx.QueryRow(`
                SELECT COUNT(*) 
                FROM user_discount_codes 
                WHERE discount_code_id = ?
            `, discount.ID).Scan(&usageCount)

				if err == nil && usageCount >= *discount.UsageLimit {
					// ❌ ตั้งค่า active = 0 เมื่อใช้ครบจำนวน
					tx.Exec("UPDATE discount_codes SET active = 0 WHERE id = ?", discount.ID)
					fmt.Printf("🚫 Discount code deactivated: ID=%d, usage reached limit\n", discount.ID)

					tx.Rollback()
					utils.JSONError(w, "Discount code usage limit reached", http.StatusBadRequest)
					return
				}
			}

			// Check if user already used this code
			if discount.SingleUsePerUser {
				var used bool
				err := tx.QueryRow(`
					SELECT EXISTS(
						SELECT 1 FROM user_discount_codes 
						WHERE user_id = ? AND discount_code_id = ?
					)
				`, userID, discount.ID).Scan(&used)
				if err != nil {
					tx.Rollback()
					utils.JSONError(w, "Error checking discount usage", http.StatusInternalServerError)
					return
				}
				if used {
					tx.Rollback()
					utils.JSONError(w, "Discount code already used", http.StatusBadRequest)
					return
				}
			}

			// Apply discount
			if discount.Type == "percent" {
				discountValue = total * (discount.Value / 100)
			} else {
				discountValue = discount.Value
			}

			finalAmount = total - discountValue
			if finalAmount < 0 {
				finalAmount = 0
			}

			discountCodeID = &discount.ID

			fmt.Printf("✅ Discount applied in checkout: Code=%s, Discount=%.2f, Final=%.2f\n",
				req.DiscountCode, discountValue, finalAmount)
		} else if err != sql.ErrNoRows {
			// ❌ Database error (ไม่ใช่แค่หาไม่เจอ)
			tx.Rollback()
			utils.JSONError(w, "Error checking discount code", http.StatusInternalServerError)
			return
		}
		// ถ้า err == sql.ErrNoRows ก็แค่ไม่ใช้ส่วนลด (ไม่ต้องทำอะไร)
	}

	// Check wallet balance
	var walletBalance float64
	err = tx.QueryRow("SELECT wallet_balance FROM users WHERE id = ?", userID).Scan(&walletBalance)
	if err != nil {
		tx.Rollback()
		utils.JSONError(w, "Error checking wallet balance", http.StatusInternalServerError)
		return
	}

	if walletBalance < finalAmount {
		tx.Rollback()
		utils.JSONError(w, "Insufficient wallet balance", http.StatusBadRequest)
		return
	}

	// Create purchase record
	result, err := tx.Exec(`
		INSERT INTO purchases (user_id, total_amount, discount_code_id, final_amount)
		VALUES (?, ?, ?, ?)
	`, userID, total, discountCodeID, finalAmount)
	if err != nil {
		tx.Rollback()
		utils.JSONError(w, "Error creating purchase record", http.StatusInternalServerError)
		return
	}

	purchaseID, _ := result.LastInsertId()

	// Add purchase items and mark games as purchased
	for _, item := range cartItems {
		// Add to purchase items
		_, err := tx.Exec(`
			INSERT INTO purchase_items (purchase_id, game_id, price_at_purchase)
			VALUES (?, ?, ?)
		`, purchaseID, item.GameID, item.Price)
		if err != nil {
			tx.Rollback()
			utils.JSONError(w, "Error recording purchase items", http.StatusInternalServerError)
			return
		}

		// Add to purchased games
		_, err = tx.Exec(`
			INSERT INTO purchased_games (user_id, game_id) 
			VALUES (?, ?)
		`, userID, item.GameID)
		if err != nil {
			tx.Rollback()
			utils.JSONError(w, "Error adding to library", http.StatusInternalServerError)
			return
		}

		// Update ranking sales count
		_, err = tx.Exec(`
			INSERT INTO ranking (game_id, sales_count) 
			VALUES (?, 1)
			ON DUPLICATE KEY UPDATE sales_count = sales_count + 1
		`, item.GameID)
		if err != nil {
			tx.Rollback()
			utils.JSONError(w, "Error updating rankings", http.StatusInternalServerError)
			return
		}
	}

	// Update rankings order
	_, err = tx.Exec(`
		UPDATE ranking 
		SET rank_position = (
			SELECT rnk FROM (
				SELECT game_id, RANK() OVER (ORDER BY sales_count DESC) as rnk
				FROM ranking
			) r WHERE r.game_id = ranking.game_id
		)
	`)
	if err != nil {
		tx.Rollback()
		utils.JSONError(w, "Error updating rank positions", http.StatusInternalServerError)
		return
	}

	// Record discount usage
	if discountCodeID != nil {
		_, err = tx.Exec(`
            INSERT INTO user_discount_codes (user_id, discount_code_id)
            VALUES (?, ?)
        `, userID, *discountCodeID)
		if err != nil {
			tx.Rollback()
			utils.JSONError(w, "Error recording discount usage", http.StatusInternalServerError)
			return
		}

		// ✅ ตรวจสอบว่าถึงขีดจำกัดการใช้งานแล้วหรือไม่
		var usageCount int
		var usageLimit *int
		err = tx.QueryRow(`
            SELECT usage_limit FROM discount_codes WHERE id = ?
        `, *discountCodeID).Scan(&usageLimit)

		if err == nil && usageLimit != nil {
			err = tx.QueryRow(`
                SELECT COUNT(*) FROM user_discount_codes WHERE discount_code_id = ?
            `, *discountCodeID).Scan(&usageCount)

			if err == nil && usageCount >= *usageLimit {
				// 🚫 ตั้งค่า active = 0 เมื่อใช้ครบจำนวน
				_, err = tx.Exec("UPDATE discount_codes SET active = 0 WHERE id = ?", *discountCodeID)
				if err == nil {
					fmt.Printf("🚫 Discount code auto-deactivated: ID=%d, usage reached limit (%d/%d)\n",
						*discountCodeID, usageCount, *usageLimit)
				}
			}
		}
	}

	// Update wallet balance
	_, err = tx.Exec("UPDATE users SET wallet_balance = wallet_balance - ? WHERE id = ?",
		finalAmount, userID)
	if err != nil {
		tx.Rollback()
		utils.JSONError(w, "Error updating wallet", http.StatusInternalServerError)
		return
	}

	// Record transaction
	_, err = tx.Exec(`
		INSERT INTO user_transactions (user_id, type, amount, description)
		VALUES (?, 'purchase', ?, ?)
	`, userID, finalAmount, fmt.Sprintf("Purchase #%d", purchaseID))
	if err != nil {
		tx.Rollback()
		utils.JSONError(w, "Error recording transaction", http.StatusInternalServerError)
		return
	}

	// Clear cart
	_, err = tx.Exec("DELETE FROM cart_items WHERE cart_id = (SELECT id FROM carts WHERE user_id = ?)", userID)
	if err != nil {
		tx.Rollback()
		utils.JSONError(w, "Error clearing cart", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		utils.JSONError(w, "Error completing purchase", http.StatusInternalServerError)
		return
	}

	fmt.Printf("✅ Checkout completed: user_id=%d, purchase_id=%d, total=%.2f, final=%.2f\n",
		userID, purchaseID, total, finalAmount)

	utils.JSONResponse(w, map[string]interface{}{
		"message":      "Purchase completed successfully",
		"purchase_id":  purchaseID,
		"total":        total,
		"discount":     discountValue,
		"final_amount": finalAmount,
		"games_count":  len(cartItems),
	}, http.StatusOK)
}

// ApplyDiscountHandler handles discount code validation and application
func ApplyDiscountHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Code        string  `json:"code"`
		TotalAmount float64 `json:"total_amount"`
		UserID      int     `json:"user_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.JSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	fmt.Printf("🔍 Applying discount code: %s for user %d, total: %.2f\n", req.Code, req.UserID, req.TotalAmount)

	// Check if discount code exists and is valid
	var discount struct {
		ID               int
		Type             string
		Value            float64
		MinTotal         float64
		UsageLimit       *int
		SingleUsePerUser bool
		Active           bool
		StartDate        *time.Time
		EndDate          *time.Time
	}

	// ใช้ sql.NullString เพื่อรับค่า date จาก database
	var startDateStr, endDateStr sql.NullString

	err := db.QueryRow(`
        SELECT id, type, value, min_total, usage_limit, single_use_per_user, 
               active, start_date, end_date
        FROM discount_codes 
        WHERE code = ? AND active = 1
    `, req.Code).Scan(
		&discount.ID, &discount.Type, &discount.Value, &discount.MinTotal,
		&discount.UsageLimit, &discount.SingleUsePerUser, &discount.Active,
		&startDateStr, &endDateStr, // รับเป็น string ก่อน
	)

	if err != nil {
		fmt.Printf("❌ Database error: %v\n", err)
		if err == sql.ErrNoRows {
			utils.JSONError(w, "Discount code not found or inactive", http.StatusBadRequest)
		} else {
			utils.JSONError(w, "Error checking discount code", http.StatusInternalServerError)
		}
		return
	}

	// Convert string date to time.Time
	if startDateStr.Valid && startDateStr.String != "" {
		startDate, err := time.Parse("2006-01-02", startDateStr.String)
		if err != nil {
			fmt.Printf("❌ Error parsing start date: %v\n", err)
		} else {
			discount.StartDate = &startDate
		}
	}

	if endDateStr.Valid && endDateStr.String != "" {
		endDate, err := time.Parse("2006-01-02", endDateStr.String)
		if err != nil {
			fmt.Printf("❌ Error parsing end date: %v\n", err)
		} else {
			discount.EndDate = &endDate
		}
	}

	fmt.Printf("✅ Discount found: ID=%d, StartDate=%v, EndDate=%v\n",
		discount.ID, discount.StartDate, discount.EndDate)

	// Validate discount code
	now := time.Now()

	// Check date validity
	if discount.StartDate != nil && now.Before(*discount.StartDate) {
		utils.JSONError(w, "Discount code not yet valid", http.StatusBadRequest)
		return
	}
	if discount.EndDate != nil && now.After(*discount.EndDate) {
		utils.JSONError(w, "Discount code has expired", http.StatusBadRequest)
		return
	}

	// Check minimum total
	if discount.MinTotal > 0 && req.TotalAmount < discount.MinTotal {
		utils.JSONError(w, fmt.Sprintf("Minimum purchase of $%.2f required", discount.MinTotal), http.StatusBadRequest)
		return
	}

	// Check usage limit
	if discount.UsageLimit != nil {
		var usageCount int
		err := db.QueryRow(`
            SELECT COUNT(*) 
            FROM user_discount_codes 
            WHERE discount_code_id = ?
        `, discount.ID).Scan(&usageCount)

		if err == nil && usageCount >= *discount.UsageLimit {
			// ❌ ตั้งค่า active = 0 เมื่อใช้ครบจำนวน
			db.Exec("UPDATE discount_codes SET active = 0 WHERE id = ?", discount.ID)
			fmt.Printf("🚫 Discount code deactivated: ID=%d, usage reached limit\n", discount.ID)

			utils.JSONError(w, "Discount code usage limit reached", http.StatusBadRequest)
			return
		}
	}

	// Check if user already used this code (for single-use codes)
	if discount.SingleUsePerUser {
		var used bool
		err := db.QueryRow(`
            SELECT EXISTS(
                SELECT 1 FROM user_discount_codes 
                WHERE user_id = ? AND discount_code_id = ?
            )
        `, req.UserID, discount.ID).Scan(&used)

		if err != nil {
			fmt.Printf("❌ Error checking single use: %v\n", err)
		} else if used {
			utils.JSONError(w, "Discount code already used", http.StatusBadRequest)
			return
		}
	}

	// Calculate discount amount
	var discountAmount float64
	var finalAmount float64

	if discount.Type == "percent" {
		discountAmount = req.TotalAmount * (discount.Value / 100)
	} else {
		discountAmount = discount.Value
	}

	finalAmount = req.TotalAmount - discountAmount
	if finalAmount < 0 {
		finalAmount = 0
	}

	fmt.Printf("✅ Discount applied: Code=%s, Type=%s, Value=%.2f, Discount=%.2f, Final=%.2f\n",
		req.Code, discount.Type, discount.Value, discountAmount, finalAmount)

	// Return successful response
	utils.JSONResponse(w, map[string]interface{}{
		"valid":           true,
		"discount_id":     discount.ID,
		"code":            req.Code,
		"type":            discount.Type,
		"value":           discount.Value,
		"min_total":       discount.MinTotal,
		"discount_amount": discountAmount,
		"final_amount":    finalAmount,
		"original_amount": req.TotalAmount,
		"message":         "Discount applied successfully",
	}, http.StatusOK)
}
