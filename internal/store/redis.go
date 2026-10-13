package store

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/redis/go-redis/v9"
	"github.com/ulasdursun/job-hunter/internal/config"
)

// RedisClient, go-redis client'ını stream-specific metodlarla sarmalar.
// Stream isimleri hardcoded değil — config'den gelir.
type RedisClient struct {
	client *redis.Client
	cfg    *config.RedisConfig
}

// NewRedisClient, Redis'e bağlanır ve gerekli stream'leri + consumer group'larını oluşturur.
func NewRedisClient(ctx context.Context, addr string, cfg *config.RedisConfig) (*RedisClient, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: "",
		DB:       0,
	})

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping failed — is the container running?: %w", err)
	}

	rc := &RedisClient{client: client, cfg: cfg}

	if err := rc.initStreamsAndGroups(ctx); err != nil {
		client.Close()
		return nil, fmt.Errorf("initializing redis streams: %w", err)
	}

	slog.Info("redis connected", "addr", addr)
	return rc, nil
}

// initStreamsAndGroups, orchestrator'ın okuyacağı stream'ler için consumer group oluşturur.
// MKSTREAM: stream yoksa otomatik oluşturur.
// BUSYGROUP hatası: group zaten var demek — bu beklenen durum, ignore edilir.
func (rc *RedisClient) initStreamsAndGroups(ctx context.Context) error {
	streams := []string{
		rc.cfg.StreamRaw,
		rc.cfg.StreamFiltered,
		rc.cfg.StreamFailed,
	}

	for _, stream := range streams {
		err := rc.client.XGroupCreateMkStream(ctx, stream, rc.cfg.ConsumerGroup, "0").Err()
		if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
			return fmt.Errorf("creating consumer group for stream %q: %w", stream, err)
		}
		slog.Debug("stream ready",
			"stream", stream,
			"group", rc.cfg.ConsumerGroup,
		)
	}

	return nil
}

// Client, raw redis.Client'ı döndürür.
func (rc *RedisClient) Client() *redis.Client {
	return rc.client
}

// StreamLengths, tüm stream'lerin mevcut uzunluklarını döndürür.
// Prometheus publisher depth metriği için kullanılır.
func (rc *RedisClient) StreamLengths(ctx context.Context) (map[string]int64, error) {
	streams := map[string]string{
		"raw":      rc.cfg.StreamRaw,
		"filtered": rc.cfg.StreamFiltered,
		"failed":   rc.cfg.StreamFailed,
	}

	lengths := make(map[string]int64, len(streams))
	for label, stream := range streams {
		length, err := rc.client.XLen(ctx, stream).Result()
		if err != nil {
			return nil, fmt.Errorf("getting length of stream %q: %w", stream, err)
		}
		lengths[label] = length
	}

	return lengths, nil
}

// Close, Redis bağlantısını kapatır.
func (rc *RedisClient) Close() error {
	slog.Info("redis connection closed")
	return rc.client.Close()
}
