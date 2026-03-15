package supabase

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: Config{
				URL:    "https://example.supabase.co",
				APIKey: "test-key",
			},
			wantErr: false,
		},
		{
			name: "missing URL",
			cfg: Config{
				APIKey: "test-key",
			},
			wantErr: true,
		},
		{
			name: "missing API Key",
			cfg: Config{
				URL: "https://example.supabase.co",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewClient() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && client == nil {
				t.Error("NewClient() returned nil client without error")
			}
		})
	}
}

func TestGetPendingTasks(t *testing.T) {
	mockTasks := []ContentTask{
		{
			ID:     "task-1",
			UserID: "user-1",
			Status: "pending",
		},
		{
			ID:     "task-2",
			UserID: "user-1",
			Status: "pending",
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != http.MethodGet {
			t.Errorf("expected GET request, got %s", r.Method)
		}
		if r.URL.Path != "/rest/v1/content_tasks" {
			t.Errorf("expected path /rest/v1/content_tasks, got %s", r.URL.Path)
		}
		if r.Header.Get("apikey") != "test-key" {
			t.Errorf("expected apikey header, got %s", r.Header.Get("apikey"))
		}

		// Send mock response
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockTasks)
	}))
	defer server.Close()

	client, _ := NewClient(Config{
		URL:    server.URL,
		APIKey: "test-key",
	})

	tasks, err := client.GetPendingTasks(context.Background())
	if err != nil {
		t.Fatalf("GetPendingTasks() error = %v", err)
	}

	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(tasks))
	}
	if tasks[0].ID != "task-1" {
		t.Errorf("expected task-1, got %s", tasks[0].ID)
	}
}

func TestUpdateTaskStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH request, got %s", r.Method)
		}
		if r.URL.Path != "/rest/v1/content_tasks" {
			t.Errorf("expected path /rest/v1/content_tasks, got %s", r.URL.Path)
		}

		var payload map[string]interface{}
		json.NewDecoder(r.Body).Decode(&payload)
		if payload["status"] != "completed" {
			t.Errorf("expected status completed, got %v", payload["status"])
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client, _ := NewClient(Config{
		URL:    server.URL,
		APIKey: "test-key",
	})

	err := client.UpdateTaskStatus(context.Background(), "task-1", "completed", "success")
	if err != nil {
		t.Fatalf("UpdateTaskStatus() error = %v", err)
	}
}

func TestGetUserByID(t *testing.T) {
	mockUsers := []User{
		{
			ID:    "user-1",
			Email: "test@example.com",
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockUsers)
	}))
	defer server.Close()

	client, _ := NewClient(Config{
		URL:    server.URL,
		APIKey: "test-key",
	})

	user, err := client.GetUserByID(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("GetUserByID() error = %v", err)
	}

	if user.ID != "user-1" {
		t.Errorf("expected user-1, got %s", user.ID)
	}
}

func TestUpsertInstagramAccount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST request, got %s", r.Method)
		}
		if r.Header.Get("Prefer") != "resolution=merge-duplicates,return=representation" {
			t.Errorf("expected Prefer header, got %s", r.Header.Get("Prefer"))
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	client, _ := NewClient(Config{
		URL:    server.URL,
		APIKey: "test-key",
	})

	err := client.UpsertInstagramAccount(context.Background(), "user-1", "ig-1", "token", "username", time.Now())
	if err != nil {
		t.Fatalf("UpsertInstagramAccount() error = %v", err)
	}
}

func TestGetMediaAsset(t *testing.T) {
	mockAssets := []MediaAsset{
		{
			ID:     "asset-1",
			UserID: "user-1",
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockAssets)
	}))
	defer server.Close()

	client, _ := NewClient(Config{
		URL:    server.URL,
		APIKey: "test-key",
	})

	asset, err := client.GetMediaAsset(context.Background(), "asset-1")
	if err != nil {
		t.Fatalf("GetMediaAsset() error = %v", err)
	}

	if asset.ID != "asset-1" {
		t.Errorf("expected asset-1, got %s", asset.ID)
	}
}

func TestUpdateMediaVisionAnalysis(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client, _ := NewClient(Config{
		URL:    server.URL,
		APIKey: "test-key",
	})

	err := client.UpdateMediaVisionAnalysis(context.Background(), "asset-1", map[string]interface{}{"tags": []string{"nature"}})
	if err != nil {
		t.Fatalf("UpdateMediaVisionAnalysis() error = %v", err)
	}
}

func TestGetTopPerformingAssets(t *testing.T) {
	mockAssets := []MediaAsset{
		{ID: "asset-1", UserID: "user-1"},
		{ID: "asset-2", UserID: "user-1"},
	}
	mockAnalytics1 := []InteractionAnalytics{{AssetID: "asset-1", EngagementRate: 0.5}}
	mockAnalytics2 := []InteractionAnalytics{{AssetID: "asset-2", EngagementRate: 0.8}}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/rest/v1/media_assets" {
			json.NewEncoder(w).Encode(mockAssets)
		} else if r.URL.Path == "/rest/v1/interaction_analytics" {
			if r.URL.Query().Get("asset_id") == "eq.asset-1" {
				json.NewEncoder(w).Encode(mockAnalytics1)
			} else {
				json.NewEncoder(w).Encode(mockAnalytics2)
			}
		}
	}))
	defer server.Close()

	client, _ := NewClient(Config{
		URL:    server.URL,
		APIKey: "test-key",
	})

	results, err := client.GetTopPerformingAssets(context.Background(), "user-1", 10)
	if err != nil {
		t.Fatalf("GetTopPerformingAssets() error = %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
	// Verify sorting (asset-2 has higher ER)
	if results[0].Asset.ID != "asset-2" {
		t.Errorf("expected asset-2 to be first, got %s", results[0].Asset.ID)
	}
}

