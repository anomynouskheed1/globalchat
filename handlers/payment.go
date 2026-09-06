package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
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

type CloudPayTokenResponse struct {
	AccessToken string `json:"access_token"`
}

// Helper function to get OAuth token with response status checking
func getCloudPayAccessToken(consumerKey, consumerSecret string) (string, error) {
	tokenURL := "https://www.pay.cloud.or.ke/api/oauth/token"

	req, err := http.NewRequest(http.MethodPost, tokenURL, nil)
	if err != nil {
		return "", fmt.Errorf("request creation failed: %w", err)
	}

	req.SetBasicAuth(consumerKey, consumerSecret)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{
		Timeout: 15 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read token response: %w", err)
	}

	log.Printf("CLOUDPAY TOKEN STATUS: %d", resp.StatusCode)
	log.Printf("CLOUDPAY TOKEN RESPONSE: %s", string(body))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf(
			"CloudPay authentication failed: HTTP %d",
			resp.StatusCode,
		)
	}

	var tokenResponse CloudPayTokenResponse

	if err := json.Unmarshal(body, &tokenResponse); err != nil {
		return "", fmt.Errorf("invalid token response: %w", err)
	}

	if tokenResponse.AccessToken == "" {
		return "", fmt.Errorf("CloudPay returned an empty access token")
	}

	return tokenResponse.AccessToken, nil
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

	phone := strings.TrimSpace(req.Phone)
	phone = strings.ReplaceAll(phone, " ", "")
	if strings.HasPrefix(phone, "+") {
		phone = strings.Replace(phone, "+", "", 1)
	}
	if strings.HasPrefix(phone, "07") || strings.HasPrefix(phone, "01") {
		phone = "254" + phone[1:]
	}

	consumerKey := os.Getenv("CLOUDPAY_CONSUMER_KEY")
	consumerSecret := os.Getenv("CLOUDPAY_CONSUMER_SECRET")
	merchantID := os.Getenv("CLOUDPAY_MERCHANT_ID")

	if consumerKey == "" || consumerSecret == "" || merchantID == "" {
		log.Println("Missing CloudPay credentials")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "CloudPay payment gateway not configured",
		})
		return
	}

	token, err := getCloudPayAccessToken(consumerKey, consumerSecret)
	if err != nil {
		log.Println("Failed to obtain CloudPay access token:", err)
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "Failed to authenticate with CloudPay gateway"})
		return
	}

	payload := map[string]interface{}{
		"phone":       phone,
		"amount":      req.Amount,
		"description": "GlobalChat " + req.Plan,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "Failed to serialize payment request"})
		return
	}

	cloudPayURL := "https://www.pay.cloud.or.ke/api/payments/mpesa/stkpush"

	reqHttp, err := http.NewRequest("POST", cloudPayURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "Failed to construct gateway request"})
		return
	}

	reqHttp.Header.Set("Content-Type", "application/json")
	reqHttp.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			log.Printf(
				"CLOUDPAY REDIRECT: Method=%s URL=%s",
				req.Method,
				req.URL.String(),
			)

			for i, previous := range via {
				log.Printf(
					"CLOUDPAY REDIRECT HISTORY %d: Method=%s URL=%s",
					i,
					previous.Method,
					previous.URL.String(),
				)
			}

			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(reqHttp)
	if err != nil {
		log.Println("CLOUDPAY STK REQUEST FAILED:", err)
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "Failed to connect to CloudPay server"})
		return
	}
	defer resp.Body.Close()

	responseBody, _ := io.ReadAll(resp.Body)
	log.Printf(
		"CLOUDPAY FINAL RESPONSE: Method=%s URL=%s Status=%d",
		resp.Request.Method,
		resp.Request.URL.String(),
		resp.StatusCode,
	)

	log.Println("CLOUDPAY STATUS CODE:", resp.StatusCode)
	log.Println("CLOUDPAY RESPONSE:", string(responseBody))

	var data CloudPayResponse
	_ = json.Unmarshal(responseBody, &data)

	if resp.StatusCode >= 400 {
		w.WriteHeader(resp.StatusCode)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "CloudPay transaction initiation failed"})
		return
	}

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

	if payload.Status == "COMPLETED" || payload.Status == "SUCCESS" {
		log.Println("CLOUDPAY PAYMENT SUCCESSFUL FOR REF:", payload.Reference)

		if err := db.ActivateMembership(payload.Reference); err != nil {
			log.Println("Failed to activate membership:", err)
		}
	}

	w.WriteHeader(http.StatusOK)
}
