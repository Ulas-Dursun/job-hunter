package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/ulasdursun/job-hunter/internal/notifier"

	"github.com/ulasdursun/job-hunter/internal/analyzer"
	"github.com/ulasdursun/job-hunter/internal/config"
	"github.com/ulasdursun/job-hunter/internal/filter"
	"github.com/ulasdursun/job-hunter/internal/metrics"
	"github.com/ulasdursun/job-hunter/internal/store"
)

// Pool, jobs:filtered stream'inden ilanları tüketen goroutine worker havuzu.
// Her worker: oku → Gemini analiz et → PostgreSQL'e yaz → ACK.
type Pool struct {
	size          int
	client        *redis.Client
	analyzer      *analyzer.GeminiAnalyzer
	jobStore      *store.JobStore
	metrics       *metrics.Metrics
	cfg           *config.RedisConfig
	scoring       *config.ScoringConfig
	notifier      *notifier.Telegram
	notifyEnabled bool

	attemptsMu sync.Mutex
	attempts   map[string]int // msgID → DB yazma / geçici AI hatası deneme sayısı

	preFilter *filter.CheapFilter // opsiyonel: kuyrukta bekleyen eski ilanları AI öncesi eler
}

// SetPreFilter, worker'ın AI çağrısından önce uygulayacağı CheapFilter'ı ayarlar.
func (p *Pool) SetPreFilter(f *filter.CheapFilter) { p.preFilter = f }

// maxDBRetries, bir mesajın DB'ye yazılamadığında jobs:failed'e taşınmadan önceki deneme sınırıdır.
const maxDBRetries = 5

// maxAITransientRetries, geçici AI kota hatalarında (429/503) jobs:failed'e taşımadan önceki deneme sınırıdır.
const maxAITransientRetries = 8

func (p *Pool) bumpAttempts(id string) int {
	p.attemptsMu.Lock()
	defer p.attemptsMu.Unlock()
	p.attempts[id]++
	return p.attempts[id]
}

func (p *Pool) clearAttempts(id string) {
	p.attemptsMu.Lock()
	delete(p.attempts, id)
	p.attemptsMu.Unlock()
}

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
		size:          size,
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

// Run, N worker goroutine başlatır.
// WaitGroup garantisi: tüm worker'lar durmadan Run() dönmez.
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
	// Worker 0 ana ConsumerName'i kullanır (eski pending mesajları da devralır);
	// diğer worker'lar benzersiz ad alır ki aynı pending mesajı iki worker aynı anda çekmesin.
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
			p.processJob(ctx, msg, workerID)
		}
	}
}

func (p *Pool) processJob(ctx context.Context, msg redis.XMessage, workerID int) {
	title := field(msg.Values, "title")
	company := field(msg.Values, "company")
	location := field(msg.Values, "location")
	description := field(msg.Values, "description")
	source := field(msg.Values, "source")
	externalID := field(msg.Values, "external_id")
	url := field(msg.Values, "url")
	postedAt := field(msg.Values, "posted_at")

	// ── Poison Pill (Bozuk Payload) Koruması ──────────────────────────────────
	// Zorunlu alanları eksik bir mesaj gelirse LLM kotası harcamadan ve panic almadan DLQ'ya taşı ve ACK et.
	if title == "" || company == "" || source == "" || externalID == "" || url == "" {
		slog.Warn("worker dropping malformed poison-pill message",
			"id", msg.ID, "source", source, "external_id", externalID, "title", title, "company", company)
		p.toFailed(ctx, msg)
		return
	}

	// ── Ön filtre (kuyrukta bekleyen eski ilanlar için) ───────────────────────
	// jobs:filtered'a eski/gevşek kurallarla girmiş ilanlar AI kotası harcamadan elenir.
	if p.preFilter != nil {
		if fr := p.preFilter.Check(title, company, description, source); fr.Rejected {
			slog.Debug("worker prefilter rejected", "title", title, "reason", fr.Reason)
			p.metrics.JobsCheapFiltered.Inc()
			p.ack(ctx, p.cfg.StreamFiltered, msg.ID)
			return
		}
	}

	// Aynı ilan (source+external_id veya son 30 günde aynı company+title+location) DB'de zaten puanlandıysa LLM kotası harcama.
	if exists, err := p.jobStore.AlreadyScored(ctx, source, externalID, company, title, location); err == nil && exists {
		slog.Debug("worker skipped already-scored job in db", "title", title, "company", company)
		p.ack(ctx, p.cfg.StreamFiltered, msg.ID)
		return
	}

	// Aynı şirket+başlık daha önce düşük puan aldıysa (çok şehirli kopya ilan) AI'ya gitme.
	if twin, err := p.jobStore.HasLowScoreTwin(ctx, company, title); err == nil && twin {
		slog.Debug("worker skipped low-score twin", "title", title, "company", company)
		p.metrics.JobsCheapFiltered.Inc()
		p.ack(ctx, p.cfg.StreamFiltered, msg.ID)
		return
	}

	// ── Gemini analizi ────────────────────────────────────────────────────────
	start := time.Now()
	result, err := p.analyzer.Analyze(ctx, title, company, location, description)
	elapsed := time.Since(start).Seconds()

	p.metrics.GeminiLatencySeconds.Observe(elapsed)

	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		p.metrics.GeminiRequests.With(map[string]string{"status": "error"}).Inc()

		// Geçici kota/yoğunluk hatası: ilanı kaybetme, ACK'leme; backoff ile pending'den yeniden dene.
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
		p.toFailed(ctx, msg)
		return
	}
	p.clearAttempts(msg.ID)

	// Action'ı modelden değil, skordan deterministik olarak hesapla.
	// LLM'ler action/score arasında bazen tutarsız oluyor (örn: 8.0 skor + "watch"
	// üretebiliyor) — bu satır garanti eder ki eşik kuralı her zaman tutarlı uygulanır.
	result.Action = deriveAction(result.MatchScore)

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
	// Tüm puanlanan ilanlar (skip/watch/apply) veritabanına kaydedilir.
	// Bu sayede dashboard'da elenen ilanlar incelenebilir ve pazar analizi tam veriyle çalışır.
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

		// Zehirli mesaj koruması: ack'lenmeyen mesaj pending listesinden anında tekrar okunur.
		// Backoff + deneme sınırı olmadan sonsuz sıcak döngü (LLM kotası + CPU) oluşurdu.
		attempts := p.bumpAttempts(msg.ID)
		if attempts >= maxDBRetries {
			slog.Error("db upsert retries exhausted, moving to failed stream", "id", msg.ID, "attempts", attempts)
			p.clearAttempts(msg.ID)
			p.toFailed(ctx, msg)
			return
		}
		select {
		case <-time.After(time.Duration(attempts) * 2 * time.Second):
		case <-ctx.Done():
		}
		return
	}
	p.clearAttempts(msg.ID)

	p.ack(ctx, p.cfg.StreamFiltered, msg.ID)

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

func (p *Pool) toFailed(ctx context.Context, msg redis.XMessage) {
	_, _ = p.client.XAdd(ctx, &redis.XAddArgs{
		Stream: p.cfg.StreamFailed,
		MaxLen: 1000,
		Approx: true,
		Values: msg.Values,
	}).Result()

	// Başarısız ilanın dedup anahtarlarını (hem eski SET hem yeni TTL'li URL/içerik anahtarları) kaldır.
	// Bu sayede sonraki taramada veya yeniden denemede ilan kalıcı olarak kilitli kalmaz.
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

	p.ack(ctx, p.cfg.StreamFiltered, msg.ID)
}

func (p *Pool) ack(ctx context.Context, stream, id string) {
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

// deriveAction, match_score'a göre action'ı deterministik olarak belirler.
// Modelin kendi ürettiği action alanına güvenilmez — LLM tutarsız olabilir
// (örn: 8.0 skor ile "watch" üretebilir). Bu fonksiyon eşik kuralını
// kod seviyesinde garanti eder.
func deriveAction(score float64) string {
	switch {
	case score >= 7.0:
		return "apply"
	case score >= 5.0:
		return "watch"
	default:
		return "skip"
	}
}
