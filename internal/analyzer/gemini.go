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
	"sync/atomic"
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
	ProviderGemini     Provider = "gemini"
	ProviderGroq       Provider = "groq"
	ProviderOpenAIComp Provider = "openai_compat"
)

// providerEndpoint represents a single API key + model + endpoint URL in the failover chain.
type providerEndpoint struct {
	provider Provider
	apiKey   string
	model    string
	baseURL  string
	label    string
}

// collectEnvKeys reads primary and numbered/comma-separated env vars (e.g. GEMINI_API_KEY, GEMINI_API_KEY_2 .. _5).
func collectEnvKeys(baseEnv string) []string {
	var raw []string
	seen := make(map[string]bool)
	for _, envName := range []string{baseEnv, baseEnv + "_2", baseEnv + "_3", baseEnv + "_4", baseEnv + "_5"} {
		val := strings.TrimSpace(os.Getenv(envName))
		if val == "" {
			continue
		}
		for _, part := range strings.Split(val, ",") {
			k := strings.TrimSpace(part)
			if k != "" && !seen[k] {
				seen[k] = true
				raw = append(raw, k)
			}
		}
	}
	return raw
}

// buildFailoverChain constructs an ordered slice of Gemini, Groq (gpt-oss-120b + qwen3.8-27b),
// and optional Mistral/Cerebras/OpenRouter endpoints from environment variables.
func buildFailoverChain(cfgModel string) []providerEndpoint {
	geminiKeys := collectEnvKeys("GEMINI_API_KEY")
	groqKeys := collectEnvKeys("GROQ_API_KEY")
	mistralKeys := collectEnvKeys("MISTRAL_API_KEY")
	cerebrasKeys := collectEnvKeys("CEREBRAS_API_KEY")
	openrouterKeys := collectEnvKeys("OPENROUTER_API_KEY")

	geminiModel := "gemini-flash-lite-latest"
	if cfgModel != "" && strings.HasPrefix(cfgModel, "gemini") {
		geminiModel = cfgModel
	}

	var chain []providerEndpoint
	for idx, k := range geminiKeys {
		chain = append(chain, providerEndpoint{
			provider: ProviderGemini,
			apiKey:   k,
			model:    geminiModel,
			baseURL:  geminiAPIURL,
			label:    fmt.Sprintf("gemini#%d", idx+1),
		})
	}

	// Groq üzerinde her iki aktif modelin (openai/gpt-oss-120b ve qwen/qwen3.8-27b) kotaları ayrıdır.
	for idx, k := range groqKeys {
		chain = append(chain,
			providerEndpoint{
				provider: ProviderGroq,
				apiKey:   k,
				model:    "openai/gpt-oss-120b",
				baseURL:  groqAPIURL,
				label:    fmt.Sprintf("groq#%d(gpt-oss-120b)", idx+1),
			},
			providerEndpoint{
				provider: ProviderGroq,
				apiKey:   k,
				model:    "qwen/qwen3.8-27b",
				baseURL:  groqAPIURL,
				label:    fmt.Sprintf("groq#%d(qwen3.8-27b)", idx+1),
			},
		)
	}

	for idx, k := range mistralKeys {
		chain = append(chain, providerEndpoint{
			provider: ProviderOpenAIComp,
			apiKey:   k,
			model:    "mistral-small-latest",
			baseURL:  "https://api.mistral.ai/v1/chat/completions",
			label:    fmt.Sprintf("mistral#%d", idx+1),
		})
	}

	for idx, k := range cerebrasKeys {
		chain = append(chain, providerEndpoint{
			provider: ProviderOpenAIComp,
			apiKey:   k,
			model:    "llama3.1-8b",
			baseURL:  "https://api.cerebras.ai/v1/chat/completions",
			label:    fmt.Sprintf("cerebras#%d", idx+1),
		})
	}

	for idx, k := range openrouterKeys {
		chain = append(chain, providerEndpoint{
			provider: ProviderOpenAIComp,
			apiKey:   k,
			model:    "meta-llama/llama-3.3-70b-instruct:free",
			baseURL:  "https://openrouter.ai/api/v1/chat/completions",
			label:    fmt.Sprintf("openrouter#%d", idx+1),
		})
	}

	return chain
}

// GeminiAnalyzer, Google Gemini ve Groq API havuzu üzerinden kesintisiz (failover destekli) iş ilanı analizi yapar.
type GeminiAnalyzer struct {
	endpoints []providerEndpoint
	cursor    atomic.Uint64
	client    *http.Client
	limiter   *rate.Limiter
	builder   *PromptBuilder
	maxTok    int
}

const (
	groqAPIURL   = "https://api.groq.com/openai/v1/chat/completions"
	// API anahtarı URL'de değil, x-goog-api-key header'ında gönderilir (log/hata sızıntısı riski yok).
	geminiAPIURL = "https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent"
)

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

	endpoints := buildFailoverChain(cfgModel)
	if len(endpoints) == 0 {
		slog.Warn("ai analyzer: ne GEMINI_API_KEY ne de GROQ_API_KEY ortamda bulunamadı!")
	} else {
		var labels []string
		for _, ep := range endpoints {
			labels = append(labels, ep.label)
		}
		slog.Info("ai analyzer failover havuzu aktif",
			"toplam_istasyon", len(endpoints),
			"zincir", strings.Join(labels, " -> "),
			"rpm", rpm,
		)
	}

	interval := time.Duration(60000/rpm) * time.Millisecond
	limiter := rate.NewLimiter(rate.Every(interval), 1)

	return &GeminiAnalyzer{
		endpoints: endpoints,
		client:    &http.Client{Timeout: 35 * time.Second},
		limiter:   limiter,
		builder:   NewPromptBuilder(candidate),
		maxTok:    maxTok,
	}
}

// Analyze, bir iş ilanını AI ile analiz eder.
// Aktif anahtar 429 (Rate Limit) veya 503 döndüğünde imleci (cursor) kalıcı olarak sıradaki sağlam anahtara kaydırır.
func (g *GeminiAnalyzer) Analyze(
	ctx context.Context,
	title, company, location, description string,
) (*ScoreResult, error) {
	n := len(g.endpoints)
	if n == 0 {
		return nil, fmt.Errorf("no AI API keys configured (set GEMINI_API_KEY or GROQ_API_KEY in .env)")
	}

	prompt := g.builder.Build(title, company, location, description)
	maxRounds := 2

	for round := 0; round < maxRounds; round++ {
		if err := g.limiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("rate limiter: %w", err)
		}

		startIdx := int(g.cursor.Load() % uint64(n))
		for step := 0; step < n; step++ {
			idx := (startIdx + step) % n
			ep := g.endpoints[idx]

			var res *ScoreResult
			var err error

			if ep.provider == ProviderGemini {
				res, err = g.callGeminiWith(ctx, ep, prompt)
			} else {
				res, err = g.callGroqWith(ctx, ep, prompt)
			}

			if err == nil {
				if step > 0 {
					slog.Info("ai analyzer yedek istasyon ile başarılı oldu", "endpoint", ep.label, "company", company)
				}
				return res, nil
			}

			// Bu istasyon hata verdi; imleci bir sonraki istasyona ilerlet ki sonraki ilanlar doğrudan sağlam anahtara gitsin
			nextIdx := (idx + 1) % n
			g.cursor.Store(uint64(nextIdx))

			if step+1 < n {
				slog.Warn("ai istasyonu meşgul/limitli, sıradaki yedek anahtara geçiliyor...",
					"failed_endpoint", ep.label,
					"next_endpoint", g.endpoints[nextIdx].label,
					"company", company,
				)
				continue
			}

			if strings.Contains(err.Error(), "429") || strings.Contains(err.Error(), "503") {
				backoffDuration := time.Duration((round+1)*4) * time.Second
				slog.Warn("tüm ai istasyonları meşgul (429/503), kısa süre bekleniyor...",
					"round", round+1,
					"wait", backoffDuration.String(),
					"company", company,
				)
				select {
				case <-time.After(backoffDuration):
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			} else {
				return nil, err
			}
		}
	}

	return nil, fmt.Errorf("all %d AI endpoints rate-limited or failed after %d rounds", n, maxRounds)
}

func (g *GeminiAnalyzer) callGeminiWith(ctx context.Context, ep providerEndpoint, prompt string) (*ScoreResult, error) {
	reqURL := fmt.Sprintf(geminiAPIURL, ep.model)

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
	req.Header.Set("x-goog-api-key", ep.apiKey)

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

func (g *GeminiAnalyzer) callGroqWith(ctx context.Context, ep providerEndpoint, prompt string) (*ScoreResult, error) {
	reqBody := map[string]any{
		"model": ep.model,
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

	targetURL := ep.baseURL
	if targetURL == "" {
		targetURL = groqAPIURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("creating groq/openai request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+ep.apiKey)

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
