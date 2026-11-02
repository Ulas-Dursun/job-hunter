package worker

import (
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/ulasdursun/job-hunter/internal/config"
)

func TestDeriveAction(t *testing.T) {
	tests := []struct {
		score    float64
		expected string
	}{
		{9.5, "apply"},
		{7.0, "apply"},
		{6.9, "watch"},
		{5.0, "watch"},
		{4.9, "skip"},
		{2.0, "skip"},
		{0.0, "skip"},
	}

	for _, tt := range tests {
		got := DeriveAction(tt.score)
		if got != tt.expected {
			t.Errorf("DeriveAction(%v) = %q; want %q", tt.score, got, tt.expected)
		}
	}
}

func TestField(t *testing.T) {
	vals := map[string]any{
		"title":   "Software Engineer",
		"company": "Acme",
		"count":   123, // not string
	}

	if field(vals, "title") != "Software Engineer" {
		t.Errorf("expected Software Engineer")
	}
	if field(vals, "company") != "Acme" {
		t.Errorf("expected Acme")
	}
	if field(vals, "non_existent") != "" {
		t.Errorf("expected empty string")
	}
	if field(vals, "count") != "" {
		t.Errorf("expected empty string for non-string value")
	}
}

func TestRetryIdentifier(t *testing.T) {
	msgWithHash := redis.XMessage{
		ID:     "1-0",
		Values: map[string]any{"dedup_hash": "abc123hash", "source": "linkedin", "external_id": "99"},
	}
	if got := retryIdentifier(msgWithHash); got != "abc123hash" {
		t.Errorf("expected dedup_hash 'abc123hash', got %q", got)
	}

	msgWithSrcExt := redis.XMessage{
		ID:     "2-0",
		Values: map[string]any{"source": "indeed", "external_id": "42"},
	}
	if got := retryIdentifier(msgWithSrcExt); got != "indeed:42" {
		t.Errorf("expected 'indeed:42', got %q", got)
	}

	msgFallback := redis.XMessage{
		ID:     "3-0",
		Values: map[string]any{},
	}
	if got := retryIdentifier(msgFallback); got != "3-0" {
		t.Errorf("expected fallback msg ID '3-0', got %q", got)
	}
}

func TestProcessorAttemptsLifecycle(t *testing.T) {
	p := NewJobProcessor(nil, nil, nil, nil, &config.RedisConfig{ConsumerName: "worker"}, nil, nil, false)
	if n := p.bumpAttempts("msg-1"); n != 1 {
		t.Fatalf("expected attempt 1, got %d", n)
	}
	if n := p.bumpAttempts("msg-1"); n != 2 {
		t.Fatalf("expected attempt 2, got %d", n)
	}
	p.clearAttempts("msg-1")
	if n := p.bumpAttempts("msg-1"); n != 1 {
		t.Fatalf("expected attempt reset to 1 after clearAttempts, got %d", n)
	}
}

func TestPoolConsumerNameFor(t *testing.T) {
	pool := New(3, nil, nil, nil, nil, &config.RedisConfig{ConsumerName: "worker"}, nil, nil, false)
	if got := pool.consumerNameFor(0); got != "worker" {
		t.Errorf("expected 'worker' for workerID 0, got %q", got)
	}
	if got := pool.consumerNameFor(2); got != "worker-2" {
		t.Errorf("expected 'worker-2' for workerID 2, got %q", got)
	}
}

func TestPoolInFlightLocking(t *testing.T) {
	pool := New(2, nil, nil, nil, nil, &config.RedisConfig{ConsumerName: "worker"}, nil, nil, false)

	msgID := "test-msg-123"
	// Simüle et: 1 numaralı worker bu mesajı işlemeye başladı
	pool.inFlight.Store(msgID, 1)

	// Başka bir worker (ör. 2 numaralı worker veya XAUTOCLAIM) aynı mesajı almaya çalışırsa skip etmeli
	if _, loaded := pool.inFlight.LoadOrStore(msgID, 2); !loaded {
		t.Fatalf("expected message %s to be recognized as already in-flight", msgID)
	}

	// 1 numaralı worker bitirdiğinde kilit serbest kalmalı
	pool.inFlight.Delete(msgID)

	if _, loaded := pool.inFlight.LoadOrStore(msgID, 2); loaded {
		t.Fatalf("expected message %s to be free for processing after deletion", msgID)
	}
	pool.inFlight.Delete(msgID)
}
