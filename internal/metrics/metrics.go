package metrics

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics, orchestrator'ın tüm Prometheus metriklerini bir arada tutar.
// Uygulama başında bir kez oluşturulur, ilgili yerlere pointer olarak geçilir.
type Metrics struct {
	// ── Scraping ──────────────────────────────────────────────────────────
	// Hangi kaynaktan kaç ilan geldi? Label: source (kariyer_net, linkedin, ...)
	JobsIngested *prometheus.CounterVec

	// Mevcut alanların altına ekle
	JobsScored *prometheus.CounterVec

	// Dedup filtresi kaç ilanı durdurdu?
	JobsDuplicated prometheus.Counter

	// Keyword cheap filter kaç ilanı Gemini'ye gitmeden eledi?
	JobsCheapFiltered prometheus.Counter

	// ── Gemini API ────────────────────────────────────────────────────────
	// API çağrısı sonuçları. Label: status (success, error, rate_limited)
	GeminiRequests *prometheus.CounterVec

	// API yanıt süresi dağılımı
	GeminiLatencySeconds prometheus.Histogram

	// Match score dağılımı — histogram ile kaç ilanın hangi score aralığına
	// düştüğünü görürüz. "Çoğu ilan 4-6 arası geliyor" gibi insight'lar çıkar.
	MatchScores prometheus.Histogram

	// ── Queue ─────────────────────────────────────────────────────────────
	// Her stream'deki mevcut mesaj sayısı. Label: stream (raw, filtered, failed)
	QueueDepth *prometheus.GaugeVec

	// ── Bildirim ──────────────────────────────────────────────────────────
	TelegramSent   prometheus.Counter
	TelegramFailed prometheus.Counter
}

// New, tüm metrikleri Prometheus default registry'ye kaydeder ve döndürür.
func New() *Metrics {
	m := &Metrics{
		JobsIngested: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "jobhunter_jobs_ingested_total",
				Help: "Total job postings received from all sources.",
			},
			[]string{"source"},
		),

		JobsDuplicated: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "jobhunter_jobs_duplicated_total",
			Help: "Job postings rejected by deduplication.",
		}),

		JobsCheapFiltered: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "jobhunter_jobs_cheap_filtered_total",
			Help: "Job postings rejected by keyword filter before reaching Gemini.",
		}),

		JobsScored: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "jobhunter_jobs_scored_total",
				Help: "Total jobs scored by Gemini, by outcome (apply/watch/skip).",
			},
			[]string{"outcome"},
		),
		GeminiRequests: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "jobhunter_gemini_requests_total",
				Help: "Total Gemini API calls.",
			},
			[]string{"status"}, // success | error | rate_limited
		),

		GeminiLatencySeconds: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "jobhunter_gemini_latency_seconds",
			Help:    "Gemini API call latency in seconds.",
			Buckets: prometheus.DefBuckets,
		}),

		MatchScores: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "jobhunter_match_scores",
			Help:    "Distribution of AI match scores (0–10).",
			Buckets: []float64{1, 2, 3, 4, 5, 6, 7, 7.5, 8, 8.5, 9, 10},
		}),

		QueueDepth: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "jobhunter_queue_depth",
				Help: "Current number of messages in each Redis stream.",
			},
			[]string{"stream"}, // raw | filtered | failed
		),

		TelegramSent: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "jobhunter_telegram_sent_total",
			Help: "Telegram notifications successfully sent.",
		}),

		TelegramFailed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "jobhunter_telegram_failed_total",
			Help: "Telegram notification send failures.",
		}),
	}

	prometheus.MustRegister(
		m.JobsIngested,
		m.JobsDuplicated,
		m.JobsCheapFiltered,
		m.GeminiRequests,
		m.GeminiLatencySeconds,
		m.MatchScores,
		m.QueueDepth,
		m.TelegramSent,
		m.TelegramFailed,
		m.JobsScored,
	)

	return m
}

// StartServer, Prometheus metrics endpoint'ini bir goroutine'de başlatır.
// /metrics → Prometheus scraper buradan okur
// /health  → basit liveness check
func StartServer(port int) {
	addr := fmt.Sprintf(":%d", port)

	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		})

		slog.Info("metrics server listening", "addr", addr+"/metrics")
		if err := http.ListenAndServe(addr, mux); err != nil {
			slog.Error("metrics server crashed", "error", err)
		}
	}()
}
