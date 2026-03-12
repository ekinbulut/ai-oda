package supabase

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Config, Supabase istemcisi için gerekli yapılandırmayı tutar.
type Config struct {
	URL    string // Supabase proje URL'i (ör: https://xxx.supabase.co)
	APIKey string // Supabase service_role anahtarı
}

// Client, Supabase REST API ve Auth işlemleri için istemcidir.
type Client struct {
	config     Config
	httpClient *http.Client
}

// User, Supabase'deki kullanıcıyı temsil eder.
type User struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

// ContentTask, zamanlanmış bir içerik görevini temsil eder.
type ContentTask struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	Status      string    `json:"status"` // pending, processing, completed, failed
	ContentType string    `json:"content_type"`
	Prompt      string    `json:"prompt"`
	Result      string    `json:"result,omitempty"`
	ScheduledAt time.Time `json:"scheduled_at"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// NewClient, yeni bir Supabase istemcisi oluşturur.
func NewClient(cfg Config) (*Client, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("supabase URL boş olamaz")
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("supabase API key boş olamaz")
	}

	return &Client{
		config: cfg,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// GetPendingTasks, durumu "pending" olan ve zamanı gelmiş görevleri getirir.
func (c *Client) GetPendingTasks(ctx context.Context) ([]ContentTask, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	url := fmt.Sprintf("%s/rest/v1/content_tasks?status=eq.pending&scheduled_at=lte.%s&order=scheduled_at.asc", c.config.URL, now)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("istek oluşturulamadı: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("istek gönderilemedi: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("beklenmeyen durum kodu %d: %s", resp.StatusCode, string(body))
	}

	var tasks []ContentTask
	if err := json.NewDecoder(resp.Body).Decode(&tasks); err != nil {
		return nil, fmt.Errorf("yanıt çözümlenemedi: %w", err)
	}

	return tasks, nil
}

// UpdateTaskStatus, bir görevin durumunu günceller.
func (c *Client) UpdateTaskStatus(ctx context.Context, taskID, status, result string) error {
	url := fmt.Sprintf("%s/rest/v1/content_tasks?id=eq.%s", c.config.URL, taskID)

	payload := map[string]interface{}{
		"status":     status,
		"result":     result,
		"updated_at": time.Now().UTC().Format(time.RFC3339),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("payload serileştirilemedi: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("istek oluşturulamadı: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("istek gönderilemedi: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("beklenmeyen durum kodu %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// GetUserByID, kullanıcı bilgilerini ID'ye göre getirir.
func (c *Client) GetUserByID(ctx context.Context, userID string) (*User, error) {
	url := fmt.Sprintf("%s/rest/v1/users?id=eq.%s", c.config.URL, userID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("istek oluşturulamadı: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("istek gönderilemedi: %w", err)
	}
	defer resp.Body.Close()

	var users []User
	if err := json.NewDecoder(resp.Body).Decode(&users); err != nil {
		return nil, fmt.Errorf("yanıt çözümlenemedi: %w", err)
	}

	if len(users) == 0 {
		return nil, fmt.Errorf("kullanıcı bulunamadı: %s", userID)
	}

	return &users[0], nil
}

// UpsertInstagramAccount, bir Instagram hesabını Supabase'e kaydeder veya mevcut kaydı günceller.
// UPSERT mantığı: aynı (user_id, instagram_account_id) kombinasyonu varsa günceller, yoksa yeni oluşturur.
func (c *Client) UpsertInstagramAccount(ctx context.Context, userID, igAccountID, accessToken, username string, tokenExpiresAt time.Time) error {
	url := fmt.Sprintf("%s/rest/v1/instagram_accounts", c.config.URL)

	payload := map[string]interface{}{
		"user_id":              userID,
		"instagram_account_id": igAccountID,
		"access_token":         accessToken,
		"username":             username,
		"token_expires_at":     tokenExpiresAt.UTC().Format(time.RFC3339),
		"is_active":            true,
		"updated_at":           time.Now().UTC().Format(time.RFC3339),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("payload serileştirilemedi: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("istek oluşturulamadı: %w", err)
	}
	c.setHeaders(req)
	// Supabase UPSERT: conflict olan sütunlarda güncelle
	req.Header.Set("Prefer", "resolution=merge-duplicates,return=representation")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("istek gönderilemedi: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("instagram hesabı kaydedilemedi (status %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// setHeaders, Supabase REST API istekleri için gerekli başlıkları ayarlar.
func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("apikey", c.config.APIKey)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.config.APIKey))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Prefer", "return=representation")
}

