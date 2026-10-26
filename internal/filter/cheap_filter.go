package filter

import (
	"strconv"
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

var defaultBlacklistKeywords = []string{
	"stage", "stajyeri", "stajı", "team leader", "müdür", "müdürü", "yöneticisi", "ürün yöneticisi",
	"product manager", "project manager", "scrum master", "account management",
	"customer success", "fincrime", "financial crime", "risk analyst", "operational risk",
	"credit risk", "deal intelligence", "cost analyst", "strategic finance",
	"investor relations", "workday", "field engineer", "dijital pazarlama",
}

// NewCheapFilter, config'deki blacklist_keywords listesinden ve yerleşik kurumsal/kıdem redlerinden filter oluşturur.
func NewCheapFilter(cfg *config.CandidateConfig) *CheapFilter {
	seen := make(map[string]struct{}, len(cfg.BlacklistKeywords)+len(defaultBlacklistKeywords))
	blacklist := make([]string, 0, len(cfg.BlacklistKeywords)+len(defaultBlacklistKeywords))
	for _, kw := range cfg.BlacklistKeywords {
		k := strings.ToLower(strings.TrimSpace(kw))
		if k != "" {
			if _, ok := seen[k]; !ok {
				seen[k] = struct{}{}
				blacklist = append(blacklist, k)
			}
		}
	}
	for _, kw := range defaultBlacklistKeywords {
		if _, ok := seen[kw]; !ok {
			seen[kw] = struct{}{}
			blacklist = append(blacklist, kw)
		}
	}
	return &CheapFilter{blacklist: blacklist}
}

var techTitleKeywords = []string{
	"developer", "engineer", "software", "backend", "frontend", "fullstack", "full-stack", "full stack",
	"programmer", "architect", "geliştirici", "mühendis", "yazılım", "bilişim",
	"devops", "cloud", "platform", "sre", "site reliability",
	"infrastructure", "sysadmin", "systems",
	"data", "data analyst", "analytics", "business intelligence", "bi analyst", "bi developer",
	"technical analyst", "systems analyst", "security analyst", "soc analyst", "database", "dba",
	"golang", "python", "java", "rust", "c++", "sql",
	"cns", "avionics", "atm", "asterix", "surveillance",
	"genç yetenek", "mt programı", "management trainee", "uzman yardımcısı",
	"security", "cybersecurity", "siber güvenlik",
	// ML / Data / AI ve geniş teknoloji/çözüm rolleri (yazılım mühendisi geniş yelpazede başvurabilir)
	"scientist", "machine learning", "ml", "mlops", "ai",
	"artificial intelligence", "veri", "veri analisti", "veri bilimci", "iş zekası", "sistem analisti", "yapay zeka",
	"solutions engineer", "integration", "automation",
}

// entrySignals, ilanın giriş seviyesi (new grad / junior / 0-2 yıl) olduğunu gösteren ifadelerdir.
// Başlıkta veya açıklamanın ilk bölümünde bu sinyal varsa ve ilan bariz kara listede değilse,
// yumuşak filtre (soft pass) devreye girer ve ilanı AI değerlendirmesine geçirir.
var entrySignals = []string{
	"graduate", "new grad", "entry level", "entry-level", "junior", "trainee", "associate",
	"yeni mezun", "genç yetenek", "mt programı", "management trainee", "uzman yardımcısı",
	"0-2 years", "0-1 years", "0-2 yıl", "0-1 yıl", "1-2 years", "1-3 years", "early career", "young graduate",
}

// techDescTerms, açıklamanın gerçekten yazılım/veri işi olduğunu kanıtlayan somut araç/teknoloji terimleridir.
var techDescTerms = []string{
	"python", "java", "golang", "sql", "backend", "software", "api", "rest", "docker", "kubernetes",
	"machine learning", "aws", "linux", "git", "yazılım", "database", "postgresql", "redis", "mongodb",
	"c++", "rust", "microservices", "pipeline", "etl", "pandas", "spark", "power bi", "tableau",
	"computer science", "software engineering", "bilgisayar mühendisliği", "yazılım mühendisliği",
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

	// ── Katman 2: Pozitif Teknoloji/Analitik Uygunluğu + Yumuşak 0-2 Yıl Kapısı ──
	// Tıbbi, idari, satış veya tesisat gibi alakasız rolleri AI'ya göndermeden kes.
	// Ancak başlıkta VEYA açıklamada 0-2 yıl / new grad / junior sinyali varsa ve açıklamada
	// en az 1 teknik/yazılım/veri sinyali geçiyorsa yumuşak filtre ile geçir (AI değerlendirsin).
	titleLower := strings.ToLower(title)
	descPreview := strings.ToLower(description[:min(len(description), 2500)])
	if sourceName != "eurocontrol" && !hasAnyTechKeyword(titleLower) {
		softEntryPass := (hasEntrySignal(titleLower) || hasEntrySignal(descPreview)) && countTechTerms(descPreview) >= 1
		if !softEntryPass {
			return FilterResult{Rejected: true, Reason: "non_tech_role"}
		}
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
	// Çok baytlı (Türkçe/Almanca) karakteri ortadan kesme.
	for checkLen > 0 && checkLen < len(description) && !utf8.RuneStart(description[checkLen]) {
		checkLen--
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

	// ── Katman 4: Deterministik dil / kıdem kapıları (AI kotası koruma) ─────────
	// Tüm açıklama taranır (gereksinimler sıklıkla 2500. karakterden sonra gelir).
	fullLower := strings.ToLower(description)

	if sourceName != "eurocontrol" {
		if p, ok := hasLanguageBarrier(fullLower); ok {
			return FilterResult{Rejected: true, Reason: "language_barrier:" + p}
		}
		if lang, ok := nonEnglishPosting(fullLower); ok {
			return FilterResult{Rejected: true, Reason: "non_english_posting:" + lang}
		}
	}

	// Başlıkta açık giriş seviyesi sinyali (junior/graduate/trainee...) varsa yıl şartı AI'ya bırakılır.
	if !hasEntrySignal(titleLower) {
		if n := minYearsRequired(fullLower); n >= maxAllowedMinYears {
			return FilterResult{Rejected: true, Reason: "exp_req_years:" + strconv.Itoa(n)}
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

func hasEntrySignal(titleLower string) bool {
	for _, kw := range entrySignals {
		if containsWord(titleLower, kw) {
			return true
		}
	}
	return false
}

func countTechTerms(descLower string) int {
	n := 0
	for _, kw := range techDescTerms {
		if containsWord(descLower, kw) {
			n++
		}
	}
	return n
}

func isInternshipKeyword(kw string) bool {
	switch strings.ToLower(kw) {
	case "intern", "internship", "stajyer", "stajyeri", "staj", "stajı", "working student", "werkstudent", "part-time intern", "long-term intern",
		"praktikum", "praktikant", "praktikantin", "stagiair", "stagiaire", "stage":
		return true
	default:
		return false
	}
}
