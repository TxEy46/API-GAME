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
// ฟังก์ชันหลักสำหรับจัดการรหัสส่วนลดโดยผู้ดูแลระบบ
func AdminDiscountHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("🎯 AdminDiscountHandler: %s %s\n", r.Method, r.URL.Path)

	// Extract ID จาก URL ถ้ามี
	// ตัวอย่าง URL: /admin/discounts/123 → id = 123
	var id int
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) >= 3 {
		if parsedID, err := strconv.Atoi(pathParts[2]); err == nil {
			id = parsedID
		}
	}

	// กำหนดการทำงานตาม HTTP Method
	switch r.Method {
	case "GET":
		if id > 0 {
			getDiscountByID(w, r, id) // ดึงส่วนลดเฉพาะ ID
		} else {
			getAllDiscounts(w, r) // ดึงส่วนลดทั้งหมด
		}
	case "POST":
		createDiscount(w, r) // สร้างส่วนลดใหม่
	case "PUT":
		if id > 0 {
			updateDiscountWithReset(w, r, id) // อัพเดทส่วนลด + รีเซ็ตการใช้งาน
		} else {
			utils.JSONError(w, "Discount ID required", http.StatusBadRequest)
		}
	case "DELETE":
		if id > 0 {
			deleteDiscountWithCleanup(w, r, id) // ลบส่วนลด + ลบประวัติการใช้งาน
		} else {
			utils.JSONError(w, "Discount ID required", http.StatusBadRequest)
		}
	default:
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// GET /admin/discounts - ดึงส่วนลดทั้งหมด
func getAllDiscounts(w http.ResponseWriter, r *http.Request) {
	// เรียกตรวจสอบอัตโนมัติก่อนดึงข้อมูล (รันใน goroutine เพื่อไม่ให้ block request)
	go autoDeactivateDiscounts()
	go autoDeleteAllExpiredAndInactiveDiscounts()
	fmt.Println("🔍 Fetching all discount codes")

	// ดึงข้อมูลส่วนลดทั้งหมดพร้อมจำนวนการใช้งาน
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

	// อ่านข้อมูลส่วนลดทีละแถว
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

		// สร้าง object ส่วนลด
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

		// ตั้งค่าวันที่ถ้ามีค่า
		if startDate.Valid {
			discount["start_date"] = startDate.String
		}
		if endDate.Valid {
			discount["end_date"] = endDate.String
		}

		discounts = append(discounts, discount)
		count++
	}

	// ตรวจสอบข้อผิดพลาดระหว่างการอ่านข้อมูล
	if err = rows.Err(); err != nil {
		fmt.Printf("❌ Error during rows iteration: %v\n", err)
		utils.JSONError(w, "Error processing discount codes", http.StatusInternalServerError)
		return
	}

	fmt.Printf("✅ Total discount codes found: %d\n", count)

	// ส่ง response กลับ
	utils.JSONResponse(w, map[string]interface{}{
		"discounts": discounts,
		"total":     count,
	}, http.StatusOK)
}

// GET /admin/discounts/{id} - ดึงส่วนลดโดย ID
func getDiscountByID(w http.ResponseWriter, r *http.Request, id int) {
	fmt.Printf("🔍 Fetching discount code: ID=%d\n", id)

	// ตัวแปรสำหรับเก็บข้อมูลส่วนลด
	var code, discountType string
	var value, minTotal float64
	var startDate, endDate, createdAt sql.NullString
	var usageLimit sql.NullInt64
	var singleUsePerUser, active bool
	var usageCount int

	// ดึงข้อมูลส่วนลดจากฐานข้อมูล
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

	// สร้าง object ส่วนลด
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

	// ตั้งค่าวันที่ถ้ามีค่า
	if startDate.Valid {
		discount["start_date"] = startDate.String
	}
	if endDate.Valid {
		discount["end_date"] = endDate.String
	}

	fmt.Printf("✅ Discount code found: ID=%d, Code=%s, Usage Count=%d\n", id, code, usageCount)
	utils.JSONResponse(w, discount, http.StatusOK)
}

// POST /admin/discounts - สร้างส่วนลดใหม่
func createDiscount(w http.ResponseWriter, r *http.Request) {
	fmt.Println("➕ Creating new discount code")

	// โครงสร้างสำหรับเก็บข้อมูลจาก request
	var req struct {
		Code             string  `json:"code"`                // รหัสส่วนลด
		Type             string  `json:"type"`                // ประเภท (percent/fixed)
		Value            float64 `json:"value"`               // ค่าส่วนลด
		MinTotal         float64 `json:"min_total"`           // ยอดซื้อขั้นต่ำ
		StartDate        *string `json:"start_date"`          // วันที่เริ่มใช้งาน
		EndDate          *string `json:"end_date"`            // วันที่สิ้นสุด
		UsageLimit       *int    `json:"usage_limit"`         // จำนวนครั้งที่ใช้ได้
		SingleUsePerUser bool    `json:"single_use_per_user"` // ใช้ได้คนละครั้งเดียว
		Active           bool    `json:"active"`              // สถานะใช้งาน
	}

	// แปลง JSON request body เป็น struct
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.JSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validation ข้อมูล
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

	// Parse dates จาก string เป็น time.Time
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

	// ส่ง response สำเร็จกลับ
	utils.JSONResponse(w, map[string]interface{}{
		"message": "Discount code created successfully",
		"id":      id,
	}, http.StatusCreated)
}

// PUT /admin/discounts/{id} - อัพเดทส่วนลด + รีเซ็ตการใช้งานเมื่อเปิดใช้งานใหม่
func updateDiscountWithReset(w http.ResponseWriter, r *http.Request, id int) {
	fmt.Printf("✏️ Updating discount code with reset: ID=%d\n", id)

	// โครงสร้างสำหรับเก็บข้อมูลจาก request
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

	// แปลง JSON request body เป็น struct
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.JSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validation ข้อมูล
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

	// เริ่ม transaction เพื่อความปลอดภัยของข้อมูล
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

	// Parse dates จาก string เป็น time.Time
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

	// ตรวจสอบว่ามีแถวถูกอัพเดทจริงหรือไม่
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		tx.Rollback()
		utils.JSONError(w, "Discount code not found", http.StatusNotFound)
		return
	}

	// ยืนยัน transaction
	if err := tx.Commit(); err != nil {
		utils.JSONError(w, "Error completing update", http.StatusInternalServerError)
		return
	}

	fmt.Printf("✅ Discount code updated: ID=%d, Code=%s, Active=%t\n", id, req.Code, req.Active)

	// ส่ง response สำเร็จกลับ
	utils.JSONResponse(w, map[string]interface{}{
		"message":     "Discount code updated successfully",
		"id":          id,
		"active":      req.Active,
		"reset_usage": resetUsage, // บอกว่าทำการรีเซ็ตการใช้งานหรือไม่
	}, http.StatusOK)
}

// DELETE /admin/discounts/{id} - ลบส่วนลด + ลบประวัติการใช้งานทั้งหมด
func deleteDiscountWithCleanup(w http.ResponseWriter, r *http.Request, id int) {
	fmt.Printf("🗑️ Deleting discount code with cleanup: ID=%d\n", id)

	// เริ่ม transaction เพื่อความปลอดภัยของข้อมูล
	tx, err := db.Begin()
	if err != nil {
		utils.JSONError(w, "Error starting transaction", http.StatusInternalServerError)
		return
	}

	// 1. ลบข้อมูลใน purchases ที่ใช้ discount นี้ก่อน
	_, err = tx.Exec("UPDATE purchases SET discount_code_id = NULL WHERE discount_code_id = ?", id)
	if err != nil {
		tx.Rollback()
		fmt.Printf("❌ Error updating purchases: %v\n", err)
		utils.JSONError(w, "Error updating related purchases", http.StatusInternalServerError)
		return
	}
	fmt.Printf("✅ Updated purchases for discount ID: %d\n", id)

	// 2. ลบประวัติการใช้งานใน user_discount_codes
	_, err = tx.Exec("DELETE FROM user_discount_codes WHERE discount_code_id = ?", id)
	if err != nil {
		tx.Rollback()
		fmt.Printf("❌ Error deleting discount usage history: %v\n", err)
		utils.JSONError(w, "Error deleting discount usage history", http.StatusInternalServerError)
		return
	}
	fmt.Printf("✅ Deleted usage history for discount ID: %d\n", id)

	// 3. ลบ discount code
	result, err := tx.Exec("DELETE FROM discount_codes WHERE id = ?", id)
	if err != nil {
		tx.Rollback()
		fmt.Printf("❌ Error deleting discount code: %v\n", err)
		utils.JSONError(w, "Error deleting discount code", http.StatusInternalServerError)
		return
	}

	// ตรวจสอบว่ามีแถวถูกลบจริงหรือไม่
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		tx.Rollback()
		utils.JSONError(w, "Discount code not found", http.StatusNotFound)
		return
	}

	// ยืนยัน transaction
	if err := tx.Commit(); err != nil {
		utils.JSONError(w, "Error completing deletion", http.StatusInternalServerError)
		return
	}

	fmt.Printf("✅ Discount code deleted: ID=%d\n", id)

	// ส่ง response สำเร็จกลับ
	utils.JSONResponse(w, map[string]interface{}{
		"message":      "Discount code deleted successfully",
		"id":           id,
		"cleanup_done": true,
	}, http.StatusOK)
}

// ฟังก์ชันสำหรับตรวจสอบและลบส่วนลดที่ inactive อัตโนมัติ
func autoDeactivateDiscounts() {
	fmt.Println("🔄 Checking for inactive discount codes to delete...")

	// ค้นหาส่วนลดที่ inactive (active = 0)
	rows, err := db.Query(`
        SELECT dc.id, dc.code, dc.usage_limit, COUNT(udc.id) as usage_count
        FROM discount_codes dc
        LEFT JOIN user_discount_codes udc ON dc.id = udc.discount_code_id
        WHERE dc.active = 0
        GROUP BY dc.id
    `)
	if err != nil {
		fmt.Printf("❌ Error checking inactive discounts: %v\n", err)
		return
	}
	defer rows.Close()

	var deletedCount int

	// อ่านข้อมูลส่วนลดที่ inactive และลบทิ้ง
	for rows.Next() {
		var discountID int
		var discountCode string
		var usageLimit sql.NullInt64
		var usageCount int

		err := rows.Scan(&discountID, &discountCode, &usageLimit, &usageCount)
		if err != nil {
			continue
		}

		// เริ่ม transaction สำหรับการลบ
		tx, err := db.Begin()
		if err != nil {
			fmt.Printf("❌ Error starting transaction for discount ID %d: %v\n", discountID, err)
			continue
		}

		// 1. อัพเดท purchases ที่ใช้ discount นี้ให้เป็น NULL
		_, err = tx.Exec("UPDATE purchases SET discount_code_id = NULL WHERE discount_code_id = ?", discountID)
		if err != nil {
			tx.Rollback()
			fmt.Printf("❌ Error updating purchases for discount ID %d: %v\n", discountID, err)
			continue
		}

		// 2. ลบประวัติการใช้งานใน user_discount_codes
		_, err = tx.Exec("DELETE FROM user_discount_codes WHERE discount_code_id = ?", discountID)
		if err != nil {
			tx.Rollback()
			fmt.Printf("❌ Error deleting usage history for discount ID %d: %v\n", discountID, err)
			continue
		}

		// 3. ลบ discount code
		_, err = tx.Exec("DELETE FROM discount_codes WHERE id = ?", discountID)
		if err != nil {
			tx.Rollback()
			fmt.Printf("❌ Error deleting discount code ID %d: %v\n", discountID, err)
			continue
		}

		// ยืนยัน transaction
		if err := tx.Commit(); err != nil {
			fmt.Printf("❌ Error committing transaction for discount ID %d: %v\n", discountID, err)
			continue
		}

		fmt.Printf("🗑️ Auto-deleted inactive discount: ID=%d, Code=%s, Usage=%d\n",
			discountID, discountCode, usageCount)
		deletedCount++
	}

	if deletedCount > 0 {
		fmt.Printf("✅ Auto-deleted %d inactive discount codes\n", deletedCount)
	} else {
		fmt.Println("✅ No inactive discount codes to delete")
	}
}

// ฟังก์ชันสำหรับลบส่วนลดทั้งหมดที่ควรลบ (inactive, หมดอายุ, ใช้ครบ)
func autoDeleteAllExpiredAndInactiveDiscounts() {
	fmt.Println("🔄 Checking for all discount codes to delete...")

	// ค้นหาส่วนลดที่ควรลบทั้งหมด (inactive, หมดอายุ, หรือใช้ครบ)
	rows, err := db.Query(`
        SELECT dc.id, dc.code, dc.active, 
               DATE_FORMAT(dc.end_date, '%Y-%m-%d') as end_date,
               dc.usage_limit, COUNT(udc.id) as usage_count
        FROM discount_codes dc
        LEFT JOIN user_discount_codes udc ON dc.id = udc.discount_code_id
        WHERE dc.active = 0 
           OR (dc.end_date IS NOT NULL AND dc.end_date < CURDATE())
           OR (dc.usage_limit IS NOT NULL AND dc.active = 1)
        GROUP BY dc.id
        HAVING dc.active = 0 
           OR (dc.end_date IS NOT NULL AND dc.end_date < CURDATE())
           OR (dc.usage_limit IS NOT NULL AND usage_count >= dc.usage_limit)
    `)
	if err != nil {
		fmt.Printf("❌ Error checking discounts to delete: %v\n", err)
		return
	}
	defer rows.Close()

	var deletedCount int
	var inactiveCount int
	var expiredCount int
	var usageLimitCount int

	// อ่านข้อมูลส่วนลดที่ต้องลบ
	for rows.Next() {
		var discountID int
		var discountCode string
		var active bool
		var endDate sql.NullString
		var usageLimit sql.NullInt64
		var usageCount int

		err := rows.Scan(&discountID, &discountCode, &active, &endDate, &usageLimit, &usageCount)
		if err != nil {
			continue
		}

		// ตรวจสอบเหตุผลที่ต้องลบ
		reason := ""
		if !active {
			reason = "inactive"
			inactiveCount++
		} else if endDate.Valid {
			if endTime, _ := time.Parse("2006-01-02", endDate.String); endTime.Before(time.Now()) {
				reason = "expired"
				expiredCount++
			}
		} else if usageLimit.Valid && usageCount >= int(usageLimit.Int64) {
			reason = "usage limit reached"
			usageLimitCount++
		}

		// เริ่ม transaction สำหรับการลบ
		tx, err := db.Begin()
		if err != nil {
			continue
		}

		// 1. อัพเดท purchases ที่ใช้ discount นี้ให้เป็น NULL
		tx.Exec("UPDATE purchases SET discount_code_id = NULL WHERE discount_code_id = ?", discountID)

		// 2. ลบประวัติการใช้งานใน user_discount_codes
		tx.Exec("DELETE FROM user_discount_codes WHERE discount_code_id = ?", discountID)

		// 3. ลบ discount code
		_, err = tx.Exec("DELETE FROM discount_codes WHERE id = ?", discountID)
		if err != nil {
			tx.Rollback()
			continue
		}

		// ยืนยัน transaction
		if err := tx.Commit(); err != nil {
			continue
		}

		fmt.Printf("🗑️ Auto-deleted discount: ID=%d, Code=%s, Reason=%s\n",
			discountID, discountCode, reason)
		deletedCount++
	}

	if deletedCount > 0 {
		fmt.Printf("✅ Auto-deleted %d discount codes (inactive: %d, expired: %d, usage limit: %d)\n",
			deletedCount, inactiveCount, expiredCount, usageLimitCount)
	} else {
		fmt.Println("✅ No discount codes to delete")
	}
}
