package supabase

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// contextKey, context'e değer eklemek için kullanılan özel tip.
type contextKey string

const (
	// UserContextKey, doğrulanmış kullanıcı bilgisinin context'teki anahtarıdır.
	UserContextKey contextKey = "authenticated_user"
)

// AuthUser, JWT'den çözümlenen doğrulanmış kullanıcı bilgilerini tutar.
type AuthUser struct {
	ID    string `json:"sub"`
	Email string `json:"email"`
	Role  string `json:"role"`
	Aud   string `json:"aud"`
}

// AuthMiddleware, Supabase JWT doğrulaması yapan middleware'dir.
// Gelen isteklerdeki Authorization başlığından Bearer token'ı alır,
// Supabase Auth API'ye göndererek doğrular ve kullanıcı bilgisini context'e ekler.
type AuthMiddleware struct {
	supabaseURL string
	apiKey      string
	httpClient  *http.Client
}

// NewAuthMiddleware, yeni bir AuthMiddleware oluşturur.
func NewAuthMiddleware(supabaseURL, apiKey string) *AuthMiddleware {
	return &AuthMiddleware{
		supabaseURL: strings.TrimRight(supabaseURL, "/"),
		apiKey:      apiKey,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Authenticate, chi middleware zincirinde kullanılmak üzere http.Handler döner.
// Token doğrulanmazsa 401 Unauthorized yanıtı döner.
func (am *AuthMiddleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, err := extractBearerToken(r)
		if err != nil {
			writeAuthError(w, http.StatusUnauthorized, "Token bulunamadı", err.Error())
			return
		}

		// Supabase Auth API ile token doğrula
		authUser, err := am.verifyToken(r.Context(), token)
		if err != nil {
			log.Printf("🔒 Token doğrulama başarısız: %v", err)
			writeAuthError(w, http.StatusUnauthorized, "Geçersiz veya süresi dolmuş token", err.Error())
			return
		}

		// Kullanıcı bilgisini context'e ekle
		ctx := context.WithValue(r.Context(), UserContextKey, authUser)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireRole, belirli bir role sahip olmayı zorunlu kılan middleware.
// Authenticate middleware'inden sonra kullanılmalıdır.
func (am *AuthMiddleware) RequireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := GetUserFromContext(r.Context())
			if !ok {
				writeAuthError(w, http.StatusUnauthorized, "Kimlik doğrulama gerekli", "context'te kullanıcı bulunamadı")
				return
			}

			if user.Role != role {
				writeAuthError(w, http.StatusForbidden, "Yetersiz yetki", fmt.Sprintf("gerekli rol: %s, mevcut rol: %s", role, user.Role))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// verifyToken, Supabase Auth API'nin /auth/v1/user endpoint'ini kullanarak
// JWT token'ı doğrular ve kullanıcı bilgilerini döner.
func (am *AuthMiddleware) verifyToken(ctx context.Context, token string) (*AuthUser, error) {
	url := fmt.Sprintf("%s/auth/v1/user", am.supabaseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("istek oluşturulamadı: %w", err)
	}

	req.Header.Set("apikey", am.apiKey)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	resp, err := am.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("supabase auth isteği başarısız: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("token geçersiz veya süresi dolmuş")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("beklenmeyen durum kodu: %d", resp.StatusCode)
	}

	// Supabase /auth/v1/user yanıtı:
	// { "id": "uuid", "email": "...", "role": "authenticated", "aud": "authenticated", ... }
	var userResp struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		Role  string `json:"role"`
		Aud   string `json:"aud"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&userResp); err != nil {
		return nil, fmt.Errorf("kullanıcı yanıtı çözümlenemedi: %w", err)
	}

	if userResp.ID == "" {
		return nil, fmt.Errorf("kullanıcı ID boş döndü")
	}

	return &AuthUser{
		ID:    userResp.ID,
		Email: userResp.Email,
		Role:  userResp.Role,
		Aud:   userResp.Aud,
	}, nil
}

// extractBearerToken, Authorization başlığından Bearer token'ı çıkarır.
func extractBearerToken(r *http.Request) (string, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", fmt.Errorf("Authorization başlığı bulunamadı")
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", fmt.Errorf("geçersiz Authorization formatı, 'Bearer <token>' bekleniyor")
	}

	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", fmt.Errorf("token boş")
	}

	return token, nil
}

// GetUserFromContext, context'ten doğrulanmış kullanıcı bilgisini çıkarır.
// Handler fonksiyonları içinde kullanılır.
func GetUserFromContext(ctx context.Context) (*AuthUser, bool) {
	user, ok := ctx.Value(UserContextKey).(*AuthUser)
	return user, ok
}

// MustGetUserFromContext, context'ten kullanıcı bilgisini çıkarır.
// Kullanıcı bulunamazsa panic atar. Sadece Authenticate middleware'inden
// sonraki handler'larda kullanılmalıdır.
func MustGetUserFromContext(ctx context.Context) *AuthUser {
	user, ok := GetUserFromContext(ctx)
	if !ok {
		panic("MustGetUserFromContext: Authenticate middleware olmadan çağrıldı")
	}
	return user
}

// writeAuthError, standart JSON hata yanıtı yazar.
func writeAuthError(w http.ResponseWriter, status int, message, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{
		"error":   message,
		"detail":  detail,
	})
}
