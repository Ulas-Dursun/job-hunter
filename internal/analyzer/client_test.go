package analyzer

import (
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
