-- ─────────────────────────────────────────────────────────────────────────
-- Migration 002: Add cover_letter, tailored_cv, and interview_prep columns
-- Idempotent: güvenle birden fazla çalıştırılabilir
-- ─────────────────────────────────────────────────────────────────────────

ALTER TABLE jobs ADD COLUMN IF NOT EXISTS cover_letter TEXT;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS tailored_cv JSONB;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS interview_prep JSONB;
