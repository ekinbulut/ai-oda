package watcher

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/ekinbulut/x/internal/ai"
	"github.com/ekinbulut/x/internal/bridge"
	"github.com/ekinbulut/x/internal/supabase"
)

// DefaultInactivityThreshold, varsayılan inaktivite eşiği (48 saat).
const DefaultInactivityThreshold = 48 * time.Hour

// DefaultWatchInterval, kontrol döngüsü aralığı (15 dakika).
const DefaultWatchInterval = 15 * time.Minute

// Config, Watcher servisi yapılandırmasını tutar.
type Config struct {
	InactivityThreshold time.Duration // Kullanıcının inaktif sayılacağı süre (varsayılan 48 saat)
	WatchInterval       time.Duration // Kontrol aralığı (varsayılan 15 dakika)
}

// Watcher, kullanıcı inaktivitesini izleyen ve otomatik "Senaryo B" tetikleyen servistir.
// Senaryo B: Mevcut (yayınlanmamış) içerikten otomatik paylaşım oluşturma.
type Watcher struct {
	config      Config
	sbClient    *supabase.Client
	aiGen       *ai.Generator
	redisBridge *bridge.RedisBridge
}

// New, yeni bir Watcher örneği oluşturur.
func New(cfg Config, sbClient *supabase.Client, aiGen *ai.Generator, redisBridge *bridge.RedisBridge) *Watcher {
	if cfg.InactivityThreshold <= 0 {
		cfg.InactivityThreshold = DefaultInactivityThreshold
	}
	if cfg.WatchInterval <= 0 {
		cfg.WatchInterval = DefaultWatchInterval
	}

	return &Watcher{
		config:      cfg,
		sbClient:    sbClient,
		aiGen:       aiGen,
		redisBridge: redisBridge,
	}
}

// NewFromEnv, ortam değişkenlerinden yapılandırma okuyarak yeni bir Watcher oluşturur.
// WATCHER_INACTIVITY_HOURS: inaktivite eşiği (saat cinsinden, varsayılan 48)
// WATCHER_INTERVAL_MINUTES: kontrol aralığı (dakika cinsinden, varsayılan 15)
func NewFromEnv(sbClient *supabase.Client, aiGen *ai.Generator, redisBridge *bridge.RedisBridge) *Watcher {
	cfg := Config{}

	if hours := os.Getenv("WATCHER_INACTIVITY_HOURS"); hours != "" {
		if h, err := strconv.Atoi(hours); err == nil && h > 0 {
			cfg.InactivityThreshold = time.Duration(h) * time.Hour
		}
	}

	if mins := os.Getenv("WATCHER_INTERVAL_MINUTES"); mins != "" {
		if m, err := strconv.Atoi(mins); err == nil && m > 0 {
			cfg.WatchInterval = time.Duration(m) * time.Minute
		}
	}

	return New(cfg, sbClient, aiGen, redisBridge)
}

// Start, Watcher döngüsünü başlatır. Bu metod context iptal edilene kadar bloklar.
// Bir goroutine içinde çağrılmalıdır.
func (w *Watcher) Start(ctx context.Context) {
	log.Printf("👁️ Watcher başlatıldı — inaktivite eşiği: %v, kontrol aralığı: %v",
		w.config.InactivityThreshold, w.config.WatchInterval)

	// İlk çalıştırmada 30 saniye bekle (Worker'ın bağlanması için)
	select {
	case <-time.After(30 * time.Second):
	case <-ctx.Done():
		return
	}

	ticker := time.NewTicker(w.config.WatchInterval)
	defer ticker.Stop()

	// İlk kontrol hemen yapılsın
	w.CheckUserInactivity(ctx)

	for {
		select {
		case <-ticker.C:
			w.CheckUserInactivity(ctx)
		case <-ctx.Done():
			log.Println("🛑 Watcher durduruluyor...")
			return
		}
	}
}

// CheckUserInactivity, tüm aktif kullanıcıların son paylaşım tarihini kontrol eder.
// Eğer belirlenen inaktivite eşiği aşıldıysa, kullanıcıya sormadan "Senaryo B"yi tetikler.
// Senaryo B: Mevcut yayınlanmamış içeriklerden otomatik paylaşım görevi oluşturma.
func (w *Watcher) CheckUserInactivity(ctx context.Context) {
	log.Println("👁️ Kullanıcı inaktivite kontrolü başlatılıyor...")

	// 1. Aktif kullanıcıları getir (ajan konfigürasyonu aktif olan)
	users, err := w.sbClient.GetActiveAgentUsers(ctx)
	if err != nil {
		log.Printf("❌ Aktif kullanıcılar alınamadı: %v", err)
		return
	}

	if len(users) == 0 {
		log.Println("ℹ️ Kontrol edilecek aktif kullanıcı yok")
		return
	}

	log.Printf("👥 %d aktif kullanıcı kontrol ediliyor...", len(users))

	inactiveCount := 0
	triggeredCount := 0

	for _, userID := range users {
		// 2. Her kullanıcının son paylaşım tarihini kontrol et
		lastPublished, err := w.sbClient.GetLastPublishedTaskTime(ctx, userID)
		if err != nil {
			log.Printf("⚠️ Kullanıcı %s için son paylaşım tarihi alınamadı: %v", userID, err)
			continue
		}

		// 3. İnaktivite süresini hesapla
		var inactiveDuration time.Duration
		if lastPublished.IsZero() {
			// Hiç paylaşım yapmamış — kullanıcı oluşturulma tarihinden itibaren kontrol et
			inactiveDuration = time.Since(time.Now().Add(-w.config.InactivityThreshold - time.Hour))
		} else {
			inactiveDuration = time.Since(lastPublished)
		}

		// 4. Eşik aşıldı mı?
		if inactiveDuration < w.config.InactivityThreshold {
			continue // Bu kullanıcı hâlâ aktif, atla
		}

		inactiveCount++
		log.Printf("⏰ Kullanıcı %s inaktif: son paylaşım %.1f saat önce (eşik: %.1f saat)",
			userID, inactiveDuration.Hours(), w.config.InactivityThreshold.Hours())

		// 5. Halihazırda bekleyen bir otonom görev var mı? (çift tetiklemeyi önle)
		hasPending, err := w.sbClient.HasPendingAutoTask(ctx, userID)
		if err != nil {
			log.Printf("⚠️ Kullanıcı %s için bekleyen görev kontrolü hatalı: %v", userID, err)
			continue
		}
		if hasPending {
			log.Printf("ℹ️ Kullanıcı %s için zaten bekleyen otonom görev var, atlanıyor", userID)
			continue
		}

		// 6. Recycle Content'i tetikle
		if err := w.triggerRecycleContent(ctx, userID); err != nil {
			log.Printf("❌ Recycle Content tetiklenemedi (kullanıcı %s): %v", userID, err)
			continue
		}

		triggeredCount++
	}

	if inactiveCount > 0 {
		log.Printf("👁️ Kontrol tamamlandı: %d inaktif kullanıcı, %d Recycle Content tetiklendi",
			inactiveCount, triggeredCount)
	}
}

// triggerRecycleContent, inaktif bir kullanıcı için "Recycle Content" akışını başlatır.
// 1. Redis üzerinden CrewAI'ya "Recycle Content" emri gönder.
// 2. Yanıt olarak gelen En İyi Görsel + Yeni Caption'ı al.
// 3. Kullanıcının en etkileşimli olduğu saati hesapla.
// 4. Görevi STATUS_SCHEDULED olarak veritabanına kaydet.
func (w *Watcher) triggerRecycleContent(ctx context.Context, userID string) error {
	log.Printf("🚀 Recycle Content tetikleniyor: kullanıcı %s", userID)

	if w.redisBridge == nil {
		return fmt.Errorf("redis köprüsü aktif değil, Recycle Content çalıştırılamaz")
	}

	req := &bridge.StrategyRequest{
		RequestID: uuid.New().String(),
		UserID:    userID,
		TaskID:    fmt.Sprintf("recycle-content-%d", time.Now().Unix()),
		Command:   "Recycle Content",
	}

	// 3 dakika timeout ile CrewAI yanıtını bekle
	strategyResult, err := w.redisBridge.PublishStrategyRequest(ctx, req, 3*time.Minute)
	if err != nil {
		return fmt.Errorf("CrewAI strateji hatası: %w", err)
	}

	if strategyResult.Status != "success" || len(strategyResult.Contents) == 0 {
		return fmt.Errorf("CrewAI içerik üretemedi veya başarısız oldu")
	}

	content := strategyResult.Contents[0]

	// En etkileşimli saati bul we rezerve et
	scheduledAt := w.getPeakEngagementHour(ctx, userID)

	return w.createScheduledTask(ctx, userID, content.AssetID, content.Caption, content.Hashtags, scheduledAt)
}

// getPeakEngagementHour, kullanıcının en iyi performans gösteren görselinin
// paylaşıldığı veya oluşturulduğu saati bularak bugünün/yarının aynı saatini döner.
func (w *Watcher) getPeakEngagementHour(ctx context.Context, userID string) time.Time {
	topAssets, err := w.sbClient.GetTopPerformingAssets(ctx, userID, 1)

	now := time.Now().UTC()
	targetHour := 19 // Başarısız olursa varsayılan 19:00

	if err == nil && len(topAssets) > 0 {
		targetHour = topAssets[0].Asset.CreatedAt.Hour()
	}

	scheduled := time.Date(now.Year(), now.Month(), now.Day(), targetHour, 0, 0, 0, time.UTC)
	// Eğer o saat bugün geçtiyse yarına planla
	if scheduled.Before(now) {
		scheduled = scheduled.Add(24 * time.Hour)
	}

	return scheduled
}

// createScheduledTask, otonom tetikleyici tarafından oluşturulan içerik görevini "scheduled" statüsünde kaydeder.
func (w *Watcher) createScheduledTask(ctx context.Context, userID string, assetID, caption, hashtags string, scheduledAt time.Time) error {
	imageURL := ""
	asset, err := w.sbClient.GetMediaAsset(ctx, assetID)
	if err == nil {
		imageURL = asset.StorageURL
	}

	task := supabase.AutoContentTask{
		ID:          uuid.New().String(),
		UserID:      userID,
		Status:      "scheduled",
		ContentType: "photo",
		Prompt:      fmt.Sprintf("[Recycle Content — Otonom Tetikleyici] Geri Dönüştürülen Görsel: %s", assetID),
		Result:      marshalAutoResult(imageURL, caption, hashtags),
		ScheduledAt: scheduledAt,
	}

	if err := w.sbClient.CreateAutoContentTask(ctx, task); err != nil {
		return fmt.Errorf("otonom görev oluşturulamadı: %w", err)
	}

	// Görseli tekrar işlenmemesi için flag'leyebiliriz (opsiyonel)
	_ = w.sbClient.MarkAssetPublished(ctx, assetID)

	log.Printf("✅ Recycle Content görevi oluşturuldu: kullanıcı=%s, görsel=%s, görev=%s, zaman=%v",
		userID, assetID, task.ID, scheduledAt)

	return nil
}

// marshalAutoResult, Senaryo B sonuçlarını JSON olarak serileştirir.
func marshalAutoResult(imageURL, caption, hashtags string) string {
	result := map[string]string{
		"image_url": imageURL,
		"caption":   caption,
		"hashtags":  hashtags,
		"source":    "autonomous_watcher_recycle_content",
	}
	data, _ := json.Marshal(result)
	return string(data)
}
