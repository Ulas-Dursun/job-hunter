package api

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
)

// StartServer starts the dashboard HTTP server in a background goroutine.
// Serves both the REST API and the static frontend from the "web" directory.
func StartServer(port int, pool *pgxpool.Pool) {
	repo := NewRepository(pool)
	h := NewHandlers(repo)

	mux := http.NewServeMux()

	// ── API routes ───────────────────────────────────────────────────────────
	mux.HandleFunc("GET /api/jobs", h.ListJobs)
	mux.HandleFunc("GET /api/stats", h.GetStats)
	mux.HandleFunc("PATCH /api/jobs/{id}/status", h.UpdateJobStatus)

	// ── Static frontend ──────────────────────────────────────────────────────
	// Everything under web/ is served as-is; index.html is the dashboard entry point.
	mux.Handle("/", http.FileServer(http.Dir("web")))

	addr := fmt.Sprintf(":%d", port)

	go func() {
		slog.Info("dashboard server listening", "addr", "http://localhost"+addr)
		if err := http.ListenAndServe(addr, withCORS(mux)); err != nil {
			slog.Error("dashboard server crashed", "error", err)
		}
	}()
}

// withCORS allows the frontend to call the API even if served from a
// different port during local development (e.g. live-reload tools).
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, PATCH, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}
