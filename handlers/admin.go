package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"globalchat/db"
)

// isAdmin checks if the logged-in user is authorized as an administrator.
func isAdmin(u *db.User) bool {
	if u == nil {
		return false
	}

	adminEmail := strings.TrimSpace(os.Getenv("ADMIN_EMAIL"))

	if adminEmail != "" {
		return strings.EqualFold(u.Email, adminEmail)
	}

	// Fallback admin account.
	return strings.EqualFold(u.Email, "admin@globalchat.com")
}

// authenticateAdmin verifies that the request belongs to an authenticated admin.
//
// IMPORTANT:
// The rest of the application uses the "gc_session" cookie.
// We therefore use the same cookie here.
func authenticateAdmin(w http.ResponseWriter, r *http.Request) (*db.User, bool) {
	cookie, err := r.Cookie("gc_session")
	if err != nil || cookie.Value == "" {
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

// AdminGetPaymentsHandler returns transaction records to the admin UI.
func AdminGetPaymentsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if _, ok := authenticateAdmin(w, r); !ok {
		return
	}

	txs, err := db.GetAllTransactions()
	if err != nil {
		http.Error(w, "Database query error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if txs == nil {
		txs = []db.AdminTransaction{}
	}

	if err := json.NewEncoder(w).Encode(txs); err != nil {
		return
	}
}

// AdminGetMembershipsHandler returns membership records to the admin UI.
func AdminGetMembershipsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if _, ok := authenticateAdmin(w, r); !ok {
		return
	}

	memberships, err := db.GetAllMemberships()
	if err != nil {
		http.Error(w, "Database query error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if memberships == nil {
		memberships = []db.AdminMembership{}
	}

	if err := json.NewEncoder(w).Encode(memberships); err != nil {
		return
	}
}

// AdminActivateMembershipHandler allows an authenticated admin
// to manually activate a pending membership using its payment reference.
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

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	req.PaymentRef = strings.TrimSpace(req.PaymentRef)

	if req.PaymentRef == "" {
		http.Error(w, "Payment reference is required", http.StatusBadRequest)
		return
	}

	if err := db.ActivateMembership(req.PaymentRef); err != nil {
		http.Error(w, "Failed to activate membership", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "success",
		"message": "Membership activated successfully",
	})
}
