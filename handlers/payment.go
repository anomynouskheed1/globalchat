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

// -------------------------
// REQUEST / RESPONSE TYPES
// -------------------------

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
	Status    string `json:"status"`
	Amount    int    `json:"amount"`
	UserID    int    `json:"user_id"`
	Plan      string `json:"plan"`
}

// CloudPay OAuth response:
//
// {
//   "status": "success",
//   "message": "Access token issued...",
//   "data": {
//       "access_token": "...",
//       "token_type": "Bearer",
//       "expires_in": 3600,
//       "accountId": 248,
//       "scope": "payments"
//   }
// }

type CloudPayTokenResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`

	Data struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
		AccountID   int    `json:"accountId"`
		Scope       string `json:"scope"`
	} `json:"data"`
}

// -------------------------
// CLOUDPAY OAUTH TOKEN
// -------------------------

func getCloudPayAccessToken(consumerKey, consumerSecret string) (string, error) {
	tokenURL := "https://www.pay.cloud.or.ke/api/oauth/token"

	req, err := http.NewRequest(
		http.MethodPost,
		tokenURL,
		nil,
	)
	if err != nil {
		return "", fmt.Errorf(
			"failed to create OAuth request: %w",
			err,
		)
	}

	// CloudPay OAuth uses:
	//
	// username = Consumer Key
	// password = Consumer Secret
	//
	// This becomes:
	// Authorization: Basic base64(key:secret)

	req.SetBasicAuth(
		consumerKey,
		consumerSecret,
	)

	req.Header.Set(
		"Accept",
		"application/json",
	)

	client := &http.Client{
		Timeout: 15 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf(
			"CloudPay OAuth request failed: %w",
			err,
		)
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf(
			"failed to read CloudPay OAuth response: %w",
			err,
		)
	}

	log.Printf(
		"CLOUDPAY TOKEN STATUS: %d",
		resp.StatusCode,
	)

	// IMPORTANT:
	// Do NOT log the response body here because it contains
	// the live access token.

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf(
			"CLOUDPAY TOKEN ERROR RESPONSE: %s",
			string(body),
		)

		return "", fmt.Errorf(
			"CloudPay authentication failed: HTTP %d",
			resp.StatusCode,
		)
	}

	var tokenResponse CloudPayTokenResponse

	if err := json.Unmarshal(
		body,
		&tokenResponse,
	); err != nil {
		return "", fmt.Errorf(
			"invalid CloudPay token response: %w",
			err,
		)
	}

	if tokenResponse.Data.AccessToken == "" {
		return "", fmt.Errorf(
			"CloudPay returned an empty access token",
		)
	}

	log.Printf(
		"CLOUDPAY TOKEN RECEIVED: type=%s expires_in=%d account=%d scope=%s",
		tokenResponse.Data.TokenType,
		tokenResponse.Data.ExpiresIn,
		tokenResponse.Data.AccountID,
		tokenResponse.Data.Scope,
	)

	return tokenResponse.Data.AccessToken, nil
}

// -------------------------
// PHONE NORMALIZATION
// -------------------------

func normalizeKenyanPhone(phone string) string {
	phone = strings.TrimSpace(phone)

	// Remove spaces
	phone = strings.ReplaceAll(phone, " ", "")

	// Remove dashes
	phone = strings.ReplaceAll(phone, "-", "")

	// Convert +254XXXXXXXXX -> 254XXXXXXXXX
	if strings.HasPrefix(phone, "+") {
		phone = strings.TrimPrefix(phone, "+")
	}

	// Convert 07XXXXXXXX -> 2547XXXXXXXX
	if strings.HasPrefix(phone, "07") {
		phone = "254" + phone[1:]
	}

	// Convert 01XXXXXXXX -> 2541XXXXXXXX
	if strings.HasPrefix(phone, "01") {
		phone = "254" + phone[1:]
	}

	return phone
}

// -------------------------
// CLOUDPAY STK PUSH HANDLER
// -------------------------

func CloudPayPaymentHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	// -------------------------
	// METHOD CHECK
	// -------------------------

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Method not allowed",
		})

		return
	}

	// -------------------------
	// SESSION CHECK
	// -------------------------

	cookie, err := r.Cookie("gc_session")
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Unauthorized session",
		})

		return
	}

	user, err := db.GetSessionUser(cookie.Value)

	if err != nil || user == nil {
		w.WriteHeader(http.StatusUnauthorized)

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Invalid session",
		})

		return
	}

	// -------------------------
	// READ PAYMENT REQUEST
	// -------------------------

	var paymentReq PaymentRequest

	if err := json.NewDecoder(r.Body).Decode(&paymentReq); err != nil {
		w.WriteHeader(http.StatusBadRequest)

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Invalid payload format",
		})

		return
	}

	// Basic validation

	if paymentReq.Phone == "" {
		w.WriteHeader(http.StatusBadRequest)

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Phone number is required",
		})

		return
	}

	if paymentReq.Amount <= 0 {
		w.WriteHeader(http.StatusBadRequest)

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Invalid payment amount",
		})

		return
	}

	if paymentReq.Plan == "" {
		w.WriteHeader(http.StatusBadRequest)

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Payment plan is required",
		})

		return
	}

	// -------------------------
	// NORMALIZE PHONE
	// -------------------------

	phone := normalizeKenyanPhone(paymentReq.Phone)

	log.Printf(
		"CLOUDPAY PAYMENT: user=%d phone=%s amount=%d plan=%s",
		user.ID,
		phone,
		paymentReq.Amount,
		paymentReq.Plan,
	)

	// -------------------------
	// CLOUDPAY ENVIRONMENT
	// -------------------------

	consumerKey := os.Getenv(
		"CLOUDPAY_CONSUMER_KEY",
	)

	consumerSecret := os.Getenv(
		"CLOUDPAY_CONSUMER_SECRET",
	)

	merchantID := os.Getenv(
		"CLOUDPAY_MERCHANT_ID",
	)

	appURL := strings.TrimRight(
		os.Getenv("APP_URL"),
		"/",
	)

	if consumerKey == "" ||
		consumerSecret == "" ||
		merchantID == "" {

		log.Println(
			"Missing CloudPay OAuth credentials",
		)

		w.WriteHeader(
			http.StatusInternalServerError,
		)

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "CloudPay payment gateway not configured",
		})

		return
	}

	if appURL == "" {
		log.Println(
			"Missing APP_URL environment variable",
		)

		w.WriteHeader(
			http.StatusInternalServerError,
		)

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Application URL not configured",
		})

		return
	}

	// -------------------------
	// GET CLOUDPAY TOKEN
	// -------------------------

	token, err := getCloudPayAccessToken(
		consumerKey,
		consumerSecret,
	)

	if err != nil {
		log.Println(
			"Failed to obtain CloudPay access token:",
			err,
		)

		w.WriteHeader(
			http.StatusBadGateway,
		)

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Failed to authenticate with CloudPay gateway",
		})

		return
	}

	// -------------------------
	// PAYMENT REFERENCE
	// -------------------------

	reference := fmt.Sprintf(
		"MEMBERSHIP_%d_%d",
		user.ID,
		time.Now().Unix(),
	)

	// -------------------------
	// CALLBACK URL
	// -------------------------

	callbackURL :=
		appURL +
			"/api/payment/cloudpay/webhook"

	// -------------------------
	// CLOUDPAY STK PAYLOAD
	// -------------------------

	payload := map[string]interface{}{
		"merchant_id":  merchantID,
		"phone":        phone,
		"amount":       paymentReq.Amount,
		"currency":     "KES",
		"reference":    reference,
		"user_id":      user.ID,
		"plan":         paymentReq.Plan,
		"callback_url": callbackURL,
	}

	bodyBytes, err := json.Marshal(
		payload,
	)

	if err != nil {
		log.Println(
			"Failed to encode CloudPay payload:",
			err,
		)

		w.WriteHeader(
			http.StatusInternalServerError,
		)

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Failed to encode payment request",
		})

		return
	}

	// -------------------------
	// CLOUDPAY STK URL
	// -------------------------

	cloudPayURL :=
		"https://www.pay.cloud.or.ke/api/payments/mpesa/stkpush"

	// -------------------------
	// CREATE STK REQUEST
	// -------------------------

	reqHTTP, err := http.NewRequest(
		http.MethodPost,
		cloudPayURL,
		bytes.NewBuffer(bodyBytes),
	)

	if err != nil {
		log.Println(
			"Failed to construct CloudPay request:",
			err,
		)

		w.WriteHeader(
			http.StatusInternalServerError,
		)

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Failed to construct gateway request",
		})

		return
	}

	reqHTTP.Header.Set(
		"Content-Type",
		"application/json",
	)

	reqHTTP.Header.Set(
		"Accept",
		"application/json",
	)

	reqHTTP.Header.Set(
		"Authorization",
		"Bearer "+token,
	)

	// -------------------------
	// LOG REQUEST
	// -------------------------

	log.Printf(
		"SENDING CLOUDPAY REQUEST: Method=%s, URL=%s",
		reqHTTP.Method,
		reqHTTP.URL.String(),
	)

	// -------------------------
	// SEND STK REQUEST
	// -------------------------

	client := &http.Client{
		Timeout: 30 * time.Second,

		CheckRedirect: func(
			req *http.Request,
			via []*http.Request,
		) error {

			log.Printf(
				"CLOUDPAY REDIRECT: Method=%s URL=%s",
				req.Method,
				req.URL.String(),
			)

			// Prevent automatic redirect.
			//
			// This is important because redirects can change
			// how POST requests are handled.

			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Do(reqHTTP)

	if err != nil {
		log.Println(
			"CLOUDPAY STK REQUEST FAILED:",
			err,
		)

		w.WriteHeader(
			http.StatusBadGateway,
		)

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Failed to connect to CloudPay server",
		})

		return
	}

	defer resp.Body.Close()

	// -------------------------
	// READ CLOUDPAY RESPONSE
	// -------------------------

	responseBody, err := io.ReadAll(
		resp.Body,
	)

	if err != nil {
		log.Println(
			"Failed to read CloudPay STK response:",
			err,
		)

		w.WriteHeader(
			http.StatusBadGateway,
		)

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Failed to read CloudPay response",
		})

		return
	}

	log.Printf(
		"CLOUDPAY STATUS CODE: %d",
		resp.StatusCode,
	)

	log.Printf(
		"CLOUDPAY RESPONSE: %s",
		string(responseBody),
	)

	// -------------------------
	// PARSE RESPONSE
	// -------------------------

	var cloudPayResponse CloudPayResponse

	if err := json.Unmarshal(
		responseBody,
		&cloudPayResponse,
	); err != nil {

		log.Println(
			"CloudPay returned invalid JSON:",
			err,
		)

		if resp.StatusCode >= 400 {
			w.WriteHeader(resp.StatusCode)

			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "CloudPay payment request failed",
			})

			return
		}
	}

	// -------------------------
	// CLOUDPAY ERROR
	// -------------------------

	if resp.StatusCode < 200 ||
		resp.StatusCode >= 300 {

		w.WriteHeader(
			resp.StatusCode,
		)

		message :=
			"CloudPay transaction initiation failed"

		if cloudPayResponse.Message != "" {
			message = cloudPayResponse.Message
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": message,
			"status":  cloudPayResponse.Status,
		})

		return
	}

	// -------------------------
	// GET REFERENCE
	// -------------------------

	ref := cloudPayResponse.Ref

	if ref == "" {
		ref = reference
	}

	// -------------------------
	// SAVE PENDING MEMBERSHIP
	// -------------------------

	if err := db.CreatePendingMembership(
		user.ID,
		paymentReq.Plan,
		ref,
	); err != nil {

		log.Println(
			"Failed to record pending membership:",
			err,
		)

		// Don't fail the payment response here.
		//
		// The STK request may already have been
		// successfully accepted by CloudPay.
	}

	// -------------------------
	// SUCCESS RESPONSE
	// -------------------------

	w.WriteHeader(
		http.StatusOK,
	)

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"message":   "CloudPay M-Pesa prompt sent. Check your phone.",
		"reference": ref,
		"status":    cloudPayResponse.Status,
	})
}

// -------------------------
// CLOUDPAY WEBHOOK HANDLER
// -------------------------

func CloudPayWebhookHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	// -------------------------
	// METHOD CHECK
	// -------------------------

	if r.Method != http.MethodPost {
		w.WriteHeader(
			http.StatusMethodNotAllowed,
		)

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Method not allowed",
		})

		return
	}

	// -------------------------
	// READ WEBHOOK
	// -------------------------

	body, err := io.ReadAll(r.Body)

	if err != nil {
		http.Error(
			w,
			"Error reading body",
			http.StatusBadRequest,
		)

		return
	}

	log.Println(
		"CLOUDPAY WEBHOOK RECEIVED:",
		string(body),
	)

	// -------------------------
	// PARSE WEBHOOK
	// -------------------------

	var payload CloudPayWebhookPayload

	if err := json.Unmarshal(
		body,
		&payload,
	); err != nil {

		http.Error(
			w,
			"Invalid JSON payload",
			http.StatusBadRequest,
		)

		return
	}

	// -------------------------
	// VALIDATE REFERENCE
	// -------------------------

	if payload.Reference == "" {
		log.Println(
			"CLOUDPAY WEBHOOK MISSING REFERENCE",
		)

		w.WriteHeader(
			http.StatusBadRequest,
		)

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Missing payment reference",
		})

		return
	}

	// -------------------------
	// PAYMENT SUCCESS
	// -------------------------

	status := strings.ToUpper(
		strings.TrimSpace(payload.Status),
	)

	if status == "COMPLETED" ||
		status == "SUCCESS" {

		log.Println(
			"CLOUDPAY PAYMENT SUCCESSFUL FOR REF:",
			payload.Reference,
		)

		if err := db.ActivateMembership(
			payload.Reference,
		); err != nil {

			log.Println(
				"Failed to activate membership:",
				err,
			)

			// Return 500 so CloudPay knows our webhook
			// processing failed.

			w.WriteHeader(
				http.StatusInternalServerError,
			)

			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "Failed to process payment",
			})

			return
		}
	}

	// -------------------------
	// WEBHOOK SUCCESS
	// -------------------------

	w.WriteHeader(
		http.StatusOK,
	)

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Webhook received",
	})
}
