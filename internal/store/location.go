package store

import "strings"

// IsPoisonedByLanguageOrRegion, önceki düşük puanlı ilanın yerel dil bariyeri veya denizaşırı (US/APAC)
// konum sebebiyle elenip elenmediğini denetler; böylece aynı şirketin İngilizce/Avrupa ilanı bloklanmaz.
func IsPoisonedByLanguageOrRegion(redFlagsJoined string) bool {
	low := strings.ToLower(redFlagsJoined)
	for _, marker := range []string{
		"language", "german", "deutsch", "dutch", "nederlands", "french", "français", "danish", "dansk",
		"us on-site", "overseas", "apac",
	} {
		if strings.Contains(low, marker) {
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
	scopes := []struct {
		country string
		markers []string
	}{
		{"tr", []string{"turkey", "türkiye", "istanbul", "ankara", "izmir", "bursa", "kocaeli", "antalya"}},
		{"be", []string{"belgium", "belgique", "belgië", "brussels", "bruxelles", "brüksel", "antwerp", "ghent"}},
		{"nl", []string{"netherlands", "hollanda", "amsterdam", "rotterdam", "eindhoven", "utrecht", "the hague"}},
		{"de", []string{"germany", "deutschland", "almanya", "berlin", "munich", "münchen", "frankfurt", "hamburg", "cologne", "stuttgart"}},
		{"dk", []string{"denmark", "danimarka", "copenhagen", "københavn", "kobenhavn", "aarhus", "odense"}},
		{"uk", []string{"united kingdom", "england", "scotland", "london", "manchester", "edinburgh", "cambridge", "bristol"}},
		{"ch", []string{"switzerland", "isviçre", "zurich", "zürich", "geneva", "genève", "basel", "bern"}},
		{"fr", []string{"france", "fransa", "paris", "lyon", "toulouse"}},
		{"us", []string{"united states", "usa", "u.s.", "new york", "san francisco", "seattle", "austin", "boston", "chicago", "california"}},
		{"apac", []string{"singapore", "tokyo", "japan", "australia", "sydney", "melbourne", "india", "bangalore", "bengaluru", "apac"}},
	}
	for _, sc := range scopes {
		for _, m := range sc.markers {
			if strings.Contains(locLower, m) {
				return sc.country
			}
		}
	}
	return ""
}
