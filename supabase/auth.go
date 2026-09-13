package supabase

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

type AuthUser struct {
	ID           string                 `json:"id"`
	Email        string                 `json:"email"`
	UserMetadata map[string]interface{} `json:"user_metadata"`
}

type AuthResponse struct {
	AccessToken  string   `json:"access_token"`
	RefreshToken string   `json:"refresh_token"`
	User         AuthUser `json:"user"`
}

func authRequest(endpoint string, payload interface{}, key string) (*AuthResponse, error) {
	baseURL := os.Getenv("SUPABASE_URL")

	if baseURL == "" {
		return nil, fmt.Errorf("SUPABASE_URL is missing")
	}

	if key == "" {
		return nil, fmt.Errorf("Supabase API key is missing")
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode request: %w", err)
	}

	url := baseURL + "/auth/v1/" + endpoint

	fmt.Println("SUPABASE AUTH REQUEST:", endpoint)

	req, err := http.NewRequest(
		http.MethodPost,
		url,
		bytes.NewBuffer(body),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", key)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Supabase Auth connection failed: %w", err)
	}

	defer resp.Body.Close()

	var responseBody map[string]interface{}

	if err := json.NewDecoder(resp.Body).Decode(&responseBody); err != nil {
		return nil, fmt.Errorf(
			"could not read Supabase response (HTTP %d): %w",
			resp.StatusCode,
			err,
		)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf(
			"Supabase Auth error (%d): %v",
			resp.StatusCode,
			responseBody,
		)
	}

	raw, err := json.Marshal(responseBody)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to process Supabase response: %w",
			err,
		)
	}

	var result AuthResponse

	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf(
			"failed to decode Supabase Auth response: %w",
			err,
		)
	}

	// Supabase may return a successful signup without an access token
	// when email confirmation is enabled. In that case, the user object
	// should still contain the Auth user ID.
	if result.User.ID == "" {
		if rawUser, ok := responseBody["user"].(map[string]interface{}); ok {
			if id, ok := rawUser["id"].(string); ok {
				result.User.ID = id
			}

			if email, ok := rawUser["email"].(string); ok {
				result.User.Email = email
			}

			if metadata, ok := rawUser["user_metadata"].(map[string]interface{}); ok {
				result.User.UserMetadata = metadata
			}
		}
	}

	if result.User.ID == "" {
		return nil, fmt.Errorf(
			"Supabase signup succeeded but returned no user ID",
		)
	}

	return &result, nil
}

func SignUp(email, password, name, phone string, termsAccepted bool) (*AuthResponse, error) {
	key := os.Getenv("SUPABASE_ANON_KEY")

	payload := map[string]interface{}{
		"email":    email,
		"password": password,
		"data": map[string]interface{}{
			"full_name":      name,
			"phone":          phone,
			"terms_accepted": termsAccepted,
		},
	}

	return authRequest("signup", payload, key)
}

func SignIn(email, password string) (*AuthResponse, error) {
	key := os.Getenv("SUPABASE_ANON_KEY")

	payload := map[string]interface{}{
		"email":    email,
		"password": password,
	}

	return authRequest("token?grant_type=password", payload, key)
}
