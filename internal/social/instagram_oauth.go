package social

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OAuthConfig, Instagram OAuth akışı için gerekli yapılandırmayı tutar.
type OAuthConfig struct {
	ClientID     string // Facebook/Instagram App ID
	ClientSecret string // Facebook/Instagram App Secret
	RedirectURI  string // OAuth callback URL'i (ör: https://yourdomain.com/auth/instagram/callback)
}

// OAuthClient, Instagram OAuth işlemlerini yöneten istemcidir.
type OAuthClient struct {
	config     OAuthConfig
	httpClient *http.Client
}

// OAuthTokenResponse, Instagram'dan dönen token yanıtını tutar.
type OAuthTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"` // saniye cinsinden (long-lived: ~60 gün)
}

// InstagramProfile, Instagram kullanıcı profil bilgilerini tutar.
type InstagramProfile struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
}

// ConnectedAccount, OAuth akışı sonucunda elde edilen tüm bilgileri tutar.
// Bu struct, Supabase'e kaydedilecek veriyi temsil eder.
type ConnectedAccount struct {
	InstagramAccountID string
	Username           string
	AccessToken        string
	TokenExpiresAt     time.Time
}

// NewOAuthClient, yeni bir Instagram OAuth istemcisi oluşturur.
func NewOAuthClient(cfg OAuthConfig) (*OAuthClient, error) {
	if cfg.ClientID == "" {
		return nil, fmt.Errorf("instagram client ID boş olamaz")
	}
	if cfg.ClientSecret == "" {
		return nil, fmt.Errorf("instagram client secret boş olamaz")
	}
	if cfg.RedirectURI == "" {
		return nil, fmt.Errorf("instagram redirect URI boş olamaz")
	}

	return &OAuthClient{
		config: cfg,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}, nil
}

// GetAuthorizationURL, kullanıcıyı yönlendireceğimiz Instagram yetkilendirme URL'ini oluşturur.
// state parametresi CSRF koruması için kullanılır (genellikle JWT veya random token).
func (oc *OAuthClient) GetAuthorizationURL(state string) string {
	params := url.Values{
		"client_id":     {oc.config.ClientID},
		"redirect_uri":  {oc.config.RedirectURI},
		"scope":         {"instagram_basic,instagram_content_publish,instagram_manage_insights,pages_show_list"},
		"response_type": {"code"},
		"state":         {state},
	}

	return fmt.Sprintf("https://www.facebook.com/v21.0/dialog/oauth?%s", params.Encode())
}

// HandleCallback, Instagram OAuth callback'ini işler.
// Tam akış: authorization code → short-lived token → long-lived token → profil bilgisi
func (oc *OAuthClient) HandleCallback(ctx context.Context, code string) (*ConnectedAccount, error) {
	// Adım 1: Authorization code ile short-lived token al
	shortToken, err := oc.exchangeCodeForToken(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("code → token değişimi başarısız: %w", err)
	}

	// Adım 2: Short-lived token'ı long-lived token'a çevir
	longToken, err := oc.exchangeForLongLivedToken(ctx, shortToken.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("long-lived token alınamadı: %w", err)
	}

	// Adım 3: Kullanıcının Instagram Business hesap ID'sini bul
	profile, err := oc.getInstagramProfile(ctx, longToken.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("instagram profili alınamadı: %w", err)
	}

	// Token süresini hesapla
	expiresAt := time.Now().Add(time.Duration(longToken.ExpiresIn) * time.Second)

	return &ConnectedAccount{
		InstagramAccountID: profile.ID,
		Username:           profile.Username,
		AccessToken:        longToken.AccessToken,
		TokenExpiresAt:     expiresAt,
	}, nil
}

// exchangeCodeForToken, authorization code'u short-lived access token ile değiştirir.
// Facebook OAuth endpoint: POST https://graph.facebook.com/v21.0/oauth/access_token
func (oc *OAuthClient) exchangeCodeForToken(ctx context.Context, code string) (*OAuthTokenResponse, error) {
	params := url.Values{
		"client_id":     {oc.config.ClientID},
		"client_secret": {oc.config.ClientSecret},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {oc.config.RedirectURI},
		"code":          {code},
	}

	reqURL := "https://graph.facebook.com/v21.0/oauth/access_token"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, strings.NewReader(params.Encode()))
	if err != nil {
		return nil, fmt.Errorf("istek oluşturulamadı: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := oc.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("istek gönderilemedi: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token alınamadı (status %d): %s", resp.StatusCode, string(body))
	}

	var tokenResp OAuthTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("token yanıtı çözümlenemedi: %w", err)
	}

	return &tokenResp, nil
}

// exchangeForLongLivedToken, short-lived token'ı ~60 günlük long-lived token'a çevirir.
// GET https://graph.facebook.com/v21.0/oauth/access_token?grant_type=fb_exchange_token&...
func (oc *OAuthClient) exchangeForLongLivedToken(ctx context.Context, shortToken string) (*OAuthTokenResponse, error) {
	params := url.Values{
		"grant_type":        {"fb_exchange_token"},
		"client_id":         {oc.config.ClientID},
		"client_secret":     {oc.config.ClientSecret},
		"fb_exchange_token": {shortToken},
	}

	reqURL := fmt.Sprintf("https://graph.facebook.com/v21.0/oauth/access_token?%s", params.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("istek oluşturulamadı: %w", err)
	}

	resp, err := oc.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("istek gönderilemedi: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("long-lived token alınamadı (status %d): %s", resp.StatusCode, string(body))
	}

	var tokenResp OAuthTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("token yanıtı çözümlenemedi: %w", err)
	}

	return &tokenResp, nil
}

// getInstagramProfile, Facebook token'ı ile bağlı Instagram Business hesabını bulur.
// Akış: token → /me/accounts → Page ID → /{page-id}?fields=instagram_business_account → IG ID
func (oc *OAuthClient) getInstagramProfile(ctx context.Context, accessToken string) (*InstagramProfile, error) {
	// Adım A: Kullanıcının yönettiği Facebook sayfalarını getir
	pagesURL := fmt.Sprintf(
		"https://graph.facebook.com/v21.0/me/accounts?fields=id,name,instagram_business_account&access_token=%s",
		accessToken,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pagesURL, nil)
	if err != nil {
		return nil, fmt.Errorf("istek oluşturulamadı: %w", err)
	}

	resp, err := oc.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("istek gönderilemedi: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sayfalar alınamadı (status %d): %s", resp.StatusCode, string(body))
	}

	var pagesResp struct {
		Data []struct {
			ID                       string `json:"id"`
			Name                     string `json:"name"`
			InstagramBusinessAccount *struct {
				ID string `json:"id"`
			} `json:"instagram_business_account"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&pagesResp); err != nil {
		return nil, fmt.Errorf("sayfa yanıtı çözümlenemedi: %w", err)
	}

	// Instagram Business hesabı bağlı olan ilk sayfayı bul
	var igAccountID string
	for _, page := range pagesResp.Data {
		if page.InstagramBusinessAccount != nil {
			igAccountID = page.InstagramBusinessAccount.ID
			break
		}
	}

	if igAccountID == "" {
		return nil, fmt.Errorf("bağlı Instagram Business hesabı bulunamadı; hesabınızın bir Facebook sayfasına bağlı olduğundan emin olun")
	}

	// Adım B: Instagram hesap detaylarını çek
	igURL := fmt.Sprintf(
		"https://graph.facebook.com/v21.0/%s?fields=id,username,name&access_token=%s",
		igAccountID, accessToken,
	)

	igReq, err := http.NewRequestWithContext(ctx, http.MethodGet, igURL, nil)
	if err != nil {
		return nil, fmt.Errorf("istek oluşturulamadı: %w", err)
	}

	igResp, err := oc.httpClient.Do(igReq)
	if err != nil {
		return nil, fmt.Errorf("istek gönderilemedi: %w", err)
	}
	defer igResp.Body.Close()

	if igResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(igResp.Body)
		return nil, fmt.Errorf("instagram profili alınamadı (status %d): %s", igResp.StatusCode, string(body))
	}

	var profile InstagramProfile
	if err := json.NewDecoder(igResp.Body).Decode(&profile); err != nil {
		return nil, fmt.Errorf("profil yanıtı çözümlenemedi: %w", err)
	}

	return &profile, nil
}

// TokenManager, token yenileme ve bildirim süreçlerini yöneten yapıdır.
type TokenManager struct {
	oauthClient *OAuthClient
	store       TokenStore
	notifier    TokenNotifier
}

// TokenInfo, token saklama biriminden okunacak hesap bilgisidir.
type TokenInfo struct {
	UserID         string
	AccountID      string
	AccessToken    string
	TokenExpiresAt time.Time
}

// TokenStore, veritabanı işlemlerini soyutlar.
type TokenStore interface {
	GetActiveAccounts(ctx context.Context) ([]TokenInfo, error)
	UpdateToken(ctx context.Context, userID, accountID, newAccessToken string, newExpiresAt time.Time) error
}

// TokenNotifier, başarısız yenileme durumunda kullanıcıya bildirim gönderir.
type TokenNotifier interface {
	NotifyUser(ctx context.Context, userID, message string)
}

// NewTokenManager yeni bir TokenManager oluşturur.
func NewTokenManager(client *OAuthClient, store TokenStore, notifier TokenNotifier) *TokenManager {
	return &TokenManager{
		oauthClient: client,
		store:       store,
		notifier:    notifier,
	}
}

// CheckAndRefreshTokens süresi dolmak üzere olan hesapları bulur ve yeniler.
func (tm *TokenManager) CheckAndRefreshTokens(ctx context.Context) error {
	accounts, err := tm.store.GetActiveAccounts(ctx)
	if err != nil {
		return fmt.Errorf("aktif hesaplar çekilemedi: %w", err)
	}

	now := time.Now()
	// 72 saat sınırı
	threshold := now.Add(72 * time.Hour)

	for _, acc := range accounts {
		if !acc.TokenExpiresAt.IsZero() && acc.TokenExpiresAt.Before(threshold) {
			// Yenilemeye çalış
			resp, err := tm.oauthClient.RefreshAccessToken(ctx, acc.AccessToken)
			if err != nil {
				// Hata bildirimi
				tm.notifier.NotifyUser(ctx, acc.UserID, "Instagram erişim token süreniz dolmak üzere ve otomatik yenilenemedi. Lütfen tekrar giriş yapın.")
				continue
			}

			// Başarılı, DB'yi güncelle
			newExpiresAt := time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second)
			_ = tm.store.UpdateToken(ctx, acc.UserID, acc.AccountID, resp.AccessToken, newExpiresAt)
		}
	}

	return nil
}

// RefreshAccessToken, mevcut bir long-lived token'ı yeniler.
func (oc *OAuthClient) RefreshAccessToken(ctx context.Context, currentToken string) (*OAuthTokenResponse, error) {
	return oc.exchangeForLongLivedToken(ctx, currentToken)
}
