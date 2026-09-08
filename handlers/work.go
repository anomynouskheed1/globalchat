package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"globalchat/db" // Adjust import path if your go.mod module name differs
)

type WorkCompleteResponse struct {
	Success    bool   `json:"success"`
	Payout     int64  `json:"payout"`
	NewBalance int64  `json:"new_balance"`
	FilesDone  int    `json:"files_done"`
	Message    string `json:"message"`
}

func CompleteWorkHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(WorkCompleteResponse{
			Success: false,
			Message: "Method not allowed",
		})
		return
	}

	// 1. Authenticate User via Session Cookie
	cookie, err := r.Cookie("gc_session")
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(WorkCompleteResponse{
			Success: false,
			Message: "Unauthorized session. Please log in.",
		})
		return
	}

	user, err := db.GetSessionUser(cookie.Value)
	if err != nil || user == nil {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(WorkCompleteResponse{
			Success: false,
			Message: "Invalid or expired session.",
		})
		return
	}

	// 2. Process Task Completion & Database Credit Transaction
	rewardKES, filesDone, newBalance, err := db.ProcessFileCompletion(user.ID)
	if err != nil {
		if errors.Is(err, db.ErrDailyLimitReached) {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(WorkCompleteResponse{
				Success:    false,
				Payout:     0,
				NewBalance: int64(newBalance),
				FilesDone:  filesDone,
				Message:    "Daily limit reached. Upgrade your membership to process unlimited files.",
			})
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(WorkCompleteResponse{
			Success: false,
			Message: "Failed to process task reward. Please try again.",
		})
		return
	}

	// 3. Return Success Payload
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(WorkCompleteResponse{
		Success:    true,
		Payout:     int64(rewardKES),
		NewBalance: int64(newBalance),
		FilesDone:  filesDone,
		Message:    "Task completed successfully!",
	})
}
