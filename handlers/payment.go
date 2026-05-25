package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
)

type PaymentRequest struct {
	Phone  string `json:"phone"`
	Amount int    `json:"amount"`
	Plan   string `json:"plan"`
}

type IntaSendResponse struct {
	Invoice struct {
		InvoiceID string `json:"invoice_id"`
		State     string `json:"state"`
	} `json:"invoice"`
}

type WebhookPayload struct {
	InvoiceID string `json:"invoice_id"`
	State     string `json:"state"`
	Value     string `json:"value"`
	Challenge string `json:"challenge"`
}

// -------------------------
// STK PUSH HANDLER
// -------------------------
func MpesaPaymentHandler(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req PaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// -------------------------
	// PHONE FORMAT (CRITICAL)
	// -------------------------
	phone := strings.TrimSpace(req.Phone)
	phone = strings.ReplaceAll(phone, " ", "")

	if strings.HasPrefix(phone, "+") {
		phone = strings.Replace(phone, "+", "", 1)
	}
	if strings.HasPrefix(phone, "07") {
		phone = "254" + phone[1:]
	}

	log.Println("FORMATTED PHONE:", phone)

	// -------------------------
	// ENV CHECK
	// -------------------------
	publicKey := os.Getenv("INTASEND_PUBLIC_KEY")
	secretKey := os.Getenv("INTASEND_SECRET_KEY")

	if publicKey == "" || secretKey == "" {
		log.Println("Missing IntaSend keys")
		http.Error(w, "Payment not configured", http.StatusInternalServerError)
		return
	}

	// -------------------------
	// CLEAN LIVE PAYLOAD
	// -------------------------
	payload := map[string]interface{}{
		"public_key":   publicKey,
		"amount":       req.Amount,
		"currency":     "KES",
		"phone_number": phone,
		"api_ref":      "GLOBALCHAT_MEMBERSHIP",
		"name":         "GlobalChat User",
		"email":        "customer@globalchat.com",
	}

	bodyBytes, _ := json.Marshal(payload)

	// -------------------------
	// LIVE ENDPOINT
	// -------------------------
	url := "https://payment.intasend.com/api/v1/payment/mpesa-stk-push/"

	reqHttp, err := http.NewRequest("POST", url, bytes.NewBuffer(bodyBytes))
	if err != nil {
		http.Error(w, "Request error", http.StatusInternalServerError)
		return
	}

	reqHttp.Header.Set("Content-Type", "application/json")
	reqHttp.Header.Set("Authorization", "Bearer "+secretKey)

	client := &http.Client{}
	resp, err := client.Do(reqHttp)
	if err != nil {
		log.Println("STK REQUEST FAILED:", err)
		http.Error(w, "Payment gateway error", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	responseBody, _ := io.ReadAll(resp.Body)

	log.Println("INTASEND STATUS:", resp.StatusCode)
	log.Println("INTASEND RESPONSE:", string(responseBody))

	// -------------------------
	// RESPONSE PARSE
	// -------------------------
	var data IntaSendResponse
	_ = json.Unmarshal(responseBody, &data)

	invoiceID := data.Invoice.InvoiceID
	state := data.Invoice.State

	// -------------------------
	// CLEAN RESPONSE TO FRONTEND
	// -------------------------
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    true,
		"message":    "M-Pesa prompt sent. Check your phone.",
		"invoice_id": invoiceID,
		"status":     state,
	})
}

// -------------------------
// WEBHOOK
// -------------------------
func IntaSendWebhookHandler(w http.ResponseWriter, r *http.Request) {

	body, _ := io.ReadAll(r.Body)

	log.Println("WEBHOOK:", string(body))

	var payload WebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if payload.Challenge != "" {
		json.NewEncoder(w).Encode(map[string]string{
			"challenge": payload.Challenge,
		})
		return
	}

	if payload.State == "COMPLETED" || payload.State == "COMPLETE" {
		log.Println("PAYMENT SUCCESS:", payload.InvoiceID)

		// TODO:
		// activate membership
		// credit wallet
	}

	w.WriteHeader(http.StatusOK)
}