package filter

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// maxAllowedMinYears: ilanın istediği asgari deneyim bu değerin üstündeyse (>=) AI'ya gitmeden elenir.
// Aday 0-2 yıl bandında; "3+ yıl" sınırda olduğu için AI'ya bırakılır, 4+ kesin red.
const maxAllowedMinYears = 4

var (
	// "5 years of experience", "4+ yrs ... experience", "3-5 years experience" (alt sınır alınır)
	reYearsThenExp = regexp.MustCompile(`(\d{1,2})\s*\+?\s*(?:[-–]\s*\d{1,2}\s*)?(?:years?|yrs?|jahre[n]?|jaar|yıl|ans|års?|år)\b.{0,40}?(?:experience|erfahrung|ervaring|deneyim|expérience|erfaring)`)
	// "experience of 4+ years", "Berufserfahrung von mindestens 5 Jahren"
	reExpThenYears = regexp.MustCompile(`(?:experience|erfahrung|ervaring|deneyim|expérience|erfaring)\D{0,30}(\d{1,2})\s*\+?\s*(?:years?|yrs?|jahre[n]?|jaar|yıl|ans|års?|år)\b`)
	// "at least 5 years", "minimum of 6 years", "mindestens 4 Jahre", "en az 5 yıl"
	reMinYears = regexp.MustCompile(`(?:at least|minimum(?: of)?|min\.|mindestens|en az|minstens|au moins|mindst)\s*(\d{1,2})\s*(?:years?|yrs?|jahre[n]?|jaar|yıl|ans|års?|år)\b`)
)

// companyClaimPrefixes: sayının öncesindeki bağlamda geçerse ifade şirket/ekip tanıtımıdır
// ("over 20 years of experience", "our team has 10 years of experience", "work with seniors having 5+ years"),
// adaydan istenen asgari şart değildir.
var companyClaimPrefixes = []string{
	"over ", "more than ", "nearly ", "almost ", "meer dan ", "plus de ", "mehr als ", "über ", "since ", "for ",
	"our team", "the team", "team has", "team with", "team of", "we have", "we bring", "company has", "company with",
	"with seniors", "senior engineers", "seniors having", "seniors with", "mentors with", "colleagues with",
	"leaders with", "experts with", "combined ", "collective ", "founded ", "established ", "backed by",
}

// minYearsRequired, açıklamada adaydan istenen asgari deneyim şartlarının en küçüğünü (alt sınır olarak) döndürür; yoksa 0.
// Örneğin ilanda hem Junior ("1-2 years") hem Mid ("4-6 years") kademesi geçiyorsa veya ekip kıdemi ("seniors with 5+ years")
// anlatılıyorsa, en küçük geçerli aday şartını (1) baz alır. 13+ yıl ve şirket/ekip tanıtımı kalıpları yok sayılır.
func minYearsRequired(descLower string) int {
	minFound := 0
	found := false
	for _, re := range []*regexp.Regexp{reYearsThenExp, reExpThenYears, reMinYears} {
		for _, loc := range re.FindAllStringSubmatchIndex(descLower, -1) {
			n, err := strconv.Atoi(descLower[loc[2]:loc[3]])
			if err != nil || n < 0 || n > 12 {
				continue
			}
			from := loc[0] - 55
			if from < 0 {
				from = 0
			}
			contextBefore := descLower[from:loc[2]]
			claim := false
			for _, p := range companyClaimPrefixes {
				if strings.Contains(contextBefore, p) {
					claim = true
					break
				}
			}
			if claim {
				continue
			}
			if !found || n < minFound {
				minFound = n
				found = true
			}
		}
	}
	if !found {
		return 0
	}
	return minFound
}

// languageBarrierPatterns: aday İngilizce (C1/C2) + Türkçe (ana dil) + Almanca (başlangıç) bilir.
// Almanca/Felemenkçe/Fransızca'yı zorunlu kılan ifadeler (aşağıdaki yumuşatıcı kelimeler yoksa) red sebebidir.
var languageBarrierPatterns = []string{
	"fluent german", "fluent in german", "german fluent", "fluent dutch", "fluent in dutch", "dutch fluent",
	"fluent french", "fluent in french", "french fluent", "native german", "german native", "native dutch",
	"native french", "german (c1", "german c1", "german c2", "german (c2", "dutch (c1", "dutch c1",
	"verhandlungssicher", "fließend deutsch", "fliessend deutsch", "fließende deutschkenntnisse",
	"sehr gute deutschkenntnisse", "gute deutschkenntnisse", "deutsch c1", "deutsch c2", "muttersprache deutsch",
	"vloeiend nederlands", "nederlands vereist", "goede beheersing van het nederlands",
	"uitstekende beheersing van het nederlands", "français courant", "maîtrise du français",
	"fluent danish", "fluent in danish", "danish fluent", "native danish", "flydende dansk", "dansk i skrift og tale",
}

// languageSoftWords: bariyer ifadesinin yakınında geçerse dil şartı "artı/tercih" sayılır ve ilan elenmez.
// Not: "english/englisch/engels/anglais" burada YER ALMAZ — çünkü "Fluent German and English required"
// gibi çift dilli zorunlu ilanlarda "english" kelimesi bariyeri yanlışlıkla devre dışı bırakır.
var languageSoftWords = []string{
	"plus", "advantage", "nice to have", "nice-to-have", "preferred", "desirable", "bonus", "beneficial",
	"optional", "not required", "nicht erforderlich", "niet vereist",
	"von vorteil", "wünschenswert", "vorteilhaft",
}

// hasLanguageBarrier, Almanca/Felemenkçe/Fransızca zorunluluğu bildiren bir ifade bulursa (ifade) döndürür.
func hasLanguageBarrier(descLower string) (string, bool) {
	for _, p := range languageBarrierPatterns {
		from := 0
		for {
			i := strings.Index(descLower[from:], p)
			if i < 0 {
				break
			}
			idx := from + i
			lo := idx - 60
			if lo < 0 {
				lo = 0
			}
			hi := idx + len(p) + 80
			if hi > len(descLower) {
				hi = len(descLower)
			}
			// Rune sınırlarına hizala (UTF-8 ortasından kesme).
			for lo > 0 && lo < len(descLower) && !isRuneStart(descLower[lo]) {
				lo--
			}
			for hi < len(descLower) && !isRuneStart(descLower[hi]) {
				hi++
			}
			window := descLower[lo:hi]
			soft := false
			for _, w := range languageSoftWords {
				if strings.Contains(window, w) {
					soft = true
					break
				}
			}
			if !soft {
				return p, true
			}
			from = idx + len(p)
		}
	}
	return "", false
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

var (
	stopEN = toSet("the", "and", "we", "with", "for", "you", "are", "our", "will", "to", "of", "in", "your", "is", "have", "be")
	stopDE = toSet("und", "der", "die", "das", "wir", "mit", "für", "sie", "ein", "eine", "den", "im", "von", "zu", "auf", "ist", "sind", "werden", "du", "deine", "bei", "unser", "unsere", "dich", "uns", "oder", "als")
	stopNL = toSet("het", "een", "wij", "voor", "met", "van", "je", "jij", "ons", "bij", "zijn", "naar", "ook", "jouw", "wat", "dat", "worden", "onze", "kun")
	stopFR = toSet("les", "des", "nous", "vous", "pour", "avec", "une", "est", "dans", "sur", "que", "du", "au", "aux", "votre", "notre", "vos", "nos")
	stopDA = toSet("og", "det", "til", "med", "på", "vi", "du", "som", "har", "kan", "skal", "din", "vores", "hos", "eller", "fra", "vil")
)

func toSet(words ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(words))
	for _, w := range words {
		m[w] = struct{}{}
	}
	return m
}

// englishFriendlyPhrases: ilan metni yerel dilde olsa bile İngilizce çalışmanın açıkça kabul edildiğini gösterir.
var englishFriendlyPhrases = []string{
	"english-speaking", "english speaking", "working language is english", "company language is english",
	"dutch is not required", "dutch not required", "no dutch required", "german is not required",
	"german not required", "no german required", "danish is not required", "danish not required", "no danish required",
	"arbeitssprache ist englisch", "voertaal is engels",
}

// nonEnglishPosting, ilan metni ağırlıklı olarak Almanca/Felemenkçe/Fransızca/Danca yazılmışsa (dil kodu) döndürür.
// İki dilli ilanlarda İngilizce stop-word sayısı da yüksek olacağından oran koşulu yanlış pozitifi önler.
func nonEnglishPosting(descLower string) (string, bool) {
	for _, ok := range englishFriendlyPhrases {
		if strings.Contains(descLower, ok) {
			return "", false
		}
	}
	words := strings.FieldsFunc(descLower, func(r rune) bool { return !unicode.IsLetter(r) })
	if len(words) < 60 {
		return "", false
	}
	var en, de, nl, fr, da int
	for _, w := range words {
		if _, ok := stopEN[w]; ok {
			en++
		}
		if _, ok := stopDE[w]; ok {
			de++
		}
		if _, ok := stopNL[w]; ok {
			nl++
		}
		if _, ok := stopFR[w]; ok {
			fr++
		}
		if _, ok := stopDA[w]; ok {
			da++
		}
	}
	switch {
	case de >= 12 && de > 2*en:
		return "de", true
	case nl >= 10 && nl > 2*en:
		return "nl", true
	case fr >= 12 && fr > 2*en:
		return "fr", true
	case da >= 10 && da > 2*en:
		return "da", true
	}
	return "", false
}
