package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/ulasdursun/job-hunter/internal/analyzer"
)

// Handlers holds the repository dependency and exposes HTTP handler methods.
type Handlers struct {
	repo   *Repository
	tailor *analyzer.TailorService
}

func NewHandlers(repo *Repository, tailor *analyzer.TailorService) *Handlers {
	return &Handlers{repo: repo, tailor: tailor}
}

// ListJobs handles GET /api/jobs?status=&action=&role_family=&min_score=&search=&sort=&limit=&offset=
func (h *Handlers) ListJobs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	filter := JobFilter{
		Status:     nullableParam(q.Get("status")),
		Action:     nullableParam(q.Get("action")),
		RoleFamily: nullableParam(q.Get("role_family")),
		Search:     nullableParam(q.Get("search")),
		SortByDate: q.Get("sort") == "date",
		Limit:      parseIntDefault(q.Get("limit"), 50),
		Offset:     parseIntDefault(q.Get("offset"), 0),
	}

	if minScoreStr := q.Get("min_score"); minScoreStr != "" {
		if v, err := strconv.ParseFloat(minScoreStr, 64); err == nil {
			filter.MinScore = &v
		}
	}

	jobs, total, err := h.repo.ListJobs(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, JobListResponse{
		Jobs:  jobs,
		Total: total,
	})
}

// GetStats handles GET /api/stats
func (h *Handlers) GetStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.repo.GetStats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// UpdateJobStatus handles PATCH /api/jobs/{id}/status
func (h *Handlers) UpdateJobStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, errBadRequest("missing job id"))
		return
	}

	var body StatusUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, errBadRequest("invalid request body"))
		return
	}

	if err := h.repo.UpdateStatus(r.Context(), id, body.Status); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	job, err := h.repo.GetJob(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	slog.Info("job status updated", "id", id, "status", body.Status)
	writeJSON(w, http.StatusOK, job)
}

// DeleteJob handles DELETE /api/jobs/{id}
func (h *Handlers) DeleteJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, errBadRequest("missing job id"))
		return
	}

	if err := h.repo.DeleteJob(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	slog.Info("job deleted", "id", id)
	w.WriteHeader(http.StatusNoContent)
}

// ClearJobs handles DELETE /api/jobs
func (h *Handlers) ClearJobs(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("confirm") != "true" && r.Header.Get("X-Confirm") != "true" {
		writeError(w, http.StatusBadRequest, errBadRequest("confirmation required: pass ?confirm=true or X-Confirm: true to delete all jobs"))
		return
	}

	if err := h.repo.ClearAllJobs(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	slog.Info("all jobs cleared from db")
	w.WriteHeader(http.StatusNoContent)
}

// TailorJob handles POST /api/jobs/{id}/tailor
func (h *Handlers) TailorJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, errBadRequest("missing job id"))
		return
	}

	forceRefresh := r.URL.Query().Get("refresh") == "true"

	// 1. Check cache first if not explicitly force-refreshed
	if !forceRefresh {
		cached, err := h.repo.GetTailoredAssets(r.Context(), id)
		if err == nil && cached != nil {
			slog.Info("returning cached tailored assets", "id", id)
			writeJSON(w, http.StatusOK, cached)
			return
		}
	}

	// 2. Fetch Job details from DB
	job, err := h.repo.GetJob(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("job not found: %w", err))
		return
	}

	// 3. Generate tailored assets via Gemini
	if h.tailor == nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("tailor service is not configured"))
		return
	}

	assets, err := h.tailor.GenerateTailoredAssets(
		r.Context(),
		job.Title,
		job.Company,
		job.Location,
		job.Description,
	)
	if err != nil {
		slog.Error("failed to generate tailored assets", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, fmt.Errorf("generating tailored assets: %w", err))
		return
	}

	// 4. Save to DB cache
	if err := h.repo.SaveTailoredAssets(r.Context(), id, assets.CoverLetter, assets); err != nil {
		slog.Warn("failed to cache tailored assets in db", "id", id, "error", err)
	}

	res := TailorResponseDTO{
		JobID:                     id,
		CoverLetter:               assets.CoverLetter,
		TailoredSummary:           assets.TailoredSummary,
		TailoredExperienceBullets: assets.TailoredExperienceBullets,
		TailoredProjectBullets:    assets.TailoredProjectBullets,
		SkillsHighlight:           assets.SkillsHighlight,
		ATSKeywords:               assets.ATSKeywords,
		Cached:                    false,
	}

	slog.Info("tailored assets generated successfully", "id", id, "company", job.Company)
	writeJSON(w, http.StatusOK, res)
}

// InterviewPrepJob handles POST /api/jobs/{id}/interview-prep
func (h *Handlers) InterviewPrepJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, errBadRequest("missing job id"))
		return
	}

	forceRefresh := r.URL.Query().Get("refresh") == "true"

	// 1. Check cache first if not explicitly force-refreshed
	if !forceRefresh {
		cached, err := h.repo.GetInterviewPrep(r.Context(), id)
		if err == nil && cached != nil {
			slog.Info("returning cached interview prep", "id", id)
			writeJSON(w, http.StatusOK, cached)
			return
		}
	}

	// 2. Fetch Job details from DB
	job, err := h.repo.GetJob(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("job not found: %w", err))
		return
	}

	// 3. Generate interview prep kit via Gemini
	if h.tailor == nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("tailor service is not configured"))
		return
	}

	prep, err := h.tailor.GenerateInterviewPrep(
		r.Context(),
		job.Title,
		job.Company,
		job.Location,
		job.Description,
	)
	if err != nil {
		slog.Error("failed to generate interview prep", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, fmt.Errorf("generating interview prep: %w", err))
		return
	}

	// 4. Save to DB cache
	if err := h.repo.SaveInterviewPrep(r.Context(), id, prep); err != nil {
		slog.Warn("failed to cache interview prep in db", "id", id, "error", err)
	}

	res := InterviewPrepResponseDTO{
		JobID:               id,
		RoleFocusSummary:    prep.RoleFocusSummary,
		TechnicalQuestions:  prep.TechnicalQuestions,
		BehavioralQuestions: prep.BehavioralQuestions,
		QuestionsToAsk:      prep.QuestionsToAsk,
		Cached:              false,
	}

	slog.Info("interview prep generated successfully", "id", id, "company", job.Company)
	writeJSON(w, http.StatusOK, res)
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func nullableParam(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func parseIntDefault(v string, def int) int {
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("failed to encode JSON response", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, err error) {
	slog.Error("api error", "status", status, "error", err)
	writeJSON(w, status, ErrorResponse{Error: err.Error()})
}

type simpleError string

func (e simpleError) Error() string  { return string(e) }
func errBadRequest(msg string) error { return simpleError(msg) }
