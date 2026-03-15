package supabase

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthMiddleware_Authenticate(t *testing.T) {
	mockUser := AuthUser{
		ID:    "user-1",
		Email: "test@example.com",
		Role:  "authenticated",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer valid-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			ID    string `json:"id"`
			Email string `json:"email"`
			Role  string `json:"role"`
			Aud   string `json:"aud"`
		}{
			ID:    mockUser.ID,
			Email: mockUser.Email,
			Role:  mockUser.Role,
			Aud:   "authenticated",
		})
	}))
	defer server.Close()

	am := NewAuthMiddleware(server.URL, "test-key")

	tests := []struct {
		name           string
		token          string
		expectedStatus int
	}{
		{
			name:           "valid token",
			token:          "valid-token",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "invalid token",
			token:          "invalid-token",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "missing token",
			token:          "",
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := am.Authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				user, ok := GetUserFromContext(r.Context())
				if !ok {
					t.Error("expected user in context")
				}
				if user.ID != mockUser.ID {
					t.Errorf("expected user ID %s, got %s", mockUser.ID, user.ID)
				}
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, "http://example.com/api", nil)
			if tt.token != "" {
				req.Header.Set("Authorization", "Bearer "+tt.token)
			}
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, w.Code)
			}
		})
	}
}

func TestAuthMiddleware_RequireRole(t *testing.T) {
	am := NewAuthMiddleware("https://example.supabase.co", "test-key")

	tests := []struct {
		name           string
		user           *AuthUser
		requiredRole   string
		expectedStatus int
	}{
		{
			name: "correct role",
			user: &AuthUser{
				ID:   "user-1",
				Role: "admin",
			},
			requiredRole:   "admin",
			expectedStatus: http.StatusOK,
		},
		{
			name: "incorrect role",
			user: &AuthUser{
				ID:   "user-1",
				Role: "authenticated",
			},
			requiredRole:   "admin",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "no user in context",
			user:           nil,
			requiredRole:   "admin",
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := am.RequireRole(tt.requiredRole)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, "http://example.com/api", nil)
			if tt.user != nil {
				ctx := context.WithValue(req.Context(), UserContextKey, tt.user)
				req = req.WithContext(ctx)
			}
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, w.Code)
			}
		})
	}
}
