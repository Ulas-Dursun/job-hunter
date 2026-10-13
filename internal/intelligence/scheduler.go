package intelligence

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ulasdursun/job-hunter/internal/config"
	"github.com/ulasdursun/job-hunter/internal/notifier"
)

// Scheduler, haftalık market raporunu otomatik gönderir.
// Her Pazartesi 09:00'da çalışır.
type Scheduler struct {
	analyzer *Analyzer
	notifier *notifier.Telegram
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
		analyzer: NewAnalyzer(pool, allSkills),
		notifier: n,
	}
}

// Run, context iptal edilene kadar haftalık rapor döngüsünü çalıştırır.
func (s *Scheduler) Run(ctx context.Context) {
	slog.Info("market report scheduler started")

	// İlk başlatmada hemen bir rapor üret — sistemi test etmek için
	s.generateAndSend(ctx)

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
