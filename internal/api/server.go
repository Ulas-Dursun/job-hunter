package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ulasdursun/job-hunter/internal/analyzer"
)

// maxBodyBytes, istek gövdesi için üst sınırdır (PATCH status gibi küçük JSON'lar için fazlasıyla yeterli).
const maxBodyBytes = 1 << 20

// StartServer starts the dashboard HTTP server in a background goroutine.
// Serves both the REST API and the static frontend from the "web" directory.
// ctx iptal edildiğinde sunucu graceful shutdown yapar.
func StartServer(ctx context.Context, port int, pool *pgxpool.Pool, tailor *analyzer.TailorService) {
	repo := NewRepository(pool)
	h := NewHandlers(repo, tailor)

	mux := http.NewServeMux()

	// ── API routes ───────────────────────────────────────────────────────────
	mux.HandleFunc("GET /api/jobs", h.ListJobs)
	mux.HandleFunc("GET /api/jobs/{id}", h.GetJob)
	mux.HandleFunc("GET /api/stats", h.GetStats)
	mux.HandleFunc("PATCH /api/jobs/{id}/status", h.UpdateJobStatus)
	mux.HandleFunc("POST /api/jobs/{id}/tailor", h.TailorJob)
	mux.HandleFunc("POST /api/jobs/{id}/interview-prep", h.InterviewPrepJob)
	mux.HandleFunc("DELETE /api/jobs/{id}", h.DeleteJob)
	mux.HandleFunc("DELETE /api/jobs", h.ClearJobs)

	// ── Static frontend ──────────────────────────────────────────────────────
	// Everything under web/ is served as-is; index.html is the dashboard entry point.
	// noCache ensures UI updates are immediately reflected without stale browser cache.
	mux.Handle("/", noCache(http.FileServer(http.Dir("web"))))

	addr := fmt.Sprintf("127.0.0.1:%d", port)

	srv := &http.Server{
		Addr:              addr,
		Handler:           guard(port, mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		// Tailor/Interview Prep LLM çağrıları (failover + backoff) uzun sürebilir.
		WriteTimeout: 5 * time.Minute,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("dashboard server listening", "addr", "http://"+addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("dashboard server crashed", "error", err)
		}
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Warn("dashboard server shutdown error", "error", err)
		}
	}()
}

func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate, max-age=0")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		next.ServeHTTP(w, r)
	})
}

// guard; Host doğrulama (DNS rebinding), katı CORS, cross-site state-changing istek reddi,
// gövde boyutu sınırı ve temel güvenlik başlıklarını tek middleware'de toplar.
func guard(port int, next http.Handler) http.Handler {
	allowedOrigins := map[string]bool{
		fmt.Sprintf("http://localhost:%d", port): true,
		fmt.Sprintf("http://127.0.0.1:%d", port): true,
	}
	allowedHosts := map[string]bool{
		fmt.Sprintf("localhost:%d", port): true,
		fmt.Sprintf("127.0.0.1:%d", port): true,
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// DNS rebinding koruması: yalnızca beklenen Host değerleri.
		if !allowedHosts[r.Host] {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}

		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")

		origin := r.Header.Get("Origin")
		originOK := origin != "" && allowedOrigins[origin]
		if originOK {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Confirm")
		}

		if r.Method == http.MethodOptions {
			if origin != "" && !originOK {
				http.Error(w, "origin not allowed", http.StatusForbidden)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// State-changing isteklerde yabancı origin / cross-site istekleri reddet (CSRF).
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if (origin != "" && !originOK) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				http.Error(w, "cross-origin request blocked", http.StatusForbidden)
				return
			}
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		next.ServeHTTP(w, r)
	})
}
