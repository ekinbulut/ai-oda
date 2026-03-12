-- ============================================================
-- Supabase SQL Şeması - Otonom Sosyal Medya Ajan Sistemi
-- Bu dosyayı Supabase SQL Editörüne yapıştırarak çalıştırın.
-- ============================================================

-- UUID uzantısını etkinleştir (Supabase'de genellikle varsayılan olarak aktif)
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Vektör benzerlik araması için pgvector uzantısı
CREATE EXTENSION IF NOT EXISTS "vector";

-- ============================================================
-- 1. Kullanıcı Profilleri
-- Supabase Auth ile entegre kullanıcı bilgileri
-- ============================================================
CREATE TABLE IF NOT EXISTS public.profiles (
    id UUID PRIMARY KEY REFERENCES auth.users(id) ON DELETE CASCADE,
    email TEXT,
    full_name TEXT,
    avatar_url TEXT,
    subscription_status TEXT DEFAULT 'free' CHECK (subscription_status IN ('free', 'pro', 'enterprise')),
    subscription_expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Yeni kullanıcı oluşturulduğunda profil otomatik oluşsun
CREATE OR REPLACE FUNCTION public.handle_new_user()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO public.profiles (id, email, full_name, avatar_url)
    VALUES (
        NEW.id,
        NEW.email,
        NEW.raw_user_meta_data->>'full_name',
        NEW.raw_user_meta_data->>'avatar_url'
    );
    RETURN NEW;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER;

CREATE OR REPLACE TRIGGER on_auth_user_created
    AFTER INSERT ON auth.users
    FOR EACH ROW EXECUTE FUNCTION public.handle_new_user();

-- ============================================================
-- 2. Instagram Hesapları
-- Kullanıcıların bağladığı Instagram Business hesapları
-- ============================================================
CREATE TABLE IF NOT EXISTS public.instagram_accounts (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES public.profiles(id) ON DELETE CASCADE,
    instagram_account_id TEXT NOT NULL,
    access_token TEXT NOT NULL,
    token_expires_at TIMESTAMPTZ,
    username TEXT,
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),

    UNIQUE(user_id, instagram_account_id)
);

-- ============================================================
-- 3. İçerik Görevleri
-- Otonom ajanlar tarafından işlenecek zamanlanmış görevler
-- ============================================================
CREATE TABLE IF NOT EXISTS public.content_tasks (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES public.profiles(id) ON DELETE CASCADE,
    instagram_account_id UUID REFERENCES public.instagram_accounts(id) ON DELETE SET NULL,
    status TEXT DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'completed', 'failed', 'cancelled')),
    content_type TEXT DEFAULT 'photo' CHECK (content_type IN ('photo', 'carousel', 'reel', 'story')),
    prompt TEXT NOT NULL,
    generated_caption TEXT,
    generated_hashtags TEXT,
    generated_image_prompt TEXT,
    image_url TEXT,
    instagram_post_id TEXT,
    error_message TEXT,
    scheduled_at TIMESTAMPTZ NOT NULL,
    published_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- ============================================================
-- 4. Ajan Yapılandırmaları
-- Her kullanıcı için otonom ajan ayarları
-- ============================================================
CREATE TABLE IF NOT EXISTS public.agent_configs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES public.profiles(id) ON DELETE CASCADE,
    ai_provider TEXT DEFAULT 'openai' CHECK (ai_provider IN ('openai', 'gemini')),
    ai_model TEXT DEFAULT 'gpt-4o',
    tone TEXT DEFAULT 'professional',
    language TEXT DEFAULT 'tr',
    posting_frequency TEXT DEFAULT 'daily' CHECK (posting_frequency IN ('hourly', 'daily', 'weekly', 'custom')),
    preferred_posting_times JSONB DEFAULT '["10:00", "14:00", "19:00"]'::jsonb,
    target_audience TEXT,
    brand_keywords TEXT[],
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),

    UNIQUE(user_id)
);

-- ============================================================
-- 5. Gönderi Analizleri
-- Yayınlanan gönderilerin performans metrikleri
-- ============================================================
CREATE TABLE IF NOT EXISTS public.post_analytics (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    task_id UUID NOT NULL REFERENCES public.content_tasks(id) ON DELETE CASCADE,
    impressions INTEGER DEFAULT 0,
    reach INTEGER DEFAULT 0,
    engagement INTEGER DEFAULT 0,
    saves INTEGER DEFAULT 0,
    likes INTEGER DEFAULT 0,
    comments INTEGER DEFAULT 0,
    shares INTEGER DEFAULT 0,
    fetched_at TIMESTAMPTZ DEFAULT NOW(),

    UNIQUE(task_id, fetched_at)
);

-- ============================================================
-- 6. Audit Log
-- Sistem olaylarının kaydı
-- ============================================================
CREATE TABLE IF NOT EXISTS public.audit_logs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES public.profiles(id) ON DELETE SET NULL,
    action TEXT NOT NULL,
    entity_type TEXT,
    entity_id UUID,
    metadata JSONB,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- ============================================================
-- 7. Medya Varlıkları
-- Kullanıcının tüm geçmiş ve yeni yüklenen görsellerini/videolarını tutar.
-- AI vision analizi ve vektör embedding ile içerik tanıma sağlar.
-- ============================================================
CREATE TABLE IF NOT EXISTS public.media_assets (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES public.profiles(id) ON DELETE CASCADE,
    media_type TEXT NOT NULL CHECK (media_type IN ('image', 'video')),
    storage_url TEXT NOT NULL,
    is_published BOOLEAN DEFAULT false,
    vision_analysis JSONB DEFAULT '{}'::jsonb,
    embedding vector(1536),
    original_filename TEXT,
    file_size_bytes BIGINT,
    mime_type TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

COMMENT ON COLUMN public.media_assets.vision_analysis IS 'AI tarafından çıkarılan etiketler, nesneler, renkler, sahne açıklaması vb.';
COMMENT ON COLUMN public.media_assets.embedding IS '1536 boyutlu vektör - benzerlik araması için (OpenAI ada-002 uyumlu)';
COMMENT ON COLUMN public.media_assets.is_published IS 'Bu medya sosyal medyada paylaşıldı mı?';

-- ============================================================
-- 8. Etkileşim Analizleri
-- Her medya varlığının performans metriklerini takip eder.
-- CrewAI hangi görselin "kazanan" olduğunu buradan anlayacak.
-- ============================================================
CREATE TABLE IF NOT EXISTS public.interaction_analytics (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    asset_id UUID NOT NULL REFERENCES public.media_assets(id) ON DELETE CASCADE,
    likes INTEGER DEFAULT 0,
    comments INTEGER DEFAULT 0,
    shares INTEGER DEFAULT 0,
    saves INTEGER DEFAULT 0,
    impressions INTEGER DEFAULT 0,
    reach INTEGER DEFAULT 0,
    engagement_rate NUMERIC(5,2) DEFAULT 0.00,
    fetched_at TIMESTAMPTZ DEFAULT NOW(),
    created_at TIMESTAMPTZ DEFAULT NOW()
);

COMMENT ON TABLE public.interaction_analytics IS 'CrewAI ajanları bu tabloyu analiz ederek en başarılı içerik kalıplarını belirler.';

-- ============================================================
-- 9. Ajan Bağlamı
-- Her kullanıcı için marka sesi, hedef kitle ve içerik tercihleri.
-- CrewAI ajanları bu bağlamı kullanarak kişiselleştirilmiş içerik üretir.
-- ============================================================
CREATE TABLE IF NOT EXISTS public.agent_context (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES public.profiles(id) ON DELETE CASCADE,
    brand_voice TEXT,
    target_audience TEXT,
    negative_prompts TEXT[],
    posting_frequency_preference TEXT DEFAULT 'daily' CHECK (posting_frequency_preference IN ('hourly', 'twice_daily', 'daily', 'every_other_day', 'weekly', 'custom')),
    content_pillars TEXT[],
    visual_style_preferences JSONB DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),

    UNIQUE(user_id)
);

COMMENT ON COLUMN public.agent_context.brand_voice IS 'Markanın iletişim tonu ve stili (örn: samimi, profesyonel, eğlenceli)';
COMMENT ON COLUMN public.agent_context.negative_prompts IS 'İçerik üretiminde kaçınılması gereken konular ve ifadeler';
COMMENT ON COLUMN public.agent_context.content_pillars IS 'Marka için ana içerik temaları';

-- ============================================================
-- İndeksler
-- ============================================================
CREATE INDEX IF NOT EXISTS idx_content_tasks_status ON public.content_tasks(status);
CREATE INDEX IF NOT EXISTS idx_content_tasks_scheduled ON public.content_tasks(scheduled_at);
CREATE INDEX IF NOT EXISTS idx_content_tasks_user ON public.content_tasks(user_id);
CREATE INDEX IF NOT EXISTS idx_instagram_accounts_user ON public.instagram_accounts(user_id);
CREATE INDEX IF NOT EXISTS idx_post_analytics_task ON public.post_analytics(task_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_user ON public.audit_logs(user_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_action ON public.audit_logs(action);

-- media_assets indeksleri
CREATE INDEX IF NOT EXISTS idx_media_assets_user ON public.media_assets(user_id);
CREATE INDEX IF NOT EXISTS idx_media_assets_published ON public.media_assets(is_published);
CREATE INDEX IF NOT EXISTS idx_media_assets_type ON public.media_assets(media_type);
CREATE INDEX IF NOT EXISTS idx_media_assets_created ON public.media_assets(created_at DESC);

-- interaction_analytics indeksleri
CREATE INDEX IF NOT EXISTS idx_interaction_analytics_asset ON public.interaction_analytics(asset_id);
CREATE INDEX IF NOT EXISTS idx_interaction_analytics_fetched ON public.interaction_analytics(fetched_at DESC);
CREATE INDEX IF NOT EXISTS idx_interaction_analytics_engagement ON public.interaction_analytics(engagement_rate DESC);

-- agent_context indeksleri
CREATE INDEX IF NOT EXISTS idx_agent_context_user ON public.agent_context(user_id);

-- ============================================================
-- Row Level Security (RLS)
-- ============================================================
ALTER TABLE public.profiles ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.instagram_accounts ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.content_tasks ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.agent_configs ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.post_analytics ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.audit_logs ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.media_assets ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.interaction_analytics ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.agent_context ENABLE ROW LEVEL SECURITY;

-- Kullanıcılar sadece kendi verilerine erişebilir
CREATE POLICY "Users can view own profile" ON public.profiles
    FOR SELECT USING (auth.uid() = id);

CREATE POLICY "Users can update own profile" ON public.profiles
    FOR UPDATE USING (auth.uid() = id);

CREATE POLICY "Users can view own instagram accounts" ON public.instagram_accounts
    FOR ALL USING (auth.uid() = user_id);

CREATE POLICY "Users can manage own content tasks" ON public.content_tasks
    FOR ALL USING (auth.uid() = user_id);

CREATE POLICY "Users can manage own agent config" ON public.agent_configs
    FOR ALL USING (auth.uid() = user_id);

CREATE POLICY "Users can view own post analytics" ON public.post_analytics
    FOR SELECT USING (
        EXISTS (
            SELECT 1 FROM public.content_tasks
            WHERE content_tasks.id = post_analytics.task_id
            AND content_tasks.user_id = auth.uid()
        )
    );

CREATE POLICY "Users can view own audit logs" ON public.audit_logs
    FOR SELECT USING (auth.uid() = user_id);

CREATE POLICY "Users can manage own media assets" ON public.media_assets
    FOR ALL USING (auth.uid() = user_id);

CREATE POLICY "Users can view own interaction analytics" ON public.interaction_analytics
    FOR SELECT USING (
        EXISTS (
            SELECT 1 FROM public.media_assets
            WHERE media_assets.id = interaction_analytics.asset_id
            AND media_assets.user_id = auth.uid()
        )
    );

CREATE POLICY "Users can manage own agent context" ON public.agent_context
    FOR ALL USING (auth.uid() = user_id);

-- ============================================================
-- updated_at otomatik güncelleme fonksiyonu
-- ============================================================
CREATE OR REPLACE FUNCTION public.update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE TRIGGER update_profiles_updated_at
    BEFORE UPDATE ON public.profiles
    FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column();

CREATE OR REPLACE TRIGGER update_instagram_accounts_updated_at
    BEFORE UPDATE ON public.instagram_accounts
    FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column();

CREATE OR REPLACE TRIGGER update_content_tasks_updated_at
    BEFORE UPDATE ON public.content_tasks
    FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column();

CREATE OR REPLACE TRIGGER update_agent_configs_updated_at
    BEFORE UPDATE ON public.agent_configs
    FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column();

CREATE OR REPLACE TRIGGER update_media_assets_updated_at
    BEFORE UPDATE ON public.media_assets
    FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column();

CREATE OR REPLACE TRIGGER update_agent_context_updated_at
    BEFORE UPDATE ON public.agent_context
    FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column();
