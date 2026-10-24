package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound, istenen ilan kaydı bulunamadığında döner (HTTP 404'e çevrilir).
var ErrNotFound = errors.New("job not found")

// ErrInvalidStatus, izin verilen durum listesinde olmayan status değeri için döner (HTTP 400).
var ErrInvalidStatus = errors.New("invalid status")

// Repository handles all database queries for the dashboard API.
type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// JobFilter holds optional filters for listing jobs.
// Pointer fields mean "not set" — nil is passed straight to SQL as NULL,
// which the query treats as "don't filter on this".
type JobFilter struct {
	Status     *string
	Action     *string
	RoleFamily *string
	MinScore   *float64
	Search     *string
	SortByDate bool
	Limit      int
	Offset     int
}

const baseJobColumns = `
	id, source, title, company, location, url,
	COALESCE(description, ''), posted_at,
	match_score::float8 AS match_score,
	COALESCE(skill_overlap, '{}'),
	COALESCE(missing_skills, '{}'),
	COALESCE(red_flags, '{}'),
	ai_action, ai_reasoning, role_family, status,
	(cover_letter IS NOT NULL AND cover_letter <> '' AND tailored_cv IS NOT NULL) AS has_tailored,
	(interview_prep IS NOT NULL) AS has_interview_prep,
	scraped_at`

// listJobColumns: liste yanıtında açıklama 2000 karaktere kırpılır (kategori sınıflandırması ilk ~1800 karaktere bakar).
// Tam metin modal açılınca GET /api/jobs/{id} ile çekilir; böylece 30 sn'lik yenilemede ~1.7MB yerine ~1MB'dan az veri taşınır.
var listJobColumns = strings.Replace(baseJobColumns, "COALESCE(description, '')", "left(COALESCE(description, ''), 2000)", 1)

// ListJobs returns jobs matching the given filter, sorted and paginated.
func (r *Repository) ListJobs(ctx context.Context, f JobFilter) ([]JobDTO, int, error) {
	orderClause := "ORDER BY match_score DESC NULLS LAST, scraped_at DESC"
	if f.SortByDate {
		orderClause = "ORDER BY scraped_at DESC, match_score DESC NULLS LAST"
	}

	query := fmt.Sprintf(`
		SELECT %s
		FROM jobs
		WHERE match_score IS NOT NULL
		  AND ($1::text   IS NULL OR status = $1::text)
		  AND ($2::text   IS NULL OR ai_action = $2::text)
		  AND ($3::text   IS NULL OR role_family = $3::text)
		  AND ($4::float8 IS NULL OR match_score >= $4::float8)
		  AND ($5::text   IS NULL OR title ILIKE '%%' || $5::text || '%%' OR company ILIKE '%%' || $5::text || '%%')
		%s
		LIMIT $6::int OFFSET $7::int
	`, listJobColumns, orderClause)

	rows, err := r.pool.Query(ctx, query,
		f.Status, f.Action, f.RoleFamily, f.MinScore, f.Search, f.Limit, f.Offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("listing jobs: %w", err)
	}
	defer rows.Close()

	var jobs []JobDTO
	for rows.Next() {
		var j JobDTO
		if err := rows.Scan(
			&j.ID, &j.Source, &j.Title, &j.Company, &j.Location, &j.URL,
			&j.Description, &j.PostedAt,
			&j.MatchScore, &j.SkillOverlap, &j.MissingSkills, &j.RedFlags,
			&j.AIAction, &j.AIReasoning, &j.RoleFamily, &j.Status,
			&j.HasTailored, &j.HasInterviewPrep, &j.ScrapedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scanning job row: %w", err)
		}
		jobs = append(jobs, j)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	total, err := r.countJobs(ctx, f)
	if err != nil {
		return nil, 0, err
	}

	return jobs, total, nil
}

// countJobs returns the total number of jobs matching the filter,
// ignoring pagination — used for the "Total: N" UI element.
func (r *Repository) countJobs(ctx context.Context, f JobFilter) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM jobs
		WHERE match_score IS NOT NULL
		  AND ($1::text   IS NULL OR status = $1::text)
		  AND ($2::text   IS NULL OR ai_action = $2::text)
		  AND ($3::text   IS NULL OR role_family = $3::text)
		  AND ($4::float8 IS NULL OR match_score >= $4::float8)
		  AND ($5::text   IS NULL OR title ILIKE '%' || $5::text || '%' OR company ILIKE '%' || $5::text || '%')
	`
	var total int
	err := r.pool.QueryRow(ctx, query,
		f.Status, f.Action, f.RoleFamily, f.MinScore, f.Search,
	).Scan(&total)
	return total, err
}

// GetJob returns a single job by ID.
func (r *Repository) GetJob(ctx context.Context, id string) (*JobDTO, error) {
	query := fmt.Sprintf(`SELECT %s FROM jobs WHERE id = $1::uuid`, baseJobColumns)

	var j JobDTO
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&j.ID, &j.Source, &j.Title, &j.Company, &j.Location, &j.URL,
		&j.Description, &j.PostedAt,
		&j.MatchScore, &j.SkillOverlap, &j.MissingSkills, &j.RedFlags,
		&j.AIAction, &j.AIReasoning, &j.RoleFamily, &j.Status,
		&j.HasTailored, &j.HasInterviewPrep, &j.ScrapedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("getting job %s: %w", id, err)
	}
	return &j, nil
}

// allowedStatuses defines the valid job status state machine values.
var allowedStatuses = map[string]bool{
	"new": true, "seen": true, "applied": true,
	"interview": true, "rejected": true, "offer": true, "skipped": true,
}

// UpdateStatus changes a job's tracking status.
// Returns an error if the status value is not in allowedStatuses.
func (r *Repository) UpdateStatus(ctx context.Context, id, status string) error {
	if !allowedStatuses[status] {
		return fmt.Errorf("%w: %s", ErrInvalidStatus, status)
	}

	tag, err := r.pool.Exec(ctx,
		`UPDATE jobs SET status = $1::text WHERE id = $2::uuid`,
		status, id,
	)
	if err != nil {
		return fmt.Errorf("updating status for job %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GetStats returns aggregated statistics for the dashboard header and charts.
func (r *Repository) GetStats(ctx context.Context) (*StatsDTO, error) {
	stats := &StatsDTO{}

	row := r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE ai_action = 'apply'),
			COUNT(*) FILTER (WHERE ai_action = 'watch'),
			COUNT(*) FILTER (WHERE ai_action = 'skip'),
			COALESCE(AVG(match_score)::float8, 0),
			COALESCE(MAX(match_score)::float8, 0)
		FROM jobs
		WHERE match_score IS NOT NULL
	`)
	if err := row.Scan(
		&stats.TotalJobs, &stats.ApplyCount, &stats.WatchCount, &stats.SkipCount,
		&stats.AvgScore, &stats.TopScore,
	); err != nil {
		return nil, fmt.Errorf("scanning stats: %w", err)
	}

	buckets, err := r.scoreBuckets(ctx)
	if err != nil {
		return nil, err
	}
	stats.ScoreBuckets = buckets

	companies, err := r.topCompanies(ctx)
	if err != nil {
		return nil, err
	}
	stats.TopCompanies = companies

	return stats, nil
}

// scoreBuckets returns a fixed-order histogram of match scores for charting.
func (r *Repository) scoreBuckets(ctx context.Context) ([]ScoreBucket, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			CASE
				WHEN match_score < 2 THEN '0-2'
				WHEN match_score < 4 THEN '2-4'
				WHEN match_score < 6 THEN '4-6'
				WHEN match_score < 7 THEN '6-7'
				WHEN match_score < 8 THEN '7-8'
				WHEN match_score < 9 THEN '8-9'
				ELSE '9-10'
			END AS bucket,
			COUNT(*)
		FROM jobs
		WHERE match_score IS NOT NULL
		GROUP BY bucket
	`)
	if err != nil {
		return nil, fmt.Errorf("scoring buckets: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var bucket string
		var count int
		if err := rows.Scan(&bucket, &count); err != nil {
			return nil, err
		}
		counts[bucket] = count
	}

	// Fixed order so the chart bars always appear left-to-right correctly.
	order := []string{"0-2", "2-4", "4-6", "6-7", "7-8", "8-9", "9-10"}
	result := make([]ScoreBucket, len(order))
	for i, r := range order {
		result[i] = ScoreBucket{Range: r, Count: counts[r]}
	}
	return result, rows.Err()
}

func (r *Repository) topCompanies(ctx context.Context) ([]CompanyDTO, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT company, COUNT(*), ROUND(AVG(match_score)::numeric, 2)::float8
		FROM jobs
		WHERE match_score IS NOT NULL
		  AND ai_action IN ('apply', 'watch')
		GROUP BY company
		ORDER BY COUNT(*) DESC, AVG(match_score) DESC
		LIMIT 8
	`)
	if err != nil {
		return nil, fmt.Errorf("top companies: %w", err)
	}
	defer rows.Close()

	var companies []CompanyDTO
	for rows.Next() {
		var c CompanyDTO
		if err := rows.Scan(&c.Company, &c.JobCount, &c.AvgScore); err != nil {
			return nil, err
		}
		companies = append(companies, c)
	}
	return companies, rows.Err()
}

// DeleteJob deletes a single job by id. Kayıt yoksa ErrNotFound döner.
func (r *Repository) DeleteJob(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM jobs WHERE id = $1::uuid`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ClearAllJobs truncates the jobs table.
func (r *Repository) ClearAllJobs(ctx context.Context) error {
	_, err := r.pool.Exec(ctx, `TRUNCATE TABLE jobs`)
	return err
}

// GetTailoredAssets retrieves previously generated cover letter and tailored CV assets from DB if cached.
func (r *Repository) GetTailoredAssets(ctx context.Context, id string) (*TailorResponseDTO, error) {
	query := `SELECT id, cover_letter, tailored_cv FROM jobs WHERE id = $1::uuid`
	var jobID string
	var coverLetter *string
	var rawCV []byte

	err := r.pool.QueryRow(ctx, query, id).Scan(&jobID, &coverLetter, &rawCV)
	if err != nil {
		return nil, fmt.Errorf("fetching tailored assets: %w", err)
	}

	if coverLetter == nil || len(*coverLetter) == 0 || len(rawCV) == 0 {
		return nil, nil // Not cached yet
	}

	var res TailorResponseDTO
	if err := json.Unmarshal(rawCV, &res); err != nil {
		return nil, nil // Corrupted or invalid JSON, re-generate
	}
	res.JobID = jobID
	res.CoverLetter = *coverLetter
	res.Cached = true
	return &res, nil
}

// SaveTailoredAssets stores the generated cover letter and CV assets in the jobs table.
func (r *Repository) SaveTailoredAssets(ctx context.Context, id string, coverLetter string, assets any) error {
	rawJSON, err := json.Marshal(assets)
	if err != nil {
		return fmt.Errorf("marshaling tailored assets: %w", err)
	}

	query := `UPDATE jobs SET cover_letter = $1, tailored_cv = $2::jsonb WHERE id = $3::uuid`
	_, err = r.pool.Exec(ctx, query, coverLetter, rawJSON, id)
	if err != nil {
		return fmt.Errorf("saving tailored assets: %w", err)
	}
	return nil
}

// GetInterviewPrep retrieves previously generated interview prep kit from DB if cached.
func (r *Repository) GetInterviewPrep(ctx context.Context, id string) (*InterviewPrepResponseDTO, error) {
	query := `SELECT id, interview_prep FROM jobs WHERE id = $1::uuid`
	var jobID string
	var rawPrep []byte

	err := r.pool.QueryRow(ctx, query, id).Scan(&jobID, &rawPrep)
	if err != nil {
		return nil, fmt.Errorf("fetching interview prep: %w", err)
	}

	if len(rawPrep) == 0 {
		return nil, nil // Not cached yet
	}

	var res InterviewPrepResponseDTO
	if err := json.Unmarshal(rawPrep, &res); err != nil {
		return nil, nil
	}
	res.JobID = jobID
	res.Cached = true
	return &res, nil
}

// SaveInterviewPrep stores the generated interview prep kit in the jobs table.
func (r *Repository) SaveInterviewPrep(ctx context.Context, id string, assets any) error {
	rawJSON, err := json.Marshal(assets)
	if err != nil {
		return fmt.Errorf("marshaling interview prep: %w", err)
	}

	query := `UPDATE jobs SET interview_prep = $1::jsonb WHERE id = $2::uuid`
	_, err = r.pool.Exec(ctx, query, rawJSON, id)
	if err != nil {
		return fmt.Errorf("saving interview prep: %w", err)
	}
	return nil
}
