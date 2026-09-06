package main

import (
	"html/template"
	"log"
	"net/http"
	"os"

	"github.com/joho/godotenv"

	"globalchat/db"
	"globalchat/handlers"
)

// RENDER FUNCTION (Parses specific page template)
func render(w http.ResponseWriter, tmplFile string, data interface{}) {
	// Parse the specific template file directly from the templates folder
	tmpl, err := template.ParseFiles("templates/" + tmplFile)
	if err != nil {
		log.Println("Template parsing error for", tmplFile, ":", err)
		http.Error(w, "Template Error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	err = tmpl.Execute(w, data)
	if err != nil {
		log.Println("Template render error for", tmplFile, ":", err)
	}
}

// INIT ENV (SAFE FOR PROD)
func init() {
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found, running in production mode")
	}
}

func main() {
	// 1. INITIALIZE DATABASE & MIGRATIONS
	db.Init()

	// 2. STATIC FILES
	fs := http.FileServer(http.Dir("static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	// 3. PUBLIC PAGES & AUTH
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		render(w, "index.html", nil)
	})

	http.HandleFunc("/register", handlers.HandleRegister)
	http.HandleFunc("/login", handlers.HandleLogin)
	http.HandleFunc("/logout", handlers.HandleLogout)

	http.HandleFunc("/screening", func(w http.ResponseWriter, r *http.Request) {
		render(w, "screening.html", nil)
	})

	http.HandleFunc("/membership", func(w http.ResponseWriter, r *http.Request) {
		render(w, "membership.html", nil)
	})

	// 4. SECURED DASHBOARD PAGES
	renderSecuredPage := func(page string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			user := handlers.GetCurrentUser(r)
			render(w, page, user)
		}
	}

	http.HandleFunc("/dashboard", handlers.RequireAuth(renderSecuredPage("dashboard.html")))
	http.HandleFunc("/wallet", handlers.RequireAuth(renderSecuredPage("wallet.html")))
	http.HandleFunc("/rewards", handlers.RequireAuth(renderSecuredPage("rewards.html")))
	http.HandleFunc("/tasks", handlers.RequireAuth(renderSecuredPage("tasks.html")))
	http.HandleFunc("/chat", handlers.RequireAuth(renderSecuredPage("chat.html")))
	http.HandleFunc("/survey", handlers.RequireAuth(renderSecuredPage("survey.html")))
	http.HandleFunc("/profile", handlers.RequireAuth(renderSecuredPage("profile.html")))
	http.HandleFunc("/leaderboard", handlers.RequireAuth(renderSecuredPage("leaderboard.html")))

	// Admin Page with Restricted Access
	http.HandleFunc("/admin", handlers.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		user := handlers.GetCurrentUser(r)

		adminEmail := os.Getenv("ADMIN_EMAIL")
		if adminEmail == "" {
			adminEmail = "admin@globalchat.com"
		}
		if user.Email != adminEmail {
			http.Error(w, "Forbidden: Admin access required", http.StatusForbidden)
			return
		}

		render(w, "admin.html", user)
	}))

	// 5. API ENDPOINTS
	// Payment Routes
	http.HandleFunc("/api/payment/cloudpay/stk", handlers.CloudPayPaymentHandler)
	http.HandleFunc("/api/payment/stk", handlers.CloudPayPaymentHandler)
	http.HandleFunc("/api/payment/cloudpay/webhook", handlers.CloudPayWebhookHandler)

	// Work / Task Routes
	http.HandleFunc("/api/work/complete", handlers.CompleteWorkHandler)

	// Admin Payment & Membership Monitoring Routes
	http.HandleFunc("/api/admin/payments", handlers.AdminGetPaymentsHandler)
	http.HandleFunc("/api/admin/memberships", handlers.AdminGetMembershipsHandler)
	http.HandleFunc("/api/admin/memberships/activate", handlers.AdminActivateMembershipHandler)

	// 6. START SERVER (RENDER SAFE)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Println("GlobalChat running on port", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
