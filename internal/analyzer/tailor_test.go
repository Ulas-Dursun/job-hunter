package analyzer

import (
	"strings"
	"testing"
)

func TestParseTailoredResult(t *testing.T) {
	rawJSON := `{
		"cover_letter": "Dear Hiring Team at Unit8, I am excited to apply...",
		"tailored_summary": "Software Engineer specialized in Go, Python, and distributed systems...",
		"tailored_experience_bullets": [
			"Developed high-throughput services integrating Go and Rust across 12 endpoints."
		],
		"tailored_project_bullets": [
			"Engineered concurrent worker pool in Go with Redis, handling 500 requests."
		],
		"skills_highlight": ["Go", "Distributed Systems", "PostgreSQL"],
		"ats_keywords": ["Go", "Concurrency", "Docker", "Data Engineering"]
	}`

	res, err := parseTailoredResult(rawJSON)
	if err != nil {
		t.Fatalf("unexpected error parsing json: %v", err)
	}

	if res.CoverLetter == "" {
		t.Errorf("expected non-empty cover letter")
	}
	if len(res.TailoredExperienceBullets) != 1 {
		t.Errorf("expected 1 experience bullet, got %d", len(res.TailoredExperienceBullets))
	}
	if len(res.ATSKeywords) != 4 {
		t.Errorf("expected 4 keywords, got %d", len(res.ATSKeywords))
	}
}

func TestParseTailoredResult_NormalizesNilSlices(t *testing.T) {
	rawJSON := `{"cover_letter": "Hello", "tailored_summary": "Summary"}`
	res, err := parseTailoredResult(rawJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TailoredExperienceBullets == nil || res.TailoredProjectBullets == nil || res.SkillsHighlight == nil || res.ATSKeywords == nil {
		t.Errorf("expected nil slices to be normalized to empty slices")
	}
}

func TestBuildPrompt_TruncatesLongDescription(t *testing.T) {
	svc := NewTailorService(nil)
	longDesc := strings.Repeat("A", 6000)
	prompt := svc.buildPrompt("Backend Engineer", "Acme", "Istanbul", longDesc)

	if strings.Contains(prompt, strings.Repeat("A", 4500)) {
		t.Errorf("expected description to be truncated to 4000 runes")
	}
	if !strings.Contains(prompt, strings.Repeat("A", 4000)+"...") {
		t.Errorf("expected truncated description with ellipsis")
	}
}
