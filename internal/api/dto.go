package api

import "time"

// JobDTO is the JSON representation of a job posting returned to the dashboard.
type JobDTO struct {
	ID            string    `json:"id"`
	Source        string    `json:"source"`
	Title         string    `json:"title"`
	Company       string    `json:"company"`
	Location      string    `json:"location"`
	URL           string    `json:"url"`
	MatchScore    *float64  `json:"match_score"`
	SkillOverlap  []string  `json:"skill_overlap"`
	MissingSkills []string  `json:"missing_skills"`
	RedFlags      []string  `json:"red_flags"`
	AIAction      *string   `json:"ai_action"`
	AIReasoning   *string   `json:"ai_reasoning"`
	RoleFamily    *string   `json:"role_family"`
	Status        string    `json:"status"`
	ScrapedAt     time.Time `json:"scraped_at"`
}

// JobListResponse wraps a paginated job list with metadata.
type JobListResponse struct {
	Jobs  []JobDTO `json:"jobs"`
	Total int      `json:"total"`
}

// StatsDTO holds aggregated dashboard statistics.
type StatsDTO struct {
	TotalJobs    int           `json:"total_jobs"`
	ApplyCount   int           `json:"apply_count"`
	WatchCount   int           `json:"watch_count"`
	SkipCount    int           `json:"skip_count"`
	AvgScore     float64       `json:"avg_score"`
	TopScore     float64       `json:"top_score"`
	ScoreBuckets []ScoreBucket `json:"score_buckets"`
	TopCompanies []CompanyDTO  `json:"top_companies"`
}

// ScoreBucket represents one bar in the score distribution histogram.
type ScoreBucket struct {
	Range string `json:"range"`
	Count int    `json:"count"`
}

// CompanyDTO holds per-company aggregated statistics.
type CompanyDTO struct {
	Company  string  `json:"company"`
	JobCount int     `json:"job_count"`
	AvgScore float64 `json:"avg_score"`
}

// StatusUpdateRequest is the body for PATCH /api/jobs/{id}/status.
type StatusUpdateRequest struct {
	Status string `json:"status"`
}

// ErrorResponse is the standard JSON error envelope.
type ErrorResponse struct {
	Error string `json:"error"`
}
