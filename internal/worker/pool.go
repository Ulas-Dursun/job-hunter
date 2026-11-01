package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/ulasdursun/job-hunter/internal/analyzer"
	"github.com/ulasdursun/job-hunter/internal/config"
	"github.com/ulasdursun/job-hunter/internal/filter"
	"github.com/ulasdursun/job-hunter/internal/metrics"
	"github.com/ulasdursun/job-hunter/internal/notifier"
	"github.com/ulasdursun/job-hunter/internal/store"
)

// orphanedMinIdle is the minimum idle time in PEL before a pending message is reclaimed via XAUTOCLAIM.
const orphanedMinIdle = 5 * time.Minute

// autoClaimInterval controls how often workers check for orphaned PEL messages from dead consumers.
const autoClaimInterval = 2 * time.Minute

// Pool coordinates worker goroutines consuming jobs:filtered stream via JobProcessor.
type Pool struct {
	size      int
	client    *redis.Client
	cfg       *config.RedisConfig
	processor *JobProcessor
}

// SetPreFilter configures pre-filtering before invoking AI models.
func (p *Pool) SetPreFilter(f *filter.CheapFilter) {
	if p.processor != nil {
		p.processor.SetPreFilter(f)
	}
}

// New creates and initializes worker pool and its underlying processor.
func New(
	size int,
	client *redis.Client,
	a *analyzer.GeminiAnalyzer,
	js *store.JobStore,
	m *metrics.Metrics,
	cfg *config.RedisConfig,
	scoring *config.ScoringConfig,
	n *notifier.Telegram,
	notifyEnabled bool,
) *Pool {
	return &Pool{
		size:   size,
		client: client,
		cfg:    cfg,
		processor: NewJobProcessor(
			client, a, js, m, cfg, scoring, n, notifyEnabled,
		),
	}
}

// Run starts worker goroutines and blocks until context cancellation and worker termination.
func (p *Pool) Run(ctx context.Context) {
	slog.Info("worker pool started",
		"size", p.size,
		"stream", p.cfg.StreamFiltered,
	)

	var wg sync.WaitGroup
	for i := range p.size {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			p.runWorker(ctx, id)
		}(i)
	}

	wg.Wait()
	slog.Info("worker pool stopped")
}

func (p *Pool) runWorker(ctx context.Context, id int) {
	slog.Info("worker started", "id", id)
	defer slog.Info("worker stopped", "id", id)

	// Başlangıçta 5 dakikadan uzun süredir PEL'de sahipsiz bekleyen mesajları XAUTOCLAIM ile kurtar.
	p.reclaimOrphanedMessages(ctx, id)
	lastAutoClaim := time.Now()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			if time.Since(lastAutoClaim) >= autoClaimInterval {
				p.reclaimOrphanedMessages(ctx, id)
				lastAutoClaim = time.Now()
			}
			p.processNext(ctx, id)
		}
	}
}

func (p *Pool) consumerNameFor(workerID int) string {
	if workerID > 0 {
		return fmt.Sprintf("%s-%d", p.cfg.ConsumerName, workerID)
	}
	return p.cfg.ConsumerName
}

// reclaimOrphanedMessages uses XAUTOCLAIM to take ownership of PEL entries idle for >= 5 minutes.
func (p *Pool) reclaimOrphanedMessages(ctx context.Context, workerID int) {
	if p.client == nil || p.cfg == nil {
		return
	}
	consumerName := p.consumerNameFor(workerID)
	start := "0-0"
	for {
		if ctx.Err() != nil {
			return
		}
		msgs, nextStart, err := p.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
			Stream:   p.cfg.StreamFiltered,
			Group:    p.cfg.ConsumerGroup,
			Consumer: consumerName,
			MinIdle:  orphanedMinIdle,
			Start:    start,
			Count:    10,
		}).Result()
		if err != nil {
			if !errors.Is(err, context.Canceled) && !errors.Is(err, redis.Nil) {
				slog.Debug("worker xautoclaim skipped/error", "worker_id", workerID, "error", err)
			}
			return
		}
		if len(msgs) == 0 {
			return
		}
		slog.Info("worker reclaimed orphaned PEL messages via XAUTOCLAIM",
			"worker_id", workerID, "consumer", consumerName, "count", len(msgs))
		for _, msg := range msgs {
			p.processor.Process(ctx, msg, workerID)
		}
		if nextStart == "" || nextStart == "0-0" || nextStart == start {
			return
		}
		start = nextStart
	}
}

func (p *Pool) processNext(ctx context.Context, workerID int) {
	consumerName := p.consumerNameFor(workerID)

	// 1. Önce bekleyen (pending) mesajları kontrol et (ID: "0")
	streams, err := p.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    p.cfg.ConsumerGroup,
		Consumer: consumerName,
		Streams:  []string{p.cfg.StreamFiltered, "0"},
		Count:    1,
	}).Result()

	// Eğer pending mesaj yoksa veya boşsa, yeni mesajları bekle (ID: ">", Block: 2s)
	if err != nil || len(streams) == 0 || len(streams[0].Messages) == 0 {
		streams, err = p.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    p.cfg.ConsumerGroup,
			Consumer: consumerName,
			Streams:  []string{p.cfg.StreamFiltered, ">"},
			Count:    1,
			Block:    2 * time.Second,
		}).Result()
	}

	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, redis.Nil) {
			return
		}
		slog.Error("worker xreadgroup error", "worker_id", workerID, "error", err)
		return
	}

	for _, stream := range streams {
		for _, msg := range stream.Messages {
			p.processor.Process(ctx, msg, workerID)
		}
	}
}
