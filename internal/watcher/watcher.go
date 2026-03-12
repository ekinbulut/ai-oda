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

		// 6. Senaryo B'yi tetikle
		if err := w.triggerScenarioB(ctx, userID); err != nil {
			log.Printf("❌ Senaryo B tetiklenemedi (kullanıcı %s): %v", userID, err)
			continue
		}

		triggeredCount++
	}

	if inactiveCount > 0 {
		log.Printf("👁️ Kontrol tamamlandı: %d inaktif kullanıcı, %d Senaryo B tetiklendi",
			inactiveCount, triggeredCount)
	}
}

// triggerScenarioB, inaktif bir kullanıcı için "Senaryo B" akışını başlatır.
// Senaryo B:
// 1. Kullanıcının yayınlanmamış medya varlıklarını çek
// 2. En uygun görseli seç (vision analizi + engagement geçmişi)
// 3. AI ile otomatik caption oluştur
// 4. İçerik görevini "pending" olarak kaydet (worker döngüsü tarafından işlenecek)
func (w *Watcher) triggerScenarioB(ctx context.Context, userID string) error {
	log.Printf("🚀 Senaryo B tetikleniyor: kullanıcı %s", userID)

	// 1. Yayınlanmamış görselleri çek
	unpublished, err := w.sbClient.GetUnpublishedAssets(ctx, userID, 5)
	if err != nil {
		return fmt.Errorf("yayınlanmamış varlıklar alınamadı: %w", err)
	}

	if len(unpublished) == 0 {
		log.Printf("ℹ️ Kullanıcı %s için yayınlanmamış medya varlığı bulunamadı, atlanıyor", userID)
		return nil
	}

	// 2. En uygun görseli seç
	selectedAsset := w.selectBestAsset(unpublished)
	log.Printf("📸 Seçilen görsel: %s (%s)", selectedAsset.ID, selectedAsset.StorageURL)

	// 3. Marka bağlamını al
	agentCtx, err := w.sbClient.GetAgentContext(ctx, userID)
	if err != nil {
		log.Printf("⚠️ Marka bağlamı alınamadı (kullanıcı %s), varsayılan kullanılacak: %v", userID, err)
	}

	// 4. CrewAI üzerinden strateji iste (mümkünse)
	if w.redisBridge != nil {
		strategyResult, err := w.requestCrewAIForScenarioB(ctx, userID, selectedAsset)
		if err != nil {
			log.Printf("⚠️ CrewAI strateji hatası (Senaryo B, kullanıcı %s): %v — fallback AI kullanılacak", userID, err)
		} else if strategyResult != nil && strategyResult.Status == "success" && len(strategyResult.Contents) > 0 {
			// CrewAI başarılı — görev oluştur
			return w.createAutoTask(ctx, userID, selectedAsset, strategyResult.Contents[0].Caption, strategyResult.Contents[0].Hashtags)
		}
	}

	// 5. Fallback: Doğrudan AI ile caption oluştur
	prompt := w.buildScenarioBPrompt(selectedAsset, agentCtx)
	content, err := w.aiGen.GenerateContent(ctx, prompt)
	if err != nil {
		return fmt.Errorf("AI içerik üretme hatası: %w", err)
	}

	// 6. Otonom görev oluştur
	return w.createAutoTask(ctx, userID, selectedAsset, content.Caption, content.Hashtags)
}

// selectBestAsset, yayınlanmamış görseller arasından en uygununu seçer.
// Seçim kriterleri: vision analizi varsa tercih et, yoksa en yenisini seç.
func (w *Watcher) selectBestAsset(assets []supabase.MediaAsset) supabase.MediaAsset {
	// Vision analizi olan görselleri tercih et
	for _, asset := range assets {
		if len(asset.VisionAnalysis) > 0 {
			return asset
		}
	}
	// Hiçbirinde vision analizi yoksa en yenisini döndür (liste zaten created_at DESC sıralı)
	return assets[0]
}

// buildScenarioBPrompt, Senaryo B için AI'ya verilecek prompt'u oluşturur.
func (w *Watcher) buildScenarioBPrompt(asset supabase.MediaAsset, agentCtx *supabase.AgentContext) string {
	prompt := "Mevcut bir görselimiz var ve bunun için çekici bir Instagram paylaşımı oluşturmam gerekiyor.\n\n"

	// Vision analizi varsa ekle
	if desc, ok := asset.VisionAnalysis["description"].(string); ok && desc != "" {
		prompt += fmt.Sprintf("Görsel açıklaması: %s\n", desc)
	}
	if mood, ok := asset.VisionAnalysis["mood"].(string); ok && mood != "" {
		prompt += fmt.Sprintf("Görselin havası: %s\n", mood)
	}

	// Marka bağlamı varsa ekle
	if agentCtx != nil {
		if agentCtx.BrandVoice != "" {
			prompt += fmt.Sprintf("\nMarka sesi: %s", agentCtx.BrandVoice)
		}
		if agentCtx.TargetAudience != "" {
			prompt += fmt.Sprintf("\nHedef kitle: %s", agentCtx.TargetAudience)
		}
		if len(agentCtx.ContentPillars) > 0 {
			prompt += "\nİçerik temaları: "
			for i, p := range agentCtx.ContentPillars {
				if i > 0 {
					prompt += ", "
				}
				prompt += p
			}
		}
	}

	prompt += "\n\nBu görsel için ilgi çekici, etkileşim odaklı bir Instagram caption ve hashtag'ler oluştur."
	return prompt
}

// requestCrewAIForScenarioB, Senaryo B için CrewAI'dan strateji talebinde bulunur.
func (w *Watcher) requestCrewAIForScenarioB(ctx context.Context, userID string, asset supabase.MediaAsset) (*bridge.StrategyResponse, error) {
	req := &bridge.StrategyRequest{
		RequestID: uuid.New().String(),
		UserID:    userID,
		TaskID:    fmt.Sprintf("auto-scenarioB-%s", asset.ID),
	}

	// 3 dakika timeout ile CrewAI yanıtını bekle
	return w.redisBridge.PublishStrategyRequest(ctx, req, 3*time.Minute)
}

// createAutoTask, otonom tetikleyici tarafından oluşturulan içerik görevini Supabase'e kaydeder.
func (w *Watcher) createAutoTask(ctx context.Context, userID string, asset supabase.MediaAsset, caption, hashtags string) error {
	task := supabase.AutoContentTask{
		ID:          uuid.New().String(),
		UserID:      userID,
		Status:      "pending",
		ContentType: "photo",
		Prompt:      fmt.Sprintf("[Senaryo B — Otonom Tetikleyici] Mevcut görsel: %s", asset.ID),
		Result:      marshalAutoResult(asset.StorageURL, caption, hashtags),
		ScheduledAt: time.Now().UTC(), // Hemen işlenmesi için şimdi zamanla
	}

	if err := w.sbClient.CreateAutoContentTask(ctx, task); err != nil {
		return fmt.Errorf("otonom görev oluşturulamadı: %w", err)
	}

	// Görseli "yayınlanmak üzere seçildi" olarak işaretle (çift seçimi önle)
	_ = w.sbClient.MarkAssetPublished(ctx, asset.ID)

	log.Printf("✅ Senaryo B görevi oluşturuldu: kullanıcı=%s, görsel=%s, görev=%s",
		userID, asset.ID, task.ID)

	return nil
}

// marshalAutoResult, Senaryo B sonuçlarını JSON olarak serileştirir.
func marshalAutoResult(imageURL, caption, hashtags string) string {
	result := map[string]string{
		"image_url": imageURL,
		"caption":   caption,
		"hashtags":  hashtags,
		"source":    "autonomous_watcher_scenario_b",
	}
	data, _ := json.Marshal(result)
	return string(data)
}
