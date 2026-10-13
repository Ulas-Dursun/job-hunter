package filter

import (
	"strings"

	"github.com/ulasdursun/job-hunter/internal/config"
)

// FilterResult, cheap filter kararını ve gerekçesini tutar.
type FilterResult struct {
	Rejected bool
	Reason   string
}

// CheapFilter, AI'ya gitmeden önce açıkça uygunsuz ilanları eler.
// Tasarım prensibi: Yanlış negatif (iyi ilanı elemek) her zaman
// yanlış pozitiften (kötü ilanı geçirmek) daha kötüdür.
// Şüpheli ilanlar AI'ya gider — bu filter sadece %100 uygunsuzları keser.
type CheapFilter struct {
	blacklist []string // config'den gelir, küçük harfe normalize edilmiş
}

// NewCheapFilter, config'deki blacklist_keywords listesinden filter oluşturur.
func NewCheapFilter(cfg *config.CandidateConfig) *CheapFilter {
	blacklist := make([]string, len(cfg.BlacklistKeywords))
	for i, kw := range cfg.BlacklistKeywords {
		blacklist[i] = strings.ToLower(kw)
	}
	return &CheapFilter{blacklist: blacklist}
}

// Check, ilanı üç katmanda filtreler:
//
//  1. Şirket adı geçerli mi?
//  2. Title'da blacklist keyword var mı?
//  3. Description'da yanlış stack veya aşırı deneyim var mı?
func (f *CheapFilter) Check(title, company, description string) FilterResult {
	// ── Katman 1: Şirket geçerlilik ──────────────────────────────────────────
	// jobspy bazen boş veya "nan" company döndürür — bunlar işlenemez.
	companyLower := strings.ToLower(strings.TrimSpace(company))
	if companyLower == "" || companyLower == "nan" {
		return FilterResult{Rejected: true, Reason: "empty_company"}
	}

	// ── Katman 2: Title blacklist ─────────────────────────────────────────────
	// Config'deki blacklist_keywords title'da aranır.
	// "Senior", "Manager", "Sales", "QA" gibi kesin red flag'lar burada yakalanır.
	titleLower := strings.ToLower(title)
	for _, kw := range f.blacklist {
		if strings.Contains(titleLower, kw) {
			return FilterResult{Rejected: true, Reason: "title_blacklist:" + kw}
		}
	}

	// ── Katman 3: Description kontrolü ───────────────────────────────────────
	// Description'ın ilk 800 karakteri kontrol edilir — requirements genellikle
	// başta gelir. Tüm description'ı taramak false positive riskini artırır.
	// Örnek: "We don't use .NET" dese bile match eder — bu yüzden 800 ile sınırlı.
	checkLen := 800
	if len(description) < checkLen {
		checkLen = len(description)
	}
	descLower := strings.ToLower(description[:checkLen])

	// Stack blacklist — title'da geçmeyip description'da primary requirement
	// olarak geçen yanlış teknoloji stack'leri.
	// Not: "c#" yerine "c# " ve "c#," kullanıyoruz — "C#" içeren Go veya
	// Rust tartışmalarını yanlışlıkla elemememek için.
	stackPatterns := []string{
		".net core",
		".net framework",
		"asp.net",
		"entity framework",
		"c# developer",
		"c# engineer",
		"expertise in c#",
		"proficiency in c#",
		"strong in c#",
		"ruby on rails",
		"laravel",
		"symfony",
		"django only",
	}
	for _, pattern := range stackPatterns {
		if strings.Contains(descLower, pattern) {
			return FilterResult{Rejected: true, Reason: "desc_stack:" + pattern}
		}
	}

	// Deneyim yılı pattern'leri — 5+ yıl isteyen ilanlar new grad için uygun değil.
	// 4+ yıl sınırı bilinçli: 4-5 yıl isteyen bazı ilanlar hâlâ match olabilir,
	// AI bu kararı verecek. 5+ kesin red flag.
	yearPatterns := []string{
		"minimum 5 yıl", "minimum 6 yıl", "minimum 7 yıl",
		"minimum 8 yıl", "minimum 10 yıl",
		"minimum 5 years", "minimum 6 years", "minimum 7 years",
		"minimum 8 years", "minimum 10 years",
		"en az 5 yıl", "en az 6 yıl", "en az 7 yıl", "en az 10 yıl",
		"5+ years", "6+ years", "7+ years", "8+ years", "10+ years",
		"5 years of experience", "6 years of experience",
		"7 years of experience", "10 years of experience",
		"at least 5 years", "at least 6 years", "at least 7 years",
	}
	for _, pattern := range yearPatterns {
		if strings.Contains(descLower, pattern) {
			return FilterResult{Rejected: true, Reason: "exp_req:" + pattern}
		}
	}

	return FilterResult{Rejected: false}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
