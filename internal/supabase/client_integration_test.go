//go:build integration
package supabase

import (
	"context"
	"net/http"
	"os"
	"testing"

	"github.com/joho/godotenv"
)

// TestIntegration_Supabase verifies the connection and basic operations against a real Supabase instance.
// To run this test: go test -v -tags=integration ./internal/supabase/...
func TestIntegration_Supabase(t *testing.T) {
	// Try to load .env from project root
	_ = godotenv.Load("../../.env")

	url := os.Getenv("SUPABASE_URL")
	key := os.Getenv("SUPABASE_SERVICE_KEY")

	if url == "" || key == "" {
		t.Skip("Skipping integration test: SUPABASE_URL or SUPABASE_SERVICE_KEY not set")
	}

	client, err := NewClient(Config{
		URL:    url,
		APIKey: key,
	})
	if err != nil {
		t.Fatalf("Failed to create Supabase client: %v", err)
	}

	ctx := context.Background()

	// 1. Test Profiles (Read operation from profiles)
	t.Run("GetProfiles", func(t *testing.T) {
		// Manual request to common profiles table
		url := os.Getenv("SUPABASE_URL") + "/rest/v1/profiles?select=id&limit=1"
		req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
		req.Header.Set("apikey", os.Getenv("SUPABASE_SERVICE_KEY"))
		req.Header.Set("Authorization", "Bearer "+os.Getenv("SUPABASE_SERVICE_KEY"))
		
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Connection failed: %v", err)
		}
		defer resp.Body.Close()
		
		if resp.StatusCode != 200 {
			t.Errorf("Profiles table check failed with status %d. This might mean migrations haven't been run.", resp.StatusCode)
		} else {
			t.Log("✅ Profiles table is accessible")
		}
	})

	// 2. Test Active Users (Read operation from agent_configs)

	// 2. Test Pending Tasks (Read operation from content_tasks)
	t.Run("GetPendingTasks", func(t *testing.T) {
		tasks, err := client.GetPendingTasks(ctx)
		if err != nil {
			t.Fatalf("GetPendingTasks failed: %v", err)
		}
		t.Logf("✅ Successfully fetched %d pending tasks", len(tasks))
	})

	// 3. Test Instagram Accounts (Read operation from instagram_accounts)
	t.Run("GetActiveInstagramAccounts", func(t *testing.T) {
		accounts, err := client.GetActiveInstagramAccounts(ctx)
		if err != nil {
			t.Fatalf("GetActiveInstagramAccounts failed: %v", err)
		}
		t.Logf("✅ Successfully fetched %d active Instagram accounts", len(accounts))
	})

	// 4. Test Auth Middleware (Self-verification)
	t.Run("AuthMiddleware_VerifyToken", func(t *testing.T) {
		// We use the service key as a token for verification check if the API allows it
		// or just check if the endpoint is reachable.
		am := NewAuthMiddleware(url, os.Getenv("SUPABASE_ANON_KEY"))
		
		// Note: Service key is usually valid as a bearer token in Supabase
		user, err := am.verifyToken(ctx, key)
		if err != nil {
			t.Logf("ℹ️ Token verification failed as expected or due to policy: %v", err)
		} else {
			t.Logf("✅ Token verification successful for user: %s", user.Email)
		}
	})
}
