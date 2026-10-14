package filter

import (
	"strings"
	"unicode"
	"unicode/utf8"

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

var techTitleKeywords = []string{
	"developer", "engineer", "software", "backend", "frontend", "fullstack",
	"programmer", "architect", "geliştirici", "mühendis", "yazılım",
	"devops", "cloud", "platform engineer", "sre", "site reliability",
	"infrastructure", "sysadmin", "systems engineer",
	"data engineer", "data analyst", "database administrator", "database engineer",
	"golang", "python", "java developer", "java engineer", "rust", "c++",
	"cns", "avionics", "atm", "asterix", "surveillance",
	"genç yetenek", "mt programı", "yazılım uzman yardımcısı",
	"security engineer", "cybersecurity", "qa engineer", "test engineer",
}

// Check, ilanı dört katmanda filtreler:
//
//  1. Şirket adı geçerli mi?
//  2. Pozisyon bir teknoloji/yazılım/mühendislik rolü mü?
//  3. Title'da blacklist keyword var mı? (EUROCONTROL stajları hariç)
//  4. Description'da yanlış stack veya aşırı deneyim var mı?
func (f *CheapFilter) Check(title, company, description string, source ...string) FilterResult {
	sourceName := ""
	if len(source) > 0 {
		sourceName = strings.ToLower(strings.TrimSpace(source[0]))
	}

	// ── Katman 1: Şirket geçerlilik ──────────────────────────────────────────
	companyLower := strings.ToLower(strings.TrimSpace(company))
	if companyLower == "" || companyLower == "nan" {
		return FilterResult{Rejected: true, Reason: "empty_company"}
	}

	// ── Katman 2: Pozitif Teknoloji/Mühendislik Uygunluğu ────────────────────
	// Tıbbi, idari, satış veya tesisat gibi alakasız rolleri AI'ya göndermeden kes.
	titleLower := strings.ToLower(title)
	if sourceName != "eurocontrol" && !hasAnyTechKeyword(titleLower) {
		return FilterResult{Rejected: true, Reason: "non_tech_role"}
	}

	// ── Katman 3: Title blacklist ─────────────────────────────────────────────
	// Config'deki blacklist_keywords title'da aranır.
	// EUROCONTROL resmi staj pozisyonları Kural 2 (staj reddi) istisnasıdır.
	for _, kw := range f.blacklist {
		if sourceName == "eurocontrol" && isInternshipKeyword(kw) {
			continue
		}
		if containsWord(titleLower, kw) {
			return FilterResult{Rejected: true, Reason: "title_blacklist:" + kw}
		}
	}

	// ── Katman 3: Description kontrolü ───────────────────────────────────────
	// Şirket tanıtım metinleri 500-1000 karakter sürebildiği için tarama derinliği
	// 2500 karakter ile genişletildi (gereksinimler genelde ortada gelir).
	checkLen := 2500
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

// containsWord, s metninde word kelimesinin/ifadesinin tam kelime sınırı ile geçip geçmediğini
// Unicode (Türkçe ve İngilizce) karakter uyumlu olarak denetler.
// Örneğin: "intern" arandığında "international" eşleşmez, ama "Software Engineer Intern" eşleşir.
func containsWord(s, word string) bool {
	if len(word) == 0 {
		return false
	}
	start := 0
	for {
		idx := strings.Index(s[start:], word)
		if idx == -1 {
			return false
		}
		actualIdx := start + idx
		endIdx := actualIdx + len(word)

		// Önceki karakter harf/rakam olmamalı
		beforeOk := true
		if actualIdx > 0 {
			r, _ := utf8.DecodeLastRuneInString(s[:actualIdx])
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				beforeOk = false
			}
		}

		// Sonraki karakter harf/rakam olmamalı
		afterOk := true
		if endIdx < len(s) {
			r, _ := utf8.DecodeRuneInString(s[endIdx:])
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				afterOk = false
			}
		}

		if beforeOk && afterOk {
			return true
		}

		start = actualIdx + 1
		if start >= len(s) {
			break
		}
	}
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func hasAnyTechKeyword(titleLower string) bool {
	for _, kw := range techTitleKeywords {
		if containsWord(titleLower, kw) {
			return true
		}
	}
	return false
}

func isInternshipKeyword(kw string) bool {
	switch strings.ToLower(kw) {
	case "intern", "internship", "stajyer", "staj", "working student", "werkstudent", "part-time intern", "long-term intern":
		return true
	default:
		return false
	}
}
