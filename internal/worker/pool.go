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

	for {
		select {
		case <-ctx.Done():
			return
		default:
			p.processNext(ctx, id)
		}
	}
}

func (p *Pool) processNext(ctx context.Context, workerID int) {
	consumerName := p.cfg.ConsumerName
	if workerID > 0 {
		consumerName = fmt.Sprintf("%s-%d", p.cfg.ConsumerName, workerID)
	}

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
