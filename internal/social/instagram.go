package social

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/ekinbulut/x/internal/ai"
	"github.com/ekinbulut/x/internal/supabase"
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

// InstagramPost, Instagram Graph API'den dönen gönderi verisini temsil eder.
type InstagramPost struct {
	ID        string `json:"id"`
	Caption   string `json:"caption"`
	MediaType string `json:"media_type"`
	MediaURL  string `json:"media_url"`
	Permalink string `json:"permalink"`
	Timestamp string `json:"timestamp"`
}

// FetchRecentPosts, kullanıcının geçmiş gönderilerini çeker.
func (c *InstagramClient) FetchRecentPosts(ctx context.Context, limit int) ([]InstagramPost, error) {
	url := fmt.Sprintf(
		"%s/%s/media?fields=id,caption,media_type,media_url,permalink,timestamp&limit=%d&access_token=%s",
		c.baseURL, c.config.AccountID, limit, c.config.AccessToken,
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
		Data []InstagramPost `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("yanıt çözümlenemedi: %w", err)
	}

	return response.Data, nil
}

// CalculateImpactScore basit bir ağırlıklı toplam üzerinden etki puanını hesaplar.
func CalculateImpactScore(insights *InstagramInsights) float64 {
	if insights == nil {
		return 0.0
	}
	// Etkileşim verilerine göre hesaplanan impact_score:
	// Engagement ağırlığı: x5, Saves ağırlığı: x10, Impressions: x0.1, Reach: x0.2
	score := float64(insights.Engagement)*5.0 +
		float64(insights.Saves)*10.0 +
		float64(insights.Impressions)*0.1 +
		float64(insights.Reach)*0.2

	return score
}

// InitialSync, kullanıcının geçmiş postlarını çekip media_assets tablosuna
// (vision analiziyle birlikte) kaydeder. impact_score değerini de hesaplar.
func (c *InstagramClient) InitialSync(ctx context.Context, userID string, limit int, sbClient *supabase.Client, vp *ai.VisionPipeline) error {
	log.Printf("📥 Initial Sync başlatılıyor... Kullanıcı: %s", userID)

	posts, err := c.FetchRecentPosts(ctx, limit)
	if err != nil {
		return fmt.Errorf("geçmiş gönderiler çekilemedi: %w", err)
	}

	for _, post := range posts {
		var mediaType string
		if post.MediaType == "IMAGE" || post.MediaType == "CAROUSEL_ALBUM" {
			mediaType = "image"
		} else if post.MediaType == "VIDEO" {
			mediaType = "video"
		} else {
			continue // Diğer tipleri atla
		}

		// Insights verisini çek (etkileşim verilerini al)
		insights, err := c.GetPostInsights(ctx, post.ID)
		var impactScore float64 = 0
		if err == nil {
			impactScore = CalculateImpactScore(insights)
		} else {
			log.Printf("⚠️ [%s] gönderisi için insight alınamadı, puan 0: %v", post.ID, err)
		}

		// Media varlığını veritabanına kaydet
		asset := supabase.MediaAsset{
			UserID:           userID,
			MediaType:        mediaType,
			StorageURL:       post.MediaURL, // Resim veya video URL'i
			IsPublished:      true,
			OriginalFilename: "ig_sync_" + post.ID,
			ImpactScore:      impactScore,
			MimeType:         "image/jpeg", // Varsayılan tip, video ise farklı olabilir ama genelde sorun yaşatmaz
		}

		createdAsset, err := sbClient.CreateMediaAsset(ctx, asset)
		if err != nil {
			log.Printf("❌ [%s] veritabanına kaydedilemedi: %v", post.ID, err)
			continue
		}

		log.Printf("💾 [%s] başarıyla kaydedildi, db id: %s, impact_score: %.2f", post.ID, createdAsset.ID, impactScore)

		// Eğer image ise Vision Pipeline aracılığıyla analiz çalıştır
		if mediaType == "image" && vp != nil {
			_, err := vp.ProcessAsset(ctx, createdAsset.ID)
			if err != nil {
				log.Printf("⚠️ [%s] için vision analizi başarısız: %v", post.ID, err)
			} else {
				log.Printf("👁️ [%s] için vision analizi tamamlandı", post.ID)
			}
		}
	}

	log.Printf("✅ Initial Sync tamamlandı!")
	return nil
}
