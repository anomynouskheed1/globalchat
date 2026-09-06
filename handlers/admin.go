package handlers

import (
	"encoding/json"
	"net/http"
	"os"

	"globalchat/db"
)

// isAdmin checks if the user is authorized as an administrator
func isAdmin(u *db.User) bool {
	adminEmail := os.Getenv("ADMIN_EMAIL")
	if adminEmail != "" {
		return u.Email == adminEmail
	}
	// Fallback to default admin check if environment variable isn't set
	return u.Email == "admin@globalchat.com"
}

// authenticateAdmin handles authentication and authorization for admin endpoints
func authenticateAdmin(w http.ResponseWriter, r *http.Request) (*db.User, bool) {
	cookie, err := r.Cookie("session")
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return nil, false
	}
	user, err := db.GetSessionUser(cookie.Value)
	if err != nil || user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return nil, false
	}

	if !isAdmin(user) {
		http.Error(w, "Forbidden: Admin access required", http.StatusForbidden)
		return nil, false
	}

	return user, true
}

// AdminGetPaymentsHandler serves transaction logs to the admin UI
func AdminGetPaymentsHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := authenticateAdmin(w, r); !ok {
		return
	}

	txs, err := db.GetAllTransactions()
	if err != nil {
		http.Error(w, "Database query error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(txs)
}

// AdminGetMembershipsHandler serves membership records to the admin UI
func AdminGetMembershipsHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := authenticateAdmin(w, r); !ok {
		return
	}

	memberships, err := db.GetAllMemberships()
	if err != nil {
		http.Error(w, "Database query error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(memberships)
}

// AdminActivateMembershipHandler allows manual approval of a pending membership by payment_ref
func AdminActivateMembershipHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if _, ok := authenticateAdmin(w, r); !ok {
		return
	}

	var req struct {
		PaymentRef string `json:"payment_ref"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PaymentRef == "" {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	if err := db.ActivateMembership(req.PaymentRef); err != nil {
		http.Error(w, "Failed to activate membership", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "success",
		"message": "Membership activated successfully",
	})
}
