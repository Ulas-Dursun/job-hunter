package analyzer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/time/rate"

	"github.com/ulasdursun/job-hunter/internal/config"
)

// ScoreResult, AI'ın bir ilan için ürettiği yapılandırılmış analiz sonucu.
type ScoreResult struct {
	MatchScore    float64  `json:"match_score"`
	RoleFamily    string   `json:"role_family"`
	SkillOverlap  []string `json:"skill_overlap"`
	MissingSkills []string `json:"missing_skills"`
	RedFlags      []string `json:"red_flags"`
	Action        string   `json:"action"`
	Reasoning     string   `json:"reasoning"`
}

type Provider string

const (
	ProviderGemini Provider = "gemini"
	ProviderGroq   Provider = "groq"
)

// GeminiAnalyzer, Google Gemini veya Groq API üzerinden iş ilanı analizi yapar.
type GeminiAnalyzer struct {
	provider Provider
	apiKey   string
	model    string
	client   *http.Client
	limiter  *rate.Limiter
	builder  *PromptBuilder
	maxTok   int
}

const (
	groqAPIURL   = "https://api.groq.com/openai/v1/chat/completions"
	geminiAPIURL = "https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s"
)

func NewGeminiAnalyzer(cfg *config.GeminiConfig, candidate *config.CandidateConfig) *GeminiAnalyzer {
	geminiKey := strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
	groqKey := strings.TrimSpace(os.Getenv("GROQ_API_KEY"))

	provider := ProviderGroq
	apiKey := groqKey
	model := cfg.Model
	rpm := cfg.RateLimitRPM
	if rpm <= 0 {
		rpm = 4
	}

	if geminiKey != "" {
		provider = ProviderGemini
		apiKey = geminiKey
		model = "gemini-flash-lite-latest"
		if cfg.Model != "" && strings.HasPrefix(cfg.Model, "gemini") {
			model = cfg.Model
		}
		rpm = 20 // Gemini free tier: 15-30 RPM, 1.000.000 TPM, 1.500 RPD
		slog.Info("ai analyzer: Google Gemini Flash aktif", "provider", "gemini", "model", model, "rpm", rpm)
	} else if groqKey != "" {
		provider = ProviderGroq
		apiKey = groqKey
		if model == "" || strings.HasPrefix(model, "gemini") {
			model = "qwen/qwen3.8-27b"
		}
		slog.Info("ai analyzer: Groq aktif (GEMINI_API_KEY bulunamadı)", "provider", "groq", "model", model, "rpm", rpm)
	} else {
		slog.Warn("ai analyzer: ne GEMINI_API_KEY ne de GROQ_API_KEY ortamda bulunamadı!")
	}

	interval := time.Duration(60000/rpm) * time.Millisecond
	limiter := rate.NewLimiter(rate.Every(interval), 1)

	return &GeminiAnalyzer{
		provider: provider,
		apiKey:   apiKey,
		model:    model,
		client:   &http.Client{Timeout: 35 * time.Second},
		limiter:  limiter,
		builder:  NewPromptBuilder(candidate),
		maxTok:   cfg.MaxTokens,
	}
}

// Analyze, bir iş ilanını AI ile analiz eder. 429 (Rate Limit) veya 503 durumunda otomatik tekrar dener.
func (g *GeminiAnalyzer) Analyze(
	ctx context.Context,
	title, company, location, description string,
) (*ScoreResult, error) {
	maxRetries := 5

	for i := 0; i < maxRetries; i++ {
		if err := g.limiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("rate limiter: %w", err)
		}

		prompt := g.builder.Build(title, company, location, description)
		var res *ScoreResult
		var err error

		if g.provider == ProviderGemini {
			res, err = g.callGemini(ctx, prompt)
		} else {
			res, err = g.callGroq(ctx, prompt)
		}

		if err != nil {
			if strings.Contains(err.Error(), "429") || strings.Contains(err.Error(), "503") {
				backoffDuration := time.Duration((i+1)*5) * time.Second
				slog.Warn("ai rate limited or busy (429/503), backing off...",
					"provider", g.provider,
					"attempt", i+1,
					"wait", backoffDuration.String(),
					"company", company,
					"error", err.Error(),
				)

				select {
				case <-time.After(backoffDuration):
					continue
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			return nil, err
		}

		return res, nil
	}

	return nil, fmt.Errorf("%s rate limited heavily, gave up after %d retries", g.provider, maxRetries)
}

func (g *GeminiAnalyzer) callGemini(ctx context.Context, prompt string) (*ScoreResult, error) {
	reqURL := fmt.Sprintf(geminiAPIURL, g.model, g.apiKey)

	reqPayload := map[string]any{
		"contents": []map[string]any{
			{
				"parts": []map[string]any{
					{"text": prompt},
				},
			},
		},
		"generationConfig": map[string]any{
			"temperature":      0.1,
			"maxOutputTokens":  g.maxTok,
			"responseMimeType": "application/json",
		},
	}

	bodyBytes, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, fmt.Errorf("marshaling gemini payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("creating gemini request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini http call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 429 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gemini rate limited (429): %s", string(b))
	}
	if resp.StatusCode == 503 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gemini server busy (503): %s", string(b))
	}
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gemini status %d: %s", resp.StatusCode, string(b))
	}

	var geminiResp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		return nil, fmt.Errorf("decoding gemini response: %w", err)
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("empty gemini response candidates")
	}

	rawText := geminiResp.Candidates[0].Content.Parts[0].Text
	return parseResult(rawText)
}

func (g *GeminiAnalyzer) callGroq(ctx context.Context, prompt string) (*ScoreResult, error) {
	reqBody := map[string]any{
		"model": g.model,
		"messages": []map[string]any{
			{
				"role":    "user",
				"content": prompt,
			},
		},
		"max_tokens":  g.maxTok,
		"temperature": 0.1,
		"response_format": map[string]any{
			"type": "json_object",
		},
	}

	bodyBytes, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, groqAPIURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("creating groq request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+g.apiKey)

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("groq http call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 429 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("groq rate limited (429): %s", string(b))
	}
	if resp.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("groq status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decoding groq response: %w", err)
	}

	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("empty groq response")
	}

	rawText := apiResp.Choices[0].Message.Content
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
