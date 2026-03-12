package social

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// InstagramConfig, Instagram Graph API istemcisi için gerekli yapılandırmayı tutar.
type InstagramConfig struct {
	AccessToken string // Instagram / Facebook uzun ömürlü erişim token'ı
	AccountID   string // Instagram Business hesap ID'si
}

// InstagramClient, Instagram Graph API istemcisidir.
type InstagramClient struct {
	config     InstagramConfig
	httpClient *http.Client
	baseURL    string
}

// PublishResult, gönderim sonucunu tutar.
type PublishResult struct {
	PostID    string `json:"id"`
	Permalink string `json:"permalink,omitempty"`
}

// InstagramInsights, bir gönderinin etkileşim metriklerini temsil eder.
type InstagramInsights struct {
	Impressions int `json:"impressions"`
	Reach       int `json:"reach"`
	Engagement  int `json:"engagement"`
	Saves       int `json:"saved"`
}

// NewInstagramClient, yeni bir Instagram istemcisi oluşturur.
func NewInstagramClient(cfg InstagramConfig) *InstagramClient {
	return &InstagramClient{
		config: cfg,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		baseURL: "https://graph.facebook.com/v21.0",
	}
}

// PostMedia, bir fotoğrafı Instagram'a yayınlar.
// Instagram Graph API iki adımlı bir süreç kullanır:
// 1. Medya kapsayıcısı oluştur
// 2. Kapsayıcıyı yayınla
func (c *InstagramClient) PostMedia(ctx context.Context, imageURL, caption string) (*PublishResult, error) {
	// Adım 1: Medya kapsayıcısı oluştur
	containerID, err := c.createMediaContainer(ctx, imageURL, caption)
	if err != nil {
		return nil, fmt.Errorf("medya kapsayıcısı oluşturulamadı: %w", err)
	}

	// Adım 2: Medyayı yayınla
	result, err := c.publishMedia(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("medya yayınlanamadı: %w", err)
	}

	return result, nil
}

// createMediaContainer, Instagram Graph API'de bir medya kapsayıcısı oluşturur.
func (c *InstagramClient) createMediaContainer(ctx context.Context, imageURL, caption string) (string, error) {
	url := fmt.Sprintf("%s/%s/media", c.baseURL, c.config.AccountID)

	payload := map[string]string{
		"image_url":    imageURL,
		"caption":      caption,
		"access_token": c.config.AccessToken,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("payload serileştirilemedi: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("istek oluşturulamadı: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("istek gönderilemedi: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API hatası (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("yanıt çözümlenemedi: %w", err)
	}

	return result.ID, nil
}

// publishMedia, oluşturulan medya kapsayıcısını yayınlar.
func (c *InstagramClient) publishMedia(ctx context.Context, containerID string) (*PublishResult, error) {
	url := fmt.Sprintf("%s/%s/media_publish", c.baseURL, c.config.AccountID)

	payload := map[string]string{
		"creation_id":  containerID,
		"access_token": c.config.AccessToken,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("payload serileştirilemedi: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("istek oluşturulamadı: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("istek gönderilemedi: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API hatası (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result PublishResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("yanıt çözümlenemedi: %w", err)
	}

	return &result, nil
}

// GetPostInsights, bir gönderinin etkileşim istatistiklerini getirir.
func (c *InstagramClient) GetPostInsights(ctx context.Context, postID string) (*InstagramInsights, error) {
	url := fmt.Sprintf(
		"%s/%s/insights?metric=impressions,reach,engagement,saved&access_token=%s",
		c.baseURL, postID, c.config.AccessToken,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("istek oluşturulamadı: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("istek gönderilemedi: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API hatası (%d): %s", resp.StatusCode, string(body))
	}

	var response struct {
		Data []struct {
			Name   string `json:"name"`
			Values []struct {
				Value int `json:"value"`
			} `json:"values"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("yanıt çözümlenemedi: %w", err)
	}

	insights := &InstagramInsights{}
	for _, metric := range response.Data {
		value := 0
		if len(metric.Values) > 0 {
			value = metric.Values[0].Value
		}

		switch metric.Name {
		case "impressions":
			insights.Impressions = value
		case "reach":
			insights.Reach = value
		case "engagement":
			insights.Engagement = value
		case "saved":
			insights.Saves = value
		}
	}

	return insights, nil
}
