package handlers

import (
	"database/sql"
	"errors"
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

// ============================================================
// REGISTRATION
// ============================================================

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
	rawPhone := strings.TrimSpace(r.FormValue("phone"))
	password := r.FormValue("password")
	termsAccepted := r.FormValue("terms") == "on"

	log.Println("REGISTER DATA RECEIVED:", email)

	// --------------------------------------------------------
	// Basic validation
	// --------------------------------------------------------

	if name == "" || email == "" || rawPhone == "" || password == "" {
		renderWithError(
			w,
			r,
			"register.html",
			"All fields are required.",
		)
		return
	}

	if !db.ValidateName(name) {
		renderWithError(
			w,
			r,
			"register.html",
			"Please enter a valid name using letters only.",
		)
		return
	}

	if !db.ValidateEmail(email) {
		renderWithError(
			w,
			r,
			"register.html",
			"Please enter a valid email address.",
		)
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

	if !termsAccepted {
		renderWithError(
			w,
			r,
			"register.html",
			"You must accept the Terms & Conditions to continue.",
		)
		return
	}

	// --------------------------------------------------------
	// Phone validation + normalization
	// --------------------------------------------------------

	phone, err := db.NormalizePhone(rawPhone)

	if err != nil {
		renderWithError(
			w,
			r,
			"register.html",
			"Please enter a valid East African phone number with country code.",
		)
		return
	}

	log.Println("NORMALIZED PHONE:", phone)

	// --------------------------------------------------------
	// Duplicate email check
	// --------------------------------------------------------

	emailExists, err := db.EmailExists(email)

	if err != nil {
		log.Println("EMAIL CHECK ERROR:", err)

		renderWithError(
			w,
			r,
			"register.html",
			"Unable to verify your email. Please try again.",
		)
		return
	}

	if emailExists {
		renderWithError(
			w,
			r,
			"register.html",
			"An account with this email already exists. Please log in instead.",
		)
		return
	}

	// --------------------------------------------------------
	// Duplicate phone check
	// --------------------------------------------------------

	phoneExists, err := db.PhoneExists(phone)

	if err != nil {
		log.Println("PHONE CHECK ERROR:", err)

		renderWithError(
			w,
			r,
			"register.html",
			"Unable to verify your phone number. Please try again.",
		)
		return
	}

	if phoneExists {
		renderWithError(
			w,
			r,
			"register.html",
			"An account with this phone number already exists.",
		)
		return
	}

	// --------------------------------------------------------
	// Create Supabase account
	// --------------------------------------------------------

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

		message := "Registration failed. Please try again."

		errText := strings.ToLower(err.Error())

		if strings.Contains(errText, "already registered") ||
			strings.Contains(errText, "already exists") ||
			strings.Contains(errText, "user_already_exists") {
			message = "An account with this email already exists. Please log in instead."
		}

		renderWithError(
			w,
			r,
			"register.html",
			message,
		)
		return
	}

	log.Println("SUPABASE SIGNUP RETURNED SUCCESS")
	log.Println("SUPABASE USER CREATED:", auth.User.ID)

	// --------------------------------------------------------
	// Create local bridge user
	// --------------------------------------------------------

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
			"Supabase account was created, but the local account could not be completed. Please contact support.",
			http.StatusInternalServerError,
		)
		return
	}

	log.Println("LOCAL USER CREATED:", localUserID)

	// Link the local account to Supabase Auth.
	if err := db.SetSupabaseAuthID(
		int(localUserID),
		auth.User.ID,
	); err != nil {
		log.Println("SUPABASE AUTH ID LINK ERROR:", err)
	}

	// --------------------------------------------------------
	// Create local session
	// --------------------------------------------------------

	sessionID := "supabase_" +
		auth.User.ID +
		"_" +
		time.Now().Format("20060102150405.000000000")

	err = db.CreateSession(
		sessionID,
		int(localUserID),
		time.Now().Add(30*24*time.Hour),
	)

	if err != nil {
		log.Println("SESSION CREATION ERROR:", err)

		http.Error(
			w,
			"Account created but login session could not be created.",
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
		log.Println("SUPABASE SESSION CREATED")
	}

	log.Println("REDIRECTING TO MEMBERSHIP")

	http.Redirect(w, r, "/membership", http.StatusSeeOther)
}

// ============================================================
// LOGIN
// ============================================================

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

	if !db.ValidateEmail(email) {
		renderWithError(
			w,
			r,
			"register.html",
			"Please enter a valid email address.",
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
		if !errors.Is(err, sql.ErrNoRows) {
			log.Println("LOCAL USER LOOKUP ERROR:", err)

			http.Error(
				w,
				"Unable to load account.",
				http.StatusInternalServerError,
			)
			return
		}

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

		if phone != "" {
			if normalized, normalizeErr := db.NormalizePhone(phone); normalizeErr == nil {
				phone = normalized
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

	// Keep the Supabase Auth ID linked.
	if auth.User.ID != "" && localUser.SupabaseAuthID == "" {
		if err := db.SetSupabaseAuthID(
			localUser.ID,
			auth.User.ID,
		); err != nil {
			log.Println("SUPABASE AUTH ID LINK ERROR:", err)
		}
	}

	sessionID := "supabase_" +
		auth.User.ID +
		"_" +
		time.Now().Format("20060102150405.000000000")

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

	// Admin users go directly to admin dashboard.
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

	// --------------------------------------------------------
	// Membership gate
	// --------------------------------------------------------

	if db.HasActiveMembership(localUser.ID) {
		log.Println("ACTIVE MEMBERSHIP FOUND")
		log.Println("REDIRECTING TO USER DASHBOARD")

		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}

	log.Println("NO ACTIVE MEMBERSHIP")
	log.Println("REDIRECTING TO MEMBERSHIP")

	http.Redirect(w, r, "/membership", http.StatusSeeOther)
}

// ============================================================
// LOGOUT
// ============================================================

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
