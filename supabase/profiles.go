package supabase

import "fmt"

type Profile struct {
	ID              string `json:"id"`
	FullName        string `json:"full_name"`
	Phone           string `json:"phone"`
	TermsAcceptedAt string `json:"terms_accepted_at"`
}

func GetProfileByAuthID(authID string) (*Profile, error) {
	if Client == nil {
		return nil, fmt.Errorf("Supabase client is not initialized")
	}

	var profiles []Profile

	_, err := Client.
		From("profiles").
		Select("id, full_name, phone, terms_accepted_at", "exact", false).
		Eq("id", authID).
		Limit(1, "").
		ExecuteTo(&profiles)

	if err != nil {
		return nil, fmt.Errorf("failed to get profile: %w", err)
	}

	if len(profiles) == 0 {
		return nil, fmt.Errorf("profile not found")
	}

	return &profiles[0], nil
}
