package store

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresPool, pgxpool'u sarmalar.
// pgxpool goroutine-safe'dir — tek bir instance uygulama boyunca paylaşılır.
// Repository layer bu struct'ı alır, doğrudan pgxpool kullanır.
type PostgresPool struct {
	pool *pgxpool.Pool
}

// NewPostgresPool, PostgreSQL'e bağlantı pool'u oluşturur ve şema güncellemelerini doğrular.
// DSN format: "postgres://user:password@host:port/dbname?sslmode=disable"
func NewPostgresPool(ctx context.Context, dsn string) (*PostgresPool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("creating postgres connection pool: %w", err)
	}

	// Gerçekten bağlanabildiğimizi doğrula — sadece parse başarısı yetmez
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres ping failed — is the container running?: %w", err)
	}

	// Idempotent şema kontrolü (Migration 002 kolonlarının her ortamda varlığını garanti eder)
	if err := ensureSchema(ctx, pool); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ensuring database schema: %w", err)
	}

	stats := pool.Stat()
	slog.Info("postgres connected",
		"max_conns", stats.MaxConns(),
		"total_conns", stats.TotalConns(),
	)

	return &PostgresPool{pool: pool}, nil
}

// ensureSchema, sonradan eklenen kolonların mevcut veritabanında eksik olmamasını sağlar.
func ensureSchema(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		ALTER TABLE jobs ADD COLUMN IF NOT EXISTS cover_letter TEXT;
		ALTER TABLE jobs ADD COLUMN IF NOT EXISTS tailored_cv JSONB;
		ALTER TABLE jobs ADD COLUMN IF NOT EXISTS interview_prep JSONB;
	`)
	return err
}

// StartRetentionCleaner, veritabanının zamanla eski ve düşük skorlu (< 6.0) gürültü ilanlarla
// şişmesini önlemek için başlangıçta ve her 24 saatte bir güvenli temizlik (auto-prune) yapar.
// Başvurulan (applied), mülakat (interview), teklif (offer), yüksek skorlu (>= 6.0) veya
// CV/Mülakat kiti üretilmiş hiçbir ilana dokunmaz.
func (p *PostgresPool) StartRetentionCleaner(ctx context.Context) {
	go func() {
		p.pruneOldLowScoreJobs(ctx)

		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.pruneOldLowScoreJobs(ctx)
			}
		}
	}()
}

func (p *PostgresPool) pruneOldLowScoreJobs(ctx context.Context) {
	tag, err := p.pool.Exec(ctx, `
		DELETE FROM jobs
		WHERE status IN ('new', 'skipped')
		  AND (match_score IS NULL OR match_score < 6.0)
		  AND cover_letter IS NULL
		  AND tailored_cv IS NULL
		  AND interview_prep IS NULL
		  AND scraped_at < NOW() - INTERVAL '14 days'
	`)
	if err != nil {
		slog.Warn("db auto-retention cleanup failed", "error", err)
		return
	}
	if deleted := tag.RowsAffected(); deleted > 0 {
		slog.Info("db auto-retention cleanup completed (14+ günlük düşük skorlu gürültü ilanlar temizlendi)", "deleted_rows", deleted)
	}
}

// Pool, raw pgxpool'u döndürür.
// Repository fonksiyonları bu pool üzerinden query çalıştırır.
func (p *PostgresPool) Pool() *pgxpool.Pool {
	return p.pool
}

// Close, tüm bağlantıları graceful olarak kapatır.
// defer pg.Close() ile main'de çağrılır.
func (p *PostgresPool) Close() {
	p.pool.Close()
	slog.Info("postgres connection pool closed")
}
