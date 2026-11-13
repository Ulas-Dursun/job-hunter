package store

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// IsPoisonedByLanguageOrRegion, önceki düşük puanlı ilanın yerel dil bariyeri veya denizaşırı (US/APAC)
// konum sebebiyle elenip elenmediğini denetler; böylece aynı şirketin İngilizce/Avrupa ilanı bloklanmaz.
// Not: Tek başına "language" kelimesi ("programming language gap" vb.) eşleştirilmez; gerçek dil bariyeri ifadeleri aranır.
func IsPoisonedByLanguageOrRegion(redFlagsJoined string) bool {
	low := strings.ToLower(redFlagsJoined)
	for _, marker := range []string{
		"language barrier", "language requirement", "local language", "spoken language",
		"fluent", "native", "c1", "c2",
		"german", "deutsch", "dutch", "nederlands", "french", "français", "danish", "dansk",
		"us on-site", "overseas", "apac",
	} {
		if containsLocationTerm(low, marker) {
			return true
		}
	}
	return false
}

// SameLocationScope, iki lokasyon metninin aynı ülke/bölge kapsamında olup olmadığını belirler.
// ABD/Asya veya başka bir Avrupa ülkesindeki ilanın farklı ülkedeki (örn. Amsterdam, Brüksel, İstanbul)
// ilanı "düşük puanlı ikiz" olarak zehirlemesini engeller.
func SameLocationScope(locA, locB string) bool {
	a := strings.ToLower(strings.TrimSpace(locA))
	b := strings.ToLower(strings.TrimSpace(locB))
	if a == "" || b == "" {
		return a == b
	}
	if a == b {
		return true
	}
	countryA := extractCountryScope(a)
	countryB := extractCountryScope(b)
	if countryA != "" && countryB != "" {
		return countryA == countryB
	}
	return false
}

func extractCountryScope(locLower string) string {
	// Önce şehir çakışmalarını (London Ontario vs London UK, Cambridge MA vs Cambridge UK) çöz.
	if containsLocationTerm(locLower, "ontario") || containsLocationTerm(locLower, "canada") ||
		containsLocationTerm(locLower, "toronto") || containsLocationTerm(locLower, "vancouver") ||
		containsLocationTerm(locLower, "montreal") || strings.Contains(locLower, "london, on") {
		return "ca"
	}
	if strings.Contains(locLower, "cambridge, ma") || containsLocationTerm(locLower, "massachusetts") {
		return "us"
	}

	scopes := []struct {
		country string
		markers []string
	}{
		{"tr", []string{"turkey", "türkiye", "turkiye", "istanbul", "ankara", "izmir", "bursa", "kocaeli", "antalya"}},
		{"be", []string{"belgium", "belgique", "belgië", "belgie", "brussels", "brussel", "bruxelles", "brüksel", "antwerp", "antwerpen", "ghent", "gent", "leuven", "liège", "liege", "zaventem", "mechelen", "charleroi", "namur", "flanders", "vlaanderen", "wallonia", "louvain-la-neuve"}},
		{"nl", []string{"netherlands", "nederland", "hollanda", "amsterdam", "rotterdam", "eindhoven", "utrecht", "the hague", "delft"}},
		{"de", []string{"germany", "deutschland", "almanya", "berlin", "munich", "münchen", "frankfurt", "hamburg", "cologne", "köln", "stuttgart", "düsseldorf"}},
		{"dk", []string{"denmark", "danimarka", "copenhagen", "københavn", "kobenhavn", "aarhus", "odense"}},
		{"ch", []string{"switzerland", "schweiz", "suisse", "isviçre", "zurich", "zürich", "geneva", "genève", "basel", "bern", "lausanne"}},
		{"at", []string{"austria", "österreich", "avusturya", "vienna", "wien", "graz", "linz"}},
		{"se", []string{"sweden", "sverige", "isveç", "stockholm", "gothenburg", "göteborg", "malmö", "malmo"}},
		{"ie", []string{"ireland", "irlanda", "dublin", "cork", "galway"}},
		{"es", []string{"spain", "españa", "ispanya", "madrid", "barcelona", "valencia", "malaga"}},
		{"pl", []string{"poland", "polska", "polonya", "warsaw", "warszawa", "krakow", "kraków", "wroclaw", "gdansk"}},
		{"fr", []string{"france", "fransa", "paris", "lyon", "toulouse", "lille"}},
		{"it", []string{"italy", "italia", "italya", "milan", "milano", "rome", "roma", "turin", "torino"}},
		{"pt", []string{"portugal", "portekiz", "lisbon", "lisboa", "porto"}},
		{"fi", []string{"finland", "finlandiya", "helsinki", "espoo", "tampere"}},
		{"no", []string{"norway", "norveç", "oslo", "bergen", "trondheim"}},
		{"cz", []string{"czechia", "czech republic", "prague", "praha", "brno"}},
		{"lu", []string{"luxembourg", "lüksemburg"}},
		{"us", []string{"united states", "usa", "u.s.", "u.s.a.", "new york", "san francisco", "seattle", "austin", "boston", "chicago", "california", "indiana", "indianapolis", "texas"}},
		{"uk", []string{"united kingdom", "england", "scotland", "london", "manchester", "edinburgh", "cambridge", "bristol", "oxford"}},
		{"apac", []string{"singapore", "tokyo", "japan", "australia", "sydney", "melbourne", "india", "bangalore", "bengaluru", "hyderabad", "apac"}},
	}
	for _, sc := range scopes {
		for _, m := range sc.markers {
			if containsLocationTerm(locLower, m) {
				return sc.country
			}
		}
	}
	return ""
}

// containsLocationTerm, term ifadesinin s içinde tam kelime sınırıyla geçip geçmediğini kontrol eder.
// Örneğin "usa" arandığında "lausanne" eşleşmez; "india" arandığında "indiana" eşleşmez.
func containsLocationTerm(s, term string) bool {
	if len(term) == 0 {
		return false
	}
	start := 0
	for {
		idx := strings.Index(s[start:], term)
		if idx == -1 {
			return false
		}
		actual := start + idx
		end := actual + len(term)

		beforeOk := true
		if actual > 0 {
			r, _ := utf8.DecodeLastRuneInString(s[:actual])
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				beforeOk = false
			}
		}

		afterOk := true
		if end < len(s) {
			r, _ := utf8.DecodeRuneInString(s[end:])
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				afterOk = false
			}
		}

		if beforeOk && afterOk {
			return true
		}
		start = actual + 1
		if start >= len(s) {
			break
		}
	}
	return false
}
