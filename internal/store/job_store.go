package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ulasdursun/job-hunter/internal/analyzer"
)

// JobRecord, PostgreSQL jobs tablosuna yazılacak veriyi tutar.
type JobRecord struct {
	Source      string
	ExternalID  string
	URL         string
	Title       string
	Company     string
	Location    string
	Description string
	PostedAt    string
	Score       *analyzer.ScoreResult
}

// JobStore, jobs tablosuna yazma işlemlerini yönetir.
type JobStore struct {
	pool *pgxpool.Pool
}

func NewJobStore(pool *pgxpool.Pool) *JobStore {
	return &JobStore{pool: pool}
}

// Upsert, iş ilanını ve AI analiz sonucunu PostgreSQL'e yazar.
// ON CONFLICT: aynı ilan tekrar gelirse sadece score güncellenir.
func (s *JobStore) Upsert(ctx context.Context, rec JobRecord) error {
	// posted_at boşsa nil gönder — PostgreSQL TIMESTAMPTZ boş string kabul etmez
	var postedAt *string
	if rec.PostedAt != "" && rec.PostedAt != "None" {
		postedAt = &rec.PostedAt
	}

	var (
		matchScore    *float64
		skillOverlap  []string
		missingSkills []string
		redFlags      []string
		aiAction      *string
		aiReasoning   *string
		roleFamily    *string
	)

	if rec.Score != nil {
		matchScore = &rec.Score.MatchScore
		skillOverlap = rec.Score.SkillOverlap
		missingSkills = rec.Score.MissingSkills
		redFlags = rec.Score.RedFlags
		aiAction = &rec.Score.Action
		aiReasoning = &rec.Score.Reasoning
		roleFamily = &rec.Score.RoleFamily

		// Nil slice yerine boş slice — PostgreSQL NULL yerine {} yazar
		if skillOverlap == nil {
			skillOverlap = []string{}
		}
		if missingSkills == nil {
			missingSkills = []string{}
		}
		if redFlags == nil {
			redFlags = []string{}
		}
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO jobs (
			source, external_id, url, title, company, location,
			description, posted_at,
			match_score, skill_overlap, missing_skills, red_flags,
			ai_action, ai_reasoning, role_family
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8,
			$9, $10, $11, $12,
			$13, $14, $15
		)
		ON CONFLICT (source, external_id)
		DO UPDATE SET
			match_score    = EXCLUDED.match_score,
			skill_overlap  = EXCLUDED.skill_overlap,
			missing_skills = EXCLUDED.missing_skills,
			red_flags      = EXCLUDED.red_flags,
			ai_action      = EXCLUDED.ai_action,
			ai_reasoning   = EXCLUDED.ai_reasoning,
			role_family    = EXCLUDED.role_family,
			updated_at     = NOW()
		WHERE jobs.match_score IS NULL
	`,
		rec.Source, rec.ExternalID, rec.URL, rec.Title, rec.Company, rec.Location,
		rec.Description, postedAt,
		matchScore, skillOverlap, missingSkills, redFlags,
		aiAction, aiReasoning, roleFamily,
	)

	if err != nil {
		return fmt.Errorf("upserting job %s/%s: %w", rec.Source, rec.ExternalID, err)
	}

	return nil
}

// GetNotifiedAt, bir ilanın daha önce Telegram bildirimi alıp almadığını kontrol eder.
// nil dönerse hiç bildirim gönderilmemiş demektir.
func (s *JobStore) GetNotifiedAt(ctx context.Context, source, externalID string) (*time.Time, error) {
	var notifiedAt *time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT notified_at FROM jobs WHERE source = $1 AND external_id = $2`,
		source, externalID,
	).Scan(&notifiedAt)
	if err != nil {
		return nil, err
	}
	return notifiedAt, nil
}

// MarkNotified, bir ilanı "bildirim gönderildi" olarak işaretler.
// Bu, Redis dedup hafızası silinse bile duplicate Telegram bildirimini engeller —
// ikinci savunma hattı.
func (s *JobStore) MarkNotified(ctx context.Context, source, externalID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE jobs SET notified_at = NOW() WHERE source = $1 AND external_id = $2`,
		source, externalID,
	)
	return err
}
