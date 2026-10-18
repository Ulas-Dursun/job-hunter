-- ─────────────────────────────────────────────────────────────────────────
-- Migration 002: Add cover_letter and tailored_cv columns to jobs table
-- Idempotent: güvenle birden fazla çalıştırılabilir
-- ─────────────────────────────────────────────────────────────────────────

ALTER TABLE jobs ADD COLUMN IF NOT EXISTS cover_letter TEXT;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS tailored_cv JSONB;
