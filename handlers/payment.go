package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"globalchat/db" // Adjust if your module name in go.mod is different
)

type PaymentRequest struct {
	Phone  string `json:"phone"`
	Amount int    `json:"amount"`
	Plan   string `json:"plan"`
}

type CloudPayResponse struct {
	Success bool   `json:"success"`
	Ref     string `json:"reference"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

type CloudPayWebhookPayload struct {
	Reference string `json:"reference"`
	Status    string `json:"status"` // "COMPLETED" or "SUCCESS"
	Amount    int    `json:"amount"`
	UserID    int    `json:"user_id"`
	Plan      string `json:"plan"`
}

// -------------------------
// CLOUDPAY STK PUSH HANDLER
// -------------------------
func CloudPayPaymentHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "Method not allowed"})
		return
	}

	// Fetch user session to bind payment to user ID
	cookie, err := r.Cookie("session")
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "Unauthorized session"})
		return
	}

	user, err := db.GetSessionUser(cookie.Value)
	if err != nil || user == nil {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "Invalid session"})
		return
	}

	var req PaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "Invalid payload format"})
		return
	}

	// Phone formatting (254XXXXXXXXX)
	phone := strings.TrimSpace(req.Phone)
	phone = strings.ReplaceAll(phone, " ", "")
	if strings.HasPrefix(phone, "+") {
		phone = strings.Replace(phone, "+", "", 1)
	}
	if strings.HasPrefix(phone, "07") || strings.HasPrefix(phone, "01") {
		phone = "254" + phone[1:]
	}

	apiKey := os.Getenv("CLOUDPAY_API_KEY")
	merchantID := os.Getenv("CLOUDPAY_MERCHANT_ID")

	if apiKey == "" || merchantID == "" {
		log.Println("Missing CloudPay API key or Merchant ID environment variables")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "CloudPay payment gateway not configured"})
		return
	}

	payload := map[string]interface{}{
		"merchant_id":  merchantID,
		"phone_number": phone,
		"amount":       req.Amount,
		"currency":     "KES",
		"reference":    "MEMBERSHIP_" + user.Email,
		"user_id":      user.ID,
		"plan":         req.Plan,
		"callback_url": os.Getenv("APP_URL") + "/api/payment/cloudpay/webhook",
	}

	bodyBytes, _ := json.Marshal(payload)
	cloudPayURL := "https://api.cloudpay.co.ke/v1/stkpush"

	reqHttp, err := http.NewRequest("POST", cloudPayURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "Failed to construct gateway request"})
		return
	}

	reqHttp.Header.Set("Content-Type", "application/json")
	reqHttp.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Do(reqHttp)
	if err != nil {
		log.Println("CLOUDPAY STK REQUEST FAILED:", err)
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "Failed to connect to CloudPay server"})
		return
	}
	defer resp.Body.Close()

	responseBody, _ := io.ReadAll(resp.Body)
	log.Println("CLOUDPAY STATUS CODE:", resp.StatusCode)
	log.Println("CLOUDPAY RESPONSE:", string(responseBody))

	var data CloudPayResponse
	_ = json.Unmarshal(responseBody, &data)

	if resp.StatusCode >= 400 {
		w.WriteHeader(resp.StatusCode)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "CloudPay transaction initiation failed"})
		return
	}

	// Record pending membership record in SQLite database
	ref := data.Ref
	if ref == "" {
		ref = "CLOUDPAY_" + phone
	}
	_ = db.CreatePendingMembership(user.ID, req.Plan, ref)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"message":   "CloudPay M-Pesa prompt sent. Check your phone.",
		"reference": ref,
		"status":    data.Status,
	})
}

// -------------------------
// CLOUDPAY WEBHOOK
// -------------------------
func CloudPayWebhookHandler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading body", http.StatusBadRequest)
		return
	}

	log.Println("CLOUDPAY WEBHOOK RECEIVED:", string(body))

	var payload CloudPayWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	// Verify payment completion
	if payload.Status == "COMPLETED" || payload.Status == "SUCCESS" {
		log.Println("CLOUDPAY PAYMENT SUCCESSFUL FOR REF:", payload.Reference)

		// Activate user membership in SQLite
		err := db.ActivateMembership(payload.Reference)
		if err != nil {
			log.Println("Failed to activate membership:", err)
		}
	}

	w.WriteHeader(http.StatusOK)
}
