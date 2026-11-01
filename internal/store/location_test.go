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
		{"Lausanne is CH not USA", "Lausanne, Switzerland", "Zurich, Switzerland", true},
		{"Lausanne does not match USA", "Lausanne, Switzerland", "Austin, USA", false},
		{"Indiana is US not India", "Indianapolis, Indiana", "Austin, USA", true},
		{"Indiana does not match India", "Indianapolis, Indiana", "Bangalore, India", false},
		{"London Ontario is Canada not UK", "London, Ontario, Canada", "London, United Kingdom", false},
		{"Expanded EU countries (IE, ES, PL, AT, SE)", "Dublin, Ireland", "Madrid, Spain", false},
		{"Same expanded EU country (PL)", "Warsaw, Poland", "Krakow, Polska", true},
		{"Same expanded EU country (AT)", "Vienna, Austria", "Graz, Österreich", true},
		{"Same expanded EU country (SE)", "Stockholm, Sweden", "Gothenburg, Sverige", true},
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
		{"Programming language gap: missing Rust", false},
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
