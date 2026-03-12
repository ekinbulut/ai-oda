package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Config, AI üretici istemcisi için gerekli yapılandırmayı tutar.
type Config struct {
	Provider string // "openai" veya "gemini"
	APIKey   string
	Model    string // ör: "gpt-4o", "gemini-2.0-flash"
}

// Generator, AI içerik üretim istemcisidir.
type Generator struct {
	config     Config
	httpClient *http.Client
}

// ContentResult, AI tarafından üretilen içeriği tutar.
type ContentResult struct {
	Caption  string `json:"caption"`  // Instagram başlığı
	Hashtags string `json:"hashtags"` // Önerilen hashtag'ler
	ImageAlt string `json:"image_alt"` // Görsel için alternatif metin / prompt
}

// NewGenerator, yeni bir AI içerik üretici oluşturur.
func NewGenerator(cfg Config) (*Generator, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("AI API key boş olamaz")
	}
	if cfg.Provider == "" {
		cfg.Provider = "openai"
	}
	if cfg.Model == "" {
		switch cfg.Provider {
		case "openai":
			cfg.Model = "gpt-4o"
		case "gemini":
			cfg.Model = "gemini-2.0-flash"
		}
	}

	return &Generator{
		config: cfg,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}, nil
}

// GenerateContent, verilen prompt'a göre Instagram içeriği üretir.
func (g *Generator) GenerateContent(ctx context.Context, prompt string) (*ContentResult, error) {
	switch g.config.Provider {
	case "openai":
		return g.generateWithOpenAI(ctx, prompt)
	case "gemini":
		return g.generateWithGemini(ctx, prompt)
	default:
		return nil, fmt.Errorf("desteklenmeyen AI sağlayıcı: %s", g.config.Provider)
	}
}

// generateWithOpenAI, OpenAI API'sini kullanarak içerik üretir.
func (g *Generator) generateWithOpenAI(ctx context.Context, prompt string) (*ContentResult, error) {
	url := "https://api.openai.com/v1/chat/completions"

	systemPrompt := `Sen bir sosyal medya içerik uzmanısın. Verilen konuya göre Instagram için 
çekici ve etkileşim odaklı içerik üretmelisin. Yanıtını şu JSON formatında ver:
{"caption": "...", "hashtags": "#tag1 #tag2 ...", "image_alt": "Görsel için açıklama/prompt"}`

	payload := map[string]interface{}{
		"model": g.config.Model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": prompt},
		},
		"temperature":     0.8,
		"response_format": map[string]string{"type": "json_object"},
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
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", g.config.APIKey))

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("istek gönderilemedi: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("OpenAI API hatası (status %d): %s", resp.StatusCode, string(respBody))
	}

	var openAIResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&openAIResp); err != nil {
		return nil, fmt.Errorf("yanıt çözümlenemedi: %w", err)
	}

	if len(openAIResp.Choices) == 0 {
		return nil, fmt.Errorf("OpenAI boş yanıt döndü")
	}

	var result ContentResult
	if err := json.Unmarshal([]byte(openAIResp.Choices[0].Message.Content), &result); err != nil {
		return nil, fmt.Errorf("içerik JSON çözümlenemedi: %w", err)
	}

	return &result, nil
}

// generateWithGemini, Google Gemini API'sini kullanarak içerik üretir.
func (g *Generator) generateWithGemini(ctx context.Context, prompt string) (*ContentResult, error) {
	url := fmt.Sprintf(
		"https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s",
		g.config.Model, g.config.APIKey,
	)

	fullPrompt := fmt.Sprintf(`Sen bir sosyal medya içerik uzmanısın. Verilen konuya göre Instagram için 
çekici ve etkileşim odaklı içerik üretmelisin. Yanıtını şu JSON formatında ver:
{"caption": "...", "hashtags": "#tag1 #tag2 ...", "image_alt": "Görsel için açıklama/prompt"}

Konu: %s`, prompt)

	payload := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"parts": []map[string]string{
					{"text": fullPrompt},
				},
			},
		},
		"generationConfig": map[string]interface{}{
			"temperature":    0.8,
			"responseMimeType": "application/json",
		},
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

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("istek gönderilemedi: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Gemini API hatası (status %d): %s", resp.StatusCode, string(respBody))
	}

	var geminiResp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		return nil, fmt.Errorf("yanıt çözümlenemedi: %w", err)
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("Gemini boş yanıt döndü")
	}

	var result ContentResult
	if err := json.Unmarshal([]byte(geminiResp.Candidates[0].Content.Parts[0].Text), &result); err != nil {
		return nil, fmt.Errorf("içerik JSON çözümlenemedi: %w", err)
	}

	return &result, nil
}
