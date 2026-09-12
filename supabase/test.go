package supabase

import "fmt"

func TestConnection() error {
	if Client == nil {
		return fmt.Errorf("Supabase client is not initialized")
	}

	_, _, err := Client.
		From("profiles").
		Select("id", "exact", false).
		Limit(1, "").
		Execute()

	if err != nil {
		return fmt.Errorf("Supabase query failed: %w", err)
	}

	return nil
}