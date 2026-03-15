package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

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
//	@host						localhost:8080
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

	// Instagram OAuth endpoint'leri (JWT ile korumalı)
	r.Route("/auth/instagram", func(r chi.Router) {
		r.Use(authMw.Authenticate)
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
	log.Printf("📖 Swagger UI: http://localhost:%s/swagger/index.html", port)
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

	if mode == "subscribe" && token == verifyToken {
		log.Printf("✅ Instagram webhook doğrulaması başarılı")
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(challenge))
	} else {
		log.Printf("❌ Instagram webhook doğrulaması başarısız - beklenen: %s, gelen: %s", verifyToken, token)
		w.WriteHeader(http.StatusForbidden)
	}
}

// handleInstagramWebhookEvents godoc
//
//	@Summary		Instagram Webhook olayları
//	@Description	Instagram'dan gelen gerçek zamanlı bildirimleri (yorumlar, mesajlar vb.) işler.
//	@Tags			Webhooks
//	@Accept			json
//	@Produce		json
//	@Success		200	{object}	MessageResponse
//	@Router			/webhooks/instagram [post]
func handleInstagramWebhookEvents(w http.ResponseWriter, r *http.Request) {
	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Geçersiz payload", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	log.Printf("📥 Instagram webhook olayı alındı: %v", payload)

	// TODO: Olay tipine göre (comment, mention vb.) işlem yap
	
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(MessageResponse{Message: "received"})
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
	user := sbauth.MustGetUserFromContext(r.Context())

	// CSRF koruması için state token oluştur (user ID + random bytes)
	randomBytes := make([]byte, 16)
	if _, err := rand.Read(randomBytes); err != nil {
		http.Error(w, "State oluşturulamadı", http.StatusInternalServerError)
		return
	}
	state := fmt.Sprintf("%s:%s", user.ID, hex.EncodeToString(randomBytes))

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

	log.Printf("🔗 Instagram OAuth başlatıldı - kullanıcı: %s", user.ID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(InstagramLoginResponse{
		AuthorizationURL: authURL,
	})
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
	user := sbauth.MustGetUserFromContext(r.Context())

	// Hata kontrolü (kullanıcı izin vermezse Instagram error döner)
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		errDesc := r.URL.Query().Get("error_description")
		log.Printf("❌ Instagram OAuth reddedildi - kullanıcı: %s, hata: %s", user.ID, errDesc)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{
			Error:  "oauth_denied",
			Detail: errDesc,
		})
		return
	}

	// Authorization code kontrolü
	code := r.URL.Query().Get("code")
	if code == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{
			Error:  "missing_code",
			Detail: "Authorization code bulunamadı",
		})
		return
	}

	// State doğrulama (CSRF koruması)
	state := r.URL.Query().Get("state")
	stateCookie, err := r.Cookie("ig_oauth_state")
	if err != nil || stateCookie.Value != state {
		log.Printf("⚠️ State doğrulama başarısız - kullanıcı: %s", user.ID)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{
			Error:  "invalid_state",
			Detail: "State parametresi eşleşmiyor, CSRF koruması tetiklendi",
		})
		return
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
	log.Printf("🔄 Instagram OAuth callback işleniyor - kullanıcı: %s", user.ID)

	account, err := oauthClient.HandleCallback(r.Context(), code)
	if err != nil {
		log.Printf("❌ Instagram OAuth başarısız - kullanıcı: %s, hata: %v", user.ID, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(ErrorResponse{
			Error:  "oauth_failed",
			Detail: err.Error(),
		})
		return
	}

	// Hesabı Supabase'e kaydet
	if err := sbClient.UpsertInstagramAccount(
		r.Context(),
		user.ID,
		account.InstagramAccountID,
		account.AccessToken,
		account.Username,
		account.TokenExpiresAt,
	); err != nil {
		log.Printf("❌ Instagram hesabı kaydedilemedi - kullanıcı: %s, hata: %v", user.ID, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{
			Error:  "save_failed",
			Detail: "Instagram hesabı veritabanına kaydedilemedi",
		})
		return
	}

	log.Printf("✅ Instagram hesabı bağlandı - kullanıcı: %s, ig: @%s", user.ID, account.Username)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(InstagramCallbackResponse{
		Message:   "Instagram hesabı başarıyla bağlandı",
		Username:  account.Username,
		IgID:      account.InstagramAccountID,
		ExpiresAt: account.TokenExpiresAt.Format("2006-01-02T15:04:05Z"),
	})
}
