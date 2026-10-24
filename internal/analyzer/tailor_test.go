package analyzer

import (
	"strings"
	"testing"
)

func TestParseTailoredResult(t *testing.T) {
	rawJSON := `{
		"cover_letter": "Dear Hiring Team at Unit8, I am reaching out...",
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

func TestParseInterviewResult(t *testing.T) {
	rawJSON := `{
		"role_focus_summary": "Bu rol yüksek eşzamanlılık ve gerçek zamanlı veri işleme üzerine odaklanıyor.",
		"technical_questions": [
			{
				"question": "How did you handle backpressure in your 5-goroutine worker pool?",
				"why_they_ask": "Eşzamanlılık ve kuyruk yönetimi bilgisini ölçmek için.",
				"answer_strategy": "Redis kuyruğu ve exponential backoff mekanizmasını anlat."
			}
		],
		"behavioral_questions": [
			{
				"question": "Tell me about a time you collaborated in an Agile team.",
				"star_story_guide": "Sanlab 7 kişilik Scrum ekibindeki Go/Rust entegrasyonunu anlat."
			}
		],
		"questions_to_ask": [
			"What are the main latency bottlenecks in your real-time pipeline?"
		]
	}`

	res, err := parseInterviewResult(rawJSON)
	if err != nil {
		t.Fatalf("unexpected error parsing interview prep json: %v", err)
	}
	if res.RoleFocusSummary == "" {
		t.Errorf("expected non-empty role focus summary")
	}
	if len(res.TechnicalQuestions) != 1 {
		t.Errorf("expected 1 technical question, got %d", len(res.TechnicalQuestions))
	}
	if len(res.BehavioralQuestions) != 1 {
		t.Errorf("expected 1 behavioral question, got %d", len(res.BehavioralQuestions))
	}
	if len(res.QuestionsToAsk) != 1 {
		t.Errorf("expected 1 question to ask, got %d", len(res.QuestionsToAsk))
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

func TestSanitizeTailoredAssets_FiltersUnownedTech(t *testing.T) {
	rawJSON := `{
		"cover_letter": "Short letter",
		"tailored_summary": "Summary",
		"tailored_experience_bullets": ["Bullet 1"],
		"tailored_project_bullets": ["Bullet 2"],
		"skills_highlight": ["Go", "Kubernetes", "PostgreSQL", "C#"],
		"ats_keywords": ["Go", "Kubernetes", "Terraform (gap — do not claim on CV)"]
	}`

	res, err := parseTailoredResult(rawJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, sk := range res.SkillsHighlight {
		if sk == "Kubernetes" || sk == "C#" {
			t.Errorf("unowned skill %q should have been removed from skills_highlight", sk)
		}
	}
	foundMarked := false
	for _, kw := range res.ATSKeywords {
		if strings.HasPrefix(kw, "Kubernetes") && strings.Contains(kw, "gap") {
			foundMarked = true
		}
	}
	if !foundMarked {
		t.Errorf("expected unowned ATS keyword 'Kubernetes' to be marked as gap, got %v", res.ATSKeywords)
	}
}
