package intelligence

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/ulasdursun/job-hunter/internal/config"
	"github.com/ulasdursun/job-hunter/internal/notifier"
)

// Scheduler coordinates market reports (Mon 09:00), daily evening digest (20:00), and queue health monitoring.
type Scheduler struct {
	pool          *pgxpool.Pool
	analyzer      *Analyzer
	notifier      *notifier.Telegram
	digest        *EveningDigest
	healthMonitor *QueueHealthMonitor
}

// SetRedisMonitor enables queue lag and failed stream monitoring.
func (s *Scheduler) SetRedisMonitor(client *redis.Client, cfg *config.RedisConfig) {
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
func (s *Scheduler) Run(ctx context.Context) {
	slog.Info("market report & evening digest scheduler started")

	go s.runEveningDigest(ctx)
	if s.healthMonitor != nil {
		go s.healthMonitor.Run(ctx)
	}

	ticker := time.NewTicker(s.untilNextMonday())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("market report scheduler stopped")
			return
		case <-ticker.C:
			s.generateAndSend(ctx)
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
			s.digest.Send(ctx)
			ticker.Reset(24 * time.Hour)
		}
	}
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

func untilNextHour(hour, min int) time.Duration {
	now := time.Now()
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, min, 0, 0, now.Location())
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	return time.Until(next)
}

func (s *Scheduler) untilNextMonday() time.Duration {
	now := time.Now()
	daysUntilMonday := (time.Monday - now.Weekday() + 7) % 7
	if daysUntilMonday == 0 {
		daysUntilMonday = 7
	}
	nextMonday := time.Date(
		now.Year(), now.Month(), now.Day()+int(daysUntilMonday),
		9, 0, 0, 0, now.Location(),
	)
	return time.Until(nextMonday)
}
