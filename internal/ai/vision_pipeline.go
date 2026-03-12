package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/ekinbulut/x/internal/supabase"
)

// VisionPipeline, görsel analiz hattını yöneten orchestrator'dır.
// Medya varlığını veritabanından çeker, GPT-4o Vision ile analiz eder
// ve sonuçları media_assets tablosuna kaydeder.
type VisionPipeline struct {
	generator *Generator
	sbClient  *supabase.Client
}

// NewVisionPipeline, yeni bir Vision Pipeline oluşturur.
func NewVisionPipeline(generator *Generator, sbClient *supabase.Client) *VisionPipeline {
	return &VisionPipeline{
		generator: generator,
		sbClient:  sbClient,
	}
}

// ProcessAsset, belirtilen assetID için tam görsel analiz hattını çalıştırır:
// 1. Medya varlığını veritabanından çeker (storage_url'i alır)
// 2. Kullanıcının marka bağlamını getirir (varsa)
// 3. GPT-4o Vision API ile görseli analiz eder
// 4. Analiz sonucunu media_assets.vision_analysis alanına kaydeder
func (vp *VisionPipeline) ProcessAsset(ctx context.Context, assetID string) (*VisionAnalysisResult, error) {
	log.Printf("🔍 Vision Pipeline başlatılıyor - asset: %s", assetID)

	// 1. Medya varlığını getir
	asset, err := vp.sbClient.GetMediaAsset(ctx, assetID)
	if err != nil {
		return nil, fmt.Errorf("medya varlığı alınamadı: %w", err)
	}

	// Sadece image tipindeki varlıkları destekle
	if asset.MediaType != "image" {
		return nil, fmt.Errorf("vision analizi sadece image tipinde desteklenmektedir (mevcut: %s)", asset.MediaType)
	}

	log.Printf("📷 Medya varlığı bulundu - url: %s, user: %s", asset.StorageURL, asset.UserID)

	// 2. Kullanıcının marka bağlamını getir (opsiyonel)
	var brandKeywords []string
	var brandVoice string

	agentCtx, err := vp.sbClient.GetAgentContext(ctx, asset.UserID)
	if err != nil {
		// Marka bağlamı yoksa hata olmaz, devam et
		log.Printf("⚠️ Marka bağlamı alınamadı (devam ediliyor): %v", err)
	}
	if agentCtx != nil {
		brandVoice = agentCtx.BrandVoice
		brandKeywords = agentCtx.ContentPillars
		log.Printf("🏷️ Marka bağlamı yüklendi - ses: %s, sütunlar: %v", brandVoice, brandKeywords)
	}

	// 3. GPT-4o Vision ile analiz et
	log.Printf("🤖 GPT-4o Vision analizi başlatılıyor...")
	result, err := vp.generator.AnalyzeMediaWithBrandContext(ctx, asset.StorageURL, brandKeywords, brandVoice)
	if err != nil {
		return nil, fmt.Errorf("vision analizi başarısız: %w", err)
	}

	log.Printf("✅ Vision analizi tamamlandı - mood: %s", result.Mood)

	// 4. Sonucu veritabanına kaydet
	visionData := map[string]interface{}{
		"description": result.Description,
		"mood":        result.Mood,
		"brand_fit":   result.BrandFit,
		"analyzed_at": "now",
	}

	// Marka bağlamı kullanıldıysa bunu da kaydet
	if agentCtx != nil {
		visionData["brand_context_used"] = true
	}

	if err := vp.sbClient.UpdateMediaVisionAnalysis(ctx, assetID, visionData); err != nil {
		return nil, fmt.Errorf("vision sonucu kaydedilemedi: %w", err)
	}

	log.Printf("💾 Vision analizi kaydedildi - asset: %s", assetID)

	return result, nil
}

// ProcessAssetRaw, doğrudan görsel URL'i ile analiz yapar (veritabanı olmadan).
// Test ve hızlı prototipleme için kullanışlıdır.
func (vp *VisionPipeline) ProcessAssetRaw(ctx context.Context, imageURL string) (*VisionAnalysisResult, error) {
	return vp.generator.AnalyzeMedia(ctx, imageURL)
}

// VisionAnalysisToJSON, analiz sonucunu JSON string'e dönüştürür.
func VisionAnalysisToJSON(result *VisionAnalysisResult) (string, error) {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", fmt.Errorf("JSON serileştirilemedi: %w", err)
	}
	return string(data), nil
}
