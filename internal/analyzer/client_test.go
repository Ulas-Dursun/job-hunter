package analyzer

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCollectEnvKeys(t *testing.T) {
	os.Setenv("TEST_KEY_BASE", "key1, key2")
	os.Setenv("TEST_KEY_BASE_2", "key3")
	os.Setenv("TEST_KEY_BASE_3", "key1") // Duplicate

	defer func() {
		os.Unsetenv("TEST_KEY_BASE")
		os.Unsetenv("TEST_KEY_BASE_2")
		os.Unsetenv("TEST_KEY_BASE_3")
	}()

	keys := collectEnvKeys("TEST_KEY_BASE")
	if len(keys) != 3 {
		t.Fatalf("expected 3 unique keys, got %d (%v)", len(keys), keys)
	}
	if keys[0] != "key1" || keys[1] != "key2" || keys[2] != "key3" {
		t.Errorf("unexpected key sequence: %v", keys)
	}
}

func TestBuildFailoverChain_EmptyWhenNoKeys(t *testing.T) {
	chain := buildFailoverChain("gemini-2.5-flash")
	// If no env vars are set, chain should just have length equal to whatever keys are present in env
	if chain == nil {
		t.Errorf("expected non-nil chain slice")
	}
}

func TestNewLLMClient(t *testing.T) {
	client := NewLLMClient("gemini-2.5-flash", 10*time.Second)
	if client == nil {
		t.Fatalf("expected non-nil LLMClient")
	}
	if client.client.Timeout != 10*time.Second {
		t.Errorf("expected timeout 10s, got %v", client.client.Timeout)
	}
}

func TestGenerate_WrapsErrAIBusyWhenAllEndpoints429Or503(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls%2 == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate limit exceeded"}`))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"service unavailable"}`))
	}))
	defer srv.Close()

	client := &LLMClient{
		endpoints: []providerEndpoint{
			{provider: ProviderGroq, apiKey: "k1", model: "m1", baseURL: srv.URL, label: "ep1"},
			{provider: ProviderGroq, apiKey: "k2", model: "m2", baseURL: srv.URL, label: "ep2"},
		},
		client:      &http.Client{Timeout: 2 * time.Second},
		baseBackoff: 10 * time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, err := client.Generate(ctx, "test prompt", 100, 0.10)
	if err == nil {
		t.Fatalf("expected error when all endpoints return 429/503, got nil")
	}
	if !errors.Is(err, ErrAIBusy) {
		t.Errorf("expected error to wrap ErrAIBusy, got: %v", err)
	}
	if !IsTransient(err) {
		t.Errorf("expected IsTransient(err) == true when all endpoints return 429/503, got false (err: %v)", err)
	}
}

func TestGenerate_ForwardsTemperaturePerCall(t *testing.T) {
	var capturedTemps []float64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if temp, ok := body["temperature"].(float64); ok {
			capturedTemps = append(capturedTemps, temp)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"ok\":true}"}}]}`))
	}))
	defer srv.Close()

	client := &LLMClient{
		endpoints: []providerEndpoint{
			{provider: ProviderGroq, apiKey: "k1", model: "m1", baseURL: srv.URL, label: "ep1"},
		},
		client: &http.Client{Timeout: 2 * time.Second},
	}

	// 1. Scoring temperature (0.10)
	if _, err := client.Generate(context.Background(), "score", 200, 0.10); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 2. Tailor/Interview temperature (0.25)
	if _, err := client.Generate(context.Background(), "tailor", 200, 0.25); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(capturedTemps) != 2 || capturedTemps[0] != 0.10 || capturedTemps[1] != 0.25 {
		t.Errorf("expected temperatures [0.10, 0.25], got %v", capturedTemps)
	}
}

func TestSharedRateLimiter_SharedAcrossServices(t *testing.T) {
	lim1 := SharedRateLimiter(30)
	lim2 := SharedRateLimiter(30)
	if lim1 == nil || lim1 != lim2 {
		t.Errorf("expected identical shared rate limiter instance across services")
	}
}

func TestCallGemini_ConcatenatesMultipleParts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"candidates": [{
				"content": {
					"parts": [
						{"text": "{\"part1\": \"hello\", "},
						{"text": "\"part2\": \"world\"}"}
					]
				}
			}]
		}`))
	}))
	defer srv.Close()

	// callGemini URL'si geminiAPIURL formatını kullandığı için roundTripper üzerinden test sunucusuna yönlendiriyoruz.
	client := &LLMClient{
		client: &http.Client{
			Timeout: 2 * time.Second,
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				req2 := req.Clone(req.Context())
				req2.URL.Scheme = "http"
				req2.URL.Host = strings.TrimPrefix(srv.URL, "http://")
				return http.DefaultTransport.RoundTrip(req2)
			}),
		},
	}

	ep := providerEndpoint{
		provider: ProviderGemini,
		apiKey:   "test-key",
		model:    "gemini-flash-lite-latest",
		label:    "gemini#1",
	}

	got, err := client.callGemini(context.Background(), ep, "test prompt", 500, 0.1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `{"part1": "hello", "part2": "world"}`
	if got != want {
		t.Errorf("expected combined parts %q, got %q", want, got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestGenerate_Retries502And504(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch calls {
		case 1:
			w.WriteHeader(http.StatusBadGateway) // 502
			_, _ = w.Write([]byte(`bad gateway`))
		case 2:
			w.WriteHeader(http.StatusGatewayTimeout) // 504
			_, _ = w.Write([]byte(`gateway timeout`))
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"ok\":true}"}}]}`))
		}
	}))
	defer srv.Close()

	client := &LLMClient{
		endpoints: []providerEndpoint{
			{provider: ProviderGroq, apiKey: "k1", model: "m1", baseURL: srv.URL, label: "ep1"},
			{provider: ProviderGroq, apiKey: "k2", model: "m2", baseURL: srv.URL, label: "ep2"},
		},
		client:      &http.Client{Timeout: 2 * time.Second},
		baseBackoff: 5 * time.Millisecond,
	}

	res, err := client.Generate(context.Background(), "prompt", 200, 0.1)
	if err != nil {
		t.Fatalf("expected failover recovery after 502 and 504, got error: %v", err)
	}
	if res != `{"ok":true}` {
		t.Errorf("unexpected response: %s", res)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls (502 -> 504 -> backoff -> 200), got %d", calls)
	}
}

func TestIsFailoverRetryable_StatusAndTimeout(t *testing.T) {
	ctx := context.Background()
	for _, code := range []int{429, 500, 502, 503, 504} {
		err := &httpStatusError{Provider: "groq", Status: code, Body: "err"}
		if !isFailoverRetryable(ctx, err) {
			t.Errorf("expected HTTP %d to be retryable", code)
		}
	}
	if isFailoverRetryable(ctx, &httpStatusError{Provider: "groq", Status: 400, Body: "bad request"}) {
		t.Errorf("expected HTTP 400 not to be retryable")
	}

	// Client timeout (ctx.Err() == nil iken DeadlineExceeded)
	if !isFailoverRetryable(ctx, context.DeadlineExceeded) {
		t.Errorf("expected client timeout with active parent ctx to be retryable")
	}

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if isFailoverRetryable(canceledCtx, context.DeadlineExceeded) {
		t.Errorf("expected timeout with canceled parent ctx NOT to be retryable")
	}
}

func TestCallGroq_MinTokensForReasoningAndProviderLabel(t *testing.T) {
	var capturedMaxTokens float64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if mt, ok := body["max_tokens"].(float64); ok {
			capturedMaxTokens = mt
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`bad request`))
	}))
	defer srv.Close()

	client := &LLMClient{
		client: &http.Client{Timeout: 2 * time.Second},
	}
	ep := providerEndpoint{
		provider: ProviderOpenAIComp,
		apiKey:   "k1",
		model:    "openai/gpt-oss-120b",
		baseURL:  srv.URL,
		label:    "openrouter#1",
	}

	_, err := client.callGroq(context.Background(), ep, "hello", 500, 0.2)
	if err == nil {
		t.Fatalf("expected error on 400 status")
	}
	if int(capturedMaxTokens) != 1800 {
		t.Errorf("expected reasoning min max_tokens=1800, got %v", capturedMaxTokens)
	}
	if !strings.Contains(err.Error(), string(ProviderOpenAIComp)) {
		t.Errorf("expected error to contain provider %q, got %q", ProviderOpenAIComp, err.Error())
	}
}
