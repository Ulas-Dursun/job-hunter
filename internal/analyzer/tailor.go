package analyzer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

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
}

// TechnicalQA represents a targeted technical interview question and answer guide.
type TechnicalQA struct {
	Question       string `json:"question"`
	WhyTheyAsk     string `json:"why_they_ask"`
	AnswerStrategy string `json:"answer_strategy"`
}

// BehavioralSTAR represents a behavioral interview question with a tailored STAR response script.
type BehavioralSTAR struct {
	Question       string `json:"question"`
	STARStoryGuide string `json:"star_story_guide"`
}

// InterviewPrepAssets represents the structured AI Interview Prep kit for a specific job.
type InterviewPrepAssets struct {
	RoleFocusSummary    string           `json:"role_focus_summary"`
	TechnicalQuestions  []TechnicalQA    `json:"technical_questions"`
	BehavioralQuestions []BehavioralSTAR `json:"behavioral_questions"`
	QuestionsToAsk      []string         `json:"questions_to_ask"`
}

// TailorService coordinates ATS CV customization, Cover Letter generation, and Interview Prep via Gemini / Groq failover chain.
type TailorService struct {
	endpoints     []providerEndpoint
	cursor        atomic.Uint64
	client        *http.Client
	candidateName string
	masterProfile string
}

func NewTailorService(cfg *config.GeminiConfig, candidates ...*config.CandidateConfig) *TailorService {
	var cfgModel string
	if cfg != nil {
		cfgModel = cfg.Model
	}
	var candidate *config.CandidateConfig
	if len(candidates) > 0 {
		candidate = candidates[0]
	}
	name := "the candidate"
	if candidate != nil && strings.TrimSpace(candidate.Name) != "" {
		name = strings.TrimSpace(candidate.Name)
	}
	return &TailorService{
		endpoints:     buildFailoverChain(cfgModel),
		client:        &http.Client{Timeout: 45 * time.Second},
		candidateName: name,
		masterProfile: buildMasterProfile(candidate),
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
	rawText, err := s.executeWithRetry(ctx, prompt)
	if err != nil {
		return nil, err
	}
	return parseTailoredResult(rawText)
}

// GenerateInterviewPrep produces 5 technical questions, 3 STAR behavioral guides, and 2 reverse interview questions.
func (s *TailorService) GenerateInterviewPrep(
	ctx context.Context,
	title, company, location, description string,
) (*InterviewPrepAssets, error) {
	prompt := s.buildInterviewPrompt(title, company, location, description)
	rawText, err := s.executeWithRetry(ctx, prompt)
	if err != nil {
		return nil, err
	}
	return parseInterviewResult(rawText)
}

func (s *TailorService) executeWithRetry(ctx context.Context, prompt string) (string, error) {
	n := len(s.endpoints)
	if n == 0 {
		return "", fmt.Errorf("missing AI API key (set GEMINI_API_KEY or GROQ_API_KEY in .env)")
	}

	maxRounds := 2
	for round := 0; round < maxRounds; round++ {
		startIdx := int(s.cursor.Load() % uint64(n))
		sawRetryable := false
		for step := 0; step < n; step++ {
			idx := (startIdx + step) % n
			ep := s.endpoints[idx]

			var rawText string
			var err error

			if ep.provider == ProviderGemini {
				rawText, err = s.callGeminiRaw(ctx, ep, prompt)
			} else {
				rawText, err = s.callGroqRaw(ctx, ep, prompt)
			}

			if err == nil {
				if step > 0 {
					slog.Info("tailor service yedek istasyon ile başarılı oldu", "endpoint", ep.label)
				}
				return rawText, nil
			}

			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			if isRetryable(err) {
				sawRetryable = true
			}

			nextIdx := (idx + 1) % n
			s.cursor.Store(uint64(nextIdx))

			if step+1 < n {
				slog.Warn("tailor istasyonu meşgul/limitli, sıradaki yedek anahtara geçiliyor...",
					"failed_endpoint", ep.label,
					"next_endpoint", s.endpoints[nextIdx].label,
				)
				continue
			}

			if sawRetryable {
				backoff := time.Duration((round+1)*3) * time.Second
				slog.Warn("tailor service tüm istasyonlar meşgul (429/503), bekleniyor...",
					"round", round+1,
					"wait", backoff.String(),
				)
				select {
				case <-time.After(backoff):
				case <-ctx.Done():
					return "", ctx.Err()
				}
			} else {
				return "", err
			}
		}
	}

	return "", fmt.Errorf("all %d AI endpoints busy or rate-limited after %d rounds", n, maxRounds)
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

func (s *TailorService) buildInterviewPrompt(title, company, location, description string) string {
	desc := truncateUTF8(strings.TrimSpace(description), 4000)
	if desc == "" {
		desc = "(No detailed description provided — generate based on job title, company, and candidate profile.)"
	}

	return fmt.Sprintf(`You are a Staff Engineer and Hiring Manager at %s interviewing %s for the %s position (%s).
Create a concise, high-signal Interview Preparation Kit connecting %s's domain with the candidate's actual projects and experience.

REQUIREMENTS:
1. role_focus_summary:
   - 2 concise sentences in Turkish summarizing what %s cares about most for this role and which 1-2 of the candidate's real experiences or projects should be the centerpiece.
2. technical_questions (EXACTLY 3 items — quality over quantity):
   - Pick the 3 most likely technical / system-design questions for THIS specific role.
   - "question": The exact question the interviewer will ask (in English, with brief Turkish context if helpful).
   - "why_they_ask": 1 short sentence in Turkish explaining what they are testing.
   - "answer_strategy": 2-3 punchy sentences in Turkish (keeping technical terms in English) showing how the candidate should answer using real metrics and trade-offs from the Master Profile — no long essays.
3. behavioral_questions (EXACTLY 2 items):
   - "question": The behavioral question in English.
   - "star_story_guide": Concise 3-sentence STAR (Situation/Task, Action, Result) guide in Turkish based on the candidate's real experience in the Master Profile.
4. questions_to_ask (EXACTLY 2 items):
   - 2 sharp engineering questions (in English, with a 1-line Turkish note on why it works) for the end of the interview.

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
  "role_focus_summary": "string",
  "technical_questions": [
    {
      "question": "string",
      "why_they_ask": "string",
      "answer_strategy": "string"
    }
  ],
  "behavioral_questions": [
    {
      "question": "string",
      "star_story_guide": "string"
    }
  ],
  "questions_to_ask": ["string", "string"]
}`, company, s.candidateName, title, location, company, company, s.masterProfile, title, company, location, desc)
}

func (s *TailorService) callGeminiRaw(ctx context.Context, ep providerEndpoint, prompt string) (string, error) {
	reqURL := fmt.Sprintf(geminiAPIURL, ep.model)

	reqPayload := map[string]any{
		"contents": []map[string]any{
			{
				"parts": []map[string]any{
					{"text": prompt},
				},
			},
		},
		"generationConfig": map[string]any{
			"temperature":      0.25,
			"maxOutputTokens":  3500,
			"responseMimeType": "application/json",
		},
	}

	bodyBytes, err := json.Marshal(reqPayload)
	if err != nil {
		return "", fmt.Errorf("marshaling gemini payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("creating gemini request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", ep.apiKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("gemini http call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", newStatusError("gemini", resp)
	}

	var geminiResp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		return "", fmt.Errorf("decoding gemini response: %w", err)
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("empty gemini response candidates")
	}

	return geminiResp.Candidates[0].Content.Parts[0].Text, nil
}

func (s *TailorService) callGroqRaw(ctx context.Context, ep providerEndpoint, prompt string) (string, error) {
	reqBody := map[string]any{
		"model": ep.model,
		"messages": []map[string]any{
			{
				"role":    "user",
				"content": prompt,
			},
		},
		"max_tokens":  3500,
		"temperature": 0.25,
		"response_format": map[string]any{
			"type": "json_object",
		},
	}

	bodyBytes, _ := json.Marshal(reqBody)

	targetURL := ep.baseURL
	if targetURL == "" {
		targetURL = groqAPIURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("creating groq/openai request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+ep.apiKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("groq http call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", newStatusError("groq", resp)
	}

	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return "", fmt.Errorf("decoding groq response: %w", err)
	}

	if len(apiResp.Choices) == 0 {
		return "", fmt.Errorf("empty groq response")
	}

	return apiResp.Choices[0].Message.Content, nil
}

func parseTailoredResult(text string) (*TailoredAssets, error) {
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

	sanitizeTailoredAssets(&result)
	return &result, nil
}

// unownedTech, adayın profilinde kesinlikle bulunmayan teknolojilerdir;
// LLM yanlışlıkla skills_highlight içine koyarsa elenir, ats_keywords içinde ise gap olarak işaretlenir.
var unownedTech = []string{
	"c#", ".net", "asp.net", "php", "laravel", "symfony", "ruby", "rails",
	"swift", "objective-c", "kotlin", "angular", "vue.js", "cobol", "sap",
	"kubernetes", "terraform", "ansible", "kafka", "spark", "hadoop", "pytorch", "tensorflow",
}

func isUnownedSkill(s string) bool {
	low := strings.ToLower(strings.TrimSpace(s))
	for _, bad := range unownedTech {
		if low == bad || strings.Contains(low, bad) {
			return true
		}
	}
	return false
}

func sanitizeTailoredAssets(res *TailoredAssets) {
	kept := make([]string, 0, len(res.SkillsHighlight))
	for _, sk := range res.SkillsHighlight {
		if !isUnownedSkill(sk) {
			kept = append(kept, sk)
		}
	}
	res.SkillsHighlight = kept

	for i, kw := range res.ATSKeywords {
		low := strings.ToLower(kw)
		if isUnownedSkill(kw) && !strings.Contains(low, "gap") && !strings.Contains(low, "eksik") {
			res.ATSKeywords[i] = kw + " (gap — do not claim on CV)"
		}
	}
}

func parseInterviewResult(text string) (*InterviewPrepAssets, error) {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start == -1 || end == -1 || end < start {
		return nil, fmt.Errorf("no valid JSON found in interview prep response")
	}

	var result InterviewPrepAssets
	if err := json.Unmarshal([]byte(text[start:end+1]), &result); err != nil {
		slog.Error("failed to unmarshal interview prep json", "raw", text[start:end+1])
		return nil, fmt.Errorf("unmarshaling interview prep json: %w", err)
	}

	if result.TechnicalQuestions == nil {
		result.TechnicalQuestions = []TechnicalQA{}
	}
	if result.BehavioralQuestions == nil {
		result.BehavioralQuestions = []BehavioralSTAR{}
	}
	if result.QuestionsToAsk == nil {
		result.QuestionsToAsk = []string{}
	}

	return &result, nil
}
