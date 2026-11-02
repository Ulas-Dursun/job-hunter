package analyzer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ulasdursun/job-hunter/internal/config"
)

// TailoredAssets represents the structured ATS-optimized assets generated for a job posting.
type TailoredAssets struct {
	CoverLetter               string   `json:"cover_letter"`
	TailoredSummary           string   `json:"tailored_summary"`
	TailoredExperienceBullets []string `json:"tailored_experience_bullets"`
	TailoredProjectBullets    []string `json:"tailored_project_bullets"`
	SkillsHighlight           []string `json:"skills_highlight"`
	ATSKeywords               []string `json:"ats_keywords"`
	Warnings                  []string `json:"warnings,omitempty"`
}

// TailorService coordinates ATS CV customization, Cover Letter generation, and Interview Prep via Gemini / Groq failover chain.
type TailorService struct {
	client        *LLMClient
	candidateName string
	masterProfile string
	unownedTech   []string
}

func NewTailorService(cfg *config.GeminiConfig, candidates ...*config.CandidateConfig) *TailorService {
	var cfgModel string
	rpm := 30
	if cfg != nil {
		cfgModel = cfg.Model
		if cfg.RateLimitRPM > 0 {
			rpm = cfg.RateLimitRPM
		}
	}
	var candidate *config.CandidateConfig
	if len(candidates) > 0 {
		candidate = candidates[0]
	}
	name := "the candidate"
	if candidate != nil && strings.TrimSpace(candidate.Name) != "" {
		name = strings.TrimSpace(candidate.Name)
	}
	unowned := defaultUnownedTech
	if candidate != nil && len(candidate.UnownedTech) > 0 {
		unowned = make([]string, 0, len(candidate.UnownedTech))
		for _, item := range candidate.UnownedTech {
			trimmed := strings.ToLower(strings.TrimSpace(item))
			if trimmed != "" {
				unowned = append(unowned, trimmed)
			}
		}
	}
	return &TailorService{
		client:        NewLLMClient(cfgModel, 45*time.Second, rpm),
		candidateName: name,
		masterProfile: buildMasterProfile(candidate),
		unownedTech:   unowned,
	}
}

// buildMasterProfile constructs the factual master resume text from CandidateConfig.
// Personal identifiers and private experience details live in config.local.yaml (or config.yaml), never hardcoded in Go source.
func buildMasterProfile(cfg *config.CandidateConfig) string {
	if cfg == nil {
		return "CANDIDATE PROFILE: Software Engineer (Backend, Cloud & Distributed Systems)\n"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("CANDIDATE FULL NAME: %s\n", cfg.Name))

	mobility := strings.TrimSpace(cfg.MobilityNote)
	if mobility != "" {
		sb.WriteString(fmt.Sprintf("LOCATION & MOBILITY: %s (%s)\n", cfg.Location, mobility))
	} else {
		sb.WriteString(fmt.Sprintf("LOCATION: %s\n", cfg.Location))
	}

	if strings.TrimSpace(cfg.Links) != "" {
		sb.WriteString(fmt.Sprintf("LINKS: %s\n", strings.TrimSpace(cfg.Links)))
	}

	if len(cfg.SummaryVariants) > 0 {
		sb.WriteString("\nSUMMARY VARIANTS (Adapt based on target role):\n")
		for _, v := range cfg.SummaryVariants {
			sb.WriteString(fmt.Sprintf("- %s\n", strings.TrimSpace(v)))
		}
	}

	sb.WriteString("\nEDUCATION & LANGUAGES:\n")
	if strings.TrimSpace(cfg.Education) != "" {
		sb.WriteString(fmt.Sprintf("- %s\n", strings.TrimSpace(cfg.Education)))
	} else {
		sb.WriteString(fmt.Sprintf("- B.Sc. Software Engineering (Graduation: %d)\n", cfg.GraduationYear))
	}
	if strings.TrimSpace(cfg.SpokenLanguages) != "" {
		sb.WriteString(fmt.Sprintf("- Spoken Languages: %s\n", strings.TrimSpace(cfg.SpokenLanguages)))
	}

	if len(cfg.Experience) > 0 {
		sb.WriteString("\nWORK EXPERIENCE & ASSIGNMENTS:\n")
		for i, exp := range cfg.Experience {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, strings.TrimSpace(exp)))
		}
	}

	if len(cfg.Projects) > 0 {
		sb.WriteString("\nKEY ENGINEERING PROJECTS:\n")
		for i, proj := range cfg.Projects {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, strings.TrimSpace(proj)))
		}
	}

	sb.WriteString("\nTECHNICAL SKILLS:\n")
	sb.WriteString(fmt.Sprintf("- Programming Languages: %s\n", strings.Join(cfg.Skills.Languages, ", ")))
	sb.WriteString(fmt.Sprintf("- Frameworks & Core Concepts: %s, %s\n", strings.Join(cfg.Skills.Frameworks, ", "), strings.Join(cfg.Skills.Concepts, ", ")))
	sb.WriteString(fmt.Sprintf("- Databases: %s\n", strings.Join(cfg.Skills.Databases, ", ")))
	if len(cfg.Skills.Cloud) > 0 {
		sb.WriteString(fmt.Sprintf("- Cloud & Certifications: %s\n", strings.Join(cfg.Skills.Cloud, ", ")))
	}
	sb.WriteString(fmt.Sprintf("- DevOps & Tooling: %s, %s\n", strings.Join(cfg.Skills.Infra, ", "), strings.Join(cfg.Skills.OS, ", ")))

	return sb.String()
}

// GenerateTailoredAssets produces an ATS-optimized cover letter, tailored summary, and bullet points.
func (s *TailorService) GenerateTailoredAssets(
	ctx context.Context,
	title, company, location, description string,
) (*TailoredAssets, error) {
	prompt := s.buildPrompt(title, company, location, description)
	rawText, err := s.client.Generate(ctx, prompt, 3500, 0.25)
	if err != nil {
		return nil, err
	}
	return parseTailoredResult(rawText, s.unownedTech...)
}

func (s *TailorService) buildPrompt(title, company, location, description string) string {
	desc := truncateUTF8(strings.TrimSpace(description), 4000)
	if desc == "" {
		desc = "(No detailed description provided in listing — tailor based on job title, company, and core engineering requirements.)"
	}

	return fmt.Sprintf(`You are a Principal Software Engineer and European Tech Hiring Lead coaching %s.
Your goal is to write a sharp, recruiter-friendly, zero-fluff Cover Letter and concise CV bullet revisions for the job below.

CRITICAL ZERO-HALLUCINATION POLICY:
1. NEVER invent companies, degrees, years of experience, or technologies not in the Candidate's Master Profile.
2. Keep all factual numbers and metrics from the Candidate's Master Profile intact.
3. LANGUAGE MATCHING: Detect the language of the job posting. If the posting is primarily in Turkish, write cover_letter, tailored_summary, and all bullets in natural, professional Turkish (keeping technical terms in English). Otherwise, write in crisp English.

COVER LETTER — SHORT, PUNCHY & ENGINEER-TO-ENGINEER (130 to 170 words MAX, 3 short paragraphs):
Recruiters and hiring managers scan cover letters in 15 seconds. Long, repetitive letters hurt the candidate.
1. BANNED AI CLICHÉS (NEVER USE):
   - "I am thrilled/excited to apply", "I am writing to express my strong interest", "Throughout my academic journey", "deep passion for", "thrive in", "testament to", "delve", "synergy".
2. STRUCTURE:
   - Paragraph 1 (Direct Hook, 2 sentences): Reference what %s is building and why the %s role fits %s's profile (mention European mobility/traineeship or new-grad availability if relevant to location/domain).
   - Paragraph 2 (Focused Proof, 2-3 sentences): Pick ONLY the 2 most relevant experiences/projects from the Candidate's Master Profile for THIS specific role (do NOT list everything). Connect the concrete engineering trade-offs and metrics directly to %s's stack.
   - Paragraph 3 (Clean Close, 1-2 sentences): Direct, confident sign-off without begging or fluff.

TAILORED CV REVISIONS — HIGH-SIGNAL, SCANNABLE BULLETS:
1. tailored_summary (MAX 2 sentences, <= 32 words): Based on the Candidate's Summary Variants and Master Profile, write a sharp 1-2 sentence summary tailored to this role's focus.
2. tailored_experience_bullets (EXACTLY 3 bullets, each <= 22 words):
   - Rewrite the candidate's work experience / internship / traineeship bullets angled toward the target job's terminology while keeping all verified metrics intact.
3. tailored_project_bullets (EXACTLY 3 bullets, each <= 22 words):
   - Pick the 3 strongest bullets across the candidate's key engineering projects that best fit this role's domain and stack.
4. HONEST KEYWORD MAPPING:
   - "skills_highlight": 5-7 skills the candidate ACTUALLY has in the Master Profile that match this posting (safe to put at the top of the CV).
   - "ats_keywords": Key technical terms from the job posting. If the candidate has the skill, list it normally (e.g. "PostgreSQL"). If the posting requires a tool the candidate does NOT have in the Master Profile, append " (gap — do not claim on CV)" (e.g. "Kubernetes (gap — do not claim on CV)").

CANDIDATE MASTER PROFILE:
%s

TARGET JOB POSTING:
Title: %s
Company: %s
Location: %s
Description:
%s

OUTPUT FORMAT:
Respond with a strict, valid JSON object without markdown formatting, backticks, or preamble:
{
  "cover_letter": "string",
  "tailored_summary": "string",
  "tailored_experience_bullets": ["string", "string", "string"],
  "tailored_project_bullets": ["string", "string", "string"],
  "skills_highlight": ["string", "string"],
  "ats_keywords": ["string", "string"]
}`, s.candidateName, company, title, s.candidateName, company, s.masterProfile, title, company, location, desc)
}

func parseTailoredResult(text string, customUnowned ...string) (*TailoredAssets, error) {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start == -1 || end == -1 || end < start {
		return nil, fmt.Errorf("no valid JSON found in response")
	}

	var result TailoredAssets
	if err := json.Unmarshal([]byte(text[start:end+1]), &result); err != nil {
		slog.Error("failed to unmarshal tailored json", "raw", text[start:end+1])
		return nil, fmt.Errorf("unmarshaling tailored json: %w", err)
	}

	if result.TailoredExperienceBullets == nil {
		result.TailoredExperienceBullets = []string{}
	}
	if result.TailoredProjectBullets == nil {
		result.TailoredProjectBullets = []string{}
	}
	if result.SkillsHighlight == nil {
		result.SkillsHighlight = []string{}
	}
	if result.ATSKeywords == nil {
		result.ATSKeywords = []string{}
	}

	unowned := defaultUnownedTech
	if len(customUnowned) > 0 {
		unowned = customUnowned
	}
	sanitizeTailoredAssets(&result, unowned)
	return &result, nil
}

// defaultUnownedTech, candidate.unowned_tech belirtilmediğinde kullanılan varsayılan listedir;
// LLM yanlışlıkla skills_highlight içine koyarsa elenir, ats_keywords içinde ise gap olarak işaretlenir.
var defaultUnownedTech = []string{
	"c#", ".net", "asp.net", "php", "laravel", "symfony", "ruby", "rails",
	"swift", "objective-c", "kotlin", "angular", "vue.js", "cobol", "sap",
	"kubernetes", "terraform", "ansible", "kafka", "spark", "hadoop", "pytorch", "tensorflow",
}

func isUnownedSkill(s string, unownedList ...string) bool {
	low := strings.ToLower(strings.TrimSpace(s))
	if low == "" {
		return false
	}
	list := defaultUnownedTech
	if len(unownedList) > 0 {
		list = unownedList
	}
	for _, bad := range list {
		b := strings.ToLower(strings.TrimSpace(bad))
		if b != "" && containsTechToken(low, b) {
			return true
		}
	}
	return false
}

// containsTechToken checks if tech appears as a standalone token/word in s,
// avoiding false positives like "sap" in "shape"/"encapsulate" or "spark" in "sparked".
func containsTechToken(s, tech string) bool {
	if s == tech {
		return true
	}
	start := 0
	for {
		idx := strings.Index(s[start:], tech)
		if idx == -1 {
			return false
		}
		actual := start + idx
		end := actual + len(tech)

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

func sanitizeTailoredAssets(res *TailoredAssets, unownedList []string) {
	kept := make([]string, 0, len(res.SkillsHighlight))
	for _, sk := range res.SkillsHighlight {
		if !isUnownedSkill(sk, unownedList...) {
			kept = append(kept, sk)
		}
	}
	res.SkillsHighlight = kept

	for i, kw := range res.ATSKeywords {
		low := strings.ToLower(kw)
		if isUnownedSkill(kw, unownedList...) && !strings.Contains(low, "gap") && !strings.Contains(low, "eksik") {
			res.ATSKeywords[i] = kw + " (gap — do not claim on CV)"
		}
	}

	// Serbest metinlerde (ön yazı, özet ve maddeler) adayın profilinde bulunmayan teknoloji denetimi.
	// Cümle gramerini bozmamak için metin körlemesine kırpılmaz; bunun yerine belirgin uyarı (warning)
	// üretilerek arayüzde kullanıcıya "Manuel Kontrol Gerekli" durumu sunulur ve loglanır.
	detectedUnowned := make(map[string]bool)
	checkField := func(fieldName, text string) {
		low := strings.ToLower(text)
		for _, bad := range unownedList {
			b := strings.ToLower(strings.TrimSpace(bad))
			if b != "" && containsTechToken(low, b) {
				key := fmt.Sprintf("%s:%s", fieldName, b)
				if !detectedUnowned[key] {
					detectedUnowned[key] = true
					if res.Warnings == nil {
						res.Warnings = []string{}
					}
					slog.Warn("unowned technology detected in tailored free text", "field", fieldName, "tech", b)
					res.Warnings = append(res.Warnings, fmt.Sprintf("%s içinde adayın profilinde yer almayan teknoloji tespit edildi: %s (gözden geçirin)", fieldName, b))
				}
			}
		}
	}

	checkField("Cover Letter", res.CoverLetter)
	checkField("Özet (Summary)", res.TailoredSummary)
	for _, b := range res.TailoredExperienceBullets {
		checkField("Deneyim Maddeleri", b)
	}
	for _, b := range res.TailoredProjectBullets {
		checkField("Proje Maddeleri", b)
	}
}
