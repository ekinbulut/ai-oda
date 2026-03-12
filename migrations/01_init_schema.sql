-- ============================================================
-- Supabase SQL Şeması - Otonom Sosyal Medya Ajan Sistemi
-- Bu dosyayı Supabase SQL Editörüne yapıştırarak çalıştırın.
-- ============================================================

-- UUID uzantısını etkinleştir (Supabase'de genellikle varsayılan olarak aktif)
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

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
-- İndeksler
-- ============================================================
CREATE INDEX IF NOT EXISTS idx_content_tasks_status ON public.content_tasks(status);
CREATE INDEX IF NOT EXISTS idx_content_tasks_scheduled ON public.content_tasks(scheduled_at);
CREATE INDEX IF NOT EXISTS idx_content_tasks_user ON public.content_tasks(user_id);
CREATE INDEX IF NOT EXISTS idx_instagram_accounts_user ON public.instagram_accounts(user_id);
CREATE INDEX IF NOT EXISTS idx_post_analytics_task ON public.post_analytics(task_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_user ON public.audit_logs(user_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_action ON public.audit_logs(action);

-- ============================================================
-- Row Level Security (RLS)
-- ============================================================
ALTER TABLE public.profiles ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.instagram_accounts ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.content_tasks ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.agent_configs ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.post_analytics ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.audit_logs ENABLE ROW LEVEL SECURITY;

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
