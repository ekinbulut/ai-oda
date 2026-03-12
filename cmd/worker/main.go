package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/ekinbulut/x/internal/ai"
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

	// Graceful shutdown için context oluştur
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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
			processAgentTasks(ctx, sbClient, aiGenerator, igClient)
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
) {
	log.Println("🔄 Bekleyen görevler kontrol ediliyor...")

	// TODO: Aşağıdaki adımları uygula:
	// 1. Supabase'den zamanlanmış ve bekleyen görevleri çek
	// 2. Her görev için:
	//    a. AI ile içerik üret (metin + görsel prompt)
	//    b. İçeriği Instagram'a gönder
	//    c. Sonuçları Supabase'e kaydet
	//    d. Görevi tamamlandı olarak işaretle

	_ = ctx
	_ = sbClient
	_ = aiGen
	_ = igClient
}
