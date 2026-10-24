package intelligence

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/ulasdursun/job-hunter/internal/config"
	"github.com/ulasdursun/job-hunter/internal/notifier"
)

const (
	queueLagAlertThreshold = 200
	failedDeltaThreshold   = 5
	healthCheckInterval    = 10 * time.Minute
	healthAlertCooldown    = 4 * time.Hour
)

// Scheduler, haftalık market raporunu (Pazartesi 09:00),
// günlük akşam fırsat özetini (her gün 20:00 — Top 5 Apply/Watch)
// ve kuyruk/hata sağlık uyarılarını otomatik gönderir.
type Scheduler struct {
	pool     *pgxpool.Pool
	analyzer *Analyzer
	notifier *notifier.Telegram
	redis    *redis.Client
	redisCfg *config.RedisConfig
}

// SetRedisMonitor, kuyruk birikmesi (>200) ve jobs:failed artışı için Redis izlemesini etkinleştirir.
func (s *Scheduler) SetRedisMonitor(client *redis.Client, cfg *config.RedisConfig) {
	s.redis = client
	s.redisCfg = cfg
}

func NewScheduler(
	pool *pgxpool.Pool,
	cfg *config.CandidateConfig,
	n *notifier.Telegram,
) *Scheduler {
	// Tüm candidate skill'lerini tek listede topla
	var allSkills []string
	allSkills = append(allSkills, cfg.Skills.Languages...)
	allSkills = append(allSkills, cfg.Skills.Frameworks...)
	allSkills = append(allSkills, cfg.Skills.Databases...)
	allSkills = append(allSkills, cfg.Skills.Infra...)
	allSkills = append(allSkills, cfg.Skills.Cloud...)
	allSkills = append(allSkills, cfg.Skills.Concepts...)

	return &Scheduler{
		pool:     pool,
		analyzer: NewAnalyzer(pool, allSkills),
		notifier: n,
	}
}

// Run, context iptal edilene kadar haftalık rapor ve günlük 20:00 akşam özeti döngülerini çalıştırır.
func (s *Scheduler) Run(ctx context.Context) {
	slog.Info("market report & evening digest scheduler started")

	go s.runEveningDigest(ctx)
	if s.redis != nil && s.redisCfg != nil {
		go s.runQueueHealthMonitor(ctx)
	}

	// Sonraki rapor için Pazartesi 09:00'ı hesapla
	ticker := time.NewTicker(s.untilNextMonday())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("market report scheduler stopped")
			return
		case <-ticker.C:
			s.generateAndSend(ctx)
			// Her çalışmadan sonra bir sonraki Pazartesi'ye ayarla
			ticker.Reset(7 * 24 * time.Hour)
		}
	}
}

func (s *Scheduler) runEveningDigest(ctx context.Context) {
	ticker := time.NewTicker(untilNextHour(20, 0))
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sendEveningDigest(ctx)
			ticker.Reset(24 * time.Hour)
		}
	}
}

func (s *Scheduler) runQueueHealthMonitor(ctx context.Context) {
	// Başlangıçtaki mevcut failed sayısını baz al (eski hatalar için açılışta alarm çalma).
	lastFailed, _ := s.redis.XLen(ctx, s.redisCfg.StreamFailed).Result()
	var lastLagAlert time.Time

	ticker := time.NewTicker(healthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.notifier == nil || !s.notifier.IsConfigured() {
				continue
			}

			var lag int64
			if groups, err := s.redis.XInfoGroups(ctx, s.redisCfg.StreamFiltered).Result(); err == nil {
				for _, g := range groups {
					if g.Name == s.redisCfg.ConsumerGroup {
						lag = g.Lag + g.Pending
						break
					}
				}
			}
			failedNow, _ := s.redis.XLen(ctx, s.redisCfg.StreamFailed).Result()
			newFailures := failedNow - lastFailed

			needLagAlert := lag >= queueLagAlertThreshold && time.Since(lastLagAlert) >= healthAlertCooldown
			needFailAlert := newFailures >= failedDeltaThreshold

			if !needLagAlert && !needFailAlert {
				continue
			}

			var sb strings.Builder
			sb.WriteString("⚠️ <b>SİSTEM SAĞLIK UYARISI (Job Hunter)</b>\n\n")
			if needLagAlert {
				sb.WriteString(fmt.Sprintf("• <b>Kuyruk Birikmesi (jobs:filtered):</b> <code>%d</code> ilan bekliyor (eşik: %d)\n", lag, queueLagAlertThreshold))
			}
			if needFailAlert {
				sb.WriteString(fmt.Sprintf("• <b>Yeni Hatalı İlan (jobs:failed):</b> <code>+%d</code> yeni hata (toplam: <code>%d</code>)\n", newFailures, failedNow))
			}
			sb.WriteString("\n<i>Not: AI anahtar kotaları dolmuş veya worker yavaşlamış olabilir.</i>")

			if err := s.notifier.SendRaw(ctx, sb.String()); err != nil {
				slog.Warn("health monitor telegram send failed", "error", err)
				continue
			}
			if needLagAlert {
				lastLagAlert = time.Now()
			}
			if needFailAlert {
				lastFailed = failedNow
			}
			slog.Info("queue health alert sent", "lag", lag, "failed_total", failedNow)
		}
	}
}

func (s *Scheduler) sendEveningDigest(ctx context.Context) {
	if s.notifier == nil || !s.notifier.IsConfigured() {
		return
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id::text, title, company, COALESCE(location, ''), url, match_score::float8, COALESCE(ai_reasoning, '')
		FROM jobs
		WHERE match_score >= 6.8
		  AND notified_at IS NULL
		  AND status = 'new'
		  AND scraped_at >= NOW() - INTERVAL '24 hours'
		ORDER BY match_score DESC, scraped_at DESC
		LIMIT 5
	`)
	if err != nil {
		slog.Error("evening digest query failed", "error", err)
		return
	}
	defer rows.Close()

	type item struct {
		id, title, company, loc, url, reasoning string
		score                                   float64
	}
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.id, &it.title, &it.company, &it.loc, &it.url, &it.score, &it.reasoning); err == nil {
			items = append(items, it)
		}
	}
	if len(items) == 0 {
		slog.Info("evening digest: no new unnotified >=6.8 opportunities in last 24h")
		return
	}

	esc := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	escAttr := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;")
	var sb strings.Builder
	sb.WriteString("🌆 <b>GÜNÜN EN İYİ FIRSATLARI (Akşam Özeti)</b>\n")
	sb.WriteString("<i>Anlık bildirim (≥8.0) altında kalan en güçlü 6.8–7.9 adayları:</i>\n\n")

	var ids []string
	for i, it := range items {
		ids = append(ids, it.id)
		// Telegram mesaj sınırı 4096 karakter: gerekçeyi rune-güvenli kısalt.
		reasoning := []rune(it.reasoning)
		if len(reasoning) > 220 {
			reasoning = append(reasoning[:220], '…')
		}
		sb.WriteString(fmt.Sprintf(
			"<b>%d. [%.1f/10] %s</b> — <i>%s</i> (%s)\n%s\n🔗 <a href=\"%s\">İlana Git</a>\n\n",
			i+1,
			it.score,
			esc.Replace(it.title),
			esc.Replace(it.company),
			esc.Replace(it.loc),
			esc.Replace(string(reasoning)),
			escAttr.Replace(it.url),
		))
	}

	if err := s.notifier.SendRaw(ctx, sb.String()); err != nil {
		slog.Warn("evening digest send failed", "error", err)
		return
	}

	for _, id := range ids {
		_, _ = s.pool.Exec(ctx, `UPDATE jobs SET notified_at = NOW() WHERE id = $1::uuid`, id)
	}
	slog.Info("evening digest sent", "count", len(items))
}

func untilNextHour(hour, min int) time.Duration {
	now := time.Now()
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, min, 0, 0, now.Location())
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	return time.Until(next)
}

func (s *Scheduler) generateAndSend(ctx context.Context) {
	slog.Info("generating market report")

	report, err := s.analyzer.Analyze(ctx, 7)
	if err != nil {
		slog.Error("market report analysis failed", "error", err)
		return
	}

	if report.TotalJobs == 0 {
		slog.Warn("no jobs in database yet, skipping report")
		return
	}

	message := FormatTelegramReport(report)

	if err := s.notifier.SendRaw(ctx, message); err != nil {
		slog.Error("market report send failed", "error", err)
		return
	}

	slog.Info("market report sent",
		"total_jobs", report.TotalJobs,
		"apply_jobs", report.ApplyJobs,
		"gap_skills", len(report.GapSkills),
	)
}

// untilNextMonday, bir sonraki Pazartesi 09:00'a olan süreyi hesaplar.
func (s *Scheduler) untilNextMonday() time.Duration {
	now := time.Now()
	daysUntilMonday := (time.Monday - now.Weekday() + 7) % 7
	if daysUntilMonday == 0 {
		daysUntilMonday = 7 // Bu Pazartesi geçtiyse gelecek hafta
	}
	nextMonday := time.Date(
		now.Year(), now.Month(), now.Day()+int(daysUntilMonday),
		9, 0, 0, 0, now.Location(),
	)
	return time.Until(nextMonday)
}
