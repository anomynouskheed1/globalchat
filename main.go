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
	templates = template.Must(template.ParseGlob("templates/*.html"))
}

// RENDER FUNCTION
func render(w http.ResponseWriter, tmpl string, data interface{}) {
	err := templates.ExecuteTemplate(w, tmpl, data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		log.Println("Template render error:", err)
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

	// 2. LOAD TEMPLATES
	loadTemplates()

	// 3. STATIC FILES
	fs := http.FileServer(http.Dir("static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	// 4. PUBLIC PAGES
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		render(w, "index.html", nil)
	})

	http.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		render(w, "register.html", nil)
	})

	http.HandleFunc("/screening", func(w http.ResponseWriter, r *http.Request) {
		render(w, "screening.html", nil)
	})

	http.HandleFunc("/membership", func(w http.ResponseWriter, r *http.Request) {
		render(w, "membership.html", nil)
	})

	// 5. SECURED DASHBOARD PAGES
	renderSecuredPage := func(page string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("session")
			if err != nil {
				http.Redirect(w, r, "/register", http.StatusSeeOther)
				return
			}
			user, err := db.GetSessionUser(cookie.Value)
			if err != nil || user == nil {
				http.Redirect(w, r, "/register", http.StatusSeeOther)
				return
			}
			render(w, page, user)
		}
	}

	http.HandleFunc("/dashboard", renderSecuredPage("dashboard.html"))
	http.HandleFunc("/wallet", renderSecuredPage("wallet.html"))
	http.HandleFunc("/rewards", renderSecuredPage("rewards.html"))
	http.HandleFunc("/tasks", renderSecuredPage("tasks.html"))
	http.HandleFunc("/chat", renderSecuredPage("chat.html"))
	http.HandleFunc("/survey", renderSecuredPage("survey.html"))
	http.HandleFunc("/profile", renderSecuredPage("profile.html"))
	http.HandleFunc("/leaderboard", renderSecuredPage("leaderboard.html"))

	// Admin Page with Restricted Access
	http.HandleFunc("/admin", func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session")
		if err != nil {
			http.Redirect(w, r, "/register", http.StatusSeeOther)
			return
		}
		user, err := db.GetSessionUser(cookie.Value)
		if err != nil || user == nil {
			http.Redirect(w, r, "/register", http.StatusSeeOther)
			return
		}

		// Restrict access to designated admin email
		adminEmail := os.Getenv("ADMIN_EMAIL")
		if adminEmail == "" {
			adminEmail = "admin@globalchat.com"
		}
		if user.Email != adminEmail {
			http.Error(w, "Forbidden: Admin access required", http.StatusForbidden)
			return
		}

		render(w, "admin.html", user)
	})

	// 6. API ENDPOINTS
	// Payment Routes
	http.HandleFunc("/api/payment/stk", handlers.CloudPayPaymentHandler)
	http.HandleFunc("/api/payment/cloudpay/webhook", handlers.CloudPayWebhookHandler)

	// Work / Task Routes
	http.HandleFunc("/api/work/complete", handlers.CompleteWorkHandler)

	// Admin Payment & Membership Monitoring Routes
	http.HandleFunc("/api/admin/payments", handlers.AdminGetPaymentsHandler)
	http.HandleFunc("/api/admin/memberships", handlers.AdminGetMembershipsHandler)
	http.HandleFunc("/api/admin/memberships/activate", handlers.AdminActivateMembershipHandler)

	// 7. START SERVER (RENDER SAFE)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Println("GlobalChat running on port", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
