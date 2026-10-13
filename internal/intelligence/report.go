package intelligence

import (
	"fmt"
	"strings"
)

// FormatTelegramReport, MarketReport'u HTML formatlı Telegram mesajına dönüştürür.
// telegram.go parse_mode: HTML kullandığı için burada HTML tag'leri kullanılıyor.
func FormatTelegramReport(r *MarketReport) string {
	var sb strings.Builder

	sb.WriteString("📊 <b>Haftalık Market Raporu</b>\n")
	sb.WriteString(fmt.Sprintf("<i>%d günlük analiz — %s</i>\n\n",
		r.PeriodDays,
		r.GeneratedAt.Format("02 Jan 2006"),
	))

	// ── Genel istatistikler ───────────────────────────────────────────────────
	sb.WriteString("<b>📈 Genel Durum</b>\n")
	sb.WriteString(fmt.Sprintf("• Toplam ilan: <b>%d</b>\n", r.TotalJobs))
	sb.WriteString(fmt.Sprintf("• Başvur: <b>%d</b> iş | İzle: <b>%d</b> iş\n\n",
		r.ApplyJobs, r.WatchJobs,
	))

	// ── Güçlü yanlar ─────────────────────────────────────────────────────────
	if len(r.MatchingSkills) > 0 {
		sb.WriteString("<b>✅ CV'nde var, piyasada isteniyor:</b>\n")
		for _, s := range r.MatchingSkills {
			bar := progressBar(s.Frequency, 8)
			sb.WriteString(fmt.Sprintf(
				"  <code>%-16s</code> %s\n",
				capitalize(s.Skill), bar,
			))
		}
		sb.WriteString("\n")
	}

	// ── Gap analizi ───────────────────────────────────────────────────────────
	if len(r.GapSkills) > 0 {
		sb.WriteString("<b>⚠️ Piyasada isteniyor, CV'nde eksik:</b>\n")
		for _, s := range r.GapSkills {
			bar := progressBar(s.Frequency, 8)
			sb.WriteString(fmt.Sprintf(
				"  <code>%-16s</code> %s\n",
				capitalize(s.Skill), bar,
			))
		}
		sb.WriteString("\n")
	}

	// ── Top şirketler ─────────────────────────────────────────────────────────
	if len(r.TopCompanies) > 0 {
		sb.WriteString("<b>🏢 En aktif şirketler:</b>\n")
		for i, c := range r.TopCompanies {
			if i >= 5 {
				break
			}
			// HTML'de & karakteri encode edilmeli
			company := strings.ReplaceAll(c.Company, "&", "&amp;")
			sb.WriteString(fmt.Sprintf(
				"  %d. %s — %d ilan (avg: %.1f)\n",
				i+1, company, c.JobCount, c.AvgScore,
			))
		}
	}

	return sb.String()
}

func progressBar(freq float64, width int) string {
	filled := int(freq * float64(width))
	if filled > width {
		filled = width
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

func capitalize(s string) string {
	if s == "" {
		return s
	}

	knownTerms := map[string]string{
		"go": "Go", "java": "Java", "python": "Python",
		"rust": "Rust", "aws": "AWS", "sql": "SQL",
		"api": "API", "jwt": "JWT", "tcp": "TCP",
		"rest api": "REST API", "docker": "Docker",
		"postgresql": "PostgreSQL", "mongodb": "MongoDB",
		"redis": "Redis", "kubernetes": "Kubernetes",
		"terraform": "Terraform", "prometheus": "Prometheus",
		"grafana": "Grafana", "linux": "Linux",
		"spring boot": "Spring Boot", "grpc": "gRPC",
	}

	if v, ok := knownTerms[strings.ToLower(s)]; ok {
		return v
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
