package api

import (
	"time"

	"github.com/ulasdursun/job-hunter/internal/analyzer"
)

type JobDTO struct {
	ID               string     `json:"id"`
	Source           string     `json:"source"`
	Title            string     `json:"title"`
	Company          string     `json:"company"`
	Location         string     `json:"location"`
	URL              string     `json:"url"`
	Description      string     `json:"description"`
	PostedAt         *time.Time `json:"posted_at"`
	MatchScore       *float64   `json:"match_score"`
	SkillOverlap     []string   `json:"skill_overlap"`
	MissingSkills    []string   `json:"missing_skills"`
	RedFlags         []string   `json:"red_flags"`
	AIAction         *string    `json:"ai_action"`
	AIReasoning      *string    `json:"ai_reasoning"`
	RoleFamily       *string    `json:"role_family"`
	Status           string     `json:"status"`
	HasTailored      bool       `json:"has_tailored"`
	HasInterviewPrep bool       `json:"has_interview_prep"`
	ScrapedAt        time.Time  `json:"scraped_at"`
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

// TailorResponseDTO represents the response for generating ATS CV assets and Cover Letter.
type TailorResponseDTO struct {
	JobID                     string   `json:"job_id"`
	CoverLetter               string   `json:"cover_letter"`
	TailoredSummary           string   `json:"tailored_summary"`
	TailoredExperienceBullets []string `json:"tailored_experience_bullets"`
	TailoredProjectBullets    []string `json:"tailored_project_bullets"`
	SkillsHighlight           []string `json:"skills_highlight"`
	ATSKeywords               []string `json:"ats_keywords"`
	Cached                    bool     `json:"cached"`
}

// InterviewPrepResponseDTO represents the response for generating AI Interview Prep Kit.
type InterviewPrepResponseDTO struct {
	JobID               string                    `json:"job_id"`
	TechnicalQuestions  []analyzer.TechnicalQA    `json:"technical_questions"`
	BehavioralQuestions []analyzer.BehavioralSTAR `json:"behavioral_questions"`
	QuestionsToAsk      []string                  `json:"questions_to_ask"`
	Cached              bool                      `json:"cached"`
}

// ErrorResponse is the standard JSON error envelope.
type ErrorResponse struct {
	Error string `json:"error"`
}
