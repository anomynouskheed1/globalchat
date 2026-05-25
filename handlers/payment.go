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

func MpesaPaymentHandler(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodPost {

		http.Error(
			w,
			"Method not allowed",
			http.StatusMethodNotAllowed,
		)

		return
	}

	var req PaymentRequest

	err := json.NewDecoder(r.Body).Decode(&req)

	if err != nil {

		http.Error(
			w,
			"Invalid request body",
			http.StatusBadRequest,
		)

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
		"public_key": os.Getenv("INTASEND_PUBLIC_KEY"),
		"currency":   "KES",
		"amount":     req.Amount,
		"phone_number": "+" + phone,
		"email":      "customer@globalchat.com",
		"api_ref":    "GLOBALCHAT_MEMBERSHIP",
	}

	jsonPayload, _ := json.Marshal(payload)

	log.Println("INTASEND PAYLOAD:")
	log.Println(string(jsonPayload))

	request, err := http.NewRequest(
	"POST",
	"https://payment.intasend.com/api/v1/payment/mpesa-stk-push/",
	bytes.NewBuffer(jsonPayload),
)

	if err != nil {

		http.Error(
			w,
			"Failed to create request",
			http.StatusInternalServerError,
		)

		return
	}

	request.Header.Set(
		"Content-Type",
		"application/json",
	)

log.Println("SECRET KEY:", os.Getenv("INTASEND_SECRET_KEY"))

request.Header.Set(
	"Authorization",
	"Bearer "+os.Getenv("INTASEND_SECRET_KEY"),
)

	client := &http.Client{}

	response, err := client.Do(request)

	if err != nil {

		log.Println(err)

		http.Error(
			w,
			"Failed to contact IntaSend",
			http.StatusInternalServerError,
		)

		return
	}

	defer response.Body.Close()

	body, _ := io.ReadAll(response.Body)

	log.Println("INTASEND RESPONSE:")
	log.Println(string(body))

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	w.Write(body)
}