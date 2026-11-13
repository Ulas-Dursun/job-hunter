package intelligence

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/ulasdursun/job-hunter/internal/config"
	"github.com/ulasdursun/job-hunter/internal/notifier"
)

const (
	queueLagAlertThreshold = 200
	failedDeltaThreshold   = 5
	healthCheckInterval    = 10 * time.Minute
	healthAlertCooldown    = 4 * time.Hour
)

// QueueHealthMonitor checks Redis stream lag and error accumulation.
type QueueHealthMonitor struct {
	redis    *redis.Client
	redisCfg *config.RedisConfig
	notifier *notifier.Telegram
}

func NewQueueHealthMonitor(
	client *redis.Client,
	cfg *config.RedisConfig,
	n *notifier.Telegram,
) *QueueHealthMonitor {
	return &QueueHealthMonitor{
		redis:    client,
		redisCfg: cfg,
		notifier: n,
	}
}

// Run monitors the stream health periodically until context is cancelled.
func (m *QueueHealthMonitor) Run(ctx context.Context) {
	if m.redis == nil || m.redisCfg == nil {
		return
	}

	// Başlangıçtaki mevcut failed sayısını baz al (eski hatalar için açılışta alarm çalma).
	lastFailed, err := m.redis.XLen(ctx, m.redisCfg.StreamFailed).Result()
	if err != nil {
		slog.Warn("health monitor initial failed stream xlen failed", "stream", m.redisCfg.StreamFailed, "error", err)
	}
	var lastLagAlert time.Time

	ticker := time.NewTicker(healthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if m.notifier == nil || !m.notifier.IsConfigured() {
				continue
			}

			rawLag := m.streamLag(ctx, m.redisCfg.StreamRaw)
			filteredLag := m.streamLag(ctx, m.redisCfg.StreamFiltered)

			failedNow, err := m.redis.XLen(ctx, m.redisCfg.StreamFailed).Result()
			var newFailures int64
			if err != nil {
				slog.Warn("health monitor failed stream xlen failed", "stream", m.redisCfg.StreamFailed, "error", err)
			} else {
				newFailures = failedNow - lastFailed
			}

			cooldownElapsed := time.Since(lastLagAlert) >= healthAlertCooldown
			needRawLagAlert := rawLag >= queueLagAlertThreshold && cooldownElapsed
			needFilteredLagAlert := filteredLag >= queueLagAlertThreshold && cooldownElapsed
			needLagAlert := needRawLagAlert || needFilteredLagAlert
			needFailAlert := newFailures >= failedDeltaThreshold

			if !needLagAlert && !needFailAlert {
				continue
			}

			var sb strings.Builder
			sb.WriteString("⚠️ <b>SİSTEM SAĞLIK UYARISI (Job Hunter)</b>\n\n")
			if needRawLagAlert {
				sb.WriteString(fmt.Sprintf("• <b>Kuyruk Birikmesi (%s):</b> <code>%d</code> ilan bekliyor (eşik: %d)\n", m.redisCfg.StreamRaw, rawLag, queueLagAlertThreshold))
			}
			if needFilteredLagAlert {
				sb.WriteString(fmt.Sprintf("• <b>Kuyruk Birikmesi (%s):</b> <code>%d</code> ilan bekliyor (eşik: %d)\n", m.redisCfg.StreamFiltered, filteredLag, queueLagAlertThreshold))
			}
			if needFailAlert {
				sb.WriteString(fmt.Sprintf("• <b>Yeni Hatalı İlan (jobs:failed):</b> <code>+%d</code> yeni hata (toplam: <code>%d</code>)\n", newFailures, failedNow))
			}
			sb.WriteString("\n<i>Not: AI anahtar kotaları dolmuş veya worker yavaşlamış olabilir.</i>")

			if err := m.notifier.SendRaw(ctx, sb.String()); err != nil {
				slog.Warn("health monitor telegram send failed", "error", err)
				continue
			}
			if needLagAlert {
				lastLagAlert = time.Now()
			}
			if needFailAlert {
				lastFailed = failedNow
			}
			slog.Info("queue health alert sent", "raw_lag", rawLag, "filtered_lag", filteredLag, "failed_total", failedNow)
		}
	}
}

func (m *QueueHealthMonitor) streamLag(ctx context.Context, stream string) int64 {
	if stream == "" {
		return 0
	}
	groups, err := m.redis.XInfoGroups(ctx, stream).Result()
	if err != nil {
		slog.Warn("health monitor xinfogroups failed", "stream", stream, "error", err)
		return 0
	}
	for _, g := range groups {
		if g.Name == m.redisCfg.ConsumerGroup {
			return g.Lag + g.Pending
		}
	}
	return 0
}
