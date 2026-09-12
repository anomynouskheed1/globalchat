package supabase

import (
	"fmt"
	"os"

	"github.com/supabase-community/supabase-go"
)

var Client *supabase.Client

func Init() error {
	url := os.Getenv("SUPABASE_URL")
	key := os.Getenv("SUPABASE_SERVICE_ROLE_KEY")

	if url == "" {
		return fmt.Errorf("SUPABASE_URL is missing")
	}

	if key == "" {
		return fmt.Errorf("SUPABASE_SERVICE_ROLE_KEY is missing")
	}

	client, err := supabase.NewClient(url, key, nil)
	if err != nil {
		return fmt.Errorf("failed to create Supabase client: %w", err)
	}

	Client = client

	return nil
}