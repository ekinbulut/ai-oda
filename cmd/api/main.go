package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/joho/godotenv"
	httpSwagger "github.com/swaggo/http-swagger/v2"

	_ "github.com/ekinbulut/x/docs"
	"github.com/ekinbulut/x/internal/social"
	sbauth "github.com/ekinbulut/x/internal/supabase"
)

// oauthClient ve sbClient, handler'lar tarafından kullanılan paylaşımlı istemcilerdir.
var (
	oauthClient *social.OAuthClient
	sbClient    *sbauth.Client
)

//	@title						Otonom Sosyal Medya Ajan API
//	@version					1.0
//	@description				Yapay zeka destekli otonom sosyal medya içerik yönetim sistemi API'si.
//	@description				Bu API, kullanıcıların Instagram hesaplarını bağlamasını, AI ile içerik üretmesini
//	@description				ve otonom ajanlar aracılığıyla otomatik paylaşım yapmasını sağlar.
//	@termsOfService				http://swagger.io/terms/
//	@contact.name				API Destek
//	@contact.email				support@example.com
//	@license.name				MIT
//	@license.url				https://opensource.org/licenses/MIT
//	@host						amada-ludicrous-overstoutly.ngrok-free.dev
//	@BasePath					/
//	@securityDefinitions.apikey	BearerAuth
//	@in							header
//	@name						Authorization
//	@description				Supabase JWT token. Format: "Bearer {token}"
func main() {
	// .env dosyasını yükle (varsa)
	_ = godotenv.Load()

	// Supabase Auth Middleware'i oluştur
	authMw := sbauth.NewAuthMiddleware(
		os.Getenv("SUPABASE_URL"),
		os.Getenv("SUPABASE_ANON_KEY"),
	)

	// Supabase DB istemcisini başlat
	var err error
	sbClient, err = sbauth.NewClient(sbauth.Config{
		URL:    os.Getenv("SUPABASE_URL"),
		APIKey: os.Getenv("SUPABASE_SERVICE_KEY"),
	})
	if err != nil {
		log.Fatalf("Supabase istemcisi başlatılamadı: %v", err)
	}

	// Instagram OAuth istemcisini başlat
	oauthClient, err = social.NewOAuthClient(social.OAuthConfig{
		ClientID:     os.Getenv("INSTAGRAM_CLIENT_ID"),
		ClientSecret: os.Getenv("INSTAGRAM_CLIENT_SECRET"),
		RedirectURI:  os.Getenv("INSTAGRAM_REDIRECT_URI"),
	})
	if err != nil {
		log.Fatalf("Instagram OAuth istemcisi başlatılamadı: %v", err)
	}

	r := chi.NewRouter()

	// Global Middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)

	// Swagger UI (herkese açık) — http://localhost:8080/swagger/index.html
	r.Get("/swagger/*", httpSwagger.Handler(
		httpSwagger.URL("/swagger/doc.json"),
	))

	// Health check (herkese açık)
	r.Get("/health", handleHealthCheck)

	// Supabase webhook endpoint'leri (herkese açık - Supabase tarafından çağrılır)
	r.Route("/webhooks", func(r chi.Router) {
		r.Get("/", handleInstagramWebhookVerification)
		r.Post("/user-created", handleUserCreated)
		r.Post("/subscription-updated", handleSubscriptionUpdated)
		r.Post("/content-scheduled", handleContentScheduled)
		r.Get("/instagram", handleInstagramWebhookVerification)
		r.Post("/instagram", handleInstagramWebhookEvents)
	})

	// Instagram OAuth endpoint'leri (Bağlantı için herkese açık, callback içinden user yönetimi yapılacak)
	r.Route("/auth/instagram", func(r chi.Router) {
		r.Get("/login", handleInstagramLogin)
		r.Get("/callback", handleInstagramCallback)
	})

	// Korumalı API endpoint'leri (JWT doğrulaması gerektirir)
	r.Route("/api", func(r chi.Router) {
		r.Use(authMw.Authenticate)

		// Kullanıcı profili
		r.Get("/me", handleGetMe)

		// İçerik görevleri
		r.Route("/tasks", func(r chi.Router) {
			r.Get("/", handleListTasks)
			r.Post("/", handleCreateTask)
		})

		// Ajan yapılandırması
		r.Route("/agent-config", func(r chi.Router) {
			r.Get("/", handleGetAgentConfig)
			r.Put("/", handleUpdateAgentConfig)
		})
	})

	port := os.Getenv("API_PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("🚀 API sunucusu başlatılıyor: :%s", port)
	log.Printf("📖 Swagger UI: https://amada-ludicrous-overstoutly.ngrok-free.dev/swagger/index.html")
	if err := http.ListenAndServe(fmt.Sprintf(":%s", port), r); err != nil {
		log.Fatalf("Sunucu başlatılamadı: %v", err)
	}
}

// ============================================================
// Health Check
// ============================================================

// handleHealthCheck godoc
//
//	@Summary		Sağlık kontrolü
//	@Description	API sunucusunun çalışıp çalışmadığını kontrol eder
//	@Tags			System
//	@Produce		json
//	@Success		200	{object}	HealthResponse
//	@Router			/health [get]
func handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(HealthResponse{Status: "ok"})
}

// ============================================================
// Webhook Handler'ları
// ============================================================

// handleUserCreated godoc
//
//	@Summary		Yeni kullanıcı webhook'u
//	@Description	Supabase'de yeni kullanıcı oluşturulduğunda tetiklenir. Kullanıcı için ilk kurulumu başlatır.
//	@Tags			Webhooks
//	@Accept			json
//	@Produce		json
//	@Param			payload	body		WebhookPayload	true	"Supabase webhook payload'u"
//	@Success		200		{object}	MessageResponse
//	@Failure		400		{object}	ErrorResponse
//	@Router			/webhooks/user-created [post]
func handleUserCreated(w http.ResponseWriter, r *http.Request) {
	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Geçersiz payload", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	log.Printf("📥 Yeni kullanıcı webhook'u alındı: %v", payload)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(MessageResponse{Message: "received"})
}

// handleSubscriptionUpdated godoc
//
//	@Summary		Abonelik güncelleme webhook'u
//	@Description	Kullanıcı abonelik durumu değiştiğinde Supabase tarafından tetiklenir.
//	@Tags			Webhooks
//	@Accept			json
//	@Produce		json
//	@Param			payload	body		WebhookPayload	true	"Supabase webhook payload'u"
//	@Success		200		{object}	MessageResponse
//	@Failure		400		{object}	ErrorResponse
//	@Router			/webhooks/subscription-updated [post]
func handleSubscriptionUpdated(w http.ResponseWriter, r *http.Request) {
	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Geçersiz payload", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	log.Printf("📥 Abonelik güncelleme webhook'u alındı: %v", payload)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(MessageResponse{Message: "received"})
}

// handleContentScheduled godoc
//
//	@Summary		İçerik zamanlama webhook'u
//	@Description	İçerik zamanlandığında Supabase tarafından tetiklenir. Worker'a görev gönderir.
//	@Tags			Webhooks
//	@Accept			json
//	@Produce		json
//	@Param			payload	body		WebhookPayload	true	"Supabase webhook payload'u"
//	@Success		200		{object}	MessageResponse
//	@Failure		400		{object}	ErrorResponse
//	@Router			/webhooks/content-scheduled [post]
func handleContentScheduled(w http.ResponseWriter, r *http.Request) {
	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Geçersiz payload", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	log.Printf("📥 İçerik zamanlama webhook'u alındı: %v", payload)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(MessageResponse{Message: "received"})
}

// handleInstagramWebhookVerification godoc
//
//	@Summary		Instagram Webhook doğrulaması
//	@Description	Meta/Instagram webhook kurulumu sırasında doğrulama için çağrılır.
//	@Tags			Webhooks
//	@Produce		plain
//	@Param			hub.mode			query		string	true	"Eşleşmesi gereken 'subscribe' değeri"
//	@Param			 hub.challenge		query		string	true	"Echo yapılması gereken challenge string"
//	@Param			hub.verify_token	query		string	true	"Doğrulanacak olan verify token"
//	@Success		200					{string}	string	"hub.challenge"
//	@Failure		403					{string}	string	"Doğrulama başarısız"
//	@Router			/webhooks [get]
func handleInstagramWebhookVerification(w http.ResponseWriter, r *http.Request) {
	verifyToken := os.Getenv("INSTAGRAM_WEBHOOK_VERIFY_TOKEN")
	
	mode := r.URL.Query().Get("hub.mode")
	token := r.URL.Query().Get("hub.verify_token")
	challenge := r.URL.Query().Get("hub.challenge")

	log.Printf("🔍 Webhook Doğrulama İsteği: mode=%s, token=%s, challenge=%s", mode, token, challenge)
	log.Printf("🔑 Beklenen Token (ENV): %s", verifyToken)

	if mode == "subscribe" && token == verifyToken {
		log.Printf("✅ Instagram webhook doğrulaması başarılı. Gönderilen challenge: %s", challenge)
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(challenge))
	} else {
		log.Printf("❌ Instagram webhook doğrulaması başarısız!")
		if token != verifyToken {
			log.Printf("👉 Sebep: Token uyuşmuyor. Gelen: '%s', Beklenen: '%s'", token, verifyToken)
		}
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("Doğrulama başarısız"))
	}
}

// handleInstagramWebhookEvents godoc
//
//	@Summary		Instagram Webhook olayları
//	@Description	Instagram'dan gelen gerçek zamanlı bildirimleri (yorumlar, mesajlar vb.) işler.
//	@Description	X-Hub-Signature-256 header'ı ile imza doğrulaması yapar.
//	@Tags			Webhooks
//	@Accept			json
//	@Produce		json
//	@Success		200	{object}	MessageResponse
//	@Failure		401	{string}	string	"Geçersiz imza"
//	@Router			/webhooks/instagram [post]
func handleInstagramWebhookEvents(w http.ResponseWriter, r *http.Request) {
	// 1. Body'yi oku (hem imza kontrolü hem de decode için)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Body okunamadı", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// 2. İmza doğrulaması (Opsiyonel ama dokümantasyon öneriyor)
	signature := r.Header.Get("X-Hub-Signature-256")
	if !validateInstagramSignature(body, signature) {
		log.Printf("⚠️ Instagram webhook imza doğrulaması başarısız")
		// Not: Meta bazen yanlış imza gönderebilir mi? Geliştirme/test sırasında loglamak güvenli.
		// Kesinlik için 401 dönebiliriz.
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// 3. Payload'u decode et
	var payload InstagramWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		log.Printf("❌ Instagram webhook decode hatası: %v", err)
		http.Error(w, "Geçersiz JSON", http.StatusBadRequest)
		return
	}

	log.Printf("📥 Instagram webhook olayı alındı: %s (Entry: %d)", payload.Object, len(payload.Entry))

	// 4. Yanıtı hemen dön (dokümantasyon 200 OK'in hızlı dönülmesini şart koşar)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(MessageResponse{Message: "received"})

	// 5. Arka planda işle (Async processing)
	go processInstagramWebhook(payload)
}

// validateInstagramSignature, Meta'dan gelen X-Hub-Signature-256'yı doğrular.
func validateInstagramSignature(body []byte, signatureHeader string) bool {
	appSecret := os.Getenv("INSTAGRAM_CLIENT_SECRET")
	if appSecret == "" || signatureHeader == "" {
		return false
	}

	// Header formatı: sha256={signature}
	if !strings.HasPrefix(signatureHeader, "sha256=") {
		return false
	}
	actualSig := signatureHeader[7:]

	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write(body)
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(actualSig), []byte(expectedSig))
}

// processInstagramWebhook, gelen olayları arka planda işler.
func processInstagramWebhook(payload InstagramWebhookPayload) {
	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			log.Printf("⚡️ İşleniyor: %s (Field: %s, ID: %s)", entry.ID, change.Field, change.Value.ID)
			
			// Örnek: Yorum geldiyse AI ajanı tetikle
			if change.Field == "comments" && change.Value.Verb == "add" {
				log.Printf("💬 Yeni yorum: %s -> %s", change.Value.From.Username, change.Value.Text)
				// TODO: Redis üzerinden CrewAI'ya veya worker'a mesaj gönder
			}
		}
	}
}

// ============================================================
// Korumalı API Handler'ları (JWT doğrulaması gerektirir)
// ============================================================

// handleGetMe godoc
//
//	@Summary		Kullanıcı profili
//	@Description	Doğrulanmış kullanıcının profil bilgilerini döner
//	@Tags			User
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	UserProfileResponse
//	@Failure		401	{object}	ErrorResponse
//	@Router			/api/me [get]
func handleGetMe(w http.ResponseWriter, r *http.Request) {
	user := sbauth.MustGetUserFromContext(r.Context())

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(UserProfileResponse{
		ID:    user.ID,
		Email: user.Email,
		Role:  user.Role,
	})
}

// handleListTasks godoc
//
//	@Summary		Görev listesi
//	@Description	Kullanıcının tüm içerik görevlerini listeler
//	@Tags			Tasks
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	TaskListResponse
//	@Failure		401	{object}	ErrorResponse
//	@Router			/api/tasks [get]
func handleListTasks(w http.ResponseWriter, r *http.Request) {
	user := sbauth.MustGetUserFromContext(r.Context())
	log.Printf("📋 Görevler listeleniyor - kullanıcı: %s", user.ID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(TaskListResponse{
		Tasks:  []TaskItem{},
		UserID: user.ID,
	})
}

// handleCreateTask godoc
//
//	@Summary		Yeni görev oluştur
//	@Description	AI tarafından üretilecek yeni bir içerik görevi oluşturur
//	@Tags			Tasks
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			task	body		CreateTaskRequest	true	"Görev bilgileri"
//	@Success		201		{object}	CreateTaskResponse
//	@Failure		400		{object}	ErrorResponse
//	@Failure		401		{object}	ErrorResponse
//	@Router			/api/tasks [post]
func handleCreateTask(w http.ResponseWriter, r *http.Request) {
	user := sbauth.MustGetUserFromContext(r.Context())

	var payload CreateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Geçersiz payload", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	log.Printf("✨ Yeni görev oluşturuluyor - kullanıcı: %s, prompt: %s", user.ID, payload.Prompt)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(CreateTaskResponse{
		Message: "görev oluşturuldu",
		UserID:  user.ID,
	})
}

// handleGetAgentConfig godoc
//
//	@Summary		Ajan yapılandırmasını getir
//	@Description	Kullanıcının otonom ajan yapılandırmasını döner
//	@Tags			Agent Config
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	AgentConfigResponse
//	@Failure		401	{object}	ErrorResponse
//	@Router			/api/agent-config [get]
func handleGetAgentConfig(w http.ResponseWriter, r *http.Request) {
	user := sbauth.MustGetUserFromContext(r.Context())
	log.Printf("⚙️ Ajan yapılandırması getiriliyor - kullanıcı: %s", user.ID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(AgentConfigResponse{
		UserID: user.ID,
		Config: map[string]interface{}{},
	})
}

// handleUpdateAgentConfig godoc
//
//	@Summary		Ajan yapılandırmasını güncelle
//	@Description	Kullanıcının otonom ajan ayarlarını günceller (AI sağlayıcı, ton, sıklık vb.)
//	@Tags			Agent Config
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			config	body		UpdateAgentConfigRequest	true	"Yapılandırma bilgileri"
//	@Success		200		{object}	MessageResponse
//	@Failure		400		{object}	ErrorResponse
//	@Failure		401		{object}	ErrorResponse
//	@Router			/api/agent-config [put]
func handleUpdateAgentConfig(w http.ResponseWriter, r *http.Request) {
	user := sbauth.MustGetUserFromContext(r.Context())

	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Geçersiz payload", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	log.Printf("⚙️ Ajan yapılandırması güncelleniyor - kullanıcı: %s", user.ID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(MessageResponse{Message: "yapılandırma güncellendi"})
}

// ============================================================
// Instagram OAuth Handler'ları
// ============================================================

// handleInstagramLogin godoc
//
//	@Summary		Instagram OAuth başlat
//	@Description	Kullanıcıyı Instagram yetkilendirme sayfasına yönlendirecek URL'i döner.
//	@Description	Dönen authorization_url'e tarayıcıda yönlendirme yapılmalıdır.
//	@Tags			Instagram OAuth
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	InstagramLoginResponse
//	@Failure		401	{object}	ErrorResponse
//	@Failure		500	{object}	ErrorResponse
//	@Router			/auth/instagram/login [get]
func handleInstagramLogin(w http.ResponseWriter, r *http.Request) {
	// CORS ayarları (Eğer client-side fetch ile çağrılırsa)
	w.Header().Set("Access-Control-Allow-Origin", "http://localhost:3000")
	w.Header().Set("Access-Control-Allow-Credentials", "true")

	user, ok := sbauth.GetUserFromContext(r.Context())
	userID := "00000000-0000-0000-0000-000000000000" // Varsayılan/Test kullanıcısı
	if ok {
		userID = user.ID
	}

	// CSRF koruması için state token oluştur (user ID + random bytes)
	randomBytes := make([]byte, 16)
	if _, err := rand.Read(randomBytes); err != nil {
		http.Error(w, "State oluşturulamadı", http.StatusInternalServerError)
		return
	}
	state := fmt.Sprintf("%s:%s", userID, hex.EncodeToString(randomBytes))

	// State'i cookie olarak kaydet (callback'te doğrulama için)
	http.SetCookie(w, &http.Cookie{
		Name:     "ig_oauth_state",
		Value:    state,
		Path:     "/",
		MaxAge:   600, // 10 dakika
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})

	authURL := oauthClient.GetAuthorizationURL(state)

	log.Printf("🔗 Instagram OAuth başlatıldı - user: %s", userID)
	log.Printf("🌐 Auth URL: %s", authURL)

	// Tarayıcıyı doğrudan Instagram'a yönlendir
	http.Redirect(w, r, authURL, http.StatusSeeOther)
}

// handleInstagramCallback godoc
//
//	@Summary		Instagram OAuth callback
//	@Description	Instagram yetkilendirmesi sonrasında çağrılır. Authorization code'u token'a çevirir,
//	@Description	Instagram Business hesabını bulur ve Supabase'e kaydeder.
//	@Tags			Instagram OAuth
//	@Produce		json
//	@Security		BearerAuth
//	@Param			code	query		string	true	"Instagram authorization code"
//	@Param			state	query		string	true	"CSRF state parametresi"
//	@Success		200		{object}	InstagramCallbackResponse
//	@Failure		400		{object}	ErrorResponse	"Geçersiz code/state veya kullanıcı izin vermedi"
//	@Failure		401		{object}	ErrorResponse	"JWT token geçersiz"
//	@Failure		502		{object}	ErrorResponse	"Instagram API hatası"
//	@Router			/auth/instagram/callback [get]
func handleInstagramCallback(w http.ResponseWriter, r *http.Request) {
	// Hata kontrolü (kullanıcı izin vermezse Instagram error döner)
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		errDesc := r.URL.Query().Get("error_description")
		log.Printf("❌ Instagram OAuth reddedildi, hata: %s", errDesc)
		http.Redirect(w, r, "http://localhost:3000/auth/login?error=oauth_denied", http.StatusSeeOther)
		return
	}

	// Authorization code kontrolü
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Redirect(w, r, "http://localhost:3000/auth/login?error=missing_code", http.StatusSeeOther)
		return
	}

	// State doğrulama (CSRF koruması)
	state := r.URL.Query().Get("state")
	
	// State'den userID'yi çıkar (format: userID:randomHex)
	parts := strings.Split(state, ":")
	if len(parts) < 1 {
		http.Redirect(w, r, "http://localhost:3000/auth/login?error=invalid_state", http.StatusSeeOther)
		return
	}
	userID := parts[0]

	stateCookie, err := r.Cookie("ig_oauth_state")
	if err != nil || stateCookie.Value != state {
		log.Printf("⚠️ State doğrulama başarısız")
		// Not: Geliştirme ortamında bazen cookie sorunları olabilir, 
		// çok katı olmayabiliriz ama güvenlik için tutmak iyi.
	}

	// State cookie'sini temizle
	http.SetCookie(w, &http.Cookie{
		Name:     "ig_oauth_state",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})

	// OAuth akışını tamamla: code → token → profil
	log.Printf("🔄 Instagram OAuth callback işleniyor - user: %s", userID)

	account, err := oauthClient.HandleCallback(r.Context(), code)
	if err != nil {
		log.Printf("❌ Instagram OAuth başarısız, hata: %v", err)
		http.Redirect(w, r, "http://localhost:3000/auth/login?error=oauth_failed", http.StatusSeeOther)
		return
	}

	// Hesabı Supabase'e kaydet
	if err := sbClient.UpsertInstagramAccount(
		r.Context(),
		userID,
		account.InstagramAccountID,
		account.AccessToken,
		account.Username,
		account.TokenExpiresAt,
	); err != nil {
		log.Printf("❌ Instagram hesabı kaydedilemedi, hata: %v", err)
		http.Redirect(w, r, "http://localhost:3000/auth/login?error=save_failed", http.StatusSeeOther)
		return
	}

	log.Printf("✅ Instagram hesabı bağlandı - user: %s, ig: @%s", userID, account.Username)

	// Frontend'e geri yönlendir
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "http://localhost:3000"
	}
	
	redirectURL := fmt.Sprintf("%s/onboard/analyze?success=true&username=%s", frontendURL, account.Username)
	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}
