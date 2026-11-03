package analyzer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"golang.org/x/time/rate"

	"github.com/ulasdursun/job-hunter/internal/config"
)

// ScoreBreakdown, AI eşleşme puanının nesnel alt bileşenlerini tutar.
type ScoreBreakdown struct {
	TechFit      float64 `json:"tech_fit,omitempty"`
	SeniorityFit float64 `json:"seniority_fit,omitempty"`
	DomainFit    float64 `json:"domain_fit,omitempty"`
	LocationFit  float64 `json:"location_fit,omitempty"`
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
func (g *GeminiAnalyzer) Analyze(
	ctx context.Context,
	title, company, location, description string,
) (*ScoreResult, error) {
	if g.client.EndpointsCount() == 0 {
		return nil, fmt.Errorf("no AI API keys configured (set GEMINI_API_KEY or GROQ_API_KEY in .env)")
	}

	prompt := g.builder.Build(title, company, location, description)
	rawText, err := g.client.Generate(ctx, prompt, g.maxTok, 0.10)
	if err != nil {
		return nil, err
	}

	return parseResult(rawText)
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

	if result.MatchScore < 0 {
		result.MatchScore = 0
	}
	if result.MatchScore > 10 {
		result.MatchScore = 10
	}

	return &result, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
