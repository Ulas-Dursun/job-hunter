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

// Notify, yüksek skorlu bir ilan için Telegram mesajı gönderir.
func (t *Telegram) Notify(
	ctx context.Context,
	title, company, location, url string,
	score *analyzer.ScoreResult,
) error {
	if !t.IsConfigured() {
		return nil
	}

	msg := t.formatMessage(title, company, location, url, score)
	return t.send(ctx, msg)
}

// formatMessage, ilanı okunabilir Telegram mesajına dönüştürür.
func (t *Telegram) formatMessage(
	title, company, location, url string,
	score *analyzer.ScoreResult,
) string {
	emoji := "👀"
	if score.MatchScore >= 8.0 {
		emoji = "🔥"
	} else if score.MatchScore >= 7.0 {
		emoji = "✅"
	}

	overlap := "—"
	if len(score.SkillOverlap) > 0 {
		overlap = strings.Join(score.SkillOverlap, ", ")
	}

	missing := "—"
	if len(score.MissingSkills) > 0 {
		missing = strings.Join(score.MissingSkills, ", ")
	}

	flags := ""
	if len(score.RedFlags) > 0 {
		flags = fmt.Sprintf("\n⚠️ <b>Red flags:</b> %s", strings.Join(score.RedFlags, ", "))
	}

	// 💥 KESİN ÇÖZÜM: Telegram HTML parser'ın patlamaması için her şeyi temizliyoruz
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
	)

	cleanCompany := replacer.Replace(company)
	cleanTitle := replacer.Replace(title)
	cleanLocation := replacer.Replace(location)
	cleanReasoning := replacer.Replace(score.Reasoning)
	cleanOverlap := replacer.Replace(overlap)
	cleanMissing := replacer.Replace(missing)
	cleanFlags := replacer.Replace(flags)

	// URL içindeki & karakterlerini de Telegram'ın istediği formata çeviriyoruz
	cleanURL := strings.ReplaceAll(url, "&", "&amp;")

	return fmt.Sprintf(
		"%s <b>Yeni Uygun İlan</b>\n\n"+
			"🏢 <b>%s</b>\n"+
			"📌 %s\n"+
			"📍 %s\n\n"+
			"🎯 <b>Match Score: %.1f/10</b>\n"+
			"✨ <b>Eşleşen:</b> %s\n"+
			"📚 <b>Eksik:</b> %s%s\n\n"+
			"💡 <i>%s</i>\n\n"+
			"🔗 <a href=\"%s\">İlana Git</a>",
		emoji,
		cleanCompany,
		cleanTitle,
		cleanLocation,
		score.MatchScore,
		cleanOverlap,
		cleanMissing,
		cleanFlags,
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
