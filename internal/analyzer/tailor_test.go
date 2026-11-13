package analyzer

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ulasdursun/job-hunter/internal/config"
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
			},
			{
				"question": "What PostgreSQL indexing strategy would you use for job search filtering?",
				"why_they_ask": "Veritabanı optimizasyonu ve sorgu planı analizi yeteneğini ölçmek için.",
				"answer_strategy": "Compound index ve partial index yaklaşımlarını anlat."
			},
			{
				"question": "How do you prevent goroutine leaks in long-running services?",
				"why_they_ask": "Bellek yönetimi ve context lifecycle hakimiyetini test etmek için.",
				"answer_strategy": "Context cancellation ve sync.WaitGroup kullanımını örneklendir."
			}
		],
		"behavioral_questions": [
			{
				"question": "Tell me about a time you collaborated in an Agile team.",
				"star_story_guide": "7 kişilik Scrum ekibindeki Go/Rust servis entegrasyonunu anlat."
			},
			{
				"question": "Describe a situation where a production service was rate-limited.",
				"star_story_guide": "Groq/Gemini failover ve rate-limit backoff mimarisini kurma sürecini anlat."
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
	if len(res.TechnicalQuestions) != 3 {
		t.Errorf("expected 3 technical questions, got %d", len(res.TechnicalQuestions))
	}
	if len(res.BehavioralQuestions) != 2 {
		t.Errorf("expected 2 behavioral questions, got %d", len(res.BehavioralQuestions))
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

	// Word-boundary check: "Encapsulation" or "Sparked" must NOT be falsely flagged as "sap" or "spark"
	if isUnownedSkill("Encapsulation & OOP") {
		t.Errorf("'Encapsulation & OOP' should not match 'sap'")
	}
	if isUnownedSkill("Sparked API latency improvements") {
		t.Errorf("'Sparked API latency improvements' should not match 'spark'")
	}
	if !isUnownedSkill("Apache Spark") || !isUnownedSkill("SAP ERP") || !isUnownedSkill("C# / .NET") {
		t.Errorf("expected 'Apache Spark', 'SAP ERP', and 'C# / .NET' to be detected as unowned")
	}
}

func TestPromptBuilder_ChainOfThoughtOrderAndBonus(t *testing.T) {
	pb := NewPromptBuilder(nil)
	prompt := pb.Build("Junior Backend Engineer", "Adyen", "Amsterdam", "Go, Redis Streams, Concurrency, AWS")

	idxReasoning := strings.Index(prompt, `"reasoning":`)
	idxOverlap := strings.Index(prompt, `"skill_overlap":`)
	idxMissing := strings.Index(prompt, `"missing_skills":`)
	idxFlags := strings.Index(prompt, `"red_flags":`)
	idxBreakdown := strings.Index(prompt, `"score_breakdown":`)
	idxScore := strings.Index(prompt, `"match_score":`)
	idxAction := strings.Index(prompt, `"action":`)

	if idxReasoning == -1 || idxOverlap == -1 || idxMissing == -1 || idxFlags == -1 || idxBreakdown == -1 || idxScore == -1 || idxAction == -1 {
		t.Fatalf("missing expected JSON keys in prompt schema")
	}
	if !(idxReasoning < idxOverlap && idxOverlap < idxMissing && idxMissing < idxFlags && idxFlags < idxBreakdown && idxBreakdown < idxScore && idxScore < idxAction) {
		t.Errorf("expected CoT JSON field order (reasoning -> overlap -> missing -> red_flags -> score_breakdown -> match_score -> action), got indices: reasoning=%d overlap=%d missing=%d flags=%d breakdown=%d score=%d action=%d",
			idxReasoning, idxOverlap, idxMissing, idxFlags, idxBreakdown, idxScore, idxAction)
	}
	if !strings.Contains(prompt, "STRONG ARCHITECTURAL & AI PRACTITIONER BONUS (+0.4 to +0.5)") {
		t.Errorf("expected Strong Architectural & AI Practitioner bonus rule in prompt")
	}
	if !strings.Contains(prompt, "8.0 - 9.5 (JACKPOT MATCH") {
		t.Errorf("expected 8.0 threshold alignment in prompt")
	}
}

func TestSanitizeTailoredAssets_UnownedTechInFreeTextGeneratesWarnings(t *testing.T) {
	rawJSON := `{
		"cover_letter": "I have extensive production experience with Kubernetes and Terraform clusters.",
		"tailored_summary": "Expert in SAP and Apache Spark data pipelines.",
		"tailored_experience_bullets": ["Led Kubernetes migrations across AWS infrastructure."],
		"tailored_project_bullets": ["Built microservices using C#."],
		"skills_highlight": ["Go", "PostgreSQL"],
		"ats_keywords": ["Go", "PostgreSQL"]
	}`

	res, err := parseTailoredResult(rawJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(res.Warnings) == 0 {
		t.Fatalf("expected warnings for unowned technologies mentioned in free-text fields, got none")
	}

	foundK8s := false
	foundCover := false
	for _, w := range res.Warnings {
		if strings.Contains(strings.ToLower(w), "kubernetes") {
			foundK8s = true
		}
		if strings.Contains(w, "Cover Letter") {
			foundCover = true
		}
	}
	if !foundK8s {
		t.Errorf("expected warning mentioning kubernetes, got: %v", res.Warnings)
	}
	if !foundCover {
		t.Errorf("expected warning mentioning Cover Letter field, got: %v", res.Warnings)
	}
}

func TestParseInterviewResult_SemanticValidation(t *testing.T) {
	// 1. Teknik sorular 3'ten az
	insufficientTechJSON := `{
		"role_focus_summary": "Focus",
		"technical_questions": [
			{"question": "Q1", "why_they_ask": "W", "answer_strategy": "A1"},
			{"question": "Q2", "why_they_ask": "W", "answer_strategy": "A2"}
		],
		"behavioral_questions": [
			{"question": "B1", "star_story_guide": "G1"},
			{"question": "B2", "star_story_guide": "G2"}
		],
		"questions_to_ask": ["Ask 1"]
	}`
	if _, err := parseInterviewResult(insufficientTechJSON); err == nil {
		t.Errorf("expected error for fewer than 3 technical_questions, got nil")
	}

	// 2. Teknik soruda answer_strategy boş
	emptyTechAnswerJSON := `{
		"role_focus_summary": "Focus",
		"technical_questions": [
			{"question": "Q1", "why_they_ask": "W", "answer_strategy": "A1"},
			{"question": "Q2", "why_they_ask": "W", "answer_strategy": "   "},
			{"question": "Q3", "why_they_ask": "W", "answer_strategy": "A3"}
		],
		"behavioral_questions": [
			{"question": "B1", "star_story_guide": "G1"},
			{"question": "B2", "star_story_guide": "G2"}
		],
		"questions_to_ask": ["Ask 1"]
	}`
	if _, err := parseInterviewResult(emptyTechAnswerJSON); err == nil {
		t.Errorf("expected error for empty technical question answer strategy, got nil")
	}

	// 3. Davranışsal sorular 2'den az
	insufficientBehJSON := `{
		"role_focus_summary": "Focus",
		"technical_questions": [
			{"question": "Q1", "why_they_ask": "W", "answer_strategy": "A1"},
			{"question": "Q2", "why_they_ask": "W", "answer_strategy": "A2"},
			{"question": "Q3", "why_they_ask": "W", "answer_strategy": "A3"}
		],
		"behavioral_questions": [
			{"question": "B1", "star_story_guide": "G1"}
		],
		"questions_to_ask": ["Ask 1"]
	}`
	if _, err := parseInterviewResult(insufficientBehJSON); err == nil {
		t.Errorf("expected error for fewer than 2 behavioral_questions, got nil")
	}

	// 4. Davranışsal soruda star_story_guide boş
	emptyBehGuideJSON := `{
		"role_focus_summary": "Focus",
		"technical_questions": [
			{"question": "Q1", "why_they_ask": "W", "answer_strategy": "A1"},
			{"question": "Q2", "why_they_ask": "W", "answer_strategy": "A2"},
			{"question": "Q3", "why_they_ask": "W", "answer_strategy": "A3"}
		],
		"behavioral_questions": [
			{"question": "B1", "star_story_guide": "G1"},
			{"question": "B2", "star_story_guide": ""}
		],
		"questions_to_ask": ["Ask 1"]
	}`
	if _, err := parseInterviewResult(emptyBehGuideJSON); err == nil {
		t.Errorf("expected error for empty star_story_guide, got nil")
	}

	// 5. Reverse sorular (questions_to_ask) boş dizi
	noReverseJSON := `{
		"role_focus_summary": "Focus",
		"technical_questions": [
			{"question": "Q1", "why_they_ask": "W", "answer_strategy": "A1"},
			{"question": "Q2", "why_they_ask": "W", "answer_strategy": "A2"},
			{"question": "Q3", "why_they_ask": "W", "answer_strategy": "A3"}
		],
		"behavioral_questions": [
			{"question": "B1", "star_story_guide": "G1"},
			{"question": "B2", "star_story_guide": "G2"}
		],
		"questions_to_ask": []
	}`
	if _, err := parseInterviewResult(noReverseJSON); err == nil {
		t.Errorf("expected error for empty questions_to_ask, got nil")
	}

	// 6. Reverse sorularda boş metin elemanı
	emptyReverseItemJSON := `{
		"role_focus_summary": "Focus",
		"technical_questions": [
			{"question": "Q1", "why_they_ask": "W", "answer_strategy": "A1"},
			{"question": "Q2", "why_they_ask": "W", "answer_strategy": "A2"},
			{"question": "Q3", "why_they_ask": "W", "answer_strategy": "A3"}
		],
		"behavioral_questions": [
			{"question": "B1", "star_story_guide": "G1"},
			{"question": "B2", "star_story_guide": "G2"}
		],
		"questions_to_ask": ["   "]
	}`
	if _, err := parseInterviewResult(emptyReverseItemJSON); err == nil {
		t.Errorf("expected error for empty string in questions_to_ask, got nil")
	}
}

func TestParseResult_ScoreBreakdownClampingAndZeroPreservation(t *testing.T) {
	raw := `{
		"reasoning": "Test reasoning.",
		"role_family": "backend",
		"skill_overlap": ["Go"],
		"missing_skills": ["Kubernetes"],
		"red_flags": [],
		"score_breakdown": {
			"tech_fit": 7.5,
			"seniority_fit": -1.0,
			"domain_fit": 2.0,
			"location_fit": 0.0
		},
		"match_score": 12.0,
		"action": "apply"
	}`

	res, err := parseResult(raw)
	if err != nil {
		t.Fatalf("parseResult failed: %v", err)
	}
	if res.MatchScore != 10.0 {
		t.Errorf("expected MatchScore clamped to 10.0, got %v", res.MatchScore)
	}
	if res.ScoreBreakdown == nil {
		t.Fatalf("expected ScoreBreakdown to be non-nil")
	}
	if res.ScoreBreakdown.TechFit != 5.0 {
		t.Errorf("expected TechFit clamped to 5.0, got %v", res.ScoreBreakdown.TechFit)
	}
	if res.ScoreBreakdown.SeniorityFit != 0.0 {
		t.Errorf("expected SeniorityFit clamped to 0.0, got %v", res.ScoreBreakdown.SeniorityFit)
	}
	if res.ScoreBreakdown.DomainFit != 1.5 {
		t.Errorf("expected DomainFit clamped to 1.5, got %v", res.ScoreBreakdown.DomainFit)
	}
	if res.ScoreBreakdown.LocationFit != 0.0 {
		t.Errorf("expected LocationFit 0.0, got %v", res.ScoreBreakdown.LocationFit)
	}

	b, err := json.Marshal(res.ScoreBreakdown)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	jsonStr := string(b)
	if !strings.Contains(jsonStr, `"seniority_fit":0`) || !strings.Contains(jsonStr, `"location_fit":0`) {
		t.Errorf("expected 0.0 fields to be preserved in JSON (no omitempty), got: %s", jsonStr)
	}
}

func TestPromptBuilder_CurrentAssignmentsAndLocationTierRubric(t *testing.T) {
	cfg := &config.CandidateConfig{
		Name:            "Alex Rivera",
		Location:        "Brussels, Belgium",
		TargetLocations: []string{"Brussels", "Amsterdam", "London", "Zurich"},
		Experience:      []string{"Example Aerospace: Software Engineering Trainee (Active)"},
	}
	pb := NewPromptBuilder(cfg)
	prompt := pb.Build("Junior Backend Engineer", "Collibra", "Brussels", "Go, PostgreSQL")

	if !strings.Contains(prompt, "Work Experience & Current Assignments:") {
		t.Errorf("expected 'Work Experience & Current Assignments:' heading in profile")
	}
	if strings.Contains(prompt, "Work Experience & Incoming Assignments:") {
		t.Errorf("did not expect old 'Work Experience & Incoming Assignments:' heading in profile")
	}
	if !strings.Contains(prompt, "TIER-1 HIGH ACCESSIBILITY") || !strings.Contains(prompt, "TIER-3 HIGH-VALUE / SPONSORSHIP-CRITICAL") {
		t.Errorf("expected location accessibility tier rubric in prompt")
	}
	if !strings.Contains(prompt, "INCLUDING BRUSSELS FR/NL") {
		t.Errorf("expected Brussels FR/NL language barrier penalty in prompt")
	}
}

func TestParseResult_BreakdownMismatchDoesNotOverrideMatchScore(t *testing.T) {
	raw := `{
		"reasoning": "3+ years required so hard cap applied.",
		"role_family": "primary",
		"skill_overlap": ["Go", "PostgreSQL"],
		"missing_skills": ["3+ years production"],
		"red_flags": ["Requires 3+ years"],
		"score_breakdown": {
			"tech_fit": 4.5,
			"seniority_fit": 1.5,
			"domain_fit": 1.2,
			"location_fit": 0.9
		},
		"match_score": 6.5,
		"action": "watch"
	}`

	res, err := parseResult(raw)
	if err != nil {
		t.Fatalf("parseResult failed: %v", err)
	}
	if res.MatchScore != 6.5 {
		t.Errorf("expected hard-capped MatchScore 6.5 to be preserved, got %v", res.MatchScore)
	}
}

func TestParseTailoredResult_RejectsEmptyCoverLetterOrSummary(t *testing.T) {
	emptyCoverJSON := `{
		"cover_letter": "   ",
		"tailored_summary": "Valid summary",
		"skills_highlight": ["Go"]
	}`
	if _, err := parseTailoredResult(emptyCoverJSON); err == nil {
		t.Errorf("expected error when cover_letter is empty, got nil")
	}

	emptySummaryJSON := `{
		"cover_letter": "Valid cover letter",
		"tailored_summary": "",
		"skills_highlight": ["Go"]
	}`
	if _, err := parseTailoredResult(emptySummaryJSON); err == nil {
		t.Errorf("expected error when tailored_summary is empty, got nil")
	}
}

func TestSanitizeTailoredAssets_AllowlistCatchesLeaks(t *testing.T) {
	cfg := &config.CandidateConfig{
		Name: "Test Candidate",
		Skills: config.SkillsConfig{
			Languages: []string{"Go", "Python"},
			Databases: []string{"PostgreSQL", "Redis"},
			Infra:     []string{"Docker"},
			Cloud:     []string{"AWS"},
		},
		Experience: []string{
			"Example Aerospace: Software Engineering Trainee",
			"Summer Camp Counselor: Seasonal Staff",
		},
	}

	svc := NewTailorService(nil, cfg)
	if len(svc.ownedSkills) == 0 {
		t.Fatalf("expected non-empty ownedSkills allowlist when candidate is provided")
	}

	rawJSON := `{
		"cover_letter": "Concise cover letter.",
		"tailored_summary": "Backend engineer experienced in Go and PostgreSQL.",
		"tailored_experience_bullets": ["Built Go services at Example Aerospace."],
		"tailored_project_bullets": ["Designed Redis worker pool."],
		"skills_highlight": ["Go", "TypeScript", "Azure", "GCP", "gRPC", "PostgreSQL", "Docker"],
		"ats_keywords": ["Go", "PostgreSQL"]
	}`

	res, err := parseTailoredResultWithAllowlist(rawJSON, svc.ownedSkills, svc.unownedTech...)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	leaked := map[string]bool{"TypeScript": true, "Azure": true, "GCP": true, "gRPC": true}
	for _, sk := range res.SkillsHighlight {
		if leaked[sk] {
			t.Errorf("expected leaked unowned skill %q to be removed from skills_highlight", sk)
		}
	}

	kept := map[string]bool{}
	for _, sk := range res.SkillsHighlight {
		kept[sk] = true
	}
	if !kept["Go"] || !kept["PostgreSQL"] || !kept["Docker"] {
		t.Errorf("expected owned skills Go, PostgreSQL, Docker to remain, got: %v", res.SkillsHighlight)
	}

	if len(res.Warnings) < 4 {
		t.Errorf("expected warnings for TypeScript, Azure, GCP, gRPC leaks, got: %v", res.Warnings)
	}

	// candidate == nil durumunda sadece unownedTech kara listesi çalışmalı
	nilSvc := NewTailorService(nil)
	if nilSvc.ownedSkills != nil {
		t.Errorf("expected nil ownedSkills when candidate is nil")
	}
	nilRes, err := parseTailoredResultWithAllowlist(rawJSON, nilSvc.ownedSkills, nilSvc.unownedTech...)
	if err != nil {
		t.Fatalf("unexpected error with nil allowlist: %v", err)
	}
	nilKept := map[string]bool{}
	for _, sk := range nilRes.SkillsHighlight {
		nilKept[sk] = true
	}
	if !nilKept["TypeScript"] {
		t.Errorf("expected TypeScript to remain when candidate is nil (only blacklist active), got: %v", nilRes.SkillsHighlight)
	}
}

func TestBuildMasterProfile_SeparatesNonEngineeringExperienceAndInterviewPrompt(t *testing.T) {
	cfg := &config.CandidateConfig{
		Name: "Alex Rivera",
		Experience: []string{
			"Example Aerospace: Software Engineering Trainee",
			"Example Labs: Backend Developer Intern",
			"Summer Camp Counselor: Seasonal Staff (1 June 2027)",
		},
	}
	svc := NewTailorService(nil, cfg)
	if !strings.Contains(svc.masterProfile, "ENGINEERING WORK EXPERIENCE & ASSIGNMENTS") {
		t.Errorf("expected engineering experience heading in master profile")
	}
	if !strings.Contains(svc.masterProfile, "OTHER / NON-ENGINEERING EXPERIENCE") {
		t.Errorf("expected non-engineering experience heading in master profile")
	}

	interviewPrompt := svc.buildInterviewPrompt("Backend Engineer", "Collibra", "Brussels", "Go, Kubernetes")
	for _, needle := range []string{"stated availability date", "work authorization/sponsorship context", "gap-bridging"} {
		if !strings.Contains(interviewPrompt, needle) {
			t.Errorf("expected %q in interview prompt", needle)
		}
	}
}
