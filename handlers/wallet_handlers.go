package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"go-api-game/utils"
	"net/http"
	"strconv"
)

// WalletHandler handles wallet balance retrieval
// ฟังก์ชันสำหรับดึงยอดเงินในกระเป๋าเงินของผู้ใช้
func WalletHandler(w http.ResponseWriter, r *http.Request) {
	// ดึง User-ID จาก header (ถูกตั้งค่าโดย middleware การยืนยันตัวตน)
	userID := r.Header.Get("User-ID")

	var balance float64
	// ดึงยอดเงินในกระเป๋าเงินจากฐานข้อมูล
	err := db.QueryRow("SELECT wallet_balance FROM users WHERE id = ?", userID).Scan(&balance)
	if err != nil {
		utils.JSONError(w, "Error fetching wallet", http.StatusInternalServerError)
		return
	}

	// ส่ง response กลับพร้อมยอดเงิน
	utils.JSONResponse(w, map[string]interface{}{
		"balance": balance,
	}, http.StatusOK)
}

// DepositHandler handles wallet deposits
// ฟังก์ชันสำหรับฝากเงินเข้าสู่กระเป๋าเงิน
func DepositHandler(w http.ResponseWriter, r *http.Request) {
	// ตรวจสอบว่าเป็นเมธอด POST หรือไม่
	if r.Method != "POST" {
		utils.JSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// ดึง User-ID จาก header
	userID := r.Header.Get("User-ID")

	// โครงสร้างสำหรับเก็บข้อมูลจาก request
	var req struct {
		Amount float64 `json:"amount"` // จำนวนเงินที่ต้องการฝาก
	}

	// แปลง JSON request body เป็น struct
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.JSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// ตรวจสอบว่าจำนวนเงินเป็นบวก
	if req.Amount <= 0 {
		utils.JSONError(w, "Amount must be positive", http.StatusBadRequest)
		return
	}

	// เริ่มต้น transaction เพื่อความปลอดภัยของข้อมูล
	tx, err := db.Begin()
	if err != nil {
		utils.JSONError(w, "Error starting transaction", http.StatusInternalServerError)
		return
	}

	// อัพเดทยอดเงินในกระเป๋าเงิน
	_, err = tx.Exec("UPDATE users SET wallet_balance = wallet_balance + ? WHERE id = ?",
		req.Amount, userID)
	if err != nil {
		tx.Rollback()
		utils.JSONError(w, "Error updating wallet", http.StatusInternalServerError)
		return
	}

	// บันทึกประวัติธุรกรรม
	_, err = tx.Exec(`
		INSERT INTO user_transactions (user_id, type, amount, description) 
		VALUES (?, 'deposit', ?, ?)
	`, userID, req.Amount, fmt.Sprintf("Deposit: $%.2f", req.Amount))
	if err != nil {
		tx.Rollback()
		utils.JSONError(w, "Error recording transaction", http.StatusInternalServerError)
		return
	}

	// ยืนยัน transaction
	if err := tx.Commit(); err != nil {
		utils.JSONError(w, "Error committing transaction", http.StatusInternalServerError)
		return
	}

	// ส่ง response สำเร็จกลับ
	utils.JSONResponse(w, map[string]interface{}{
		"message": "Deposit successful",
		"amount":  req.Amount,
	}, http.StatusOK)
}

// TransactionsHandler handles user transaction history
// ฟังก์ชันสำหรับดึงประวัติธุรกรรมของผู้ใช้
func TransactionsHandler(w http.ResponseWriter, r *http.Request) {
	// ดึง User-ID จาก header
	userID := r.Header.Get("User-ID")

	fmt.Printf("🔍 Transactions request for user ID: %s\n", userID)

	// ตรวจสอบว่ามี User-ID หรือไม่
	if userID == "" {
		utils.JSONError(w, "User ID not found", http.StatusUnauthorized)
		return
	}

	// แปลง User-ID เป็นตัวเลข
	userIDInt, err := strconv.Atoi(userID)
	if err != nil {
		utils.JSONError(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	// ใช้ DATE_FORMAT เพื่อได้ string โดยตรงจาก MySQL
	rows, err := db.Query(`
		SELECT type, amount, description, 
		       DATE_FORMAT(created_at, '%Y-%m-%d %H:%i:%s') as created_date
		FROM user_transactions 
		WHERE user_id = ? 
		ORDER BY created_at DESC
	`, userIDInt)

	if err != nil {
		fmt.Printf("❌ Error executing transactions query: %v\n", err)
		utils.JSONError(w, "Error fetching transactions", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var transactions []map[string]interface{}

	// อ่านข้อมูลธุรกรรมทีละแถว
	for rows.Next() {
		var txType string
		var amount float64
		var description string
		var createdAt string // ใช้ string ธรรมดา

		if err := rows.Scan(&txType, &amount, &description, &createdAt); err != nil {
			fmt.Printf("❌ Error scanning transaction row: %v\n", err)
			continue
		}

		fmt.Printf("✅ Transaction found: Type=%s, Amount=%.2f\n", txType, amount)

		// สร้าง object ธุรกรรม
		transactions = append(transactions, map[string]interface{}{
			"type":        txType,
			"amount":      amount,
			"description": description,
			"date":        createdAt,
		})
	}

	// ตรวจสอบว่า transactions ไม่เป็น nil
	if transactions == nil {
		transactions = []map[string]interface{}{}
	}

	fmt.Printf("✅ Returning %d transactions\n", len(transactions))
	utils.JSONResponse(w, transactions, http.StatusOK)
}

// PurchaseHistoryHandler handles user purchase history
// ฟังก์ชันสำหรับดึงประวัติการซื้อของผู้ใช้
func PurchaseHistoryHandler(w http.ResponseWriter, r *http.Request) {
	// ดึง User-ID จาก header
	userID := r.Header.Get("User-ID")

	fmt.Printf("🔍 Purchase history request for user ID: %s\n", userID)

	// ตรวจสอบว่ามี User-ID หรือไม่
	if userID == "" {
		utils.JSONError(w, "User ID not found", http.StatusUnauthorized)
		return
	}

	// แปลง User-ID เป็นตัวเลข
	userIDInt, err := strconv.Atoi(userID)
	if err != nil {
		utils.JSONError(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	fmt.Printf("🔍 Querying purchase history for user ID: %d\n", userIDInt)

	// ใช้ DATE_FORMAT เพื่อแปลง DATETIME เป็น string โดยตรง
	rows, err := db.Query(`
		SELECT p.id, p.total_amount, p.final_amount, 
		       DATE_FORMAT(p.purchase_date, '%Y-%m-%d %H:%i:%s') as purchase_date,
		       dc.code as discount_code
		FROM purchases p
		LEFT JOIN discount_codes dc ON p.discount_code_id = dc.id
		WHERE p.user_id = ?
		ORDER BY p.purchase_date DESC
	`, userIDInt)

	if err != nil {
		fmt.Printf("❌ Error fetching purchase history: %v\n", err)
		utils.JSONError(w, "Error fetching purchase history: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var purchases []map[string]interface{}
	count := 0

	// อ่านข้อมูลการซื้อทีละแถว
	for rows.Next() {
		var id int
		var totalAmount, finalAmount float64
		var purchaseDate string
		var discountCode sql.NullString

		if err := rows.Scan(&id, &totalAmount, &finalAmount, &purchaseDate, &discountCode); err != nil {
			fmt.Printf("❌ Error scanning purchase history row: %v\n", err)
			continue
		}

		// สร้าง object การซื้อ
		purchase := map[string]interface{}{
			"id":             id,
			"total_amount":   totalAmount,
			"final_amount":   finalAmount,
			"purchase_date":  purchaseDate,
			"discount_saved": totalAmount - finalAmount, // คำนวณส่วนลดที่ได้รับ
		}

		// จัดการรหัสส่วนลด (อาจเป็น NULL)
		if discountCode.Valid {
			purchase["discount_code"] = discountCode.String
		} else {
			purchase["discount_code"] = nil
		}

		purchases = append(purchases, purchase)
		count++
		fmt.Printf("✅ Purchase found: ID=%d, Total=%.2f, Final=%.2f\n", id, totalAmount, finalAmount)
	}

	// ตรวจสอบข้อผิดพลาดระหว่างการอ่านข้อมูล
	if err = rows.Err(); err != nil {
		fmt.Printf("❌ Error during purchase history rows iteration: %v\n", err)
		utils.JSONError(w, "Error processing purchase history", http.StatusInternalServerError)
		return
	}

	fmt.Printf("✅ Total purchases found: %d\n", count)

	// ตรวจสอบว่า purchases ไม่เป็น nil
	if purchases == nil {
		purchases = []map[string]interface{}{}
	}

	utils.JSONResponse(w, purchases, http.StatusOK)
}

// TransactionStatsHandler handles transaction statistics
// ฟังก์ชันสำหรับดึงสถิติธุรกรรม (สำหรับ admin)
func TransactionStatsHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Println("📊 Fetching transaction statistics")

	stats := make(map[string]interface{})

	// ยอดรวมทั้งหมด (ฝากและซื้อ)
	var totalDeposit, totalPurchase float64
	err := db.QueryRow("SELECT COALESCE(SUM(amount), 0) FROM user_transactions WHERE type = 'deposit'").Scan(&totalDeposit)
	if err != nil {
		fmt.Printf("❌ Error getting deposit total: %v\n", err)
	}
	err = db.QueryRow("SELECT COALESCE(SUM(amount), 0) FROM user_transactions WHERE type = 'purchase'").Scan(&totalPurchase)
	if err != nil {
		fmt.Printf("❌ Error getting purchase total: %v\n", err)
	}

	// จำนวนธุรกรรมแยกตามประเภท
	var depositCount, purchaseCount int
	err = db.QueryRow("SELECT COUNT(*) FROM user_transactions WHERE type = 'deposit'").Scan(&depositCount)
	if err != nil {
		fmt.Printf("❌ Error counting deposits: %v\n", err)
	}
	err = db.QueryRow("SELECT COUNT(*) FROM user_transactions WHERE type = 'purchase'").Scan(&purchaseCount)
	if err != nil {
		fmt.Printf("❌ Error counting purchases: %v\n", err)
	}

	// ธุรกรรมล่าสุด
	var latestTransaction string
	err = db.QueryRow("SELECT DATE_FORMAT(created_at, '%Y-%m-%d %H:%i:%s') FROM user_transactions ORDER BY created_at DESC LIMIT 1").Scan(&latestTransaction)
	if err != nil && err != sql.ErrNoRows {
		fmt.Printf("❌ Error getting latest transaction: %v\n", err)
	}

	// ยอดรวมรายวัน (7 วันที่ผ่านมา)
	dailyStats := make([]map[string]interface{}, 0)
	rows, err := db.Query(`
		SELECT 
			DATE(created_at) as date,
			COUNT(*) as count,
			COALESCE(SUM(CASE WHEN type = 'deposit' THEN amount ELSE 0 END), 0) as deposit_total,
			COALESCE(SUM(CASE WHEN type = 'purchase' THEN amount ELSE 0 END), 0) as purchase_total
		FROM user_transactions 
		WHERE created_at >= DATE_SUB(NOW(), INTERVAL 7 DAY)
		GROUP BY DATE(created_at)
		ORDER BY date DESC
	`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var date string
			var count int
			var depositTotal, purchaseTotal float64
			if err := rows.Scan(&date, &count, &depositTotal, &purchaseTotal); err == nil {
				dailyStats = append(dailyStats, map[string]interface{}{
					"date":           date,
					"count":          count,
					"deposit_total":  depositTotal,
					"purchase_total": purchaseTotal,
				})
			}
		}
	}

	// รวมสถิติทั้งหมด
	stats["total_deposit"] = totalDeposit
	stats["total_purchase"] = totalPurchase
	stats["deposit_count"] = depositCount
	stats["purchase_count"] = purchaseCount
	stats["latest_transaction"] = latestTransaction
	stats["total_transactions"] = depositCount + purchaseCount
	stats["daily_stats"] = dailyStats

	fmt.Printf("✅ Transaction statistics loaded\n")

	// ส่ง response กลับพร้อมสถิติ
	utils.JSONResponse(w, map[string]interface{}{
		"stats":   stats,
		"success": true,
	}, http.StatusOK)
}
