package analyzer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"

	"github.com/ulasdursun/job-hunter/internal/config"
)

var reFlexFloat = regexp.MustCompile(`[-+]?\d+(?:\.\d+)?`)

// parseFlexFloat, JSON içindeki sayısal (4.2) veya string ("4.2", "2.3/2.5", "4,2") puan değerlerini float64'e çevirir.
func parseFlexFloat(raw json.RawMessage) (float64, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return 0, nil
	}

	var num float64
	if err := json.Unmarshal(raw, &num); err == nil {
		return num, nil
	}

	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0, err
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	if idx := strings.Index(s, "/"); idx != -1 {
		s = strings.TrimSpace(s[:idx])
	}
	s = strings.ReplaceAll(s, ",", ".")
	if m := reFlexFloat.FindString(s); m != "" {
		return strconv.ParseFloat(m, 64)
	}
	return strconv.ParseFloat(s, 64)
}

// ScoreBreakdown, AI eşleşme puanının nesnel alt bileşenlerini tutar.
type ScoreBreakdown struct {
	TechFit      float64 `json:"tech_fit"`
	SeniorityFit float64 `json:"seniority_fit"`
	DomainFit    float64 `json:"domain_fit"`
	LocationFit  float64 `json:"location_fit"`
}

func (b *ScoreBreakdown) UnmarshalJSON(data []byte) error {
	var raw struct {
		TechFit      json.RawMessage `json:"tech_fit"`
		SeniorityFit json.RawMessage `json:"seniority_fit"`
		DomainFit    json.RawMessage `json:"domain_fit"`
		LocationFit  json.RawMessage `json:"location_fit"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var err error
	if b.TechFit, err = parseFlexFloat(raw.TechFit); err != nil {
		return fmt.Errorf("tech_fit: %w", err)
	}
	if b.SeniorityFit, err = parseFlexFloat(raw.SeniorityFit); err != nil {
		return fmt.Errorf("seniority_fit: %w", err)
	}
	if b.DomainFit, err = parseFlexFloat(raw.DomainFit); err != nil {
		return fmt.Errorf("domain_fit: %w", err)
	}
	if b.LocationFit, err = parseFlexFloat(raw.LocationFit); err != nil {
		return fmt.Errorf("location_fit: %w", err)
	}
	return nil
}

// ScoreResult, AI'ın bir ilan için ürettiği yapılandırılmış analiz sonucu.
type ScoreResult struct {
	MatchScore     float64         `json:"match_score"`
	RoleFamily     string          `json:"role_family"`
	SkillOverlap   []string        `json:"skill_overlap"`
	MissingSkills  []string        `json:"missing_skills"`
	RedFlags       []string        `json:"red_flags"`
	ScoreBreakdown *ScoreBreakdown `json:"score_breakdown,omitempty"`
	Action         string          `json:"action"`
	Reasoning      string          `json:"reasoning"`
}

func (r *ScoreResult) UnmarshalJSON(data []byte) error {
	var raw struct {
		MatchScore     json.RawMessage `json:"match_score"`
		RoleFamily     string          `json:"role_family"`
		SkillOverlap   []string        `json:"skill_overlap"`
		MissingSkills  []string        `json:"missing_skills"`
		RedFlags       []string        `json:"red_flags"`
		ScoreBreakdown *ScoreBreakdown `json:"score_breakdown,omitempty"`
		Action         string          `json:"action"`
		Reasoning      string          `json:"reasoning"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	ms, err := parseFlexFloat(raw.MatchScore)
	if err != nil {
		return fmt.Errorf("match_score: %w", err)
	}
	r.MatchScore = ms
	r.RoleFamily = raw.RoleFamily
	r.SkillOverlap = raw.SkillOverlap
	r.MissingSkills = raw.MissingSkills
	r.RedFlags = raw.RedFlags
	r.ScoreBreakdown = raw.ScoreBreakdown
	r.Action = raw.Action
	r.Reasoning = raw.Reasoning
	return nil
}

// GeminiAnalyzer, Google Gemini ve Groq API havuzu üzerinden kesintisiz (failover destekli) iş ilanı analizi yapar.
type GeminiAnalyzer struct {
	client  *LLMClient
	limiter *rate.Limiter
	builder *PromptBuilder
	maxTok  int
}

func NewGeminiAnalyzer(cfg *config.GeminiConfig, candidate *config.CandidateConfig) *GeminiAnalyzer {
	var cfgModel string
	rpm := 30
	maxTok := 1000
	if cfg != nil {
		cfgModel = cfg.Model
		if cfg.RateLimitRPM > 0 {
			rpm = cfg.RateLimitRPM
		}
		if cfg.MaxTokens > 0 {
			maxTok = cfg.MaxTokens
		}
	}

	limiter := SharedRateLimiter(rpm)
	client := NewLLMClient(cfgModel, 35*time.Second, rpm)
	if client.EndpointsCount() == 0 {
		slog.Warn("ai analyzer: ne GEMINI_API_KEY ne de GROQ_API_KEY ortamda bulunamadı!")
	} else {
		slog.Info("ai analyzer failover havuzu aktif",
			"toplam_istasyon", client.EndpointsCount(),
			"zincir", strings.Join(client.EndpointLabels(), " -> "),
			"rpm", rpm,
		)
	}

	return &GeminiAnalyzer{
		client:  client,
		limiter: limiter,
		builder: NewPromptBuilder(candidate),
		maxTok:  maxTok,
	}
}

// SetFeedback, kullanıcının Kanban kararlarından üretilen geri bildirim özetini PromptBuilder'a iletir.
func (g *GeminiAnalyzer) SetFeedback(feedback string) {
	if g != nil && g.builder != nil {
		g.builder.SetFeedback(feedback)
	}
}

// Analyze, bir iş ilanını AI ile analiz eder.
// Aktif anahtar 429 (Rate Limit) veya 503 döndüğünde imleci (cursor) kalıcı olarak sıradaki sağlam anahtara kaydırır.
// Yanıt bozuk JSON ise sıradaki LLM istasyonunda tekrar dener; yine başarısız olursa hatayı geçici (ErrAIBusy) olarak işaretler.
func (g *GeminiAnalyzer) Analyze(
	ctx context.Context,
	title, company, location, description string,
) (*ScoreResult, error) {
	n := g.client.EndpointsCount()
	if n == 0 {
		return nil, fmt.Errorf("no AI API keys configured (set GEMINI_API_KEY or GROQ_API_KEY in .env)")
	}

	prompt := g.builder.Build(title, company, location, description)
	maxAttempts := min(n, 2)
	var lastParseErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		rawText, err := g.client.Generate(ctx, prompt, g.maxTok, 0.10)
		if err != nil {
			return nil, err
		}

		res, err := parseResult(rawText)
		if err == nil {
			return res, nil
		}
		lastParseErr = err
		g.client.cursor.Add(1)
		if attempt+1 < maxAttempts {
			slog.Warn("ai analyzer: bozuk JSON yanıtı alındı, sıradaki istasyonda tekrar deneniyor",
				"attempt", attempt+1,
				"error", err,
			)
		}
	}

	return nil, fmt.Errorf("%w: %v", ErrAIBusy, lastParseErr)
}

func parseResult(text string) (*ScoreResult, error) {
	start, end := -1, -1
	for i, ch := range text {
		if ch == '{' && start == -1 {
			start = i
		}
		if ch == '}' {
			end = i + 1
		}
	}

	if start == -1 || end == -1 {
		return nil, fmt.Errorf("no JSON in response: %s", text[:min(len(text), 200)])
	}

	var result ScoreResult
	if err := json.Unmarshal([]byte(text[start:end]), &result); err != nil {
		return nil, fmt.Errorf("parsing score JSON: %w", err)
	}

	rawMatchOutOfRange := result.MatchScore < 0 || result.MatchScore > 10
	if result.MatchScore < 0 {
		result.MatchScore = 0
	}
	if result.MatchScore > 10 {
		result.MatchScore = 10
	}

	langBarrier := hasLanguageBarrier(result.RedFlags, result.MissingSkills)
	stackMismatch := hasPrimaryStackMismatch(result.RedFlags, result.MissingSkills)

	if b := result.ScoreBreakdown; b != nil {
		b.TechFit = clampFloat(b.TechFit, 0, 5.0)
		b.SeniorityFit = clampFloat(b.SeniorityFit, 0, 2.5)
		b.DomainFit = clampFloat(b.DomainFit, 0, 1.5)
		b.LocationFit = clampFloat(b.LocationFit, 0, 1.0)

		if langBarrier {
			b.LocationFit = 0.0
		}

		techFitCapped := false
		if !langBarrier && stackMismatch && b.TechFit > 2.6 {
			b.TechFit = 2.6
			techFitCapped = true
		}

		// Alt bileşenlerden herhangi biri doluysa toplam ile match_score arasındaki tutarsızlığı kontrol et.
		if !rawMatchOutOfRange && (b.TechFit > 0 || b.SeniorityFit > 0 || b.DomainFit > 0 || b.LocationFit > 0) {
			breakdownSum := b.TechFit + b.SeniorityFit + b.DomainFit + b.LocationFit
			breakdownSum = clampFloat(math.Round(breakdownSum*10)/10, 0.0, 10.0)
			diff := math.Abs(breakdownSum - result.MatchScore)
			if diff > 0.5 && !techFitCapped && !langBarrier {
				slog.Warn("ai score_breakdown ile match_score arasında tutarsızlık tespit edildi, breakdown toplamı esas alınıyor",
					"breakdown_sum", breakdownSum,
					"original_match_score", result.MatchScore,
					"diff", diff,
				)
			}

			if langBarrier || b.LocationFit <= 0.1 {
				if result.MatchScore > 3.5 {
					result.MatchScore = 3.5
				}
				result.Action = "skip"
			} else if techFitCapped {
				result.MatchScore = breakdownSum
				result.Action = deriveActionFromScore(result.MatchScore)
			} else if result.MatchScore < 5.0 || result.Action == "skip" {
				if breakdownSum < result.MatchScore && diff > 0.15 {
					result.MatchScore = breakdownSum
				}
				if result.MatchScore < 5.0 {
					result.Action = "skip"
				}
			} else if diff > 0.15 {
				explicitHardCap := result.MatchScore <= 6.5 && strings.Contains(strings.ToLower(result.Reasoning), "hard cap")
				if !explicitHardCap {
					result.MatchScore = breakdownSum
					result.Action = deriveActionFromScore(result.MatchScore)
				}
			}
		} else if langBarrier {
			if result.MatchScore > 3.5 {
				result.MatchScore = 3.5
			}
			result.Action = "skip"
		}

		// Junior/Graduate apply kapısı: dil bariyeri veya lokasyon engeli yoksa,
		// seniority_fit (0, 1.7) aralığındaki genel/mid-level ilanlar apply olamaz.
		if !langBarrier && b.LocationFit > 0.1 && b.SeniorityFit > 0 && b.SeniorityFit < 1.7 {
			if result.MatchScore >= 7.0 {
				result.MatchScore = 6.8
			}
			if result.Action == "apply" {
				result.Action = "watch"
			}
		}
	} else if langBarrier {
		if result.MatchScore > 3.5 {
			result.MatchScore = 3.5
		}
		result.Action = "skip"
	}

	return &result, nil
}

func deriveActionFromScore(score float64) string {
	if score >= 7.0 {
		return "apply"
	}
	if score >= 5.0 {
		return "watch"
	}
	return "skip"
}

func containsAny(s string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}

// hasLanguageBarrier, zorunlu Almanca/Felemenkçe/Fransızca vb. dil bariyeri olup olmadığını kontrol eder.
// "preferred", "asset", "plus", "nice to have" içeren istisnaları elemez.
func hasLanguageBarrier(redFlags, missingSkills []string) bool {
	softExceptions := []string{"preferred", "asset", "plus", "nice to have"}
	reqWords := []string{"mandatory", "required", "requires", "fluency", "fluent", "native", "c1", "c2", "b2", "must speak", "must be fluent"}
	langWords := []string{"german", "dutch", "french", "spanish", "italian", "polish", "danish"}
	directMissingPhrases := []string{
		"native-level german",
		"german language fluency",
		"dutch language skills",
		"french language",
		"dutch fluency",
		"german fluency",
	}

	for _, item := range redFlags {
		lower := strings.ToLower(item)
		if containsAny(lower, softExceptions) {
			continue
		}
		if (containsAny(lower, reqWords) && containsAny(lower, langWords)) || containsAny(lower, directMissingPhrases) {
			return true
		}
	}

	for _, item := range missingSkills {
		lower := strings.ToLower(item)
		if containsAny(lower, softExceptions) {
			continue
		}
		if (containsAny(lower, reqWords) && containsAny(lower, langWords)) || containsAny(lower, directMissingPhrases) {
			return true
		}
	}

	return false
}

// hasPrimaryStackMismatch, ilanın ana stack'inin C#/.NET olup olmadığını kontrol eder.
func hasPrimaryStackMismatch(redFlags, missingSkills []string) bool {
	for _, item := range redFlags {
		lower := strings.ToLower(item)
		if strings.Contains(lower, "requires c#") ||
			strings.Contains(lower, "c#/.net stack") ||
			strings.Contains(lower, ".net stack") {
			return true
		}
	}

	hasCSharp := false
	hasDotNet := false
	for _, item := range missingSkills {
		lower := strings.ToLower(item)
		if strings.Contains(lower, "c#/.net") {
			return true
		}
		if strings.Contains(lower, "c#") {
			hasCSharp = true
		}
		if strings.Contains(lower, ".net") {
			hasDotNet = true
		}
	}
	return hasCSharp && hasDotNet
}

func parseScoreResponse(text string) (*ScoreResult, error) {
	return parseResult(text)
}

func clampFloat(v, minVal, maxVal float64) float64 {
	if v < minVal {
		return minVal
	}
	if v > maxVal {
		return maxVal
	}
	return v
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
