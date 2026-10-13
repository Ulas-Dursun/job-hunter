-- ─────────────────────────────────────────────────────────────────────────
-- Migration 001: Jobs table
-- Idempotent: güvenle birden fazla çalıştırılabilir (IF NOT EXISTS kullanıldı)
-- PostgreSQL docker init: container ilk başladığında otomatik çalışır
-- ─────────────────────────────────────────────────────────────────────────

CREATE EXTENSION IF NOT EXISTS "pgcrypto";  -- gen_random_uuid() için

-- ─────────────────────────────────────────────────────────────────────────
-- JOBS
-- Tüm kaynaklardan gelen iş ilanlarının ana tablosu.
-- Scraping'den notification'a kadar pipeline'ın tüm state'i burada tutulur.
-- ─────────────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS jobs (
                                    id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Kaynak bilgisi
    source          VARCHAR(50)  NOT NULL,       -- 'kariyer_net' | 'linkedin' | 'indeed'
    external_id     VARCHAR(255) NOT NULL,       -- Kaynağın kendi ID'si
    url             TEXT         NOT NULL,

    -- İlan detayları
    title           VARCHAR(255) NOT NULL,
    company         VARCHAR(255) NOT NULL,
    location        VARCHAR(255),
    description     TEXT,
    posted_at       TIMESTAMPTZ,                 -- İlanın yayın tarihi (kaynak sağlıyorsa)
    scraped_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    -- AI analiz sonuçları (Gemini scoring sonrası doldurulur)
    match_score     NUMERIC(4,2),                -- 0.00 – 10.00
    skill_overlap   TEXT[]       DEFAULT '{}',   -- Örtüşen yetenekler: {"Go","PostgreSQL"}
    missing_skills  TEXT[]       DEFAULT '{}',   -- Eksik: {"Kubernetes","Terraform"}
    red_flags       TEXT[]       DEFAULT '{}',   -- Uyarılar: {"5+ years required"}
    ai_action       VARCHAR(20),                 -- 'apply' | 'watch' | 'skip'
    ai_reasoning    TEXT,                        -- AI'ın karar gerekçesi
    role_family     VARCHAR(20),                 -- 'primary' | 'secondary'

-- Başvuru takibi (kullanıcı tarafından güncellenir)
-- State machine: new → seen → applied → interview → rejected | offer | skipped
    status          VARCHAR(30)  NOT NULL DEFAULT 'new',

    -- Metadata
    is_duplicate    BOOLEAN      NOT NULL DEFAULT FALSE,
    notified_at     TIMESTAMPTZ,                 -- Telegram'a gönderilme zamanı
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    -- Aynı kaynaktan aynı ilan iki kez yazılamaz
    UNIQUE (source, external_id)
    );

-- ── Index'ler ─────────────────────────────────────────────────────────────
-- Score'a göre sıralama (dashboard için)
CREATE INDEX IF NOT EXISTS idx_jobs_score
    ON jobs (match_score DESC NULLS LAST);

-- Başvuru takibi sorguları
CREATE INDEX IF NOT EXISTS idx_jobs_status
    ON jobs (status);

-- Şirket bazlı filtreleme
CREATE INDEX IF NOT EXISTS idx_jobs_company
    ON jobs (company);

-- Zaman bazlı sıralama (en yeni önce)
CREATE INDEX IF NOT EXISTS idx_jobs_scraped_at
    ON jobs (scraped_at DESC);

-- Kaynak bazlı filtreleme
CREATE INDEX IF NOT EXISTS idx_jobs_source
    ON jobs (source);

-- Primary/secondary ayrımına göre filtreleme
CREATE INDEX IF NOT EXISTS idx_jobs_role_family
    ON jobs (role_family);

-- ── Trigger: updated_at otomatik güncelleme ────────────────────────────────
CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trigger_jobs_updated_at ON jobs;
CREATE TRIGGER trigger_jobs_updated_at
    BEFORE UPDATE ON jobs
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();