package api

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

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
	ai_action, ai_reasoning, role_family, status, scraped_at`

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
	`, baseJobColumns, orderClause)

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
			&j.AIAction, &j.AIReasoning, &j.RoleFamily, &j.Status, &j.ScrapedAt,
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
		&j.AIAction, &j.AIReasoning, &j.RoleFamily, &j.Status, &j.ScrapedAt,
	)
	if err != nil {
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
		return fmt.Errorf("invalid status: %s", status)
	}

	_, err := r.pool.Exec(ctx,
		`UPDATE jobs SET status = $1::text WHERE id = $2::uuid`,
		status, id,
	)
	if err != nil {
		return fmt.Errorf("updating status for job %s: %w", id, err)
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

// DeleteJob deletes a single job by id.
func (r *Repository) DeleteJob(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM jobs WHERE id = $1`, id)
	return err
}

// ClearAllJobs truncates the jobs table.
func (r *Repository) ClearAllJobs(ctx context.Context) error {
	_, err := r.pool.Exec(ctx, `TRUNCATE TABLE jobs`)
	return err
}

