package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"

	"github.com/ekinbulut/x/internal/ai"
	"github.com/ekinbulut/x/internal/bridge"
	"github.com/ekinbulut/x/internal/social"
	"github.com/ekinbulut/x/internal/supabase"
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

	// OS sinyallerini dinle
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Ana otonom ajan döngüsü
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	log.Println("✅ Worker hazır. Otonom ajan döngüsü başlatıldı.")

	for {
		select {
		case <-ticker.C:
			processAgentTasks(ctx, sbClient, aiGenerator, igClient, redisBridge)
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
