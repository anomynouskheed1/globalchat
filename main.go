package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"globalchat/db"
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
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "Method not allowed"})
		return
	}

	// Fetch user session using gc_session cookie name
	cookie, err := r.Cookie("gc_session")
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "Unauthorized session"})
		return
	}

	user, err := db.GetSessionUser(cookie.Value)
	if err != nil || user == nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "Invalid session"})
		return
	}

	var req PaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "Invalid payload format"})
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
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "CloudPay payment gateway not configured"})
		return
	}

	payload := map[string]interface{}{
		"merchant_id":  merchantID,
		"phone":        phone,
		"amount":       req.Amount,
		"currency":     "KES",
		"reference":    "MEMBERSHIP_" + user.Email,
		"user_id":      user.ID,
		"plan":         req.Plan,
		"callback_url": os.Getenv("APP_URL") + "/api/payment/cloudpay/webhook",
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "Failed to format JSON payload"})
		return
	}

	// Live API endpoint URL
	cloudPayURL := "https://pay.cloud.or.ke/api/payments/mpesa/stkpush"

	reqHttp, err := http.NewRequest("POST", cloudPayURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "Failed to construct gateway request"})
		return
	}

	reqHttp.Header.Set("Content-Type", "application/json")
	reqHttp.Header.Set("Authorization", "Bearer "+apiKey)

	// Set 30 second timeout for network round-trips
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(reqHttp)
	if err != nil {
		log.Println("CLOUDPAY STK REQUEST FAILED:", err)
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "Failed to connect to CloudPay server"})
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
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "CloudPay transaction initiation failed"})
		return
	}

	// Record pending membership record in SQLite database
	ref := data.Ref
	if ref == "" {
		ref = "CLOUDPAY_" + phone
	}
	if err := db.CreatePendingMembership(user.ID, req.Plan, ref); err != nil {
		log.Println("Failed to record pending membership:", err)
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
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

// -------------------------
// APPLICATION ENTRY POINT
// -------------------------
func main() {
	// Initialize Database connection
	db.Init()

	// Serve Static Files
	fs := http.FileServer(http.Dir("static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	// Register Payment Endpoints
	http.HandleFunc("/api/payment/cloudpay/stk", CloudPayPaymentHandler)
	http.HandleFunc("/api/payment/cloudpay/webhook", CloudPayWebhookHandler)

	// Determine Port
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Println("GlobalChat server running on port " + port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
