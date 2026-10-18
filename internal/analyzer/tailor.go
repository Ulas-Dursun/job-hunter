package analyzer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
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

// TailorService coordinates ATS CV customization and Cover Letter generation via Gemini / Groq.
type TailorService struct {
	provider Provider
	apiKey   string
	model    string
	client   *http.Client
}

func NewTailorService(cfg *config.GeminiConfig) *TailorService {
	geminiKey := strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
	groqKey := strings.TrimSpace(os.Getenv("GROQ_API_KEY"))

	provider := ProviderGroq
	apiKey := groqKey
	model := "gemini-flash-lite-latest"
	if cfg != nil && cfg.Model != "" {
		model = cfg.Model
	}

	if geminiKey != "" {
		provider = ProviderGemini
		apiKey = geminiKey
		if model == "" || !strings.HasPrefix(model, "gemini") {
			model = "gemini-flash-lite-latest"
		}
	} else if groqKey != "" {
		provider = ProviderGroq
		apiKey = groqKey
		if model == "" || strings.HasPrefix(model, "gemini") {
			model = "qwen/qwen3.8-27b"
		}
	}

	return &TailorService{
		provider: provider,
		apiKey:   apiKey,
		model:    model,
		client:   &http.Client{Timeout: 45 * time.Second},
	}
}

// GenerateTailoredAssets produces an ATS-optimized cover letter, tailored summary, and bullet points.
// Automatically retries on 429 (Rate Limit) or 503 (Server Busy).
func (s *TailorService) GenerateTailoredAssets(
	ctx context.Context,
	title, company, location, description string,
) (*TailoredAssets, error) {
	if s.apiKey == "" {
		return nil, fmt.Errorf("missing AI API key (set GEMINI_API_KEY or GROQ_API_KEY in .env)")
	}

	prompt := s.buildPrompt(title, company, location, description)
	maxRetries := 3

	for i := 0; i < maxRetries; i++ {
		var res *TailoredAssets
		var err error

		if s.provider == ProviderGemini {
			res, err = s.callGemini(ctx, prompt)
		} else {
			res, err = s.callGroq(ctx, prompt)
		}

		if err != nil {
			if strings.Contains(err.Error(), "429") || strings.Contains(err.Error(), "503") {
				backoff := time.Duration((i+1)*3) * time.Second
				slog.Warn("tailor service rate limited or busy (429/503), retrying...",
					"provider", s.provider,
					"attempt", i+1,
					"wait", backoff.String(),
				)
				select {
				case <-time.After(backoff):
					continue
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			return nil, err
		}

		return res, nil
	}

	return nil, fmt.Errorf("%s busy or rate-limited after %d retries", s.provider, maxRetries)
}

func (s *TailorService) buildPrompt(title, company, location, description string) string {
	desc := truncateUTF8(strings.TrimSpace(description), 4000)
	if desc == "" {
		desc = "(No detailed description provided in listing — tailor based on job title, company, and core engineering requirements.)"
	}

	return fmt.Sprintf(`You are an elite Senior Tech Recruiter and ATS Optimization Specialist at a top global technology company.
Your goal is to generate an exceptional, highly-targeted, 100%% ATS-compliant Cover Letter and tailor the candidate's CV bullet points specifically for the job below.

CRITICAL INSTRUCTIONS & ZERO-HALLUCINATION POLICY:
1. NEVER invent companies, degrees, years of experience, or technologies that are NOT in the Candidate's Master Profile.
2. The candidate is Ulaş Dursun, a high-potential Software Engineer / New Graduate (2026) specializing in Go, Java, Python, Rust, PostgreSQL, MongoDB, Redis, Docker, and Distributed Systems.
3. Align the candidate's REAL achievements with the job posting's keywords using Google's XYZ formula: "Accomplished [X] as measured by [Y], by doing [Z]".
4. COVER LETTER REQUIREMENTS:
   - Written in natural, high-impact, professional English.
   - Tone: Confident, technical, articulate, not robotic or overly cliché.
   - Target length: 3 to 4 concise paragraphs (~250-330 words).
   - Paragraph 1: Enthusiastic hook referencing %s and the %s role, highlighting core backend/cloud/systems alignment.
   - Paragraph 2: Deep technical proof citing the Sanlab internship (Go/Rust/MongoDB/JWT/TCP-UDP) and/or Distributed Job Processing (Go, 5-goroutine Redis worker pool, Prometheus) or Order Management (Java 21, Spring Boot, PostgreSQL pessimistic locking, Testcontainers).
   - Paragraph 3: Relevant engineering mindset, cloud/DevOps proficiency (AWS Certified AI Practitioner, Docker, CI/CD GitHub Actions), and enthusiasm for the team's engineering challenges.
   - Paragraph 4: Professional sign-off and call to action.
5. TAILORED CV ASSETS:
   - tailored_summary: 3 impactful sentences tailored for this specific role and company.
   - tailored_experience_bullets: 3 bullet points adapted from Sanlab internship experience highlighting relevant skills.
   - tailored_project_bullets: 3-4 bullet points from projects (Distributed Job Processing / Order & Inventory Management / Capstone IoT) matching the posting stack.
   - skills_highlight: Ordered list of candidate skills most relevant to this specific role.
   - ats_keywords: Top 6-10 keywords extracted from the job posting that are addressed.

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
  "tailored_experience_bullets": ["string", "string"],
  "tailored_project_bullets": ["string", "string"],
  "skills_highlight": ["string", "string"],
  "ats_keywords": ["string", "string"]
}`, company, title, MasterCandidateProfile, title, company, location, desc)
}

func (s *TailorService) callGemini(ctx context.Context, prompt string) (*TailoredAssets, error) {
	reqURL := fmt.Sprintf(geminiAPIURL, s.model)

	reqPayload := map[string]any{
		"contents": []map[string]any{
			{
				"parts": []map[string]any{
					{"text": prompt},
				},
			},
		},
		"generationConfig": map[string]any{
			"temperature":      0.2,
			"maxOutputTokens":  3000,
			"responseMimeType": "application/json",
		},
	}

	bodyBytes, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, fmt.Errorf("marshaling gemini payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("creating gemini request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", s.apiKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini http call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gemini status %d: %s", resp.StatusCode, string(b))
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
		return nil, fmt.Errorf("decoding gemini response: %w", err)
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("empty gemini response candidates")
	}

	rawText := geminiResp.Candidates[0].Content.Parts[0].Text
	return parseTailoredResult(rawText)
}

func (s *TailorService) callGroq(ctx context.Context, prompt string) (*TailoredAssets, error) {
	reqBody := map[string]any{
		"model": s.model,
		"messages": []map[string]any{
			{
				"role":    "user",
				"content": prompt,
			},
		},
		"max_tokens":  3000,
		"temperature": 0.2,
		"response_format": map[string]any{
			"type": "json_object",
		},
	}

	bodyBytes, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, groqAPIURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("creating groq request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.apiKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("groq http call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("groq status %d: %s", resp.StatusCode, string(b))
	}

	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decoding groq response: %w", err)
	}

	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("empty groq response")
	}

	rawText := apiResp.Choices[0].Message.Content
	return parseTailoredResult(rawText)
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
