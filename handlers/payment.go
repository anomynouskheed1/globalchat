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

// ---------------------------------------------------------
// MEMBERSHIP CONSTANTS
// ---------------------------------------------------------

const (
	MembershipPlan   = "Premium"
	MembershipAmount = 99
)

// ---------------------------------------------------------
// REQUEST / RESPONSE TYPES
// ---------------------------------------------------------

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

// ---------------------------------------------------------
// CLOUDPAY OAUTH RESPONSE
// ---------------------------------------------------------

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

// ---------------------------------------------------------
// CLOUDPAY OAUTH TOKEN
// ---------------------------------------------------------

func getCloudPayAccessToken(
	consumerKey,
	consumerSecret string,
) (string, error) {

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

	if resp.StatusCode < 200 ||
		resp.StatusCode >= 300 {

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

// ---------------------------------------------------------
// CLOUDPAY STK PUSH HANDLER
// ---------------------------------------------------------

func CloudPayPaymentHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	// -----------------------------------------------------
	// METHOD CHECK
	// -----------------------------------------------------

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

	// -----------------------------------------------------
	// SESSION CHECK
	// -----------------------------------------------------

	cookie, err := r.Cookie("gc_session")

	if err != nil || cookie.Value == "" {
		w.WriteHeader(
			http.StatusUnauthorized,
		)

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Unauthorized session",
		})

		return
	}

	user, err := db.GetSessionUser(cookie.Value)

	if err != nil || user == nil {
		w.WriteHeader(
			http.StatusUnauthorized,
		)

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Invalid session",
		})

		return
	}

	// -----------------------------------------------------
	// ACTIVE MEMBERSHIP CHECK
	// -----------------------------------------------------

	activeMembership := db.HasActiveMembership(user.ID)

	if activeMembership {
		w.WriteHeader(
			http.StatusConflict,
		)

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "You already have an active membership",
		})

		return
	}

	// -----------------------------------------------------
	// READ REQUEST
	// -----------------------------------------------------

	var paymentReq PaymentRequest

	if err := json.NewDecoder(
		r.Body,
	).Decode(&paymentReq); err != nil {

		w.WriteHeader(
			http.StatusBadRequest,
		)

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Invalid payload format",
		})

		return
	}

	// -----------------------------------------------------
	// PHONE VALIDATION
	// -----------------------------------------------------

	rawPhone := strings.TrimSpace(
		paymentReq.Phone,
	)

	if rawPhone == "" {
		w.WriteHeader(
			http.StatusBadRequest,
		)

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Phone number is required",
		})

		return
	}

	phone, err := db.NormalizePhone(rawPhone)

	if err != nil {
		w.WriteHeader(
			http.StatusBadRequest,
		)

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": err.Error(),
		})

		return
	}

	// -----------------------------------------------------
	// NEVER TRUST CLIENT AMOUNT OR PLAN
	// -----------------------------------------------------

	log.Printf(
		"CLOUDPAY PAYMENT REQUEST: user=%d phone=%s client_amount=%d client_plan=%s",
		user.ID,
		phone,
		paymentReq.Amount,
		paymentReq.Plan,
	)

	amount := MembershipAmount
	plan := MembershipPlan

	// -----------------------------------------------------
	// CLOUDPAY ENVIRONMENT
	// -----------------------------------------------------

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

	// -----------------------------------------------------
	// GET CLOUDPAY TOKEN
	// -----------------------------------------------------

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

	// -----------------------------------------------------
	// PAYMENT REFERENCE
	// -----------------------------------------------------

	reference := fmt.Sprintf(
		"MEMBERSHIP_%d_%d",
		user.ID,
		time.Now().UnixNano(),
	)

	// -----------------------------------------------------
	// CALLBACK URL
	// -----------------------------------------------------

	callbackURL :=
		appURL +
			"/api/payment/cloudpay/webhook"

	// -----------------------------------------------------
	// CLOUDPAY STK PAYLOAD
	// -----------------------------------------------------

	payload := map[string]interface{}{
		"merchant_id":  merchantID,
		"phone":        phone,
		"amount":       amount,
		"currency":     "KES",
		"reference":    reference,
		"user_id":      user.ID,
		"plan":         plan,
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

	// -----------------------------------------------------
	// CLOUDPAY STK URL
	// -----------------------------------------------------

	cloudPayURL :=
		"https://www.pay.cloud.or.ke/api/payments/mpesa/stkpush"

	// -----------------------------------------------------
	// CREATE STK REQUEST
	// -----------------------------------------------------

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

	// -----------------------------------------------------
	// SEND STK REQUEST
	// -----------------------------------------------------

	log.Printf(
		"SENDING CLOUDPAY REQUEST: Method=%s URL=%s amount=%d plan=%s",
		reqHTTP.Method,
		reqHTTP.URL.String(),
		amount,
		plan,
	)

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

	// -----------------------------------------------------
	// READ CLOUDPAY RESPONSE
	// -----------------------------------------------------

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

	// -----------------------------------------------------
	// PARSE RESPONSE
	// -----------------------------------------------------

	var cloudPayResponse CloudPayResponse

	if err := json.Unmarshal(
		responseBody,
		&cloudPayResponse,
	); err != nil {

		log.Println(
			"CloudPay returned invalid JSON:",
			err,
		)

		w.WriteHeader(
			http.StatusBadGateway,
		)

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "CloudPay returned an invalid response",
		})

		return
	}

	// -----------------------------------------------------
	// CLOUDPAY ERROR
	// -----------------------------------------------------

	if resp.StatusCode < 200 ||
		resp.StatusCode >= 300 {

		message :=
			"CloudPay transaction initiation failed"

		if cloudPayResponse.Message != "" {
			message = cloudPayResponse.Message
		}

		w.WriteHeader(
			resp.StatusCode,
		)

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": message,
			"status":  cloudPayResponse.Status,
		})

		return
	}

	// -----------------------------------------------------
	// GET REFERENCE
	// -----------------------------------------------------

	ref := strings.TrimSpace(
		cloudPayResponse.Ref,
	)

	if ref == "" {
		ref = reference
	}

	// -----------------------------------------------------
	// SAVE PENDING MEMBERSHIP
	// -----------------------------------------------------

	if err := db.CreatePendingMembership(
		user.ID,
		plan,
		ref,
	); err != nil {

		log.Println(
			"Failed to record pending membership:",
			err,
		)

		// The STK request may already have been accepted.
		// The reference is still returned for investigation.
	}

	// -----------------------------------------------------
	// SUCCESS RESPONSE
	// -----------------------------------------------------

	w.WriteHeader(
		http.StatusOK,
	)

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"message":   "KES 99 M-Pesa payment prompt sent. Check your phone.",
		"reference": ref,
		"status":    cloudPayResponse.Status,
		"amount":    MembershipAmount,
		"plan":      MembershipPlan,
	})
}

// ---------------------------------------------------------
// CLOUDPAY WEBHOOK HANDLER
// ---------------------------------------------------------

func CloudPayWebhookHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	// -----------------------------------------------------
	// METHOD CHECK
	// -----------------------------------------------------

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

	// -----------------------------------------------------
	// READ WEBHOOK
	// -----------------------------------------------------

	body, err := io.ReadAll(
		r.Body,
	)

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

	// -----------------------------------------------------
	// PARSE WEBHOOK
	// -----------------------------------------------------

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

	// -----------------------------------------------------
	// VALIDATE REFERENCE
	// -----------------------------------------------------

	payload.Reference = strings.TrimSpace(
		payload.Reference,
	)

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

	// -----------------------------------------------------
	// PAYMENT STATUS
	// -----------------------------------------------------

	status := strings.ToUpper(
		strings.TrimSpace(
			payload.Status,
		),
	)

	// -----------------------------------------------------
	// ONLY PROCESS SUCCESSFUL PAYMENTS
	// -----------------------------------------------------

	if status != "COMPLETED" &&
		status != "SUCCESS" {

		log.Printf(
			"CLOUDPAY PAYMENT NOT COMPLETED: ref=%s status=%s",
			payload.Reference,
			status,
		)

		w.WriteHeader(
			http.StatusOK,
		)

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Webhook received",
		})

		return
	}

	// -----------------------------------------------------
	// VALIDATE PAYMENT AMOUNT
	// -----------------------------------------------------

	if payload.Amount > 0 &&
		payload.Amount != MembershipAmount {

		log.Printf(
			"CLOUDPAY INVALID AMOUNT: ref=%s amount=%d expected=%d",
			payload.Reference,
			payload.Amount,
			MembershipAmount,
		)

		w.WriteHeader(
			http.StatusBadRequest,
		)

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Invalid membership payment amount",
		})

		return
	}

	// -----------------------------------------------------
	// VALIDATE PLAN IF PROVIDED
	// -----------------------------------------------------

	if payload.Plan != "" &&
		!strings.EqualFold(
			strings.TrimSpace(payload.Plan),
			MembershipPlan,
		) {

		log.Printf(
			"CLOUDPAY INVALID PLAN: ref=%s plan=%s",
			payload.Reference,
			payload.Plan,
		)

		w.WriteHeader(
			http.StatusBadRequest,
		)

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Invalid membership plan",
		})

		return
	}

	// -----------------------------------------------------
	// ACTIVATE PENDING MEMBERSHIP
	// -----------------------------------------------------

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

		w.WriteHeader(
			http.StatusInternalServerError,
		)

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Failed to process payment",
		})

		return
	}

	log.Println(
		"MEMBERSHIP ACTIVATED:",
		payload.Reference,
	)

	// -----------------------------------------------------
	// WEBHOOK SUCCESS
	// -----------------------------------------------------

	w.WriteHeader(
		http.StatusOK,
	)

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Membership payment processed successfully",
	})
}

// ---------------------------------------------------------
// MEMBERSHIP STATUS HANDLER
// ---------------------------------------------------------

func MembershipStatusHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	// -----------------------------------------------------
	// METHOD CHECK
	// -----------------------------------------------------

	if r.Method != http.MethodGet {

		w.WriteHeader(
			http.StatusMethodNotAllowed,
		)

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"active":  false,
			"message": "Method not allowed",
		})

		return
	}

	// -----------------------------------------------------
	// SESSION CHECK
	// -----------------------------------------------------

	cookie, err := r.Cookie("gc_session")

	if err != nil || cookie.Value == "" {

		w.WriteHeader(
			http.StatusUnauthorized,
		)

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"active":  false,
			"message": "Unauthorized",
		})

		return
	}

	user, err := db.GetSessionUser(
		cookie.Value,
	)

	if err != nil || user == nil {

		w.WriteHeader(
			http.StatusUnauthorized,
		)

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"active":  false,
			"message": "Invalid session",
		})

		return
	}

	// -----------------------------------------------------
	// CHECK MEMBERSHIP
	// -----------------------------------------------------

	active := db.HasActiveMembership(user.ID)

	// -----------------------------------------------------
	// RESPONSE
	// -----------------------------------------------------

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"active": active,
	})
}
