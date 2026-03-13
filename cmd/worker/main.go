package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"

	"github.com/ekinbulut/x/internal/ai"
	"github.com/ekinbulut/x/internal/bridge"
	"github.com/ekinbulut/x/internal/social"
	"github.com/ekinbulut/x/internal/supabase"
	"github.com/ekinbulut/x/internal/watcher"
)

func main() {
	// .env dosyasını yükle (varsa)
	_ = godotenv.Load()

	log.Println("🤖 Worker başlatılıyor...")

	// Supabase istemcisini başlat
	sbClient, err := supabase.NewClient(supabase.Config{
		URL:    os.Getenv("SUPABASE_URL"),
		APIKey: os.Getenv("SUPABASE_SERVICE_KEY"),
	})
	if err != nil {
		log.Fatalf("Supabase bağlantı hatası: %v", err)
	}

	// AI üretici istemcisini başlat
	aiGenerator, err := ai.NewGenerator(ai.Config{
		Provider: os.Getenv("AI_PROVIDER"), // "openai" veya "gemini"
		APIKey:   os.Getenv("AI_API_KEY"),
		Model:    os.Getenv("AI_MODEL"),
	})
	if err != nil {
		log.Fatalf("AI istemci başlatma hatası: %v", err)
	}

	// Instagram istemcisini başlat
	igClient := social.NewInstagramClient(social.InstagramConfig{
		AccessToken: os.Getenv("INSTAGRAM_ACCESS_TOKEN"),
		AccountID:   os.Getenv("INSTAGRAM_ACCOUNT_ID"),
	})

	// Redis Bridge'i başlat (CrewAI haberleşmesi için)
	var redisBridge *bridge.RedisBridge
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}

	redisBridge, err = bridge.NewRedisBridge(bridge.Config{
		Addr:     redisAddr,
		Password: os.Getenv("REDIS_PASSWORD"),
	})
	if err != nil {
		log.Printf("⚠️ Redis bağlantısı kurulamadı: %v (CrewAI entegrasyonu devre dışı)", err)
		redisBridge = nil
	} else {
		defer redisBridge.Close()
	}

	// Graceful shutdown için context oluştur
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Redis yanıtlarını dinlemeye başla (arka plan goroutine)
	if redisBridge != nil {
		go redisBridge.ListenForResponses(ctx)
		log.Println("✅ Redis Bridge aktif — CrewAI haberleşmesi hazır")
	}

	// Otonom Tetikleyici (Watcher) başlat
	userWatcher := watcher.NewFromEnv(sbClient, aiGenerator, redisBridge)
	go userWatcher.Start(ctx)
	log.Println("✅ Watcher (Otonom Tetikleyici) başlatıldı")

	// OS sinyallerini dinle
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Ana otonom ajan döngüsü
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	log.Println("✅ Worker hazır. Otonom ajan döngüsü başlatıldı.")

	// Günlük etkileşim senkronizasyonu için takipçi
	lastSyncDate := ""

	for {
		select {
		case <-ticker.C:
			processAgentTasks(ctx, sbClient, aiGenerator, igClient, redisBridge)

			// Günde bir kez Insights Sync çalıştır (Örn: Gece 02:00 civarı)
			now := time.Now()
			dateStr := now.Format("2006-01-02")
			if now.Hour() == 2 && dateStr != lastSyncDate {
				log.Println("📊 Günlük etkileşim senkronizasyonu başlatılıyor...")
				syncInstagramInsights(ctx, sbClient)
				lastSyncDate = dateStr
				log.Println("✅ Günlük etkileşim senkronizasyonu tamamlandı.")
			}
		case sig := <-sigChan:
			log.Printf("🛑 Sinyal alındı: %v. Worker kapatılıyor...", sig)
			cancel()
			return
		case <-ctx.Done():
			log.Println("🛑 Context iptal edildi. Worker kapatılıyor...")
			return
		}
	}
}

// processAgentTasks bekleyen ajan görevlerini işler.
func processAgentTasks(
	ctx context.Context,
	sbClient *supabase.Client,
	aiGen *ai.Generator,
	igClient *social.InstagramClient,
	redisBridge *bridge.RedisBridge,
) {
	log.Println("🔄 Bekleyen görevler kontrol ediliyor...")

	// 1. Supabase'den zamanlanmış ve bekleyen görevleri çek
	tasks, err := sbClient.GetPendingTasks(ctx)
	if err != nil {
		log.Printf("❌ Görev sorgulama hatası: %v", err)
		return
	}

	if len(tasks) == 0 {
		return
	}

	log.Printf("📋 %d bekleyen görev bulundu", len(tasks))

	for _, task := range tasks {
		// 2. Görevi işleniyor olarak işaretle
		_ = sbClient.UpdateTaskStatus(ctx, task.ID, "processing", "")

		// 3. CrewAI üzerinden strateji talebi gönder (Redis bridge aktifse)
		if redisBridge != nil {
			strategyResult, err := requestCrewAIStrategy(ctx, redisBridge, task.UserID, task.ID)
			if err != nil {
				log.Printf("⚠️ CrewAI strateji hatası (görev %s): %v", task.ID, err)
				// CrewAI başarısız olursa doğrudan AI ile devam et
			} else {
				log.Printf("✅ CrewAI strateji sonucu alındı (görev %s): status=%s",
					task.ID, strategyResult.Status)

				// Strateji sonucunu görev sonucu olarak kaydet
				resultJSON, _ := json.Marshal(strategyResult)
				_ = sbClient.UpdateTaskStatus(ctx, task.ID, "completed", string(resultJSON))

				// DIRECT PUBLISHING (Safe-Gate)
				if strategyResult.Review != nil && strategyResult.Review.Score >= 0.80 && len(strategyResult.Contents) > 0 {
					log.Printf("🚀 Güven Puanı yeterli (%.2f). Safe-Gate kontrolleri başlatılıyor...", strategyResult.Review.Score)

					// 1. Validation Middleware: Negatif Kelime Filtresi
					var negativePrompts []string
					agentCtx, err := sbClient.GetAgentContext(ctx, task.UserID)
					if err == nil && agentCtx != nil {
						negativePrompts = agentCtx.NegativePrompts
					}

					for _, content := range strategyResult.Contents {
						imageURL := ""
						// Asset listesinden görsel URL'ini bul
						for _, asset := range strategyResult.TopAssets {
							if asset.AssetID == content.AssetID {
								imageURL = asset.StorageURL
								break
							}
						}

						// Fall-Back Mechanism: Müsait bir görsel bulunamadıysa
						if imageURL == "" {
							log.Printf("⚠️ Görev %s için görsel URL'i bulunamadı. Fall-Back tetikleniyor.", task.ID)
							NotifyUser(ctx, task.UserID, "Senin için bir şey planlayamadım, bir göz atar mısın?")
							continue
						}

						// Negatif kelime kontrolü (büyük/küçük harf duyarsız)
						hasNegativeWord := false
						captionLower := strings.ToLower(content.Caption)
						for _, word := range negativePrompts {
							if word != "" && strings.Contains(captionLower, strings.ToLower(word)) {
								log.Printf("🚨 Safe-Gate İptali: Negatif kelime tespit edildi ('%s').", word)
								hasNegativeWord = true
								break
							}
						}

						if hasNegativeWord {
							NotifyUser(ctx, task.UserID, "İçerikte negatif bir kelime tespit ettim, otonom yayını durdurdum. Lütfen bir göz atar mısın?")
							continue
						}

						// Yayınla
						res, err := igClient.PostMedia(ctx, imageURL, content.Caption)
						if err != nil {
							log.Printf("❌ Otomatik yayınlama hatası: %v", err)
							continue
						}

						// Başarılıysa DB'yi güncelle
						_ = sbClient.UpdateTaskWithPostID(ctx, task.ID, res.PostID)
						log.Printf("✅ İçerik Instagram'da paylaşıldı: %s", res.PostID)

						// Varlığı yayınlanmış olarak işaretle
						if content.AssetID != "" {
							_ = sbClient.MarkAssetPublished(ctx, content.AssetID)
						}
					}
				} else {
					// Fall-Back Mechanism: Puan 80'in altındaysa veya üretilen içerik yoksa
					score := 0.0
					if strategyResult.Review != nil {
						score = strategyResult.Review.Score
					}
					log.Printf("⚠️ Safe-Gate Fall-Back: Otonom yayın iptal edildi (Puan: %.2f, İçerik Sayısı: %d)", score, len(strategyResult.Contents))
					NotifyUser(ctx, task.UserID, "Senin için bir şey planlayamadım, bir göz atar mısın?")
				}
				continue
			}
		}

		// 4. Fallback: Doğrudan AI ile içerik üret
		content, err := aiGen.GenerateContent(ctx, task.Prompt)
		if err != nil {
			log.Printf("❌ AI içerik üretme hatası (görev %s): %v", task.ID, err)
			_ = sbClient.UpdateTaskStatus(ctx, task.ID, "failed", err.Error())
			continue
		}

		resultJSON, _ := json.Marshal(content)
		_ = sbClient.UpdateTaskStatus(ctx, task.ID, "completed", string(resultJSON))
		log.Printf("✅ Görev tamamlandı (fallback AI): %s", task.ID)
	}
}

// requestCrewAIStrategy, Redis üzerinden CrewAI'ya strateji talebi gönderir.
func requestCrewAIStrategy(
	ctx context.Context,
	redisBridge *bridge.RedisBridge,
	userID, taskID string,
) (*bridge.StrategyResponse, error) {
	req := &bridge.StrategyRequest{
		RequestID: uuid.New().String(),
		UserID:    userID,
		TaskID:    taskID,
	}

	// 5 dakika timeout ile CrewAI yanıtını bekle
	return redisBridge.PublishStrategyRequest(ctx, req, 5*time.Minute)
}

// NotifyUser kullanıcıya sistem bildirimi gönderir (Fall-Back mekanizması vs.)
func NotifyUser(ctx context.Context, userID, message string) {
	// TODO: Gerçek bildirim altyapısına (FCM, Push, WebSocket vb.) entegre edilecek.
	// Şimdilik sadece log'a ve veritabanına bildirim olarak yazabilir/konsola loglayabiliriz.
	log.Printf("🔔 BİLDİRİM [Kullanıcı: %s]: %s", userID, message)
}


// syncInstagramInsights, Instagram'dan etkileşim verilerini çekip DB'yi günceller.
func syncInstagramInsights(ctx context.Context, sbClient *supabase.Client) {
	// 1. Aktif hesapları al
	accounts, err := sbClient.GetActiveInstagramAccounts(ctx)
	if err != nil {
		log.Printf("❌ Aktif hesaplar alınamadı: %v", err)
		return
	}

	// 2. Yayınlanmış son görevleri al
	tasks, err := sbClient.GetPublishedTasks(ctx)
	if err != nil {
		log.Printf("❌ Yayınlanmış görevler alınamadı: %v", err)
		return
	}

	for _, account := range accounts {
		// Bu hesap için geçici bir IG client oluştur
		igClient := social.NewInstagramClient(social.InstagramConfig{
			AccessToken: account.AccessToken,
			AccountID:   account.InstagramAccountID,
		})

		for _, task := range tasks {
			// Sadece bu hesabın görevlerini işle
			if task.InstagramAccountID != account.ID && task.InstagramPostID == "" {
				continue
			}

			log.Printf("📉 Insights çekiliyor: Account=%s, PostID=%s", account.Username, task.InstagramPostID)

			insights, err := igClient.GetPostInsights(ctx, task.InstagramPostID)
			if err != nil {
				log.Printf("⚠️ Insights hatası (%s): %v", task.InstagramPostID, err)
				continue
			}

			// interaction_analytics tablosuna ekle
			// Not: task.Result'tan asset_id'yi çekmemiz gerekebilir
			var result bridge.StrategyResponse
			_ = json.Unmarshal([]byte(task.Result), &result)

			assetID := ""
			if len(result.Contents) > 0 {
				assetID = result.Contents[0].AssetID
			}

			if assetID != "" {
				err = sbClient.CreateInteractionAnalytics(ctx, supabase.InteractionAnalytics{
					AssetID:     assetID,
					Likes:       0, // IG insights API bazen likes'ı farklı döner, impressions/reach/engagement/saved odaklıyız
					Comments:    0,
					Shares:      0,
					Saves:       insights.Saves,
					Impressions: insights.Impressions,
					Reach:       insights.Reach,
					EngagementRate: calculateEngagementRate(insights),
				})
				if err != nil {
					log.Printf("❌ Analitik kaydetme hatası: %v", err)
				}
			}
		}
	}
}

func calculateEngagementRate(i *social.InstagramInsights) float64 {
	if i.Reach == 0 {
		return 0
	}
	return (float64(i.Engagement) / float64(i.Reach)) * 100
}
