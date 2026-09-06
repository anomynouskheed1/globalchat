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

// Helper struct for OAuth Token response
type CloudPayTokenResponse struct {
	AccessToken string `json:"access_token"`
}

// Helper function to get OAuth token
func getCloudPayAccessToken(apiKey, merchantID string) (string, error) {
	tokenURL := "https://pay.cloud.or.ke/api/oauth/token"
	req, err := http.NewRequest("POST", tokenURL, nil)
	if err != nil {
		return "", err
	}

	req.SetBasicAuth(apiKey, merchantID)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var tokResp CloudPayTokenResponse
	if err := json.Unmarshal(body, &tokResp); err != nil {
		return "", err
	}

	if tokResp.AccessToken == "" {
		// Fallback: If your dashboard supplies a raw bearer key directly, use the apiKey
		return apiKey, nil
	}

	return tokResp.AccessToken, nil
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

	cookie, err := r.Cookie("gc_session")
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

	// Fetch token
	token, err := getCloudPayAccessToken(apiKey, merchantID)
	if err != nil {
		log.Println("Failed to obtain CloudPay access token:", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "Failed to authenticate with CloudPay gateway"})
		return
	}

	// Updated payload matching CloudPay API specs ("phone", "amount", "reference")
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

	bodyBytes, _ := json.Marshal(payload)
	cloudPayURL := "https://pay.cloud.or.ke/api/payments/mpesa/stkpush"

	reqHttp, err := http.NewRequest("POST", cloudPayURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "Failed to construct gateway request"})
		return
	}

	reqHttp.Header.Set("Content-Type", "application/json")
	reqHttp.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 30 * time.Second}
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

	if payload.Status == "COMPLETED" || payload.Status == "SUCCESS" {
		log.Println("CLOUDPAY PAYMENT SUCCESSFUL FOR REF:", payload.Reference)

		err := db.ActivateMembership(payload.Reference)
		if err != nil {
			log.Println("Failed to activate membership:", err)
		}
	}

	w.WriteHeader(http.StatusOK)
}
