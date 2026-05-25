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

// INTASEND RESPONSE
type IntaSendResponse struct {
	Invoice struct {
		InvoiceID string `json:"invoice_id"`
		State     string `json:"state"`
	} `json:"invoice"`

	InvoiceID string `json:"invoice_id"`
	State     string `json:"state"`
}

// WEBHOOK PAYLOAD
type WebhookPayload struct {
	InvoiceID string `json:"invoice_id"`
	State     string `json:"state"`
	Value     string `json:"value"`
	Challenge string `json:"challenge"`
}

func MpesaPaymentHandler(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// PARSE REQUEST
	var req PaymentRequest

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// FORMAT PHONE
	phone := strings.TrimSpace(req.Phone)
	phone = strings.ReplaceAll(phone, " ", "")

	if strings.HasPrefix(phone, "+254") {
		phone = strings.Replace(phone, "+", "", 1)
	}

	if strings.HasPrefix(phone, "07") {
		phone = "254" + phone[1:]
	}

	log.Println("FORMATTED PHONE:", phone)

	// INTASEND PAYLOAD
	payload := map[string]interface{}{
		"public_key":   os.Getenv("INTASEND_PUBLIC_KEY"),
		"currency":     "KES",
		"method":       "M-PESA",
		"amount":       req.Amount,
		"phone_number": phone,
		"api_ref":      "GLOBALCHAT_MEMBERSHIP",
		"name":         "GlobalChat User",
		"email":        "customer@globalchat.com",
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, "Failed to create payload", http.StatusInternalServerError)
		return
	}

	// LIVE INTASEND API
	request, err := http.NewRequest(
		"POST",
		"https://payment.intasend.com/api/v1/payment/mpesa-stk-push/",
		bytes.NewBuffer(jsonPayload),
	)

	if err != nil {
		http.Error(w, "Failed to create request", http.StatusInternalServerError)
		return
	}

	// HEADERS
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+os.Getenv("INTASEND_SECRET_KEY"))

	// SEND REQUEST
	client := &http.Client{}

	response, err := client.Do(request)
	if err != nil {
		log.Println("INTASEND CONNECTION ERROR:", err)
		http.Error(w, "Failed to contact IntaSend", http.StatusInternalServerError)
		return
	}

	defer response.Body.Close()

	// READ RESPONSE
	body, err := io.ReadAll(response.Body)
	if err != nil {
		http.Error(w, "Failed to read response", http.StatusInternalServerError)
		return
	}

	// DEBUG RESPONSE
	log.Println("RAW RESPONSE:", string(body))

	// PARSE RESPONSE
	var intasendData IntaSendResponse

	err = json.Unmarshal(body, &intasendData)
	if err != nil {
		log.Println("JSON PARSE ERROR:", err)

		http.Error(
			w,
			"Invalid response from payment gateway",
			http.StatusInternalServerError,
		)

		return
	}

	// HANDLE BOTH RESPONSE FORMATS
	invoiceID := intasendData.Invoice.InvoiceID
	state := intasendData.Invoice.State

	if invoiceID == "" {
		invoiceID = intasendData.InvoiceID
	}

	if state == "" {
		state = intasendData.State
	}

	log.Printf(
		"STK Push initiated! Invoice: %s, State: %s\n",
		invoiceID,
		state,
	)

	// FRONTEND RESPONSE
	frontendResponse := map[string]interface{}{
		"success":    true,
		"message":    "STK push sent! Please check your phone and enter PIN.",
		"invoice_id": invoiceID,
		"status":     state,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	json.NewEncoder(w).Encode(frontendResponse)
}

// WEBHOOK HANDLER
func IntaSendWebhookHandler(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	log.Println("WEBHOOK RAW:", string(body))

	var payload WebhookPayload

	err = json.Unmarshal(body, &payload)
	if err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// INTASEND CHALLENGE VERIFICATION
	if payload.Challenge != "" {

		w.Header().Set("Content-Type", "application/json")

		json.NewEncoder(w).Encode(map[string]string{
			"challenge": payload.Challenge,
		})

		return
	}

	log.Printf(
		"Webhook received! Invoice: %s is now %s\n",
		payload.InvoiceID,
		payload.State,
	)

	// PAYMENT SUCCESS
	if payload.State == "COMPLETE" ||
		payload.State == "COMPLETED" {

		log.Printf(
			"💰 PAYMENT VERIFIED! Amount KES %s for Invoice %s\n",
			payload.Value,
			payload.InvoiceID,
		)

		/*
		   TODO:
		   ACTIVATE USER MEMBERSHIP HERE

		   Example:
		   UPDATE users SET membership = true WHERE invoice_id = payload.InvoiceID
		*/
	}

	w.WriteHeader(http.StatusOK)
}