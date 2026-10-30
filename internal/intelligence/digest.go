package intelligence

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ulasdursun/job-hunter/internal/notifier"
)

// EveningDigest fetches high-potential unnotified jobs from PostgreSQL and sends an evening summary.
type EveningDigest struct {
	pool     *pgxpool.Pool
	notifier *notifier.Telegram
}

func NewEveningDigest(pool *pgxpool.Pool, n *notifier.Telegram) *EveningDigest {
	return &EveningDigest{
		pool:     pool,
		notifier: n,
	}
}

// Send queries the database for top unnotified opportunities and dispatches Telegram summary.
func (d *EveningDigest) Send(ctx context.Context) {
	if d.notifier == nil || !d.notifier.IsConfigured() {
		return
	}

	rows, err := d.pool.Query(ctx, `
		SELECT id::text, title, company, COALESCE(location, ''), url, match_score::float8, COALESCE(ai_reasoning, '')
		FROM jobs
		WHERE match_score >= 6.8
		  AND notified_at IS NULL
		  AND status = 'new'
		  AND scraped_at >= NOW() - INTERVAL '24 hours'
		ORDER BY match_score DESC, scraped_at DESC
		LIMIT 5
	`)
	if err != nil {
		slog.Error("evening digest query failed", "error", err)
		return
	}
	defer rows.Close()

	type item struct {
		id, title, company, loc, url, reasoning string
		score                                   float64
	}
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.id, &it.title, &it.company, &it.loc, &it.url, &it.score, &it.reasoning); err == nil {
			items = append(items, it)
		}
	}
	if len(items) == 0 {
		slog.Info("evening digest: no new unnotified >=6.8 opportunities in last 24h")
		return
	}

	esc := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	escAttr := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;")
	var sb strings.Builder
	sb.WriteString("🌆 <b>GÜNÜN EN İYİ FIRSATLARI (Akşam Özeti)</b>\n")
	sb.WriteString("<i>Anlık bildirim (≥8.0) altında kalan en güçlü 6.8–7.9 adayları:</i>\n\n")

	var ids []string
	for i, it := range items {
		ids = append(ids, it.id)
		// Telegram mesaj sınırı 4096 karakter: gerekçeyi rune-güvenli kısalt.
		reasoning := []rune(it.reasoning)
		if len(reasoning) > 220 {
			reasoning = append(reasoning[:220], '…')
		}
		sb.WriteString(fmt.Sprintf(
			"<b>%d. [%.1f/10] %s</b> — <i>%s</i> (%s)\n%s\n🔗 <a href=\"%s\">İlana Git</a>\n\n",
			i+1,
			it.score,
			esc.Replace(it.title),
			esc.Replace(it.company),
			esc.Replace(it.loc),
			esc.Replace(string(reasoning)),
			escAttr.Replace(it.url),
		))
	}

	if err := d.notifier.SendRaw(ctx, sb.String()); err != nil {
		slog.Warn("evening digest send failed", "error", err)
		return
	}

	for _, id := range ids {
		_, _ = d.pool.Exec(ctx, `UPDATE jobs SET notified_at = NOW() WHERE id = $1::uuid`, id)
	}
	slog.Info("evening digest sent", "count", len(items))
}
