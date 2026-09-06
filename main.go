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

var templates *template.Template

// LOAD TEMPLATES
func loadTemplates() {
	var err error
	templates, err = template.ParseGlob("templates/*.html")
	if err != nil {
		log.Println("Error parsing templates on startup:", err)
	}
}

// RENDER FUNCTION
func render(w http.ResponseWriter, tmpl string, data interface{}) {
	// Try executing from preloaded templates first
	err := templates.ExecuteTemplate(w, tmpl, data)
	if err != nil {
		log.Printf("Execution failed for template %s: %v. Attempting full re-parse...", tmpl, err)

		// Fallback: Re-parse all templates so partials like head/nav/footer are available
		t, parseErr := template.ParseGlob("templates/*.html")
		if parseErr != nil {
			http.Error(w, "Template parse error: "+parseErr.Error(), http.StatusInternalServerError)
			return
		}

		execErr := t.ExecuteTemplate(w, tmpl, data)
		if execErr != nil {
			log.Printf("Fallback execution error for %s: %v", tmpl, execErr)
			http.Error(w, "Template execution error: "+execErr.Error(), http.StatusInternalServerError)
		}
	}
}

// INIT ENV
func init() {
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found, running in production mode")
	}
}

func main() {
	// 1. INITIALIZE DATABASE & MIGRATIONS
	db.Init()

	// 2. LOAD TEMPLATES
	loadTemplates()

	// 3. STATIC FILES
	fs := http.FileServer(http.Dir("static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	// 4. PUBLIC PAGES & AUTH
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

	// 5. SECURED DASHBOARD PAGES
	renderSecuredPage := func(page string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			user := handlers.GetCurrentUser(r)

			// Wrap user in data map so templates can evaluate .User
			data := map[string]interface{}{
				"User": user,
			}

			render(w, page, data)
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

		if user == nil || user.Email != adminEmail {
			http.Error(w, "Forbidden: Admin access required", http.StatusForbidden)
			return
		}

		data := map[string]interface{}{
			"User": user,
		}

		render(w, "admin.html", data)
	}))

	// 6. API ENDPOINTS
	http.HandleFunc("/api/payment/cloudpay/stk", handlers.CloudPayPaymentHandler)
	http.HandleFunc("/api/payment/stk", handlers.CloudPayPaymentHandler)
	http.HandleFunc("/api/payment/cloudpay/webhook", handlers.CloudPayWebhookHandler)

	http.HandleFunc("/api/work/complete", handlers.CompleteWorkHandler)

	http.HandleFunc("/api/admin/payments", handlers.AdminGetPaymentsHandler)
	http.HandleFunc("/api/admin/memberships", handlers.AdminGetMembershipsHandler)
	http.HandleFunc("/api/admin/memberships/activate", handlers.AdminActivateMembershipHandler)

	// 7. START SERVER
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Println("GlobalChat running on port", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
