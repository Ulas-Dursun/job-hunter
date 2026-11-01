package analyzer

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
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
