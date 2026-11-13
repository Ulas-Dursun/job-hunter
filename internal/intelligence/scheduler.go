package intelligence

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/ulasdursun/job-hunter/internal/config"
	"github.com/ulasdursun/job-hunter/internal/notifier"
)

const schedulerTickInterval = 1 * time.Minute

// Scheduler coordinates market reports (Mon 09:00), daily evening digest (20:00), and queue health monitoring.
type Scheduler struct {
	pool           *pgxpool.Pool
	analyzer       *Analyzer
	notifier       *notifier.Telegram
	digest         *EveningDigest
	healthMonitor  *QueueHealthMonitor
	redis          *redis.Client
	lastDigestDate string
	lastReportWeek string
}

// SetRedisMonitor enables queue lag and failed stream monitoring, and optional Redis schedule state persistence.
func (s *Scheduler) SetRedisMonitor(client *redis.Client, cfg *config.RedisConfig) {
	s.redis = client
	s.healthMonitor = NewQueueHealthMonitor(client, cfg, s.notifier)
}

func NewScheduler(
	pool *pgxpool.Pool,
	cfg *config.CandidateConfig,
	n *notifier.Telegram,
) *Scheduler {
	var allSkills []string
	allSkills = append(allSkills, cfg.Skills.Languages...)
	allSkills = append(allSkills, cfg.Skills.Frameworks...)
	allSkills = append(allSkills, cfg.Skills.Databases...)
	allSkills = append(allSkills, cfg.Skills.OS...)
	allSkills = append(allSkills, cfg.Skills.Infra...)
	allSkills = append(allSkills, cfg.Skills.Cloud...)
	allSkills = append(allSkills, cfg.Skills.Concepts...)

	return &Scheduler{
		pool:     pool,
		analyzer: NewAnalyzer(pool, allSkills),
		notifier: n,
		digest:   NewEveningDigest(pool, n),
	}
}

// Run starts weekly report, evening digest, and health monitor routines until context cancellation.
// Uyku modunda monotonic clock donmalarını aşmak için 1 dakikalık ticker ile wall-clock (time.Now()) kontrolü yapar.
func (s *Scheduler) Run(ctx context.Context) {
	slog.Info("market report & evening digest scheduler started")

	if s.healthMonitor != nil {
		go s.healthMonitor.Run(ctx)
	}

	ticker := time.NewTicker(schedulerTickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("market report scheduler stopped")
			return
		case <-ticker.C:
			s.checkSchedule(ctx, time.Now())
		}
	}
}

func (s *Scheduler) checkSchedule(ctx context.Context, now time.Time) {
	today := now.Format("2006-01-02")
	if shouldTriggerEveningDigest(now, s.lastDigestDate) {
		redisKey := "scheduler:evening_digest:" + today
		if s.isMarkedInRedis(ctx, redisKey) {
			s.lastDigestDate = today
		} else if s.digest.Send(ctx) {
			s.lastDigestDate = today
			s.markInRedis(ctx, redisKey, 48*time.Hour)
		}
	}

	weekKey := isoWeekKey(now)
	if shouldTriggerWeeklyReport(now, s.lastReportWeek) {
		redisKey := "scheduler:weekly_report:" + weekKey
		if s.isMarkedInRedis(ctx, redisKey) {
			s.lastReportWeek = weekKey
		} else if s.generateAndSend(ctx) {
			s.lastReportWeek = weekKey
			s.markInRedis(ctx, redisKey, 8*24*time.Hour)
		}
	}
}

func shouldTriggerEveningDigest(now time.Time, lastSentDate string) bool {
	if now.Hour() < 20 {
		return false
	}
	return lastSentDate != now.Format("2006-01-02")
}

func shouldTriggerWeeklyReport(now time.Time, lastSentWeek string) bool {
	if now.Weekday() != time.Monday || now.Hour() < 9 {
		return false
	}
	return lastSentWeek != isoWeekKey(now)
}

func isoWeekKey(now time.Time) string {
	year, week := now.ISOWeek()
	return fmt.Sprintf("%04d-W%02d", year, week)
}

func (s *Scheduler) isMarkedInRedis(ctx context.Context, key string) bool {
	if s.redis == nil {
		return false
	}
	val, err := s.redis.Get(ctx, key).Result()
	if err != nil && err != redis.Nil {
		slog.Warn("scheduler redis get failed", "key", key, "error", err)
		return false
	}
	return err == nil && val != ""
}

func (s *Scheduler) markInRedis(ctx context.Context, key string, ttl time.Duration) {
	if s.redis == nil {
		return
	}
	if err := s.redis.Set(ctx, key, "1", ttl).Err(); err != nil {
		slog.Warn("scheduler redis set failed", "key", key, "error", err)
	}
}

func (s *Scheduler) generateAndSend(ctx context.Context) bool {
	slog.Info("generating market report")

	report, err := s.analyzer.Analyze(ctx, 7)
	if err != nil {
		slog.Error("market report analysis failed", "error", err)
		return false
	}

	if report.TotalJobs == 0 {
		slog.Warn("no jobs in database yet, skipping report")
		return true
	}

	message := FormatTelegramReport(report)

	if err := s.notifier.SendRaw(ctx, message); err != nil {
		slog.Error("market report send failed", "error", err)
		return false
	}

	slog.Info("market report sent",
		"total_jobs", report.TotalJobs,
		"apply_jobs", report.ApplyJobs,
		"gap_skills", len(report.GapSkills),
	)
	return true
}
