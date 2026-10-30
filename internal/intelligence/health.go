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
	lastFailed, _ := m.redis.XLen(ctx, m.redisCfg.StreamFailed).Result()
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

			var lag int64
			if groups, err := m.redis.XInfoGroups(ctx, m.redisCfg.StreamFiltered).Result(); err == nil {
				for _, g := range groups {
					if g.Name == m.redisCfg.ConsumerGroup {
						lag = g.Lag + g.Pending
						break
					}
				}
			}
			failedNow, _ := m.redis.XLen(ctx, m.redisCfg.StreamFailed).Result()
			newFailures := failedNow - lastFailed

			needLagAlert := lag >= queueLagAlertThreshold && time.Since(lastLagAlert) >= healthAlertCooldown
			needFailAlert := newFailures >= failedDeltaThreshold

			if !needLagAlert && !needFailAlert {
				continue
			}

			var sb strings.Builder
			sb.WriteString("⚠️ <b>SİSTEM SAĞLIK UYARISI (Job Hunter)</b>\n\n")
			if needLagAlert {
				sb.WriteString(fmt.Sprintf("• <b>Kuyruk Birikmesi (jobs:filtered):</b> <code>%d</code> ilan bekliyor (eşik: %d)\n", lag, queueLagAlertThreshold))
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
			slog.Info("queue health alert sent", "lag", lag, "failed_total", failedNow)
		}
	}
}
