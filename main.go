package main

import (
	"fmt"
	"globalchat/db"
	"globalchat/handlers"
	"globalchat/supabase"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

var templates *template.Template

func loadTemplates() {
	var err error

	templates, err = template.ParseGlob("templates/*.html")
	if err != nil {
		log.Println("Template glob parsing notice:", err)
	}
}

// ---------------------------------------------------------
// ADMIN TEMPLATE DATA
// ---------------------------------------------------------

type AdminPageUser struct {
	Initials        string
	ID              string
	Name            string
	Phone           string
	StatusClass     string
	Status          string
	Earned          int64
	EarnedFormatted string
	BalanceKES      int64
	TasksCompleted  int
	JoinedDate      string
	JoinedAt        time.Time
}

type AdminStats struct {
	TotalUsers      int
	NewUsersToday   int
	TotalPaidOut    int64
	PaidOutToday    int64
	TasksCompleted  int
	TasksToday      int
	FraudFlags      int
	NewFraudFlags   int
	TotalTickets    int
	OpenTickets     int
	PremiumMembers  int
	NewPremiumToday int
}

type AdminPayment struct {
	Type            string
	UserIdentifier  string
	Detail          string
	AmountFormatted string
	Status          string
	StatusClass     string
}

type AdminFraudAlert struct {
	ID          string
	Severity    string
	Icon        string
	Title       string
	Description string
}

type AdminTicket struct {
	ID          string
	Subject     string
	UserName    string
	TimeAgo     string
	Status      string
	StatusClass string
}

type AdminPageData struct {
	LastUpdated string
	CSRFToken   string

	Users       []AdminPageUser
	Stats       AdminStats
	Payments    []AdminPayment
	FraudAlerts []AdminFraudAlert
	Tickets     []AdminTicket
}

// ---------------------------------------------------------
// TEMPLATE RENDERING
// ---------------------------------------------------------

func renderPage(w http.ResponseWriter, tmplName string, data interface{}) {
	if templates != nil {
		err := templates.ExecuteTemplate(w, tmplName, data)

		if err == nil {
			return
		}

		log.Printf(
			"TEMPLATE EXECUTION ERROR [%s]: %v",
			tmplName,
			err,
		)

		return
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
		log.Printf(
			"TEMPLATE EXECUTION ERROR [%s]: %v",
			tmplName,
			err,
		)
	}
}

// ---------------------------------------------------------
// AUTH / MEMBERSHIP HELPERS
// ---------------------------------------------------------

func getSessionUser(r *http.Request) (*db.User, bool) {
	cookie, err := r.Cookie("gc_session")

	if err != nil || cookie.Value == "" {
		return nil, false
	}

	user, err := db.GetSessionUser(cookie.Value)

	if err != nil || user == nil {
		return nil, false
	}

	return user, true
}

func requireLogin(w http.ResponseWriter, r *http.Request) (*db.User, bool) {
	user, ok := getSessionUser(r)

	if !ok {
		http.Redirect(
			w,
			r,
			"/login",
			http.StatusSeeOther,
		)

		return nil, false
	}

	return user, true
}

func requireMembership(w http.ResponseWriter, r *http.Request) (*db.User, bool) {
	user, ok := requireLogin(w, r)

	if !ok {
		return nil, false
	}

	if !db.HasActiveMembership(user.ID) {
		http.Redirect(
			w,
			r,
			"/membership",
			http.StatusSeeOther,
		)

		return nil, false
	}

	return user, true
}

// ---------------------------------------------------------
// ADMIN HELPERS
// ---------------------------------------------------------

func generateInitials(name string) string {
	name = strings.TrimSpace(name)

	if name == "" {
		return "U"
	}

	parts := strings.Fields(name)

	initials := ""

	for _, part := range parts {
		if part == "" {
			continue
		}

		initials += strings.ToUpper(string([]rune(part)[0]))

		if len(initials) >= 2 {
			break
		}
	}

	if initials == "" {
		return "U"
	}

	return initials
}

func buildAdminUsers(adminUsers []supabase.AdminUser) []AdminPageUser {
	users := make([]AdminPageUser, 0, len(adminUsers))

	for _, u := range adminUsers {
		name := strings.TrimSpace(u.Name)

		if name == "" {
			name = "Unknown User"
		}

		users = append(users, AdminPageUser{
			Initials: generateInitials(name),
			ID:       u.ID,
			Name:     name,
			Phone:    u.Phone,

			StatusClass: "active",
			Status:      "Active",

			Earned:          u.BalanceKES,
			EarnedFormatted: fmt.Sprintf("%d", u.BalanceKES),

			BalanceKES:     u.BalanceKES,
			TasksCompleted: u.TasksCompleted,

			JoinedDate: u.JoinedAt.Format("02 Jan 2006"),
			JoinedAt:   u.JoinedAt,
		})
	}

	return users
}

// ---------------------------------------------------------
// MAIN
// ---------------------------------------------------------

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file loaded:", err)
	}

	// -------------------------
	// SUPABASE
	// -------------------------

	if err := supabase.Init(); err != nil {
		log.Fatal("Supabase initialization failed:", err)
	}

	log.Println("Supabase connected successfully")

	if err := supabase.TestConnection(); err != nil {
		log.Fatal("Supabase database test failed:", err)
	}

	log.Println("Supabase database query successful")

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

		if r.URL.Path == "/favicon.ico" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		if r.URL.Path == "/" {
			renderPage(
				w,
				"index.html",
				nil,
			)
			return
		}

		// Remove leading slash.
		tmplName := strings.TrimPrefix(
			r.URL.Path,
			"/",
		)

		// Convert /dashboard to dashboard.html.
		if !strings.HasSuffix(
			tmplName,
			".html",
		) {
			tmplName += ".html"
		}

		// -----------------------------------------
		// ADMIN PAGE
		// -----------------------------------------

		if tmplName == "admin.html" {

			user, ok := requireLogin(w, r)

			if !ok {
				return
			}

			adminEmail := strings.TrimSpace(
				os.Getenv("ADMIN_EMAIL"),
			)

			if adminEmail == "" {
				adminEmail = "admin@globalchat.com"
			}

			if !strings.EqualFold(
				user.Email,
				adminEmail,
			) {
				http.Error(
					w,
					"Forbidden: Admin access required",
					http.StatusForbidden,
				)
				return
			}

			log.Println(
				"ADMIN DASHBOARD REQUEST:",
				user.Email,
			)

			adminUsers, err := supabase.GetAdminUsers()

			if err != nil {
				log.Println(
					"ADMIN USERS LOAD ERROR:",
					err,
				)

				http.Error(
					w,
					"Failed to load admin users",
					http.StatusInternalServerError,
				)

				return
			}

			users := buildAdminUsers(adminUsers)

			stats := AdminStats{
				TotalUsers:      len(adminUsers),
				NewUsersToday:   0,
				TotalPaidOut:    0,
				PaidOutToday:    0,
				TasksCompleted:  0,
				TasksToday:      0,
				FraudFlags:      0,
				NewFraudFlags:   0,
				TotalTickets:    0,
				OpenTickets:     0,
				PremiumMembers:  0,
				NewPremiumToday: 0,
			}

			payments := make(
				[]AdminPayment,
				0,
			)

			fraudAlerts := make(
				[]AdminFraudAlert,
				0,
			)

			tickets := make(
				[]AdminTicket,
				0,
			)

			adminData := AdminPageData{
				LastUpdated: time.Now().Format(
					"02 Jan 2006 15:04:05",
				),

				CSRFToken: "",

				Users:       users,
				Stats:       stats,
				Payments:    payments,
				FraudAlerts: fraudAlerts,
				Tickets:     tickets,
			}

			log.Printf(
				"ADMIN DASHBOARD DATA: users=%d",
				len(users),
			)

			renderPage(
				w,
				"admin.html",
				adminData,
			)

			return
		}

		// -----------------------------------------
		// LOGIN / REGISTER
		// -----------------------------------------

		if tmplName == "login.html" ||
			tmplName == "register.html" {

			renderPage(
				w,
				tmplName,
				nil,
			)

			return
		}

		// -----------------------------------------
		// SCREENING
		// -----------------------------------------

		if tmplName == "screening.html" {

			user, ok := requireLogin(w, r)

			if !ok {
				return
			}

			// Already paid? No need to screen again.
			if db.HasActiveMembership(user.ID) {
				http.Redirect(
					w,
					r,
					"/dashboard",
					http.StatusSeeOther,
				)
				return
			}

			renderPage(
				w,
				"screening.html",
				nil,
			)

			return
		}

		// -----------------------------------------
		// MEMBERSHIP
		// -----------------------------------------

		if tmplName == "membership.html" {

			user, ok := requireLogin(w, r)

			if !ok {
				return
			}

			// Already active.
			if db.HasActiveMembership(user.ID) {
				http.Redirect(
					w,
					r,
					"/dashboard",
					http.StatusSeeOther,
				)
				return
			}

			renderPage(
				w,
				"membership.html",
				nil,
			)

			return
		}

		// -----------------------------------------
		// DASHBOARD
		// -----------------------------------------

		if tmplName == "dashboard.html" {

			_, ok := requireMembership(w, r)

			if !ok {
				return
			}

			renderPage(
				w,
				"dashboard.html",
				nil,
			)

			return
		}

		// -----------------------------------------
		// PAID USER PAGES
		// -----------------------------------------

		protectedMembershipPages := map[string]bool{
			"tasks.html":       true,
			"survey.html":      true,
			"chat.html":        true,
			"wallet.html":      true,
			"rewards.html":     true,
			"leaderboard.html": true,
			"profile.html":     true,
		}

		if protectedMembershipPages[tmplName] {

			_, ok := requireMembership(w, r)

			if !ok {
				return
			}

			renderPage(
				w,
				tmplName,
				nil,
			)

			return
		}

		// -----------------------------------------
		// OTHER HTML PAGES
		// -----------------------------------------

		renderPage(
			w,
			tmplName,
			nil,
		)
	})

	// -------------------------
	// AUTHENTICATION
	// -------------------------

	http.HandleFunc(
		"/register",
		handlers.HandleRegister,
	)

	http.HandleFunc(
		"/login",
		handlers.HandleLogin,
	)

	http.HandleFunc(
		"/logout",
		handlers.HandleLogout,
	)

	// -------------------------
	// ADMIN API
	// -------------------------

	http.HandleFunc(
		"/api/admin/payments",
		handlers.AdminGetPaymentsHandler,
	)

	http.HandleFunc(
		"/api/admin/memberships",
		handlers.AdminGetMembershipsHandler,
	)

	http.HandleFunc(
		"/api/admin/memberships/activate",
		handlers.AdminActivateMembershipHandler,
	)

	// -------------------------
	// CLOUDPAY
	// -------------------------

	http.HandleFunc(
		"/api/payment/cloudpay/stk",
		handlers.CloudPayPaymentHandler,
	)

	http.HandleFunc(
		"/api/payment/cloudpay/webhook",
		handlers.CloudPayWebhookHandler,
	)
	http.HandleFunc("/api/membership/status", handlers.MembershipStatusHandler)
	// -------------------------
	// SERVER
	// -------------------------

	port := os.Getenv("PORT")

	if port == "" {
		port = "10000"
	}

	log.Println(
		"GlobalChat server running on port " + port,
	)

	log.Fatal(
		http.ListenAndServe(
			":"+port,
			nil,
		),
	)
}
