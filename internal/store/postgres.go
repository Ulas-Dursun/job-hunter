package store

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresPool, pgxpool'u sarmalar.
// pgxpool goroutine-safe'dir — tek bir instance uygulama boyunca paylaşılır.
// Repository layer bu struct'ı alır, doğrudan pgxpool kullanır.
type PostgresPool struct {
	pool *pgxpool.Pool
}

// NewPostgresPool, PostgreSQL'e bağlantı pool'u oluşturur.
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

	stats := pool.Stat()
	slog.Info("postgres connected",
		"max_conns", stats.MaxConns(),
		"total_conns", stats.TotalConns(),
	)

	return &PostgresPool{pool: pool}, nil
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
