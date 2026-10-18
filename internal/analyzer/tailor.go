package analyzer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
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

// MasterCandidateProfile represents the factual, unalterable master CV of Ulaş Dursun.
const MasterCandidateProfile = `
CANDIDATE FULL NAME: Ulaş Dursun
CONTACT: Istanbul, TR • ulasdursun03@gmail.com • +90 533 407 65 80
LINKS: linkedin.com/in/ulas-dursun • github.com/ulas-dursun

SUMMARY:
Software Engineer focused on backend systems and cloud-native development. Experience building API-driven services, concurrent systems, authentication flows, and containerized deployments using Java, Python, and Go.

EDUCATION & STATUS:
- Bahcesehir University | B.Sc. Software Engineering (2021 – 2026) | Istanbul, TR
- Fluent English (C1/C2). Open to Turkey, Belgium (Brussels), Netherlands, Germany, Switzerland, UK, and EU/Global Remote.

CERTIFICATIONS:
- AWS Certified AI Practitioner — Amazon Web Services (2026)
- AWS Certified Cloud Practitioner (in progress)

WORK EXPERIENCE:
1. Sanlab | Software Engineering Internship | Istanbul | Aug 2024 – Oct 2024
   - Developed a web-based market management system, integrating a JWT-authenticated Go application with a Rust REST API across 12 endpoints, utilizing MongoDB with atomic sequence-based ID generation.
   - Implemented client-server communication modules in Rust using TCP and UDP sockets for direct message exchange between standalone services.
   - Participated in an Agile/Scrum workflow within a 7-person engineering team, contributing to daily stand-ups, sprint planning, and code reviews.
2. Work and Travel Program | Sales & Lifeguard Roles | Jun 2025 – Sep 2025
   - Handled ~100 daily customer interactions while ensuring pool safety for 30-40 guests per shift.

KEY PROJECTS:
1. Distributed Job Processing System | Go, Redis, PostgreSQL, Docker, Prometheus
   - Developed a concurrent job processing system using a 5-goroutine worker pool and Redis with exponential backoff retries, successfully handling load tests of 500 concurrent submissions.
   - Containerized the system with Docker, instrumented metrics with Prometheus (validating 98% of jobs completed in under 100ms) and automated CI pipelines using GitHub Actions.
2. Order & Inventory Management API | Java 21, Spring Boot, PostgreSQL, Docker
   - Designed a REST API with an admin panel and real-time inventory tracking, utilizing Java 21, Spring Boot, JWT authentication, RBAC, and a state machine for order lifecycle transitions.
   - Secured concurrent stock operations using pessimistic locking and implemented integration tests against a PostgreSQL instance via Testcontainers, achieving ~75% line coverage across core domain services.
3. Capstone Project | IoT-Based Smart Indoor Plant Control System | React, Firebase, ESP32
   - Built a real-time IoT dashboard using React and Firebase to monitor plant health and manage remote irrigation via an ESP32-based sensor system.
   - Achieved >99% successful data delivery and an average end-to-end command latency of ~500ms for data transmitted at 60-second intervals.

TECHNICAL SKILLS:
- Programming Languages: Python, Java, Go, Rust, SQL
- Backend & Architecture: REST APIs, Microservices, JWT, Concurrency, TCP/UDP
- Databases: PostgreSQL, MongoDB, Redis
- DevOps & Cloud: Git, Linux (CLI, Bash), Docker, Docker Compose, CI/CD (GitHub Actions), Postman, Prometheus, Grafana, AWS
- Spoken Languages: English (Fluent), Turkish (Native), German (Beginner)
`

// TailorService coordinates ATS CV customization, Cover Letter generation, and Interview Prep via Gemini / Groq failover chain.
type TailorService struct {
	endpoints []providerEndpoint
	client    *http.Client
}

func NewTailorService(cfg *config.GeminiConfig) *TailorService {
	var cfgModel string
	if cfg != nil {
		cfgModel = cfg.Model
	}
	return &TailorService{
		endpoints: buildFailoverChain(cfgModel),
		client:    &http.Client{Timeout: 45 * time.Second},
	}
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
	if len(s.endpoints) == 0 {
		return "", fmt.Errorf("missing AI API key (set GEMINI_API_KEY or GROQ_API_KEY in .env)")
	}

	maxRounds := 3
	for round := 0; round < maxRounds; round++ {
		for idx, ep := range s.endpoints {
			var rawText string
			var err error

			if ep.provider == ProviderGemini {
				rawText, err = s.callGeminiRaw(ctx, ep, prompt)
			} else {
				rawText, err = s.callGroqRaw(ctx, ep, prompt)
			}

			if err == nil {
				if idx > 0 {
					slog.Info("tailor service yedek sağlayıcı ile başarılı oldu", "endpoint", ep.label)
				}
				return rawText, nil
			}

			if idx+1 < len(s.endpoints) {
				slog.Warn("tailor endpoint hatası, sıradaki yedek anahtara/sağlayıcıya geçiliyor...",
					"failed_endpoint", ep.label,
					"next_endpoint", s.endpoints[idx+1].label,
					"error", err.Error(),
				)
				continue
			}

			if strings.Contains(err.Error(), "429") || strings.Contains(err.Error(), "503") {
				backoff := time.Duration((round+1)*3) * time.Second
				slog.Warn("tailor service tüm sağlayıcılar meşgul (429/503), bekleniyor...",
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

	return "", fmt.Errorf("all AI endpoints busy or rate-limited after %d rounds", maxRounds)
}

func (s *TailorService) buildPrompt(title, company, location, description string) string {
	desc := truncateUTF8(strings.TrimSpace(description), 4000)
	if desc == "" {
		desc = "(No detailed description provided in listing — tailor based on job title, company, and core engineering requirements.)"
	}

	return fmt.Sprintf(`You are a Principal Software Engineer and European Tech Hiring Lead coaching Ulaş Dursun.
Your goal is to write a sharp, natural, human-sounding Cover Letter and genuinely tailored CV bullet points for the job below.

CRITICAL ZERO-HALLUCINATION POLICY:
1. NEVER invent companies, degrees, years of experience, or technologies not in the Candidate's Master Profile.
2. Keep all factual numbers intact (12 endpoints, 7-person team, 5-goroutine worker pool, 500 concurrent submissions, 98%% under 100ms, ~75%% coverage, >99%% delivery, ~500ms latency).

COVER LETTER — HUMAN & ENGINEER-TO-ENGINEER VOICE RULES (STRICT):
1. BANNED AI CLICHÉS (NEVER USE THESE PHRASES):
   - "I am thrilled to apply" / "I am excited to apply" / "I am writing to express my strong interest"
   - "Throughout my academic and practical journey"
   - "deep passion for" / "passionate about" / "thrive in environments"
   - "Furthermore, in my personal projects" / "testament to" / "delve" / "synergy"
2. LENGTH & STRUCTURE (CONCISE & PUNCHY — 180 to 220 words total, 3 short paragraphs):
   - Paragraph 1 (Direct Hook, 2-3 sentences): Open directly with what %s is building/solving and why the %s role caught your eye as a backend/systems engineer. Sound like a real engineer speaking to another engineer.
   - Paragraph 2 (Connected Technical Proof, 3-4 sentences): DO NOT copy-paste CV bullet points verbatim into a laundry list! Pick ONLY the 2 most relevant technical experiences from Ulaş's profile (e.g., if real-time/low-latency: pair Sanlab's raw Rust TCP/UDP sockets & Go APIs with the Go/Redis <100ms worker pool or ESP32 ~500ms real-time telemetry; if enterprise/backend: pair Java 21 Spring Boot pessimistic locking & Testcontainers with Go/Rust microservices). Explain *why* building those systems prepared you for %s's specific technical challenges.
   - Paragraph 3 (Pragmatic Close, 2 sentences): Briefly note your readiness to ship clean, containerized code (Docker, CI/CD, Prometheus, AWS Certified AI Practitioner) and end with a straightforward, confident sign-off.

TAILORED CV BULLETS — TRUE DOMAIN ADAPTATION RULES:
1. Do NOT just copy the original CV bullets word-for-word!
2. Rewrite and angle the bullets so they naturally incorporate the target job's terminology and engineering focus while staying 100%% truthful to Ulaş's work.
3. For tailored_project_bullets (4 bullets): Select from ALL 3 projects (Distributed Job Processing in Go/Redis, Order & Inventory API in Java 21/Spring Boot, AND Capstone Real-Time IoT System) based on what fits this job best. If the job involves real-time data, telemetry, streaming, or full-stack, MUST include a bullet from the Capstone IoT project (>99%% delivery, ~500ms latency).

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
  "tailored_project_bullets": ["string", "string", "string", "string"],
  "skills_highlight": ["string", "string"],
  "ats_keywords": ["string", "string"]
}`, company, title, company, MasterCandidateProfile, title, company, location, desc)
}

func (s *TailorService) buildInterviewPrompt(title, company, location, description string) string {
	desc := truncateUTF8(strings.TrimSpace(description), 4000)
	if desc == "" {
		desc = "(No detailed description provided — generate based on job title, company, and candidate profile.)"
	}

	return fmt.Sprintf(`You are a Staff Engineer and Hiring Manager at %s interviewing Ulaş Dursun for the %s position (%s).
Create a practical, high-signal Interview Preparation Kit specifically connecting %s's tech stack/domain with Ulaş's actual CV projects.

REQUIREMENTS:
1. role_focus_summary:
   - 2-3 concise sentences in Turkish summarizing what %s cares about most technically for this role and which of Ulaş's projects (Go/Redis worker pool, Sanlab Go/Rust TCP/UDP, Java 21 Spring Boot pessimistic locking, or ESP32 real-time telemetry) should be his centerpiece in the interview.
2. technical_questions (EXACTLY 5 items):
   - Combine the job posting's technical requirements with Ulaş's specific projects (Sanlab Go/Rust JWT & TCP/UDP sockets, Go 5-goroutine Redis worker pool with exponential backoff & Prometheus, Java 21 Spring Boot PostgreSQL pessimistic locking & Testcontainers, ESP32/React real-time telemetry).
   - "question": The exact technical / system-design question the interviewer will ask (in English, with Turkish explanation in parentheses if helpful).
   - "why_they_ask": 1 concise sentence in Turkish explaining what the interviewer is testing.
   - "answer_strategy": Concrete, senior-level talking points (in Turkish, keeping technical terms in English) showing how Ulaş should answer using his real metrics and architectural trade-offs.
3. behavioral_questions (EXACTLY 3 items):
   - "question": The behavioral question in English.
   - "star_story_guide": A ready-to-use STAR (Situation, Task, Action, Result) response guide in Turkish based on Ulaş's real experience (Sanlab 7-person Scrum team, debugging concurrency/latency in projects, or high-responsibility Work & Travel customer/safety management).
4. questions_to_ask (EXACTLY 2 items):
   - 2 sharp, impressive engineering questions (in English, with a brief Turkish note on why it's effective) that Ulaş should ask the interviewers at the end of the call.

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
}`, company, title, location, company, company, MasterCandidateProfile, title, company, location, desc)
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
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("gemini status %d: %s", resp.StatusCode, string(b))
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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, groqAPIURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("creating groq request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+ep.apiKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("groq http call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("groq status %d: %s", resp.StatusCode, string(b))
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

	return &result, nil
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
