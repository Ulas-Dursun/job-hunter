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

// GeminiAnalyzer, Groq API üzerinden iş ilanı analizi yapar.
// Groq: ücretsiz tier, 14400 RPD, llama-3.1-70b model.
type GeminiAnalyzer struct {
	apiKey  string
	model   string
	client  *http.Client
	limiter *rate.Limiter
	builder *PromptBuilder
	maxTok  int
}

const groqAPIURL = "https://api.groq.com/openai/v1/chat/completions"

func NewGeminiAnalyzer(cfg *config.GeminiConfig, candidate *config.CandidateConfig) *GeminiAnalyzer {
	// 20 RPM — Groq free tier çok daha cömert
	interval := time.Duration(60000/cfg.RateLimitRPM) * time.Millisecond
	limiter := rate.NewLimiter(rate.Every(interval), 1)

	return &GeminiAnalyzer{
		apiKey:  os.Getenv("GROQ_API_KEY"),
		model:   cfg.Model,
		client:  &http.Client{Timeout: 30 * time.Second},
		limiter: limiter,
		builder: NewPromptBuilder(candidate),
		maxTok:  cfg.MaxTokens,
	}
}

// Analyze, bir iş ilanını AI ile analiz eder.
// Analyze, bir iş ilanını AI ile analiz eder. 429 (Rate Limit) durumunda otomatik tekrar dener.
func (g *GeminiAnalyzer) Analyze(
	ctx context.Context,
	title, company, location, description string,
) (*ScoreResult, error) {
	maxRetries := 3

	for i := 0; i < maxRetries; i++ {
		if err := g.limiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("rate limiter: %w", err)
		}

		prompt := g.builder.Build(title, company, location, description)
		res, err := g.call(ctx, prompt)

		if err != nil {
			// Eğer hata 429 (Rate Limit) ise biraz bekleyip tekrar dene
			if strings.Contains(err.Error(), "429") {
				backoffDuration := time.Duration((i+1)*10) * time.Second // 10s, 20s, 30s bekleme
				slog.Warn("groq rate limited (429), backing off...",
					"attempt", i+1,
					"wait", backoffDuration.String(),
					"company", company,
				)

				select {
				case <-time.After(backoffDuration):
					continue // Bekleme süresi bitti, döngünün başına dönüp tekrar dene
				case <-ctx.Done():
					return nil, ctx.Err() // Uygulama kapanıyorsa iptal et
				}
			}
			return nil, err // 429 dışı bir hataysa doğrudan fırlat
		}

		return res, nil // Başarılıysa sonucu dön
	}

	return nil, fmt.Errorf("groq rate limited heavily, gave up after %d retries", maxRetries)
}

func (g *GeminiAnalyzer) call(ctx context.Context, prompt string) (*ScoreResult, error) {
	// Groq, OpenAI-compatible API kullanır
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
		// 🟢 BU ALANI EKLE: Groq'un kesinlikle JSON dönmesini zorunlu kılar
		"response_format": map[string]any{
			"type": "json_object",
		},
	}

	bodyBytes, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, groqAPIURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+g.apiKey)

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("groq http call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 429 {
		return nil, fmt.Errorf("groq rate limited (429)")
	}
	if resp.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("groq status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// OpenAI-compatible response format
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
	slog.Debug("groq raw response", "text", rawText[:min(len(rawText), 300)])

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
