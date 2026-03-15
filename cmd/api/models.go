package main

// Swagger dokümantasyonu için request/response model tanımları.
// Bu struct'lar sadece Swagger tarafından kullanılır; handler'lar hâlâ map[string]interface{} kullanabilir.

// HealthResponse, sağlık kontrolü yanıtı.
type HealthResponse struct {
	Status string `json:"status" example:"ok"`
}

// ErrorResponse, standart hata yanıtı.
type ErrorResponse struct {
	Error  string `json:"error" example:"unauthorized"`
	Detail string `json:"detail" example:"Token geçersiz veya süresi dolmuş"`
}

// MessageResponse, başarılı işlem yanıtı.
type MessageResponse struct {
	Message string `json:"message" example:"received"`
}

// UserProfileResponse, kullanıcı profil yanıtı.
type UserProfileResponse struct {
	ID    string `json:"id" example:"550e8400-e29b-41d4-a716-446655440000"`
	Email string `json:"email" example:"user@example.com"`
	Role  string `json:"role" example:"authenticated"`
}

// TaskListResponse, görev listesi yanıtı.
type TaskListResponse struct {
	Tasks  []TaskItem `json:"tasks"`
	UserID string     `json:"user_id" example:"550e8400-e29b-41d4-a716-446655440000"`
}

// TaskItem, tekil görev öğesi.
type TaskItem struct {
	ID          string `json:"id" example:"660e8400-e29b-41d4-a716-446655440000"`
	UserID      string `json:"user_id" example:"550e8400-e29b-41d4-a716-446655440000"`
	Status      string `json:"status" example:"pending"`
	ContentType string `json:"content_type" example:"photo"`
	Prompt      string `json:"prompt" example:"Teknoloji ile ilgili motivasyon içeriği"`
	ScheduledAt string `json:"scheduled_at" example:"2026-03-14T10:00:00Z"`
}

// CreateTaskRequest, yeni içerik görevi oluşturma isteği.
type CreateTaskRequest struct {
	Prompt      string `json:"prompt" example:"Yapay zeka ile ilgili ilham verici bir post" binding:"required"`
	ContentType string `json:"content_type" example:"photo" enums:"photo,carousel,reel,story"`
	ScheduledAt string `json:"scheduled_at" example:"2026-03-14T10:00:00Z" binding:"required"`
}

// CreateTaskResponse, görev oluşturma yanıtı.
type CreateTaskResponse struct {
	Message string `json:"message" example:"görev oluşturuldu"`
	UserID  string `json:"user_id" example:"550e8400-e29b-41d4-a716-446655440000"`
}

// AgentConfigResponse, ajan yapılandırması yanıtı.
type AgentConfigResponse struct {
	UserID string                 `json:"user_id" example:"550e8400-e29b-41d4-a716-446655440000"`
	Config map[string]interface{} `json:"config"`
}

// UpdateAgentConfigRequest, ajan yapılandırması güncelleme isteği.
type UpdateAgentConfigRequest struct {
	AIProvider          string   `json:"ai_provider" example:"openai" enums:"openai,gemini"`
	AIModel             string   `json:"ai_model" example:"gpt-4o"`
	Tone                string   `json:"tone" example:"professional"`
	Language            string   `json:"language" example:"tr"`
	PostingFrequency    string   `json:"posting_frequency" example:"daily" enums:"hourly,daily,weekly,custom"`
	PreferredPostTimes  []string `json:"preferred_posting_times" example:"10:00,14:00,19:00"`
	TargetAudience      string   `json:"target_audience" example:"Teknoloji meraklıları, 25-40 yaş"`
	BrandKeywords       []string `json:"brand_keywords" example:"yapay zeka,teknoloji,inovasyon"`
	IsActive            bool     `json:"is_active" example:"true"`
}

// InstagramLoginResponse, OAuth başlatma yanıtı.
type InstagramLoginResponse struct {
	AuthorizationURL string `json:"authorization_url" example:"https://www.facebook.com/v21.0/dialog/oauth?client_id=..."`
}

// InstagramCallbackResponse, OAuth callback başarı yanıtı.
type InstagramCallbackResponse struct {
	Message   string `json:"message" example:"Instagram hesabı başarıyla bağlandı"`
	Username  string `json:"username" example:"mybusiness"`
	IgID      string `json:"ig_id" example:"17841400123456789"`
	ExpiresAt string `json:"expires_at" example:"2026-05-12T00:00:00Z"`
}

// WebhookPayload, Supabase webhook gövdesi.
type WebhookPayload struct {
	Type      string                 `json:"type" example:"INSERT"`
	Table     string                 `json:"table" example:"profiles"`
	Record    map[string]interface{} `json:"record"`
	Schema    string                 `json:"schema" example:"public"`
	OldRecord map[string]interface{} `json:"old_record,omitempty"`
}

// InstagramWebhookPayload, Meta/Instagram'dan gelen webhook bildirimi.
type InstagramWebhookPayload struct {
	Object string                 `json:"object" example:"instagram"`
	Entry  []InstagramWebhookEntry `json:"entry"`
}

// InstagramWebhookEntry, tek bir webhook bildirimi girdisi.
type InstagramWebhookEntry struct {
	ID      string                  `json:"id" example:"17841400123456789"`
	Time    int64                   `json:"time" example:"1520383571"`
	Changes []InstagramWebhookChange `json:"changes"`
}

// InstagramWebhookChange, webhook bildirimi içindeki değişiklik detayları.
type InstagramWebhookChange struct {
	Field string                 `json:"field" example:"comments"`
	Value InstagramWebhookValue  `json:"value"`
}

// InstagramWebhookValue, değişiklik değerinin içeriği.
type InstagramWebhookValue struct {
	ID                  string `json:"id,omitempty" example:"17841405710997973"`
	Text                string `json:"text,omitempty" example:"Harika bir paylaşım!"`
	From                struct {
		ID       string `json:"id" example:"17841405710997973"`
		Username string `json:"username" example:"johndoe"`
	} `json:"from,omitempty"`
	MediaID             string `json:"media_id,omitempty"`
	InstagramUserID     string `json:"instagram_user_id,omitempty"`
	Verb                string `json:"verb,omitempty" example:"add"`
	Username            string `json:"username,omitempty"`
}
