package worker

import (
	"context"
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

// maxDBRetries, bir mesajın DB'ye yazılamadığında jobs:failed'e taşınmadan önceki deneme sınırıdır.
const maxDBRetries = 5

// maxAITransientRetries, geçici AI kota hatalarında (429/503) jobs:failed'e taşımadan önceki deneme sınırıdır.
const maxAITransientRetries = 8

// JobProcessor coordinates filtering, AI scoring, database persistence, and notifications for a job message.
type JobProcessor struct {
	client        *redis.Client
	analyzer      *analyzer.GeminiAnalyzer
	jobStore      *store.JobStore
	metrics       *metrics.Metrics
	cfg           *config.RedisConfig
	scoring       *config.ScoringConfig
	notifier      *notifier.Telegram
	notifyEnabled bool
	preFilter     *filter.CheapFilter

	attemptsMu sync.Mutex
	attempts   map[string]int
}

func NewJobProcessor(
	client *redis.Client,
	a *analyzer.GeminiAnalyzer,
	js *store.JobStore,
	m *metrics.Metrics,
	cfg *config.RedisConfig,
	scoring *config.ScoringConfig,
	n *notifier.Telegram,
	notifyEnabled bool,
) *JobProcessor {
	return &JobProcessor{
		client:        client,
		analyzer:      a,
		jobStore:      js,
		metrics:       m,
		cfg:           cfg,
		scoring:       scoring,
		notifier:      n,
		notifyEnabled: notifyEnabled,
		attempts:      make(map[string]int),
	}
}

func (p *JobProcessor) SetPreFilter(f *filter.CheapFilter) {
	p.preFilter = f
}

func (p *JobProcessor) bumpAttempts(id string) int {
	p.attemptsMu.Lock()
	defer p.attemptsMu.Unlock()
	p.attempts[id]++
	return p.attempts[id]
}

func (p *JobProcessor) clearAttempts(id string) {
	p.attemptsMu.Lock()
	delete(p.attempts, id)
	p.attemptsMu.Unlock()
}

// Process handles a single redis stream message:
// Poison-pill check -> Prefilter -> Dedup check -> AI Scoring -> DB Upsert -> Telegram Notify -> ACK
func (p *JobProcessor) Process(ctx context.Context, msg redis.XMessage, workerID int) {
	title := field(msg.Values, "title")
	company := field(msg.Values, "company")
	location := field(msg.Values, "location")
	description := field(msg.Values, "description")
	source := field(msg.Values, "source")
	externalID := field(msg.Values, "external_id")
	url := field(msg.Values, "url")
	postedAt := field(msg.Values, "posted_at")

	// ── Poison Pill (Bozuk Payload) Koruması ──────────────────────────────────
	if title == "" || company == "" || source == "" || externalID == "" || url == "" {
		slog.Warn("worker dropping malformed poison-pill message",
			"id", msg.ID, "source", source, "external_id", externalID, "title", title, "company", company)
		p.ToFailed(ctx, msg)
		return
	}

	// ── Ön filtre (kuyrukta bekleyen eski ilanlar için) ───────────────────────
	if p.preFilter != nil {
		if fr := p.preFilter.Check(title, company, description, source); fr.Rejected {
			slog.Debug("worker prefilter rejected", "title", title, "reason", fr.Reason)
			p.metrics.JobsCheapFiltered.Inc()
			p.Ack(ctx, p.cfg.StreamFiltered, msg.ID)
			return
		}
	}

	// Aynı ilan DB'de zaten puanlandıysa LLM kotası harcama.
	if exists, err := p.jobStore.AlreadyScored(ctx, source, externalID, company, title, location); err == nil && exists {
		slog.Debug("worker skipped already-scored job in db", "title", title, "company", company)
		p.Ack(ctx, p.cfg.StreamFiltered, msg.ID)
		return
	}

	// Aynı şirket+başlık aynı ülke/lokasyon kapsamında daha önce düşük puan aldıysa AI'ya gitme.
	if twin, err := p.jobStore.HasLowScoreTwin(ctx, company, title, location); err == nil && twin {
		slog.Debug("worker skipped low-score twin", "title", title, "company", company, "location", location)
		p.metrics.JobsCheapFiltered.Inc()
		p.Ack(ctx, p.cfg.StreamFiltered, msg.ID)
		return
	}

	// ── Gemini analizi ────────────────────────────────────────────────────────
	start := time.Now()
	result, err := p.analyzer.Analyze(ctx, title, company, location, description)
	elapsed := time.Since(start).Seconds()

	p.metrics.GeminiLatencySeconds.Observe(elapsed)

	if err != nil {
		if ctx.Err() != nil {
			return
		}
		p.metrics.GeminiRequests.With(map[string]string{"status": "error"}).Inc()

		if analyzer.IsTransient(err) {
			attempts := p.bumpAttempts(msg.ID)
			if attempts < maxAITransientRetries {
				wait := time.Duration(attempts) * 20 * time.Second
				if wait > 2*time.Minute {
					wait = 2 * time.Minute
				}
				slog.Warn("gemini transient failure, will retry",
					"title", title, "attempt", attempts, "wait", wait.String())
				select {
				case <-time.After(wait):
				case <-ctx.Done():
				}
				return
			}
			p.clearAttempts(msg.ID)
		}

		slog.Error("gemini failed",
			"title", title,
			"company", company,
			"error", err,
		)
		p.ToFailed(ctx, msg)
		return
	}
	p.clearAttempts(msg.ID)

	// Action'ı modelden değil, skordan deterministik olarak hesapla.
	result.Action = DeriveAction(result.MatchScore)

	p.metrics.GeminiRequests.With(map[string]string{"status": "success"}).Inc()
	p.metrics.MatchScores.Observe(result.MatchScore)
	p.metrics.JobsScored.With(map[string]string{"outcome": result.Action}).Inc()

	slog.Info("scored",
		"title", title,
		"company", company,
		"score", result.MatchScore,
		"action", result.Action,
		"latency_s", fmt.Sprintf("%.2f", elapsed),
	)

	// ── PostgreSQL'e yaz ──────────────────────────────────────────────────────
	if err := p.jobStore.Upsert(ctx, store.JobRecord{
		Source:      source,
		ExternalID:  externalID,
		URL:         url,
		Title:       title,
		Company:     company,
		Location:    location,
		Description: description,
		PostedAt:    postedAt,
		Score:       result,
	}); err != nil {
		slog.Error("db upsert failed", "title", title, "error", err)

		attempts := p.bumpAttempts(msg.ID)
		if attempts >= maxDBRetries {
			slog.Error("db upsert retries exhausted, moving to failed stream", "id", msg.ID, "attempts", attempts)
			p.clearAttempts(msg.ID)
			p.ToFailed(ctx, msg)
			return
		}
		select {
		case <-time.After(time.Duration(attempts) * 2 * time.Second):
		case <-ctx.Done():
		}
		return
	}
	p.clearAttempts(msg.ID)

	p.Ack(ctx, p.cfg.StreamFiltered, msg.ID)

	// ── Telegram bildirimi ────────────────────────────────────────────────────
	threshold := p.scoring.TelegramThresholdPrimary
	if result.RoleFamily == "secondary" {
		threshold = p.scoring.TelegramThresholdSecondary
	}

	alreadyNotified := false
	if notifiedAt, err := p.jobStore.GetNotifiedAt(ctx, source, externalID); err == nil && notifiedAt != nil {
		alreadyNotified = true
	}

	if p.notifyEnabled && !alreadyNotified && result.MatchScore >= threshold {
		if err := p.notifier.Notify(ctx, title, company, location, url, source, result); err != nil {
			slog.Warn("telegram notification failed", "error", err)
			p.metrics.TelegramFailed.Inc()
		} else {
			p.metrics.TelegramSent.Inc()
			if err := p.jobStore.MarkNotified(ctx, source, externalID); err != nil {
				slog.Error("mark notified failed", "error", err)
			}
		}
	}
}

func (p *JobProcessor) ToFailed(ctx context.Context, msg redis.XMessage) {
	_, _ = p.client.XAdd(ctx, &redis.XAddArgs{
		Stream: p.cfg.StreamFailed,
		MaxLen: 1000,
		Approx: true,
		Values: msg.Values,
	}).Result()

	dedupHash := field(msg.Values, "dedup_hash")
	contentHash := field(msg.Values, "content_hash")
	if p.cfg.DedupSetKey != "" {
		if dedupHash != "" {
			_ = p.client.SRem(ctx, p.cfg.DedupSetKey, dedupHash).Err()
			_ = p.client.Del(ctx, p.cfg.DedupSetKey+":"+dedupHash).Err()
		}
		if contentHash != "" {
			_ = p.client.Del(ctx, p.cfg.DedupSetKey+":c:"+contentHash).Err()
		}
	}

	p.Ack(ctx, p.cfg.StreamFiltered, msg.ID)
}

func (p *JobProcessor) Ack(ctx context.Context, stream, id string) {
	if err := p.client.XAck(ctx, stream, p.cfg.ConsumerGroup, id).Err(); err != nil {
		slog.Error("xack failed", "id", id, "error", err)
	}
}

func field(values map[string]any, key string) string {
	v, ok := values[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// DeriveAction determines action deterministically based on match_score.
func DeriveAction(score float64) string {
	switch {
	case score >= 7.0:
		return "apply"
	case score >= 5.0:
		return "watch"
	default:
		return "skip"
	}
}
