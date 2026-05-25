package main

import (
	"html/template"
	"log"
	"net/http"

	"github.com/joho/godotenv"

	"globalchat/handlers"
)

var templates *template.Template

func init() {
    err := godotenv.Load()

    if err != nil {
        log.Println("No .env file found, running in production mode")
    }
}

func render(w http.ResponseWriter, tmpl string, data interface{}) {

	err := templates.ExecuteTemplate(w, tmpl, data)

	if err != nil {
		http.Error(
			w,
			err.Error(),
			http.StatusInternalServerError,
		)
	}
}

func main() {

	// Static files
	fs := http.FileServer(http.Dir("static"))

	http.Handle(
		"/static/",
		http.StripPrefix("/static/", fs),
	)

	// Pages
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

	http.HandleFunc("/dashboard", func(w http.ResponseWriter, r *http.Request) {
		render(w, "dashboard.html", nil)
	})

	http.HandleFunc("/wallet", func(w http.ResponseWriter, r *http.Request) {
		render(w, "wallet.html", nil)
	})

	http.HandleFunc("/rewards", func(w http.ResponseWriter, r *http.Request) {
		render(w, "rewards.html", nil)
	})

	http.HandleFunc("/tasks", func(w http.ResponseWriter, r *http.Request) {
		render(w, "tasks.html", nil)
	})

	http.HandleFunc("/chat", func(w http.ResponseWriter, r *http.Request) {
		render(w, "chat.html", nil)
	})

	http.HandleFunc("/survey", func(w http.ResponseWriter, r *http.Request) {
		render(w, "survey.html", nil)
	})

	http.HandleFunc("/profile", func(w http.ResponseWriter, r *http.Request) {
		render(w, "profile.html", nil)
	})

	http.HandleFunc("/leaderboard", func(w http.ResponseWriter, r *http.Request) {
		render(w, "leaderboard.html", nil)
	})

	http.HandleFunc("/admin", func(w http.ResponseWriter, r *http.Request) {
		render(w, "admin.html", nil)
	})

	// PAYMENT ROUTE
	http.HandleFunc("/pay/mpesa", handlers.MpesaPaymentHandler)

	log.Println("GlobalChat running on http://localhost:8080")

	log.Fatal(
		http.ListenAndServe(":8080", nil),
	)
}