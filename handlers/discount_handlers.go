package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"go-api-game/utils"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// AdminDiscountHandler handles discount code management
func AdminDiscountHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("🎯 AdminDiscountHandler: %s %s\n", r.Method, r.URL.Path)

	// Extract ID จาก URL ถ้ามี
	var id int
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) >= 3 {
		if parsedID, err := strconv.Atoi(pathParts[2]); err == nil {
			id = parsedID
		}
	}

	switch r.Method {
	case "GET":
		if id > 0 {
			getDiscountByID(w, r, id)
		} else {
			getAllDiscounts(w, r)
		}
	case "POST":
		createDiscount(w, r)
	case "PUT":
		if id > 0 {
			updateDiscountWithReset(w, r, id)
		} else {
			utils.JSONError(w, "Discount ID required", http.StatusBadRequest)
		}
	case "DELETE":
		if id > 0 {
			deleteDiscountWithCleanup(w, r, id)
		} else {
			utils.JSONError(w, "Discount ID required", http.StatusBadRequest)
		}
	default:
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// GET /admin/discounts - ดึงทั้งหมด
func getAllDiscounts(w http.ResponseWriter, r *http.Request) {
	// เรียกตรวจสอบอัตโนมัติก่อนดึงข้อมูล
	go autoDeactivateDiscounts()
	fmt.Println("🔍 Fetching all discount codes")

	rows, err := db.Query(`
		SELECT 
			dc.id, dc.code, dc.type, dc.value, dc.min_total, 
			DATE_FORMAT(dc.start_date, '%Y-%m-%d') as start_date,
			DATE_FORMAT(dc.end_date, '%Y-%m-%d') as end_date,
			dc.usage_limit, dc.single_use_per_user, dc.active,
			dc.created_at,
			COUNT(udc.id) as usage_count
		FROM discount_codes dc
		LEFT JOIN user_discount_codes udc ON dc.id = udc.discount_code_id
		GROUP BY dc.id
		ORDER BY dc.created_at DESC
	`)
	if err != nil {
		fmt.Printf("❌ Error fetching discount codes: %v\n", err)
		utils.JSONError(w, "Error fetching discount codes", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var discounts []map[string]interface{}
	count := 0

	for rows.Next() {
		var id int
		var code, discountType string
		var value, minTotal float64
		var startDate, endDate, createdAt sql.NullString
		var usageLimit sql.NullInt64
		var singleUsePerUser, active bool
		var usageCount int

		err := rows.Scan(&id, &code, &discountType, &value, &minTotal, &startDate, &endDate, &usageLimit, &singleUsePerUser, &active, &createdAt, &usageCount)
		if err != nil {
			fmt.Printf("❌ Error scanning discount row: %v\n", err)
			continue
		}

		discount := map[string]interface{}{
			"id":                  id,
			"code":                code,
			"type":                discountType,
			"value":               value,
			"min_total":           minTotal,
			"usage_limit":         usageLimit.Int64,
			"single_use_per_user": singleUsePerUser,
			"active":              active,
			"created_at":          createdAt.String,
			"usage_count":         usageCount, // เพิ่มจำนวนการใช้งาน
		}

		if startDate.Valid {
			discount["start_date"] = startDate.String
		}
		if endDate.Valid {
			discount["end_date"] = endDate.String
		}

		discounts = append(discounts, discount)
		count++
	}

	if err = rows.Err(); err != nil {
		fmt.Printf("❌ Error during rows iteration: %v\n", err)
		utils.JSONError(w, "Error processing discount codes", http.StatusInternalServerError)
		return
	}

	fmt.Printf("✅ Total discount codes found: %d\n", count)

	utils.JSONResponse(w, map[string]interface{}{
		"discounts": discounts,
		"total":     count,
	}, http.StatusOK)
}

// GET /admin/discounts/{id} - ดึงโดย ID
func getDiscountByID(w http.ResponseWriter, r *http.Request, id int) {
	fmt.Printf("🔍 Fetching discount code: ID=%d\n", id)

	var code, discountType string
	var value, minTotal float64
	var startDate, endDate, createdAt sql.NullString
	var usageLimit sql.NullInt64
	var singleUsePerUser, active bool
	var usageCount int

	err := db.QueryRow(`
		SELECT 
			dc.code, dc.type, dc.value, dc.min_total, 
			DATE_FORMAT(dc.start_date, '%Y-%m-%d') as start_date,
			DATE_FORMAT(dc.end_date, '%Y-%m-%d') as end_date,
			dc.usage_limit, dc.single_use_per_user, dc.active, dc.created_at,
			COUNT(udc.id) as usage_count
		FROM discount_codes dc
		LEFT JOIN user_discount_codes udc ON dc.id = udc.discount_code_id
		WHERE dc.id = ?
		GROUP BY dc.id
	`, id).Scan(&code, &discountType, &value, &minTotal, &startDate, &endDate, &usageLimit, &singleUsePerUser, &active, &createdAt, &usageCount)

	if err != nil {
		if err == sql.ErrNoRows {
			utils.JSONError(w, "Discount code not found", http.StatusNotFound)
		} else {
			utils.JSONError(w, "Error fetching discount code", http.StatusInternalServerError)
		}
		return
	}

	discount := map[string]interface{}{
		"id":                  id,
		"code":                code,
		"type":                discountType,
		"value":               value,
		"min_total":           minTotal,
		"usage_limit":         usageLimit.Int64,
		"single_use_per_user": singleUsePerUser,
		"active":              active,
		"created_at":          createdAt.String,
		"usage_count":         usageCount, // เพิ่มจำนวนการใช้งาน
	}

	if startDate.Valid {
		discount["start_date"] = startDate.String
	}
	if endDate.Valid {
		discount["end_date"] = endDate.String
	}

	fmt.Printf("✅ Discount code found: ID=%d, Code=%s, Usage Count=%d\n", id, code, usageCount)
	utils.JSONResponse(w, discount, http.StatusOK)
}

// POST /admin/discounts - สร้างใหม่
func createDiscount(w http.ResponseWriter, r *http.Request) {
	fmt.Println("➕ Creating new discount code")

	var req struct {
		Code             string  `json:"code"`
		Type             string  `json:"type"`
		Value            float64 `json:"value"`
		MinTotal         float64 `json:"min_total"`
		StartDate        *string `json:"start_date"`
		EndDate          *string `json:"end_date"`
		UsageLimit       *int    `json:"usage_limit"`
		SingleUsePerUser bool    `json:"single_use_per_user"`
		Active           bool    `json:"active"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.JSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validation
	if req.Code == "" {
		utils.JSONError(w, "Discount code is required", http.StatusBadRequest)
		return
	}
	if req.Value <= 0 {
		utils.JSONError(w, "Discount value must be greater than 0", http.StatusBadRequest)
		return
	}
	if req.Type != "percent" && req.Type != "fixed" {
		utils.JSONError(w, "Discount type must be 'percent' or 'fixed'", http.StatusBadRequest)
		return
	}

	// Parse dates
	var startDate, endDate interface{}
	if req.StartDate != nil && *req.StartDate != "" {
		if date, err := time.Parse("2006-01-02", *req.StartDate); err == nil {
			startDate = date
		} else {
			utils.JSONError(w, "Invalid start date format. Use YYYY-MM-DD", http.StatusBadRequest)
			return
		}
	}
	if req.EndDate != nil && *req.EndDate != "" {
		if date, err := time.Parse("2006-01-02", *req.EndDate); err == nil {
			endDate = date
		} else {
			utils.JSONError(w, "Invalid end date format. Use YYYY-MM-DD", http.StatusBadRequest)
			return
		}
	}

	// ตรวจสอบว่า code ซ้ำหรือไม่
	var existingCode string
	err := db.QueryRow("SELECT code FROM discount_codes WHERE code = ?", req.Code).Scan(&existingCode)
	if err == nil {
		utils.JSONError(w, "Discount code already exists", http.StatusConflict)
		return
	} else if err != sql.ErrNoRows {
		utils.JSONError(w, "Error checking discount code", http.StatusInternalServerError)
		return
	}

	// สร้าง discount code ใหม่
	result, err := db.Exec(`
		INSERT INTO discount_codes 
		(code, type, value, min_total, start_date, end_date, usage_limit, single_use_per_user, active)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, req.Code, req.Type, req.Value, req.MinTotal, startDate, endDate, req.UsageLimit, req.SingleUsePerUser, req.Active)

	if err != nil {
		fmt.Printf("❌ Error creating discount code: %v\n", err)
		utils.JSONError(w, "Error creating discount code", http.StatusInternalServerError)
		return
	}

	id, _ := result.LastInsertId()
	fmt.Printf("✅ Discount code created: ID=%d, Code=%s\n", id, req.Code)

	utils.JSONResponse(w, map[string]interface{}{
		"message": "Discount code created successfully",
		"id":      id,
	}, http.StatusCreated)
}

// PUT /admin/discounts/{id} - อัพเดต + รีเซ็ตการใช้งานเมื่อเปิดใช้งานใหม่
func updateDiscountWithReset(w http.ResponseWriter, r *http.Request, id int) {
	fmt.Printf("✏️ Updating discount code with reset: ID=%d\n", id)

	var req struct {
		Code             string  `json:"code"`
		Type             string  `json:"type"`
		Value            float64 `json:"value"`
		MinTotal         float64 `json:"min_total"`
		StartDate        *string `json:"start_date"`
		EndDate          *string `json:"end_date"`
		UsageLimit       *int    `json:"usage_limit"`
		SingleUsePerUser bool    `json:"single_use_per_user"`
		Active           bool    `json:"active"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.JSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validation
	if req.Code == "" {
		utils.JSONError(w, "Discount code is required", http.StatusBadRequest)
		return
	}
	if req.Value <= 0 {
		utils.JSONError(w, "Discount value must be greater than 0", http.StatusBadRequest)
		return
	}
	if req.Type != "percent" && req.Type != "fixed" {
		utils.JSONError(w, "Discount type must be 'percent' or 'fixed'", http.StatusBadRequest)
		return
	}

	// เริ่ม transaction
	tx, err := db.Begin()
	if err != nil {
		utils.JSONError(w, "Error starting transaction", http.StatusInternalServerError)
		return
	}

	// ตรวจสอบสถานะ active ก่อนหน้า
	var currentActive bool
	err = tx.QueryRow("SELECT active FROM discount_codes WHERE id = ?", id).Scan(&currentActive)
	if err != nil {
		tx.Rollback()
		if err == sql.ErrNoRows {
			utils.JSONError(w, "Discount code not found", http.StatusNotFound)
		} else {
			utils.JSONError(w, "Error checking current discount status", http.StatusInternalServerError)
		}
		return
	}

	// ถ้ากำลังเปลี่ยนจาก inactive (false) เป็น active (true) -> ลบประวัติการใช้งาน
	resetUsage := false
	if !currentActive && req.Active {
		_, err = tx.Exec("DELETE FROM user_discount_codes WHERE discount_code_id = ?", id)
		if err != nil {
			tx.Rollback()
			utils.JSONError(w, "Error resetting discount usage history", http.StatusInternalServerError)
			return
		}
		resetUsage = true
		fmt.Printf("✅ Reset usage history for discount ID: %d (reactivated)\n", id)
	}

	// Parse dates
	var startDate, endDate interface{}
	if req.StartDate != nil && *req.StartDate != "" {
		if date, err := time.Parse("2006-01-02", *req.StartDate); err == nil {
			startDate = date
		} else {
			tx.Rollback()
			utils.JSONError(w, "Invalid start date format. Use YYYY-MM-DD", http.StatusBadRequest)
			return
		}
	}
	if req.EndDate != nil && *req.EndDate != "" {
		if date, err := time.Parse("2006-01-02", *req.EndDate); err == nil {
			endDate = date
		} else {
			tx.Rollback()
			utils.JSONError(w, "Invalid end date format. Use YYYY-MM-DD", http.StatusBadRequest)
			return
		}
	}

	// ตรวจสอบว่า code ซ้ำหรือไม่ (ไม่รวมตัวเอง)
	var existingCode string
	var existingID int
	err = tx.QueryRow("SELECT id, code FROM discount_codes WHERE code = ? AND id != ?", req.Code, id).Scan(&existingID, &existingCode)
	if err == nil {
		tx.Rollback()
		utils.JSONError(w, "Discount code already exists", http.StatusConflict)
		return
	} else if err != sql.ErrNoRows {
		tx.Rollback()
		utils.JSONError(w, "Error checking discount code", http.StatusInternalServerError)
		return
	}

	// อัพเดต discount code
	result, err := tx.Exec(`
		UPDATE discount_codes 
		SET code = ?, type = ?, value = ?, min_total = ?, start_date = ?, end_date = ?, 
		    usage_limit = ?, single_use_per_user = ?, active = ?
		WHERE id = ?
	`, req.Code, req.Type, req.Value, req.MinTotal, startDate, endDate, req.UsageLimit, req.SingleUsePerUser, req.Active, id)

	if err != nil {
		tx.Rollback()
		fmt.Printf("❌ Error updating discount code: %v\n", err)
		utils.JSONError(w, "Error updating discount code", http.StatusInternalServerError)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		tx.Rollback()
		utils.JSONError(w, "Discount code not found", http.StatusNotFound)
		return
	}

	if err := tx.Commit(); err != nil {
		utils.JSONError(w, "Error completing update", http.StatusInternalServerError)
		return
	}

	fmt.Printf("✅ Discount code updated: ID=%d, Code=%s, Active=%t\n", id, req.Code, req.Active)

	utils.JSONResponse(w, map[string]interface{}{
		"message":     "Discount code updated successfully",
		"id":          id,
		"active":      req.Active,
		"reset_usage": resetUsage, // บอกว่าทำการรีเซ็ตการใช้งานหรือไม่
	}, http.StatusOK)
}

// DELETE /admin/discounts/{id} - ลบ + ลบประวัติการใช้งาน
func deleteDiscountWithCleanup(w http.ResponseWriter, r *http.Request, id int) {
	fmt.Printf("🗑️ Deleting discount code with cleanup: ID=%d\n", id)

	// เริ่ม transaction
	tx, err := db.Begin()
	if err != nil {
		utils.JSONError(w, "Error starting transaction", http.StatusInternalServerError)
		return
	}

	// 1. ลบประวัติการใช้งานใน user_discount_codes ก่อน
	_, err = tx.Exec("DELETE FROM user_discount_codes WHERE discount_code_id = ?", id)
	if err != nil {
		tx.Rollback()
		fmt.Printf("❌ Error deleting discount usage history: %v\n", err)
		utils.JSONError(w, "Error deleting discount usage history", http.StatusInternalServerError)
		return
	}
	fmt.Printf("✅ Deleted usage history for discount ID: %d\n", id)

	// 2. ลบ discount code
	result, err := tx.Exec("DELETE FROM discount_codes WHERE id = ?", id)
	if err != nil {
		tx.Rollback()
		fmt.Printf("❌ Error deleting discount code: %v\n", err)
		utils.JSONError(w, "Error deleting discount code", http.StatusInternalServerError)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		tx.Rollback()
		utils.JSONError(w, "Discount code not found", http.StatusNotFound)
		return
	}

	if err := tx.Commit(); err != nil {
		utils.JSONError(w, "Error completing deletion", http.StatusInternalServerError)
		return
	}

	fmt.Printf("✅ Discount code deleted: ID=%d\n", id)

	utils.JSONResponse(w, map[string]interface{}{
		"message":      "Discount code deleted successfully",
		"id":           id,
		"cleanup_done": true, // บอกว่าทำการลบประวัติการใช้งานแล้ว
	}, http.StatusOK)
}

// ฟังก์ชันสำหรับตรวจสอบและปิดใช้งานส่วนลดที่ครบจำนวน
func autoDeactivateDiscounts() {
	fmt.Println("🔄 Checking for discount codes to deactivate...")

	rows, err := db.Query(`
        SELECT dc.id, dc.usage_limit, COUNT(udc.id) as usage_count
        FROM discount_codes dc
        LEFT JOIN user_discount_codes udc ON dc.id = udc.discount_code_id
        WHERE dc.active = 1 AND dc.usage_limit IS NOT NULL
        GROUP BY dc.id
        HAVING usage_count >= dc.usage_limit
    `)
	if err != nil {
		fmt.Printf("❌ Error checking discount deactivation: %v\n", err)
		return
	}
	defer rows.Close()

	var deactivatedCount int
	for rows.Next() {
		var discountID int
		var usageLimit, usageCount int
		err := rows.Scan(&discountID, &usageLimit, &usageCount)
		if err != nil {
			continue
		}

		// ปิดใช้งานส่วนลด
		_, err = db.Exec("UPDATE discount_codes SET active = 0 WHERE id = ?", discountID)
		if err == nil {
			fmt.Printf("🚫 Auto-deactivated discount: ID=%d, usage %d/%d\n",
				discountID, usageCount, usageLimit)
			deactivatedCount++
		}
	}

	if deactivatedCount > 0 {
		fmt.Printf("✅ Auto-deactivated %d discount codes\n", deactivatedCount)
	}
}
