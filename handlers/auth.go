package handlers

import (
	"globalchat/db"
	"globalchat/supabase"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

func setSupabaseSession(w http.ResponseWriter, accessToken string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "gc_supabase_token",
		Value:    accessToken,
		Expires:  time.Now().Add(1 * time.Hour),
		Path:     "/",
		HttpOnly: true,
		Secure:   false,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearSupabaseSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     "gc_supabase_token",
		Value:    "",
		Expires:  time.Unix(0, 0),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func HandleRegister(w http.ResponseWriter, r *http.Request) {
	log.Println(">>> HANDLE REGISTER HIT:", r.Method, r.URL.Path)

	if r.Method == http.MethodGet {
		Render(w, r, "register.html", nil)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	phone := strings.TrimSpace(r.FormValue("phone"))
	password := r.FormValue("password")

	log.Println("REGISTER DATA RECEIVED:", email)

	if name == "" || email == "" || phone == "" || password == "" {
		renderWithError(w, r, "register.html", "All fields are required.")
		return
	}

	if len(password) < 8 {
		renderWithError(
			w,
			r,
			"register.html",
			"Password must be at least 8 characters.",
		)
		return
	}

	termsAccepted := r.FormValue("terms") != ""

	log.Println("CALLING SUPABASE SIGNUP...")

	auth, err := supabase.SignUp(
		email,
		password,
		name,
		phone,
		termsAccepted,
	)

	if err != nil {
		log.Println("SUPABASE REGISTRATION ERROR:", err)

		renderWithError(
			w,
			r,
			"register.html",
			"Registration failed: "+err.Error(),
		)
		return
	}

	log.Println("SUPABASE SIGNUP RETURNED SUCCESS")
	log.Println("SUPABASE USER CREATED:", auth.User.ID)

	localUserID, err := db.CreateUser(
		name,
		email,
		phone,
		"SUPABASE_AUTH_USER",
	)

	if err != nil {
		log.Println("SQLITE BRIDGE ERROR:", err)

		http.Error(
			w,
			"Supabase account was created, but the temporary migration bridge failed.",
			http.StatusInternalServerError,
		)
		return
	}

	log.Println("LOCAL USER CREATED:", localUserID)

	if auth.AccessToken != "" {
		setSupabaseSession(w, auth.AccessToken)
		log.Println("SUPABASE SESSION CREATED")
	}

	log.Println("REDIRECTING TO SCREENING")

	http.Redirect(w, r, "/screening", http.StatusSeeOther)
}

func HandleLogin(w http.ResponseWriter, r *http.Request) {
	log.Println(">>> HANDLE LOGIN HIT:", r.Method, r.URL.Path)

	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/register", http.StatusSeeOther)
		return
	}

	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	password := r.FormValue("password")

	if email == "" || password == "" {
		renderWithError(
			w,
			r,
			"register.html",
			"Email and password are required.",
		)
		return
	}

	log.Println("CALLING SUPABASE LOGIN...")

	auth, err := supabase.SignIn(email, password)

	if err != nil {
		log.Println("SUPABASE LOGIN ERROR:", err)

		renderWithError(
			w,
			r,
			"register.html",
			"Invalid email or password.",
		)
		return
	}

	log.Println("SUPABASE LOGIN SUCCESS:", auth.User.ID)

	localUser, err := db.GetUserByEmail(email)

	if err != nil {
		name := email
		phone := ""

		if auth.User.UserMetadata != nil {
			if value, ok := auth.User.UserMetadata["full_name"].(string); ok && value != "" {
				name = value
			}

			if value, ok := auth.User.UserMetadata["phone"].(string); ok {
				phone = value
			}
		}

		id, createErr := db.CreateUser(
			name,
			email,
			phone,
			"SUPABASE_AUTH_USER",
		)

		if createErr != nil {
			log.Println("SQLITE LOGIN BRIDGE ERROR:", createErr)

			http.Error(
				w,
				"Login succeeded but account migration failed.",
				http.StatusInternalServerError,
			)
			return
		}

		localUser, err = db.GetUserByID(int(id))

		if err != nil {
			http.Error(
				w,
				"Unable to load account.",
				http.StatusInternalServerError,
			)
			return
		}
	}

	sessionID := "supabase_" +
		auth.User.ID +
		"_" +
		time.Now().Format("20060102150405")

	err = db.CreateSession(
		sessionID,
		localUser.ID,
		time.Now().Add(30*24*time.Hour),
	)

	if err != nil {
		log.Println("SESSION CREATION ERROR:", err)

		http.Error(
			w,
			"Unable to create session.",
			http.StatusInternalServerError,
		)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "gc_session",
		Value:    sessionID,
		Expires:  time.Now().Add(30 * 24 * time.Hour),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	if auth.AccessToken != "" {
		setSupabaseSession(w, auth.AccessToken)
	}

	// Admin users go directly to the admin dashboard.
	adminEmail := strings.TrimSpace(os.Getenv("ADMIN_EMAIL"))

	if adminEmail == "" {
		adminEmail = "admin@globalchat.com"
	}

	if strings.EqualFold(email, adminEmail) {
		log.Println("ADMIN LOGIN DETECTED:", email)
		log.Println("REDIRECTING TO ADMIN DASHBOARD")

		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}

	log.Println("REDIRECTING TO USER DASHBOARD")

	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func HandleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("gc_session")

	if err == nil && cookie.Value != "" {
		db.DeleteSession(cookie.Value)
	}

	clearSupabaseSession(w)

	http.SetCookie(w, &http.Cookie{
		Name:     "gc_session",
		Value:    "",
		Expires:  time.Unix(0, 0),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, "/", http.StatusSeeOther)
}
