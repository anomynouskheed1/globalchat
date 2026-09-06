package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"globalchat/db"
)

var templates *template.Template

func loadTemplates() {
	var err error
	templates, err = template.ParseGlob("templates/*.html")
	if err != nil {
		log.Println("Template glob parsing notice:", err)
	}
}

func renderPage(w http.ResponseWriter, tmplName string, data interface{}) {
	if templates != nil {
		err := templates.ExecuteTemplate(w, tmplName, data)
		if err == nil {
			return
		}
	}

	filePath := filepath.Join("templates", tmplName)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		http.Error(w, fmt.Sprintf("Template file not found: %s", tmplName), http.StatusNotFound)
		return
	}

	t, err := template.ParseFiles(filePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Template parse error: %v", err), http.StatusInternalServerError)
		return
	}

	filename := filepath.Base(filePath)
	if err := t.ExecuteTemplate(w, filename, data); err != nil {
		_ = t.Execute(w, data)
	}
}

// -------------------------
// DATA STRUCTURES
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

type CloudPayTokenResponse struct {
	AccessToken string `json:"access_token"`
}

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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode >= 400 {
		return apiKey, nil
	}

	var tokResp CloudPayTokenResponse
	if err := json.Unmarshal(body, &tokResp); err != nil || tokResp.AccessToken == "" {
		return apiKey, nil
	}

	return tokResp.AccessToken, nil
}

// -------------------------
// CLOUDPAY PAYMENT HANDLER
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

	apiKey := os.Getenv("CLOUDPAY_API_KEY")
	merchantID := os.Getenv("CLOUDPAY_MERCHANT_ID")

	if apiKey == "" || merchantID == "" {
		log.Println("Missing CloudPay API key or Merchant ID environment variables")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "CloudPay payment gateway not configured"})
		return
	}

	token, err := getCloudPayAccessToken(apiKey, merchantID)
	if err != nil {
		log.Println("Failed to retrieve CloudPay access token:", err)
		token = apiKey
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
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "Failed to encode request payload"})
		return
	}

	cloudPayURL := "https://pay.cloud.or.ke/api/payments/mpesa/stkpush"

	reqHttp, err := http.NewRequest("POST", cloudPayURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "Failed to construct gateway request"})
		return
	}

	reqHttp.Header.Set("Content-Type", "application/json")
	reqHttp.Header.Set("Authorization", "Bearer "+token)

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
// CLOUDPAY WEBHOOK HANDLER
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

// -------------------------
// SERVER ENTRYPOINT
// -------------------------
func main() {
	db.Init()
	loadTemplates()

	// Static Files (CSS, JS, Images)
	fs := http.FileServer(http.Dir("static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	// Catch-all route for pages (e.g., /wallet -> wallet.html)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			tmplName := strings.TrimPrefix(r.URL.Path, "/")
			if !strings.HasSuffix(tmplName, ".html") {
				tmplName += ".html"
			}
			renderPage(w, tmplName, nil)
			return
		}
		renderPage(w, "index.html", nil)
	})

	// /register and /login both use register.html template
	// Registration Route: POST saves user, sets session cookie, redirects to /screening
	http.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_ = r.ParseForm()
			name := r.FormValue("name")
			email := r.FormValue("email")
			phone := r.FormValue("phone")
			password := r.FormValue("password")

			// 1. Create the user in your database
			user, err := db.RegisterUser(name, email, phone, password)
			if err != nil {
				log.Println("Registration error:", err)
				http.Redirect(w, r, "/register?error=failed", http.StatusSeeOther)
				return
			}

			// 2. Create a session for the new user
			sessionToken, err := db.CreateSession(user.ID)
			if err == nil {
				http.SetCookie(w, &http.Cookie{
					Name:     "gc_session",
					Value:    sessionToken,
					Path:     "/",
					HttpOnly: true,
					Expires:  time.Now().Add(24 * time.Hour),
				})
			}

			// 3. Redirect to screening as intended
			http.Redirect(w, r, "/screening", http.StatusSeeOther)
			return
		}
		renderPage(w, "register.html", nil)
	})

	// Login Route: POST authenticates user, sets session cookie, redirects straight to /dashboard
	http.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_ = r.ParseForm()
			email := r.FormValue("email")
			password := r.FormValue("password")

			// 1. Authenticate user credentials against database
			user, err := db.AuthenticateUser(email, password)
			if err != nil || user == nil {
				log.Println("Login failed for:", email)
				http.Redirect(w, r, "/login?error=invalid", http.StatusSeeOther)
				return
			}

			// 2. Create login session
			sessionToken, err := db.CreateSession(user.ID)
			if err != nil {
				log.Println("Session creation error:", err)
				http.Redirect(w, r, "/login?error=session", http.StatusSeeOther)
				return
			}

			// 3. Set session cookie
			http.SetCookie(w, &http.Cookie{
				Name:     "gc_session",
				Value:    sessionToken,
				Path:     "/",
				HttpOnly: true,
				Expires:  time.Now().Add(24 * time.Hour),
			})

			// 4. Redirect straight to Dashboard!
			http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
			return
		}
		renderPage(w, "register.html", nil)
	})

	// Payment Endpoints
	http.HandleFunc("/api/payment/cloudpay/stk", CloudPayPaymentHandler)
	http.HandleFunc("/api/payment/cloudpay/webhook", CloudPayWebhookHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "10000"
	}

	log.Println("GlobalChat server running on port " + port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
