-- migrations/02_add_impact_score_to_media_assets.sql

-- media_assets tablosuna impact_score sütunu ekleniyor
ALTER TABLE public.media_assets 
ADD COLUMN IF NOT EXISTS impact_score NUMERIC(5,2) DEFAULT 0.00;

COMMENT ON COLUMN public.media_assets.impact_score IS 'Gönderinin etkileşim verilerine göre hesaplanan başarı puanı.';

-- Ajanların seçim yaparken hızlıca puanına göre sıralayabilmesi için indeks
CREATE INDEX IF NOT EXISTS idx_media_assets_impact_score ON public.media_assets(impact_score DESC);
