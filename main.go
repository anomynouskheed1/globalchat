package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"globalchat/db"
	"globalchat/handlers"
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
		http.Error(
			w,
			fmt.Sprintf("Template file not found: %s", tmplName),
			http.StatusNotFound,
		)
		return
	}

	t, err := template.ParseFiles(filePath)
	if err != nil {
		http.Error(
			w,
			fmt.Sprintf("Template parse error: %v", err),
			http.StatusInternalServerError,
		)
		return
	}

	filename := filepath.Base(filePath)

	if err := t.ExecuteTemplate(w, filename, data); err != nil {
		_ = t.Execute(w, data)
	}
}

func main() {
	// -------------------------
	// DATABASE
	// -------------------------
	db.Init()
	loadTemplates()

	// -------------------------
	// STATIC FILES
	// -------------------------
	fs := http.FileServer(http.Dir("static"))
	http.Handle(
		"/static/",
		http.StripPrefix("/static/", fs),
	)

	// -------------------------
	// PAGE ROUTES
	// -------------------------
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			tmplName := strings.TrimPrefix(r.URL.Path, "/")

			if !strings.HasSuffix(tmplName, ".html") {
				tmplName += ".html"
			}

			// Protect the admin page.
			if tmplName == "admin.html" {
				cookie, err := r.Cookie("gc_session")
				if err != nil || cookie.Value == "" {
					http.Redirect(w, r, "/login", http.StatusSeeOther)
					return
				}

				user, err := db.GetSessionUser(cookie.Value)
				if err != nil || user == nil {
					http.Redirect(w, r, "/login", http.StatusSeeOther)
					return
				}

				adminEmail := strings.TrimSpace(os.Getenv("ADMIN_EMAIL"))
				if adminEmail == "" {
					adminEmail = "admin@globalchat.com"
				}

				if !strings.EqualFold(user.Email, adminEmail) {
					http.Error(
						w,
						"Forbidden: Admin access required",
						http.StatusForbidden,
					)
					return
				}

				// The current admin template expects these fields.
				// We will improve/populate them in the next step.
				renderPage(w, "admin.html", nil)
				return
			}

			renderPage(w, tmplName, nil)
			return
		}

		renderPage(w, "index.html", nil)
	})

	// -------------------------
	// REGISTRATION
	// -------------------------
	http.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_ = r.ParseForm()

			name := r.FormValue("name")
			email := r.FormValue("email")
			phone := r.FormValue("phone")
			password := r.FormValue("password")

			userID64, err := db.CreateUser(
				name,
				email,
				phone,
				password,
			)

			if err != nil {
				log.Println("Registration error:", err)
				http.Redirect(
					w,
					r,
					"/register?error=failed",
					http.StatusSeeOther,
				)
				return
			}

			userID := int(userID64)

			sessionToken := fmt.Sprintf(
				"sess_%d_%d",
				userID,
				time.Now().UnixNano(),
			)

			expiresAt := time.Now().Add(24 * time.Hour)

			err = db.CreateSession(
				sessionToken,
				userID,
				expiresAt,
			)

			if err != nil {
				log.Println("Session creation error:", err)
			}

			http.SetCookie(w, &http.Cookie{
				Name:     "gc_session",
				Value:    sessionToken,
				Path:     "/",
				HttpOnly: true,
				Expires:  expiresAt,
			})

			http.Redirect(
				w,
				r,
				"/screening",
				http.StatusSeeOther,
			)
			return
		}

		renderPage(w, "register.html", nil)
	})

	// -------------------------
	// LOGIN
	// -------------------------
	http.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_ = r.ParseForm()

			email := r.FormValue("email")
			password := r.FormValue("password")

			user, err := db.GetUserByEmail(email)

			if err != nil ||
				user == nil ||
				user.PasswordHash != password {

				log.Println("Login failed for:", email)

				http.Redirect(
					w,
					r,
					"/login?error=invalid",
					http.StatusSeeOther,
				)
				return
			}

			sessionToken := fmt.Sprintf(
				"sess_%d_%d",
				user.ID,
				time.Now().UnixNano(),
			)

			expiresAt := time.Now().Add(24 * time.Hour)

			err = db.CreateSession(
				sessionToken,
				user.ID,
				expiresAt,
			)

			if err != nil {
				log.Println("Session creation error:", err)

				http.Redirect(
					w,
					r,
					"/login?error=session",
					http.StatusSeeOther,
				)
				return
			}

			http.SetCookie(w, &http.Cookie{
				Name:     "gc_session",
				Value:    sessionToken,
				Path:     "/",
				HttpOnly: true,
				Expires:  expiresAt,
			})

			http.Redirect(
				w,
				r,
				"/dashboard",
				http.StatusSeeOther,
			)
			return
		}

		renderPage(w, "register.html", nil)
	})

	// -------------------------
	// ADMIN API ENDPOINTS
	// -------------------------

	// Get payment transactions.
	http.HandleFunc(
		"/api/admin/payments",
		handlers.AdminGetPaymentsHandler,
	)

	// Get membership/payment records.
	http.HandleFunc(
		"/api/admin/memberships",
		handlers.AdminGetMembershipsHandler,
	)

	// Manually activate a pending membership.
	http.HandleFunc(
		"/api/admin/memberships/activate",
		handlers.AdminActivateMembershipHandler,
	)

	// -------------------------
	// CLOUDPAY PAYMENT ENDPOINTS
	// -------------------------

	// STK Push
	http.HandleFunc(
		"/api/payment/cloudpay/stk",
		handlers.CloudPayPaymentHandler,
	)

	// CloudPay webhook
	http.HandleFunc(
		"/api/payment/cloudpay/webhook",
		handlers.CloudPayWebhookHandler,
	)

	// -------------------------
	// SERVER
	// -------------------------
	port := os.Getenv("PORT")

	if port == "" {
		port = "10000"
	}

	log.Println("GlobalChat server running on port " + port)

	log.Fatal(
		http.ListenAndServe(
			":"+port,
			nil,
		),
	)
}
