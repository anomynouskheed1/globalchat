package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"globalchat/db"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func newSessionID() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func setSession(w http.ResponseWriter, userID int) {
	sid := newSessionID()
	expires := time.Now().Add(30 * 24 * time.Hour)
	db.CreateSession(sid, userID, expires)
	http.SetCookie(w, &http.Cookie{
		Name:     "gc_session",
		Value:    sid,
		Expires:  expires,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearSession(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie("gc_session")
	if err == nil {
		db.DeleteSession(c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:    "gc_session",
		Value:   "",
		Expires: time.Unix(0, 0),
		Path:    "/",
	})
}

// GetCurrentUser returns the logged-in user or nil.
func GetCurrentUser(r *http.Request) *db.User {
	c, err := r.Cookie("gc_session")
	if err != nil {
		return nil
	}
	u, err := db.GetSessionUser(c.Value)
	if err != nil {
		return nil
	}
	return u
}

// RequireAuth redirects to /register if not logged in.
func RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if GetCurrentUser(r) == nil {
			http.Redirect(w, r, "/register", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

// RequireMembership redirects to /membership if user has no active plan.
func RequireMembership(next http.HandlerFunc) http.HandlerFunc {
	return RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		u := GetCurrentUser(r)
		_, err := db.GetActiveMembership(u.ID)
		if err != nil {
			http.Redirect(w, r, "/membership?reason=required", http.StatusSeeOther)
			return
		}
		next(w, r)
	})
}

func HandleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		u := GetCurrentUser(r)
		if u != nil {
			http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
			return
		}
		Render(w, r, "register.html", nil)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	phone := strings.TrimSpace(r.FormValue("phone"))
	password := r.FormValue("password")

	if name == "" || email == "" || password == "" {
		renderWithError(w, r, "register.html", "All fields are required.")
		return
	}
	if len(password) < 8 {
		renderWithError(w, r, "register.html", "Password must be at least 8 characters.")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		renderWithError(w, r, "register.html", "Server error. Please try again.")
		return
	}

	id, err := db.CreateUser(name, email, phone, string(hash))
	if err != nil {
		renderWithError(w, r, "register.html", "Email already registered. Please log in.")
		return
	}

	setSession(w, int(id))
	http.Redirect(w, r, "/membership", http.StatusSeeOther)
}

func HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/register", http.StatusSeeOther)
		return
	}

	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	password := r.FormValue("password")

	u, err := db.GetUserByEmail(email)
	if err != nil {
		renderWithError(w, r, "register.html", "Invalid email or password.")
		return
	}

	if err = bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		renderWithError(w, r, "register.html", "Invalid email or password.")
		return
	}

	setSession(w, u.ID)
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func HandleLogout(w http.ResponseWriter, r *http.Request) {
	clearSession(w, r)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
