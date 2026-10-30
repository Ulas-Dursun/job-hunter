package worker

import (
	"testing"
)

func TestDeriveAction(t *testing.T) {
	tests := []struct {
		score    float64
		expected string
	}{
		{9.5, "apply"},
		{7.0, "apply"},
		{6.9, "watch"},
		{5.0, "watch"},
		{4.9, "skip"},
		{2.0, "skip"},
		{0.0, "skip"},
	}

	for _, tt := range tests {
		got := DeriveAction(tt.score)
		if got != tt.expected {
			t.Errorf("DeriveAction(%v) = %q; want %q", tt.score, got, tt.expected)
		}
	}
}

func TestField(t *testing.T) {
	vals := map[string]any{
		"title":   "Software Engineer",
		"company": "Acme",
		"count":   123, // not string
	}

	if field(vals, "title") != "Software Engineer" {
		t.Errorf("expected Software Engineer")
	}
	if field(vals, "company") != "Acme" {
		t.Errorf("expected Acme")
	}
	if field(vals, "non_existent") != "" {
		t.Errorf("expected empty string")
	}
	if field(vals, "count") != "" {
		t.Errorf("expected empty string for non-string value")
	}
}
