package consumer

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/ulasdursun/job-hunter/internal/config"
	"github.com/ulasdursun/job-hunter/internal/filter"
	"github.com/ulasdursun/job-hunter/internal/metrics"
)

// Consumer, jobs:raw stream'ini okur, cheap filter uygular,
// geçen ilanları jobs:filtered stream'ine yazar.
type Consumer struct {
	client  *redis.Client
	filter  *filter.CheapFilter
	metrics *metrics.Metrics
	cfg     *config.RedisConfig
}

func New(
	client *redis.Client,
	f *filter.CheapFilter,
	m *metrics.Metrics,
	cfg *config.RedisConfig,
) *Consumer {
	return &Consumer{
		client:  client,
		filter:  f,
		metrics: m,
		cfg:     cfg,
	}
}

// Run, context iptal edilene kadar jobs:raw'ı dinler.
// Graceful shutdown: ctx.Done() sinyali gelince mevcut batch biter, çıkılır.
func (c *Consumer) Run(ctx context.Context) {
	slog.Info("consumer started",
		"stream", c.cfg.StreamRaw,
		"group", c.cfg.ConsumerGroup,
		"consumer", c.cfg.ConsumerName,
	)

	for {
		select {
		case <-ctx.Done():
			slog.Info("consumer shutting down")
			return
		default:
			c.processBatch(ctx)
		}
	}
}

// processBatch, stream'den en fazla 10 mesaj okur ve işler.
// BLOCK 2s: mesaj yoksa 2 saniye bekler — CPU'yu boşa harcamaz.
func (c *Consumer) processBatch(ctx context.Context) {
	streams, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    c.cfg.ConsumerGroup,
		Consumer: c.cfg.ConsumerName,
		Streams:  []string{c.cfg.StreamRaw, ">"},
		Count:    10,
		Block:    2 * time.Second,
	}).Result()

	if err != nil {
		// Context iptal edildi — normal shutdown
		if errors.Is(err, context.Canceled) {
			return
		}
		// Mesaj yok (timeout) — normal durum
		if errors.Is(err, redis.Nil) {
			return
		}
		slog.Error("xreadgroup error", "error", err)
		return
	}

	for _, stream := range streams {
		for _, msg := range stream.Messages {
			c.processMessage(ctx, msg)
		}
	}
}

// processMessage, tek bir Redis stream mesajını işler.
// Cheap filter → geçti: jobs:filtered'a yaz → ACK
// Cheap filter → reddetti: sadece ACK (discard)
func (c *Consumer) processMessage(ctx context.Context, msg redis.XMessage) {
	title := stringField(msg.Values, "title")
	company := stringField(msg.Values, "company")
	source := stringField(msg.Values, "source")
	description := stringField(msg.Values, "description")

	// Cheap filter kontrolü
	// Cheap filter kontrolü
	result := c.filter.Check(title, company, description)

	if result.Rejected {
		slog.Debug("cheap filter rejected",
			"title", title,
			"company", company,
			"reason", result.Reason,
		)
		c.metrics.JobsCheapFiltered.Inc()
		c.ack(ctx, msg.ID)
		return
	}

	// jobs:filtered stream'ine yaz
	// Tüm original field'ları koru — downstream consumer'lar bunlara ihtiyaç duyar
	_, err := c.client.XAdd(ctx, &redis.XAddArgs{
		Stream: c.cfg.StreamFiltered,
		Values: msg.Values,
	}).Result()

	if err != nil {
		slog.Error("failed to forward to filtered stream",
			"msg_id", msg.ID,
			"error", err,
		)
		// ACK etme — retry için pending listede kalsın
		return
	}

	c.metrics.JobsIngested.With(map[string]string{"source": source}).Inc()

	slog.Info("forwarded to filtered",
		"title", title,
		"company", company,
		"source", source,
	)

	c.ack(ctx, msg.ID)
}

// ack, işlenen mesajı consumer group'tan siler.
func (c *Consumer) ack(ctx context.Context, msgID string) {
	if err := c.client.XAck(ctx, c.cfg.StreamRaw, c.cfg.ConsumerGroup, msgID).Err(); err != nil {
		slog.Error("xack failed", "msg_id", msgID, "error", err)
	}
}

// stringField, Redis message values'dan güvenli string okur.
func stringField(values map[string]any, key string) string {
	val, ok := values[key]
	if !ok {
		return ""
	}
	s, ok := val.(string)
	if !ok {
		return ""
	}
	return s
}
