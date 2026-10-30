package store

import (
	"testing"
)

func TestSameLocationScope(t *testing.T) {
	tests := []struct {
		name     string
		locA     string
		locB     string
		expected bool
	}{
		{"identical exact", "Amsterdam, Netherlands", "Amsterdam, Netherlands", true},
		{"same country different city (NL)", "Amsterdam, Netherlands", "Rotterdam, Netherlands", true},
		{"same country different city (TR)", "Istanbul, Turkey", "Ankara, Türkiye", true},
		{"same country different city (BE)", "Brussels, Belgium", "Antwerp, België", true},
		{"different country (NL vs DE)", "Amsterdam, Netherlands", "Berlin, Germany", false},
		{"different country (TR vs US)", "Istanbul, Turkey", "New York, USA", false},
		{"empty strings equal", "", "", true},
		{"one empty string", "Amsterdam", "", false},
		{"unrecognized countries match exactly", "Wakanda", "Wakanda", true},
		{"unrecognized countries differ", "Wakanda", "Atlantis", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SameLocationScope(tt.locA, tt.locB)
			if got != tt.expected {
				t.Errorf("SameLocationScope(%q, %q) = %v; want %v", tt.locA, tt.locB, got, tt.expected)
			}
		})
	}
}

func TestIsPoisonedByLanguageOrRegion(t *testing.T) {
	tests := []struct {
		flags    string
		expected bool
	}{
		{"Required fluent German C1", true},
		{"Dutch language barrier required", true},
		{"US On-Site required", true},
		{"Overseas only / APAC timezone", true},
		{"Missing Go, PostgreSQL, Docker", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.flags, func(t *testing.T) {
			got := IsPoisonedByLanguageOrRegion(tt.flags)
			if got != tt.expected {
				t.Errorf("IsPoisonedByLanguageOrRegion(%q) = %v; want %v", tt.flags, got, tt.expected)
			}
		})
	}
}
