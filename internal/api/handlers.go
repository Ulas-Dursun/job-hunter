package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"sync"

	"github.com/ulasdursun/job-hunter/internal/analyzer"
)

// Handlers holds the repository dependency and exposes HTTP handler methods.
type Handlers struct {
	repo   *Repository
	tailor *analyzer.TailorService

	mu       sync.Mutex
	inflight map[string]bool // devam eden LLM üretimleri (çift tıklama / kota koruması)
}

func NewHandlers(repo *Repository, tailor *analyzer.TailorService) *Handlers {
	return &Handlers{repo: repo, tailor: tailor, inflight: make(map[string]bool)}
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
		Limit:      clampInt(parseIntDefault(q.Get("limit"), 50), 1, maxListLimit),
		Offset:     clampInt(parseIntDefault(q.Get("offset"), 0), 0, 1_000_000),
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
	id, ok := jobID(w, r)
	if !ok {
		return
	}

	var body StatusUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, errBadRequest("invalid request body"))
		return
	}

	if err := h.repo.UpdateStatus(r.Context(), id, body.Status); err != nil {
		writeRepoError(w, err)
		return
	}

	job, err := h.repo.GetJob(r.Context(), id)
	if err != nil {
		writeRepoError(w, err)
		return
	}

	slog.Info("job status updated", "id", id, "status", body.Status)
	writeJSON(w, http.StatusOK, job)
}

// GetJob handles GET /api/jobs/{id} (tam açıklama dahil tek ilan).
func (h *Handlers) GetJob(w http.ResponseWriter, r *http.Request) {
	id, ok := jobID(w, r)
	if !ok {
		return
	}
	job, err := h.repo.GetJob(r.Context(), id)
	if err != nil {
		writeRepoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

// DeleteJob handles DELETE /api/jobs/{id}
func (h *Handlers) DeleteJob(w http.ResponseWriter, r *http.Request) {
	id, ok := jobID(w, r)
	if !ok {
		return
	}

	if err := h.repo.DeleteJob(r.Context(), id); err != nil {
		writeRepoError(w, err)
		return
	}

	slog.Info("job deleted", "id", id)
	w.WriteHeader(http.StatusNoContent)
}

// ClearJobs handles DELETE /api/jobs
// Yıkıcı işlem: yalnızca özel X-Confirm başlığı kabul edilir (query param ile tetiklenemez;
// özel header cross-origin isteklerde CORS preflight zorunlu kılar).
func (h *Handlers) ClearJobs(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Confirm") != "true" {
		writeError(w, http.StatusBadRequest, errBadRequest("confirmation required: send header X-Confirm: true to delete all jobs"))
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
	id, ok := jobID(w, r)
	if !ok {
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
		writeRepoError(w, err)
		return
	}

	// 3. Generate tailored assets via Gemini
	if h.tailor == nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("tailor service is not configured"))
		return
	}

	// Aynı ilan için eşzamanlı (çift tıklama) LLM çağrısını engelle — kota/maliyet koruması.
	release, acquired := h.acquire("tailor:" + id)
	if !acquired {
		writeError(w, http.StatusConflict, errBadRequest("generation already in progress for this job"))
		return
	}
	defer release()

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
	id, ok := jobID(w, r)
	if !ok {
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
		writeRepoError(w, err)
		return
	}

	// 3. Generate interview prep kit via Gemini
	if h.tailor == nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("tailor service is not configured"))
		return
	}

	release, acquired := h.acquire("prep:" + id)
	if !acquired {
		writeError(w, http.StatusConflict, errBadRequest("generation already in progress for this job"))
		return
	}
	defer release()

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
	msg := err.Error()
	// 5xx hatalarında iç detay (SQL, sağlayıcı yanıtı vb.) istemciye sızdırılmaz; ayrıntı yalnızca log'dadır.
	if status >= 500 {
		msg = "internal server error"
	}
	writeJSON(w, status, ErrorResponse{Error: msg})
}

// writeRepoError repository hatalarını uygun HTTP durum koduna çevirir.
func writeRepoError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, ErrNotFound)
	case errors.Is(err, ErrInvalidStatus):
		writeError(w, http.StatusBadRequest, err)
	default:
		writeError(w, http.StatusInternalServerError, err)
	}
}

const maxListLimit = 1000

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// jobID path parametresini doğrular; geçersiz UUID için DB'ye gitmeden 400 döner.
func jobID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	if !uuidRe.MatchString(id) {
		writeError(w, http.StatusBadRequest, errBadRequest("invalid job id"))
		return "", false
	}
	return id, true
}

// acquire aynı anahtar için eşzamanlı çalışmayı engeller. Başarılıysa release fonksiyonunu döner.
func (h *Handlers) acquire(key string) (release func(), ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.inflight[key] {
		return nil, false
	}
	h.inflight[key] = true
	return func() {
		h.mu.Lock()
		delete(h.inflight, key)
		h.mu.Unlock()
	}, true
}

type simpleError string

func (e simpleError) Error() string  { return string(e) }
func errBadRequest(msg string) error { return simpleError(msg) }
