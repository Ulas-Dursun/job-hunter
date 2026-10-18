package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ulasdursun/job-hunter/internal/analyzer"
)

// Telegram, Bot API üzerinden mesaj gönderir.
type Telegram struct {
	token  string
	chatID string
	client *http.Client
}

func NewTelegram() *Telegram {
	return &Telegram{
		token:  os.Getenv("TELEGRAM_BOT_TOKEN"),
		chatID: os.Getenv("TELEGRAM_CHAT_ID"),
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// IsConfigured, token ve chat ID'nin set edilip edilmediğini kontrol eder.
// Her ikisi de set edilmemişse bildirim gönderilmez — hata değil, opsiyonel özellik.
func (t *Telegram) IsConfigured() bool {
	return t.token != "" && t.chatID != ""
}

// Notify, yüksek skorlu bir ilan için zenginleştirilmiş Telegram mesajı gönderir.
func (t *Telegram) Notify(
	ctx context.Context,
	title, company, location, url, source string,
	score *analyzer.ScoreResult,
) error {
	if !t.IsConfigured() {
		return nil
	}

	msg := t.formatMessage(title, company, location, url, source, score)
	return t.send(ctx, msg)
}

func formatSourceLabel(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "greenhouse":
		return "Greenhouse ATS"
	case "lever":
		return "Lever ATS"
	case "smartrecruiters":
		return "SmartRecruiters"
	case "linkedin":
		return "LinkedIn"
	case "indeed":
		return "Indeed"
	case "arbeitnow":
		return "Arbeitnow EU"
	case "techcareer":
		return "techcareer.net"
	case "eurocontrol":
		return "EUROCONTROL"
	default:
		if source == "" {
			return "Job Board"
		}
		return source
	}
}

// formatMessage, ilanı zengin ve okunabilir Telegram HTML kartına dönüştürür.
func (t *Telegram) formatMessage(
	title, company, location, url, source string,
	score *analyzer.ScoreResult,
) string {
	headerBadge := "✅ <b>YÜKSEK UYUMLU İLAN (≥ 7.5)</b>"
	if score.MatchScore >= 8.5 {
		headerBadge = "🔥 <b>HEDEF FIRSAT — KESİN BAŞVUR!</b>"
	} else if score.MatchScore >= 8.0 {
		headerBadge = "🚀 <b>ÇOK GÜÇLÜ EŞLEŞME</b>"
	}

	overlap := "—"
	if len(score.SkillOverlap) > 0 {
		overlap = "✓ " + strings.Join(score.SkillOverlap, " • ✓ ")
	}

	missing := "Tespit edilmedi"
	if len(score.MissingSkills) > 0 {
		missing = "⚡ " + strings.Join(score.MissingSkills, " • ⚡ ")
	}

	roleTag := "Backend / Core"
	if score.RoleFamily == "secondary" {
		roleTag = "Data / Secondary"
	}

	// Telegram HTML parser güvenliği için özel karakterleri escape et
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
	)

	cleanCompany := replacer.Replace(company)
	cleanTitle := replacer.Replace(title)
	cleanLocation := replacer.Replace(location)
	if cleanLocation == "" {
		cleanLocation = "Belirtilmemiş / Remote"
	}
	cleanSource := replacer.Replace(formatSourceLabel(source))
	cleanReasoning := replacer.Replace(score.Reasoning)
	cleanOverlap := replacer.Replace(overlap)
	cleanMissing := replacer.Replace(missing)

	flagsLine := ""
	if len(score.RedFlags) > 0 {
		cleanFlags := replacer.Replace(strings.Join(score.RedFlags, ", "))
		flagsLine = fmt.Sprintf("\n⚠️ <b>Dikkat / Red Flags:</b> %s", cleanFlags)
	}

	cleanURL := strings.ReplaceAll(url, "&", "&amp;")

	return fmt.Sprintf(
		"%s\n"+
			"━━━━━━━━━━━━━━━━━━━━━━\n"+
			"🏢 <b>%s</b>  <i>(%s)</i>\n"+
			"💼 <b>%s</b>\n"+
			"📍 %s  |  🏷️ <code>%s</code>\n\n"+
			"🎯 <b>AI Uyum Skoru: %.1f / 10</b>\n"+
			"🟢 <b>Eşleşen Beceriler:</b> %s\n"+
			"🟡 <b>Eksik Beceriler:</b> %s%s\n\n"+
			"💡 <b>AI Değerlendirmesi:</b>\n<i>“%s”</i>\n"+
			"━━━━━━━━━━━━━━━━━━━━━━\n"+
			"🔗 <a href=\"%s\"><b>İlana Git &amp; Başvur ↗</b></a>\n"+
			"✨ <i>Dashboard (localhost:8080) üzerinden tek tıkla <b>ATS CV, Ön Yazı &amp; Mülakat Kiti</b> üretebilirsin.</i>",
		headerBadge,
		cleanCompany,
		cleanSource,
		cleanTitle,
		cleanLocation,
		roleTag,
		score.MatchScore,
		cleanOverlap,
		cleanMissing,
		flagsLine,
		cleanReasoning,
		cleanURL,
	)
}

// 2. send fonksiyonundaki parse_mode değerini "HTML" yap
func (t *Telegram) send(ctx context.Context, text string) error {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.token)

	payload := map[string]any{
		"chat_id":                  t.chatID,
		"text":                     text,
		"parse_mode":               "HTML", // <--- Markdown yerine HTML yaptık
		"disable_web_page_preview": false,
	}

	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating telegram request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("telegram send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("telegram returned status %d", resp.StatusCode)
	}

	slog.Info("telegram notification sent")
	return nil
}

// SendRaw, herhangi bir formatlama yapmadan (veya dışarıdan formatlanmış)
// ham mesajı Telegram'a gönderir.
func (t *Telegram) SendRaw(ctx context.Context, message string) error {
	// Önce bot token ve chat id ayarlanmış mı diye kontrol edelim (güvenlik)
	if !t.IsConfigured() {
		return nil
	}

	// Gerçekten mesajı göndermek için dosyadaki mevcut 'send' fonksiyonunu çağırıyoruz
	return t.send(ctx, message)
}

// DailyDigestItem, günlük özette listelenecek ilan özet bilgisini tutar.
type DailyDigestItem struct {
	Title   string
	Company string
	Score   float64
	URL     string
}

// SendDailyDigest, günlük tarama tamamlandığında tek bir toplu özet mesajı gönderir.
func (t *Telegram) SendDailyDigest(
	ctx context.Context,
	totalScraped, applyCount, watchCount, filteredCount int,
	topJobs []DailyDigestItem,
) error {
	if !t.IsConfigured() {
		return nil
	}

	dateStr := time.Now().Format("02.01.2006")
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("📊 <b>Günün İlan Özeti (%s)</b>\n\n", dateStr))
	sb.WriteString(fmt.Sprintf("🔍 <b>Taranan:</b> %d ilan\n", totalScraped))
	sb.WriteString(fmt.Sprintf("✅ <b>Başvuruya Uygun (Apply):</b> %d\n", applyCount))
	sb.WriteString(fmt.Sprintf("👀 <b>İncelemeye Değer (Watch):</b> %d\n", watchCount))
	sb.WriteString(fmt.Sprintf("🗑️ <b>Elenen (Filtre/Düşük):</b> %d\n\n", filteredCount))

	if len(topJobs) > 0 {
		sb.WriteString("🔥 <b>Öne Çıkan Fırsatlar:</b>\n")
		for i, job := range topJobs {
			cleanComp := strings.ReplaceAll(strings.ReplaceAll(job.Company, "&", "&amp;"), "<", "&lt;")
			cleanTitle := strings.ReplaceAll(strings.ReplaceAll(job.Title, "&", "&amp;"), "<", "&lt;")
			cleanURL := strings.ReplaceAll(job.URL, "&", "&amp;")

			sb.WriteString(fmt.Sprintf(
				"%d. <b>%s</b> — %s (🎯 <b>%.1f/10</b>)\n   🔗 <a href=\"%s\">İlana Git</a>\n",
				i+1, cleanComp, cleanTitle, job.Score, cleanURL,
			))
		}
	} else {
		sb.WriteString("<i>Bugün yüksek eşleşen yeni ilan bulunamadı. Filtreler arka planda çalışmaya devam ediyor.</i>\n")
	}

	sb.WriteString("\n💻 <i>Tüm detayları görmek ve başvuru durumunu yönetmek için dashboard: http://localhost:8080</i>")

	return t.send(ctx, sb.String())
}
