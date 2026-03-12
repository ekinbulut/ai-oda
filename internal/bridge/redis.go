package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis kanal sabitleri
const (
	ChannelStrategyRequest  = "crewai:strategy:request"  // Go → Python: strateji talebi
	ChannelStrategyResponse = "crewai:strategy:response" // Python → Go: strateji sonucu
)

// Config, Redis bağlantı yapılandırmasını tutar.
type Config struct {
	Addr     string // Redis adresi (ör: "localhost:6379")
	Password string // Redis şifresi (boş bırakılabilir)
	DB       int    // Redis veritabanı numarası
}

// RedisBridge, Go ve CrewAI (Python) arasındaki haberleşme köprüsüdür.
// Redis Pub/Sub kullanarak asenkron mesajlaşma sağlar.
type RedisBridge struct {
	client    *redis.Client
	mu        sync.Mutex
	callbacks map[string]chan *StrategyResponse // requestID → response kanalı
}

// StrategyRequest, Go tarafından Python CrewAI'ya gönderilen strateji talebidir.
type StrategyRequest struct {
	RequestID string `json:"request_id"` // Benzersiz istek kimliği
	UserID    string `json:"user_id"`    // Kullanıcı ID
	TaskID    string `json:"task_id"`    // İçerik görevi ID (opsiyonel)
	Timestamp string `json:"timestamp"`  // İstek zamanı
}

// TopAsset, CrewAI Analyst Agent tarafından seçilen en iyi performanslı görsel.
type TopAsset struct {
	AssetID        string  `json:"asset_id"`
	StorageURL     string  `json:"storage_url"`
	EngagementRate float64 `json:"engagement_rate"`
	TotalLikes     int     `json:"total_likes"`
	TotalComments  int     `json:"total_comments"`
	Description    string  `json:"description"` // Vision analizi açıklaması
	Mood           string  `json:"mood"`        // Duygu analizi
}

// GeneratedContent, CrewAI Strategy Agent tarafından üretilen içerik.
type GeneratedContent struct {
	AssetID  string `json:"asset_id"`  // İlişkili görsel ID
	Caption  string `json:"caption"`   // Üretilen metin
	Hashtags string `json:"hashtags"`  // Önerilen hashtag'ler
}

// CriticReview, CrewAI Critic Agent tarafından yapılan denetim sonucu.
type CriticReview struct {
	Approved bool     `json:"approved"` // Onay durumu
	Issues   []string `json:"issues"`   // Tespit edilen sorunlar
	Score    float64  `json:"score"`    // Kalite skoru (0-1)
}

// StrategyResponse, Python CrewAI'dan Go'ya dönen tam strateji sonucudur.
type StrategyResponse struct {
	RequestID string             `json:"request_id"` // İlişkili istek kimliği
	Status    string             `json:"status"`     // "success" veya "error"
	Error     string             `json:"error,omitempty"`
	TopAssets []TopAsset         `json:"top_assets,omitempty"`   // Analyst Agent çıktısı
	Contents  []GeneratedContent `json:"contents,omitempty"`     // Strategy Agent çıktısı
	Review    *CriticReview      `json:"critic_review,omitempty"` // Critic Agent çıktısı
	Timestamp string             `json:"timestamp"`
}

// NewRedisBridge, yeni bir Redis köprüsü oluşturur ve bağlantıyı doğrular.
func NewRedisBridge(cfg Config) (*RedisBridge, error) {
	if cfg.Addr == "" {
		cfg.Addr = "localhost:6379"
	}

	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	// Bağlantıyı doğrula
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis bağlantısı kurulamadı (%s): %w", cfg.Addr, err)
	}

	log.Printf("✅ Redis bağlantısı başarılı: %s", cfg.Addr)

	return &RedisBridge{
		client:    client,
		callbacks: make(map[string]chan *StrategyResponse),
	}, nil
}

// PublishStrategyRequest, CrewAI'ya "İçerik Stratejisi Gerekiyor" mesajı gönderir
// ve sonucu bekler. timeout süresi içinde yanıt gelmezse hata döner.
func (b *RedisBridge) PublishStrategyRequest(ctx context.Context, req *StrategyRequest, timeout time.Duration) (*StrategyResponse, error) {
	if req.RequestID == "" {
		return nil, fmt.Errorf("request_id boş olamaz")
	}
	if req.UserID == "" {
		return nil, fmt.Errorf("user_id boş olamaz")
	}

	req.Timestamp = time.Now().UTC().Format(time.RFC3339)

	// Yanıt kanalı oluştur
	responseCh := make(chan *StrategyResponse, 1)
	b.mu.Lock()
	b.callbacks[req.RequestID] = responseCh
	b.mu.Unlock()

	// Temizlik: fonksiyon dönünce callback'i sil
	defer func() {
		b.mu.Lock()
		delete(b.callbacks, req.RequestID)
		b.mu.Unlock()
	}()

	// Mesajı JSON olarak serileştir
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("istek serileştirilemedi: %w", err)
	}

	// Redis'e yayınla
	if err := b.client.Publish(ctx, ChannelStrategyRequest, string(data)).Err(); err != nil {
		return nil, fmt.Errorf("redis yayın hatası: %w", err)
	}

	log.Printf("📡 Strateji talebi gönderildi: requestID=%s, userID=%s", req.RequestID, req.UserID)

	// Yanıtı bekle
	select {
	case resp := <-responseCh:
		return resp, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("strateji yanıtı zaman aşımına uğradı (%v): requestID=%s", timeout, req.RequestID)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ListenForResponses, Python CrewAI'dan gelen strateji yanıtlarını dinler.
// Bu metod bir goroutine içinde çalıştırılmalıdır.
func (b *RedisBridge) ListenForResponses(ctx context.Context) {
	sub := b.client.Subscribe(ctx, ChannelStrategyResponse)
	defer sub.Close()

	ch := sub.Channel()

	log.Printf("👂 CrewAI yanıt kanalı dinleniyor: %s", ChannelStrategyResponse)

	for {
		select {
		case msg := <-ch:
			if msg == nil {
				continue
			}
			b.handleResponse(msg.Payload)
		case <-ctx.Done():
			log.Println("🛑 Redis yanıt dinleyicisi durduruluyor...")
			return
		}
	}
}

// handleResponse, gelen yanıt mesajını işleyerek ilgili callback kanalına yönlendirir.
func (b *RedisBridge) handleResponse(payload string) {
	var resp StrategyResponse
	if err := json.Unmarshal([]byte(payload), &resp); err != nil {
		log.Printf("⚠️ Geçersiz strateji yanıtı: %v", err)
		return
	}

	log.Printf("📥 Strateji yanıtı alındı: requestID=%s, status=%s", resp.RequestID, resp.Status)

	b.mu.Lock()
	ch, ok := b.callbacks[resp.RequestID]
	b.mu.Unlock()

	if ok {
		ch <- &resp
	} else {
		log.Printf("⚠️ Eşleşmeyen yanıt (requestID=%s), muhtemelen zaman aşımına uğramış", resp.RequestID)
	}
}

// Close, Redis bağlantısını temiz bir şekilde kapatır.
func (b *RedisBridge) Close() error {
	log.Println("🔌 Redis bağlantısı kapatılıyor...")
	return b.client.Close()
}

// Ping, Redis bağlantısının aktif olup olmadığını kontrol eder.
func (b *RedisBridge) Ping(ctx context.Context) error {
	return b.client.Ping(ctx).Err()
}
