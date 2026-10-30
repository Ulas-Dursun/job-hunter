package analyzer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

type Provider string

const (
	ProviderGemini     Provider = "gemini"
	ProviderGroq       Provider = "groq"
	ProviderOpenAIComp Provider = "openai_compat"
)

const (
	groqAPIURL   = "https://api.groq.com/openai/v1/chat/completions"
	geminiAPIURL = "https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent"
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

	chain := make([]providerEndpoint, 0)
	for idx, k := range geminiKeys {
		chain = append(chain, providerEndpoint{
			provider: ProviderGemini,
			apiKey:   k,
			model:    geminiModel,
			baseURL:  geminiAPIURL,
			label:    fmt.Sprintf("gemini#%d", idx+1),
		})
	}

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

// LLMClient manages failover, retry loops, and HTTP requests across multi-provider AI endpoints.
type LLMClient struct {
	endpoints []providerEndpoint
	cursor    atomic.Uint64
	client    *http.Client
}

// NewLLMClient creates a failover AI client with specified timeout and configured model.
func NewLLMClient(cfgModel string, timeout time.Duration) *LLMClient {
	return &LLMClient{
		endpoints: buildFailoverChain(cfgModel),
		client:    &http.Client{Timeout: timeout},
	}
}

// EndpointsCount returns the number of active endpoints in failover chain.
func (c *LLMClient) EndpointsCount() int {
	return len(c.endpoints)
}

// EndpointLabels returns human-readable labels of the failover chain.
func (c *LLMClient) EndpointLabels() []string {
	labels := make([]string, len(c.endpoints))
	for i, ep := range c.endpoints {
		labels[i] = ep.label
	}
	return labels
}

// Generate executes prompt with failover rotation and exponential backoff retry.
func (c *LLMClient) Generate(ctx context.Context, prompt string, maxTokens int) (string, error) {
	n := len(c.endpoints)
	if n == 0 {
		return "", fmt.Errorf("missing AI API key (set GEMINI_API_KEY or GROQ_API_KEY in .env)")
	}

	maxRounds := 2
	for round := 0; round < maxRounds; round++ {
		startIdx := int(c.cursor.Load() % uint64(n))
		sawRetryable := false

		for step := 0; step < n; step++ {
			idx := (startIdx + step) % n
			ep := c.endpoints[idx]

			var rawText string
			var err error

			if ep.provider == ProviderGemini {
				rawText, err = c.callGemini(ctx, ep, prompt, maxTokens)
			} else {
				rawText, err = c.callGroq(ctx, ep, prompt, maxTokens)
			}

			if err == nil {
				if step > 0 {
					slog.Info("ai failover: yedek istasyon ile başarılı oldu", "endpoint", ep.label)
				}
				return rawText, nil
			}

			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			if isRetryable(err) {
				sawRetryable = true
			}

			nextIdx := (idx + 1) % n
			c.cursor.Store(uint64(nextIdx))

			if step+1 < n {
				slog.Warn("ai failover: istasyon meşgul/limitli, sıradaki yedek anahtara geçiliyor...",
					"failed_endpoint", ep.label,
					"next_endpoint", c.endpoints[nextIdx].label,
				)
				continue
			}

			if sawRetryable {
				backoff := time.Duration((round+1)*3) * time.Second
				slog.Warn("ai failover: tüm istasyonlar meşgul (429/503), bekleniyor...",
					"round", round+1,
					"wait", backoff.String(),
				)
				select {
				case <-time.After(backoff):
				case <-ctx.Done():
					return "", ctx.Err()
				}
			} else {
				return "", err
			}
		}
	}

	return "", fmt.Errorf("all %d AI endpoints busy or rate-limited after %d rounds", n, maxRounds)
}

func (c *LLMClient) callGemini(ctx context.Context, ep providerEndpoint, prompt string, maxTokens int) (string, error) {
	reqURL := fmt.Sprintf(geminiAPIURL, ep.model)

	if maxTokens <= 0 {
		maxTokens = 2000
	}

	reqPayload := map[string]any{
		"contents": []map[string]any{
			{
				"parts": []map[string]any{
					{"text": prompt},
				},
			},
		},
		"generationConfig": map[string]any{
			"temperature":      0.25,
			"maxOutputTokens":  maxTokens,
			"responseMimeType": "application/json",
		},
	}

	bodyBytes, err := json.Marshal(reqPayload)
	if err != nil {
		return "", fmt.Errorf("marshaling gemini payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("creating gemini request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", ep.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("gemini http call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", newStatusError("gemini", resp)
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
		return "", fmt.Errorf("decoding gemini response: %w", err)
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("empty gemini response candidates")
	}

	return geminiResp.Candidates[0].Content.Parts[0].Text, nil
}

func (c *LLMClient) callGroq(ctx context.Context, ep providerEndpoint, prompt string, maxTokens int) (string, error) {
	if maxTokens <= 0 {
		maxTokens = 2000
	}

	reqBody := map[string]any{
		"model": ep.model,
		"messages": []map[string]any{
			{
				"role":    "user",
				"content": prompt,
			},
		},
		"max_tokens":  maxTokens,
		"temperature": 0.25,
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
		return "", fmt.Errorf("creating groq/openai request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+ep.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("groq http call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", newStatusError("groq", resp)
	}

	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return "", fmt.Errorf("decoding groq response: %w", err)
	}

	if len(apiResp.Choices) == 0 {
		return "", fmt.Errorf("empty groq response")
	}

	return apiResp.Choices[0].Message.Content, nil
}
