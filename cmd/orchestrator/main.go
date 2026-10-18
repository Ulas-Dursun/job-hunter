package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/ulasdursun/job-hunter/internal/analyzer"
	"github.com/ulasdursun/job-hunter/internal/api"
	"github.com/ulasdursun/job-hunter/internal/config"
	"github.com/ulasdursun/job-hunter/internal/consumer"
	"github.com/ulasdursun/job-hunter/internal/filter"
	"github.com/ulasdursun/job-hunter/internal/intelligence"
	"github.com/ulasdursun/job-hunter/internal/metrics"
	"github.com/ulasdursun/job-hunter/internal/notifier"
	"github.com/ulasdursun/job-hunter/internal/store"
	"github.com/ulasdursun/job-hunter/internal/worker"
)

func main() {
	// ── 1. Logging ────────────────────────────────────────────────────────────
	logLevel := slog.LevelInfo
	if os.Getenv("LOG_LEVEL") == "debug" {
		logLevel = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	})))

	slog.Info("job-hunter orchestrator starting")

	// ── 2. Environment Variables ──────────────────────────────────────────────
	// Overload: .env'deki değerler sistem env var'larını da override eder.
	// Bu sayede key değişikliği restart olmadan etkin olur.
	if err := godotenv.Overload(); err != nil {
		slog.Debug(".env file not found, using system environment variables")
	}

	// ── 3. Config ─────────────────────────────────────────────────────────────
	cfg, err := config.Load("config.yaml")
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	slog.Info("config loaded",
		"candidate", cfg.Candidate.Name,
		"primary_roles", len(cfg.Candidate.TargetRoles.Primary),
		"sources_enabled", countEnabledSources(cfg),
	)

	// ── 4. Context — graceful shutdown ────────────────────────────────────────
	ctx, cancel := signal.NotifyContext(context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer cancel()

	// ── 5. PostgreSQL ─────────────────────────────────────────────────────────
	pg, err := store.NewPostgresPool(ctx, config.PostgresDSN())
	if err != nil {
		slog.Error("postgres connection failed — make sure docker compose is running",
			"error", err,
			"dsn", config.MaskedPostgresDSN(),
		)
		os.Exit(1)
	}
	defer pg.Close()

	// ── 6. Redis ──────────────────────────────────────────────────────────────
	rc, err := store.NewRedisClient(ctx, config.RedisAddr(), &cfg.Redis)
	if err != nil {
		slog.Error("redis connection failed — make sure docker compose is running",
			"error", err,
			"addr", config.RedisAddr(),
		)
		os.Exit(1)
	}
	defer rc.Close()

	// ── 7. Prometheus Metrics ─────────────────────────────────────────────────
	appMetrics := metrics.New()
	metrics.StartServer(cfg.Server.MetricsPort)

	// ── 7b. Dashboard API Server & ATS Tailor Service ─────────────────────────
	tailorService := analyzer.NewTailorService(&cfg.Gemini)
	api.StartServer(cfg.Server.DashboardPort, pg.Pool(), tailorService)
	dashboardURL := fmt.Sprintf("http://localhost:%d", cfg.Server.DashboardPort)
	slog.Info("════════════════════════════════════════")
	slog.Info("  📊 DASHBOARD READY")
	slog.Info("  " + dashboardURL)
	slog.Info("════════════════════════════════════════")

	// ── 8. Cheap Filter ───────────────────────────────────────────────────────
	cheapFilter := filter.NewCheapFilter(&cfg.Candidate)

	// ── 9. Consumer (jobs:raw → jobs:filtered) ────────────────────────────────
	jobConsumer := consumer.New(rc.Client(), cheapFilter, appMetrics, &cfg.Redis)
	go jobConsumer.Run(ctx)

	// ── 10. Gemini Analyzer ───────────────────────────────────────────────────
	geminiAnalyzer := analyzer.NewGeminiAnalyzer(&cfg.Gemini, &cfg.Candidate)

	// ── 11. Job Store ─────────────────────────────────────────────────────────
	jobStore := store.NewJobStore(pg.Pool())

	// ── 12. Telegram Notifier ─────────────────────────────────────────────────
	telegramNotifier := notifier.NewTelegram()
	if telegramNotifier.IsConfigured() {
		slog.Info("telegram notifications enabled")
	} else {
		slog.Warn("telegram not configured — set TELEGRAM_BOT_TOKEN and TELEGRAM_CHAT_ID in .env")
	}

	// ── 13. Worker Pool (jobs:filtered → Gemini → PostgreSQL) ─────────────────
	workerPool := worker.New(
		cfg.Worker.PoolSize,
		rc.Client(),
		geminiAnalyzer,
		jobStore,
		appMetrics,
		&cfg.Redis,
		&cfg.Scoring,
		telegramNotifier,
		cfg.Notifications.TelegramEnabled,
	)
	go workerPool.Run(ctx)

	// ── 14. Market Intelligence Scheduler ────────────────────────────────────
	marketScheduler := intelligence.NewScheduler(pg.Pool(), &cfg.Candidate, telegramNotifier)
	go marketScheduler.Run(ctx)
	slog.Info("market intelligence scheduler started")

	slog.Info("all systems ready",
		"postgres", config.MaskedPostgresDSN(),
		"redis", config.RedisAddr(),
		"metrics", fmt.Sprintf("http://localhost:%d/metrics", cfg.Server.MetricsPort),
		"workers", cfg.Worker.PoolSize,
	)

	<-ctx.Done()
	slog.Info("shutdown signal received, stopping gracefully")
}

func countEnabledSources(cfg *config.Config) int {
	count := 0
	if cfg.Sources.KariyerNet.Enabled {
		count++
	}
	if cfg.Sources.LinkedInRSS.Enabled {
		count++
	}
	if cfg.Sources.Indeed.Enabled {
		count++
	}
	if cfg.Sources.JobSpy.Enabled {
		count++
	}
	if cfg.Sources.ATS.Enabled {
		count++
	}
	if cfg.Sources.Eurocontrol.Enabled {
		count++
	}
	if cfg.Sources.Techcareer.Enabled {
		count++
	}
	if cfg.Sources.Arbeitnow.Enabled {
		count++
	}
	return count
}
