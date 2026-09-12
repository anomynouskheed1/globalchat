package supabase

import (
	"fmt"
	"time"
)

type AdminUser struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Phone          string    `json:"phone"`
	BalanceKES     int64     `json:"balance_kes"`
	JoinedAt       time.Time `json:"joined_at"`
	TasksCompleted int       `json:"tasks_completed"`
}

type AdminPayment struct {
	ID          int64      `json:"id"`
	UserID      string     `json:"user_id"`
	PaymentRef  string     `json:"payment_ref"`
	Provider    string     `json:"provider"`
	Plan        string     `json:"plan"`
	AmountKES   int64      `json:"amount_kes"`
	Phone       string     `json:"phone"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	UserName    string     `json:"user_name"`
}

type AdminMembership struct {
	ID         int64      `json:"id"`
	UserID     string     `json:"user_id"`
	Plan       string     `json:"plan"`
	Status     string     `json:"status"`
	PaymentRef string     `json:"payment_ref"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UserName   string     `json:"user_name"`
	Phone      string     `json:"phone"`
}

// GetAdminUsers returns all profiles with wallet balances and task counts.
func GetAdminUsers() ([]AdminUser, error) {
	if Client == nil {
		return nil, fmt.Errorf("Supabase client is not initialized")
	}

	var profiles []struct {
		ID        string    `json:"id"`
		FullName  string    `json:"full_name"`
		Phone     string    `json:"phone"`
		CreatedAt time.Time `json:"created_at"`
	}

	_, err := Client.
		From("profiles").
		Select("id,full_name,phone,created_at", "exact", false).
		ExecuteTo(&profiles)

	if err != nil {
		return nil, fmt.Errorf("failed to get profiles: %w", err)
	}

	users := make([]AdminUser, 0, len(profiles))

	for _, profile := range profiles {
		user := AdminUser{
			ID:       profile.ID,
			Name:     profile.FullName,
			Phone:    profile.Phone,
			JoinedAt: profile.CreatedAt,
		}

		var wallets []struct {
			BalanceKES int64 `json:"balance_kes"`
		}

		_, walletErr := Client.
			From("wallets").
			Select("balance_kes", "exact", false).
			Eq("user_id", profile.ID).
			Limit(1, "").
			ExecuteTo(&wallets)

		if walletErr == nil && len(wallets) > 0 {
			user.BalanceKES = wallets[0].BalanceKES
		}

		var tasks []struct {
			ID int64 `json:"id"`
		}

		_, taskErr := Client.
			From("task_completions").
			Select("id", "exact", false).
			Eq("user_id", profile.ID).
			ExecuteTo(&tasks)

		if taskErr == nil {
			user.TasksCompleted = len(tasks)
		}

		users = append(users, user)
	}

	return users, nil
}

// GetAdminPayments returns real payment transactions from Supabase.
func GetAdminPayments() ([]AdminPayment, error) {
	if Client == nil {
		return nil, fmt.Errorf("Supabase client is not initialized")
	}

	var payments []AdminPayment

	_, err := Client.
		From("payment_transactions").
		Select(
			"id,user_id,payment_ref,provider,plan,amount_kes,phone,status,created_at,completed_at",
			"exact",
			false,
		).
		ExecuteTo(&payments)

	if err != nil {
		return nil, fmt.Errorf("failed to get payment transactions: %w", err)
	}

	for i := range payments {
		var profiles []struct {
			FullName string `json:"full_name"`
		}

		_, profileErr := Client.
			From("profiles").
			Select("full_name", "exact", false).
			Eq("id", payments[i].UserID).
			Limit(1, "").
			ExecuteTo(&profiles)

		if profileErr == nil && len(profiles) > 0 {
			payments[i].UserName = profiles[0].FullName
		}
	}

	return payments, nil
}

// GetAdminMemberships returns all membership records from Supabase.
func GetAdminMemberships() ([]AdminMembership, error) {
	if Client == nil {
		return nil, fmt.Errorf("Supabase client is not initialized")
	}

	var memberships []AdminMembership

	_, err := Client.
		From("memberships").
		Select(
			"id,user_id,plan,status,payment_ref,started_at,expires_at,created_at",
			"exact",
			false,
		).
		ExecuteTo(&memberships)

	if err != nil {
		return nil, fmt.Errorf("failed to get memberships: %w", err)
	}

	for i := range memberships {
		var profiles []struct {
			FullName string `json:"full_name"`
			Phone    string `json:"phone"`
		}

		_, profileErr := Client.
			From("profiles").
			Select("full_name,phone", "exact", false).
			Eq("id", memberships[i].UserID).
			Limit(1, "").
			ExecuteTo(&profiles)

		if profileErr == nil && len(profiles) > 0 {
			memberships[i].UserName = profiles[0].FullName
			memberships[i].Phone = profiles[0].Phone
		}
	}

	return memberships, nil
}

// ActivateMembership activates a membership using its payment reference.
func ActivateMembership(paymentRef string) error {
	if Client == nil {
		return fmt.Errorf("Supabase client is not initialized")
	}

	if paymentRef == "" {
		return fmt.Errorf("payment reference is required")
	}

	updates := map[string]interface{}{
		"status":     "active",
		"started_at": time.Now(),
	}

	_, _, err := Client.
		From("memberships").
		Update(updates, "", "").
		Eq("payment_ref", paymentRef).
		Execute()

	if err != nil {
		return fmt.Errorf("failed to activate membership: %w", err)
	}

	return nil
}
