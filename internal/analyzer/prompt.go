package analyzer

import (
	"fmt"
	"strings"
	"sync"

	"github.com/ulasdursun/job-hunter/internal/config"
)

// PromptBuilder, aday profili ile iş ilanını birleştiren Gemini/Groq prompt'u üretir.
// Her çağrıda aynı profil kullanılır — bir kez build edilip cache'lenir.
// Ayrıca kullanıcının Kanban kararlarından (applied/interview vs skipped/rejected)
// öğrenilen dinamik geri bildirim özeti (feedbackText) çalışma zamanında güncellenebilir.
type PromptBuilder struct {
	profileText  string
	mu           sync.RWMutex
	feedbackText string
}

func NewPromptBuilder(cfg *config.CandidateConfig) *PromptBuilder {
	return &PromptBuilder{profileText: buildProfile(cfg)}
}

// SetFeedback, kullanıcının geçmiş başvuru/eleme kararlarından üretilen özeti günceller.
func (p *PromptBuilder) SetFeedback(feedback string) {
	p.mu.Lock()
	p.feedbackText = strings.TrimSpace(feedback)
	p.mu.Unlock()
}

// Build, tek bir iş ilanı için tam prompt'u üretir.
// Description 4000 rune ile sınırlandırılır — Go ve Python tarafındaki limitlerle tam uyumludur.
func (p *PromptBuilder) Build(title, company, location, description string) string {
	desc := truncateUTF8(strings.TrimSpace(description), 4000)

	p.mu.RLock()
	fb := p.feedbackText
	p.mu.RUnlock()

	feedbackBlock := ""
	if fb != "" {
		feedbackBlock = "\nCANDIDATE HISTORICAL PREFERENCES (Calibrate scoring using his real past decisions):\n" + fb + "\n"
	}

	return fmt.Sprintf(`You are an elite technical talent partner and hiring advisor evaluating career opportunities for a versatile Software Engineering graduate (Ulaş Dursun).
Analyze this job posting and return ONLY a valid JSON object. No markdown. No explanation. No extra text.

CANDIDATE PROFILE:
%s%s
=== BEGIN JOB POSTING ===
Title: %s
Company: %s
Location: %s
Description: %s
=== END JOB POSTING ===

Your task:
Determine whether this role is a strong career match for the candidate across his 4 core target pillars (plus broad 0-2 year Software/Data/Solutions roles):
1. Backend & Systems Software: Go, Java (Spring Boot), Python, Rust, Distributed Systems, Concurrency, REST APIs, Microservices.
2. Cloud, Platform & DevOps / SRE: Docker, Linux (Bash), AWS (EC2, S3, IAM, VPC, Certified AI Practitioner), CI/CD (GitHub Actions), Prometheus, Grafana.
3. Data Engineering, Analytics & ML: SQL, Python, PostgreSQL, Real-time Data Processing, Data Pipelines, Analytics, Business Intelligence, Machine Learning fundamentals (university coursework).
4. Systems & Aerospace Software: Surveillance data decoding, ASTERIX E-ARCO (ARTAS, SDDS, SASS-C), JSON schema validation, TCP/UDP sockets, C++/Python/Java clients.

Use only evidence explicitly present in the candidate profile and job posting.
Do not hallucinate skills, experience, or projects.
Prefer "watch" over "skip" when the role is entry/junior level (0-2 years) or an adjacent growth opportunity.
Prefer "apply" when there is strong alignment with any of the 4 pillars at an entry/junior level.

Return this exact JSON structure:
{
  "match_score": <float 0.0-10.0>,
  "role_family": "<primary|secondary>",
  "skill_overlap": [<matching skills from candidate profile>],
  "missing_skills": [<required skills candidate lacks>],
  "red_flags": [<concerns: wrong stack, too senior, irrelevant domain, visa issue>],
  "action": "<apply|watch|skip>",
  "reasoning": "<2 sentence explanation>"
}

CRITICAL RULES (MUST OBEY):

1. EXPERIENCE & SCORING MATRIX (STRICT GRANULAR CALIBRATION — DO NOT CLUSTER AT 6.8):
   - 8.2 - 9.5 (JACKPOT MATCH — Triggers Instant Telegram Notification):
     * EXCLUSIVELY for top-tier entry-level alignments: explicitly Junior, New Grad, Graduate, Associate, or 0-2 years.
     * Core tech stack aligns 80%%+ with candidate's primary skills (Go, Java/Spring Boot, Python backend/data, Docker/Linux/AWS/APIs, or EUROCONTROL/aviation surveillance systems).
     * No major missing requirements, no explicit visa/language blockers.
   - 7.4 - 8.1 (STRONG FIT / APPLY — Included in Daily Evening Digest):
     * Entry-level or junior role (0-2 years) in target cities (Istanbul, Brussels, Amsterdam, Berlin, Munich, London, Zurich, Geneva, or Remote) with solid fundamentals, even if visa sponsorship is not explicitly mentioned or candidate has 1-2 secondary tool gaps (e.g., Kubernetes, Terraform, Kafka) that can be learned on the job.
     * Or ~2 year role where candidate's strong project portfolio + EUROCONTROL Brussels traineeship makes it a strong application.
   - 6.8 - 7.3 (HIGH WATCH / VIABLE STRETCH — Eligible for Evening Digest):
     * RESERVED ONLY FOR: (a) 0-2 year / Junior / Graduate roles requiring an adjacent stack transition (e.g., Full-stack TypeScript/React + Python/Java, or Solutions/BI/Technical Analyst with SQL/Python), OR (b) ~2 year (light mid-level) roles in target cities where primary stack (Java Spring Boot, Go, Python, AWS) aligns strongly.
     * Any role requiring 3+ years of experience MUST NOT be placed in 6.8-7.3 unless it explicitly welcomes New Grads/Juniors.
   - 5.8 - 6.5 (MODERATE WATCH — Mid-Level Stretch, Excluded from Evening Digest):
     * Roles requiring 3-4 years of experience where tech stack overlaps, OR roles in non-target European cities (e.g. Tallinn, Budapest, Warsaw) without remote flexibility, OR roles missing multiple tools.
     * HARD CAP: If a role requires 3+ years of experience, cap match_score at <= 6.5 (use 5.8, 6.0, 6.2, 6.4, or 6.5).
   - 5.0 - 5.7 (LOW WATCH):
     * Heavy stretch (3-4 years + adjacent stack or domain gap).
   - Below 5.0 (SKIP):
     * Incompatible roles, 4+ years required without strong match, 5+ years required in general, security clearance required, non-tech disciplines, gig/labeling work, or explicit visa/language barriers.
   - ANTI-CLUSTERING RULE: Do NOT lazily assign 6.8 to every "watch" job. Differentiate using the full decimal scale (e.g., 5.4, 5.9, 6.1, 6.4, 6.9, 7.2, 7.6, 8.1, 8.6).

2. BROAD NEW GRAD / 0-2 YEARS SOFT FLEXIBILITY:
   - A Software Engineering graduate with Python, Java, Go, SQL, C++, Cloud, and Data Pipeline experience can succeed across a wide spectrum of 0-2 year roles (Backend, Data Analyst, Data Engineer, Junior ML/AI, Cloud/DevOps, Solutions/Integration Engineer, Technical Analyst, Graduate Rotational Programs).
   - If the job is explicitly for "New Grad", "Junior", "Associate", "Graduate", "Trainee", or "0-2 years", prioritize engineering & analytical fundamentals over exact tool names.

2b. TOOL GAP FLEXIBILITY:
   If the candidate is missing only 1-2 secondary infrastructure/tool requirements (e.g., Kubernetes in production, Terraform, Spark/Kafka) but core fundamentals AND primary tech stack align well:
   - Do NOT treat this as disqualifying. Score in the 7.0-7.9 range (or 8.0+ if explicitly graduate/junior).
   - List the specific gap in missing_skills.

2c. SOFTWARE ENGINEER / FULLSTACK FLEXIBILITY:
   If a role is titled "Software Engineer", "Associate Software Engineer", or "Full Stack Developer", and the core workload is backend, APIs, data, or cloud (with secondary React/TypeScript tasks):
   - Do NOT disqualify. Candidate built a React/Firebase real-time dashboard in his Capstone IoT project.

2d. ML / DATA SCIENCE / ANALYTICS NEW-GRAD CHANCE:
   The candidate is strong in Python, SQL, Real-time Data Processing, Data Pipelines, holds the AWS Certified AI Practitioner (2026) credential, and completed university Machine Learning coursework.
   If a role is explicitly New Grad / Graduate / Junior / Entry-level / 0-2 years in ML, Data Science, AI, Business Intelligence, or Data Analytics:
   - Do NOT skip merely because production ML experience or a master's degree is not proven.
   - Score 6.5-7.8 when Python/SQL/Data fundamentals fit. Add any real gap (e.g. "No production PyTorch/Spark experience") to missing_skills.
   - Still skip PhD-required / Senior Research Scientist roles.

3. HARD FILTERS:
   - If the role title explicitly matches the "Explicitly Rejected Roles", it is an immediate skip (< 4.0).
   - If the role is clearly unrelated to technology/engineering/analytics (e.g. sales, marketing, HR, legal, nursing, civil/mechanical engineering), set action to "skip" (< 3.5).

4. ROLE FAMILY & THE 4 TARGET PILLARS:
   - "primary" for Backend, Software Engineer, Cloud/DevOps/SRE, Systems/Aerospace (C++/Go/Java/Python).
   - "secondary" for Data Engineer, Data Analyst, Data Scientist, BI Developer, ML/AI Engineer, Solutions/Integration Analyst.

5. DECISION TIE-BREAKERS:
   - If the role is 0-2 years / junior and the stack is adjacent, prefer "watch" or "apply" over "skip".
   - If the role is senior (5+ years) even with a close stack, keep the score low (< 4.5).

6. DOMAIN SPECIALIZATION CHECK:
   Do not inflate the score just because a single language matches in a highly specialized senior domain (e.g., senior quantitative trading or embedded firmware requiring 5+ years).

7. UNRELATED FIELD CHECK:
   If the posting is from an unrelated engineering discipline (mechanical, civil, chemical, biomedical), set match_score below 3.0 and action "skip".

8. VISA, CITIZENSHIP & EUROPEAN TARGET CITY STRATEGY (BRUSSELS, AMSTERDAM, BERLIN, MUNICH, ZURICH, LONDON):
   - The candidate is a Turkish citizen with an official EUROCONTROL Traineeship Agreement in Brussels, Belgium (16 Nov 2026 – 30 Apr 2027, NMD/INF/CNS Unit) and is actively preparing for European & Turkish full-time roles.
   - HARD DISQUALIFIERS (STRICT SKIP < 3.5):
     1. EXPLICIT NO-SPONSORSHIP: Explicitly mandates "EU/EEA citizenship required", "No visa sponsorship provided", or "Must already hold permanent unrestricted work permit" -> score 2.0-3.0, action "skip".
     2. NON-ENGLISH MANDATORY LANGUAGE BARRIER: Explicitly mandates fluent French, German, or Dutch ("Français courant", "Deutsch C1", "Nederlands vereist") without accepting English -> score 2.0-3.5, action "skip".
     3. US / OVERSEAS ON-SITE: On-site in US/Canada/Australia/APAC -> score 1.5-2.5, action "skip".

   - TARGET CITY 0-2 YEAR / GRADUATE OPPORTUNITY RULE (WORTH TRYING — DO NOT PENALIZE SILENCE ON VISA):
     * Most European employers do NOT explicitly mention visa sponsorship in job ads even when they sponsor strong junior/graduate candidates (especially someone already in Brussels at EUROCONTROL).
     * Therefore, if an English-speaking role in Belgium (Brussels), Netherlands (Amsterdam/Rotterdam/Eindhoven), Germany (Berlin/Munich), Switzerland, or UK does NOT forbid sponsorship:
       a) If it is explicitly Junior / Graduate / New Grad / Associate / 0-2 years AND tech fit is strong:
          -> Score 7.2 - 8.2 and set action to "apply"! Worth trying!
          -> If the posting does not explicitly confirm sponsorship, note in red_flags: "Visa sponsorship not explicitly stated (0-2 yr target-city role — worth applying)".
       b) If it is Light Mid-level (~2 years) and does not explicitly confirm sponsorship:
          -> Score 6.6 - 7.1 ("watch" or borderline "apply" depending on stack overlap).
       c) If it requires 3-4 years of experience and does not explicitly confirm sponsorship:
          -> Cap at 5.8 - 6.5 and set action to "watch" (do NOT give 6.8 to 3+ year roles).

9. STEALTH SENIORITY & SCOPE DETECTION:
   - If a job omits years of experience but requires leading company-wide architecture, mentoring senior engineers, or owning enterprise roadmaps, treat it as Senior: set match_score below 4.5 and action "skip".

10. GIG WORK, DATA LABELING & NON-ENGINEERING NOISE FILTER:
   - "AI Trainer", "Data Annotation/Labeler" (Outlier, Alignerr, Scale AI), or basic "IT Helpdesk/Desktop Support" -> match_score below 3.5, action "skip".

OUTPUT RULES:
- Return ONLY the JSON object.
- Use double quotes for all strings. No markdown fences.
- Keep reasoning to exactly 2 sentences.
- skill_overlap, missing_skills, and red_flags must be concise and evidence-based.`,
		p.profileText, feedbackBlock, title, company, location, desc,
	)
}

// buildProfile, config'den ve doğrulanmış CV/EUROCONTROL belgelerinden aday profil metnini oluşturur.
func buildProfile(cfg *config.CandidateConfig) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Name: %s\n", cfg.Name))
	sb.WriteString(fmt.Sprintf("Status: %s, B.Sc. Software Engineering (Bahcesehir University, 2021 – %d)\n", cfg.Status, cfg.GraduationYear))
	sb.WriteString(fmt.Sprintf("Location & Mobility: %s (Official EUROCONTROL Trainee in Brussels, Belgium from 16 Nov 2026 to 30 Apr 2027; open to Istanbul/Turkey, Belgium, Netherlands, Germany, Switzerland, UK, and EU/Global Remote)\n", cfg.Location))
	sb.WriteString(fmt.Sprintf("Languages: %s\n", strings.Join(cfg.Skills.Languages, ", ")))
	sb.WriteString(fmt.Sprintf("Frameworks: %s\n", strings.Join(cfg.Skills.Frameworks, ", ")))
	sb.WriteString(fmt.Sprintf("Databases: %s\n", strings.Join(cfg.Skills.Databases, ", ")))
	sb.WriteString(fmt.Sprintf("Infrastructure & DevOps: %s\n", strings.Join(cfg.Skills.Infra, ", ")))
	sb.WriteString("Cloud & Certifications: AWS Certified AI Practitioner (2026), AWS (EC2, S3, IAM, VPC)\n")
	sb.WriteString(fmt.Sprintf("Core Concepts & Data: %s, Real-time Data Processing, Data Pipelines\n", strings.Join(cfg.Skills.Concepts, ", ")))
	sb.WriteString(fmt.Sprintf("OS & Tools: %s\n", strings.Join(cfg.Skills.OS, ", ")))
	sb.WriteString(fmt.Sprintf("Primary roles: %s\n", strings.Join(cfg.TargetRoles.Primary, ", ")))
	sb.WriteString(fmt.Sprintf("Secondary roles: %s\n", strings.Join(cfg.TargetRoles.Secondary, ", ")))
	sb.WriteString(fmt.Sprintf("Preferred seniority: %s\n", strings.Join(cfg.Seniority.Preferred, ", ")))
	sb.WriteString(fmt.Sprintf("Acceptable seniority: %s\n", strings.Join(cfg.Seniority.Acceptable, ", ")))

	rejectedStr := strings.Join(cfg.Seniority.Rejected, ", ")
	sb.WriteString(fmt.Sprintf("Explicitly Rejected Roles: %s (CRITICAL: Never match to these roles regardless of stack alignment.)\n", rejectedStr))
	if len(cfg.PriorityCompanies) > 0 {
		sb.WriteString(fmt.Sprintf("Priority Target Companies: %s (If the job is at one of these companies AND is already a valid entry/junior technical match >= 6.5, award a +0.4 priority tie-breaker bonus; NEVER rescue senior or unrelated roles).\n", strings.Join(cfg.PriorityCompanies, ", ")))
	}

	sb.WriteString("Work Experience & Incoming European Assignment:\n")
	sb.WriteString("- EUROCONTROL (Brussels, Belgium | 16 Nov 2026 – 30 Apr 2027, Official Agreement 46449_TRAI_2026_NMD): Incoming Software Engineering Trainee in the NMD/INF/CNS Unit (Network Management Directorate). Developing, validating, and integrating ASTERIX E-ARCO C++ Core Library components used in operational air traffic management systems (ARTAS, SDDS, SASS-C); building advanced JSON schema validation modules, maintaining Python client interfaces, contributing to Java client & web interfaces, and automating unit/regression tests within CI/CD.\n")
	sb.WriteString("- Sanlab (Istanbul | Aug 2024 – Oct 2024): Software Engineering Internship. Developed a web-based market management system integrating a JWT-authenticated Go application with a Rust REST API across 12 endpoints (MongoDB with atomic sequence-based ID generation); implemented client-server communication modules in Rust using TCP and UDP sockets; collaborated in a 7-person Agile/Scrum engineering team.\n")
	sb.WriteString("- Work and Travel USA (Jun 2025 – Sep 2025): Sales & Lifeguard Roles. Handled ~100 daily customer interactions in English while ensuring pool safety for 30-40 guests per shift.\n")

	sb.WriteString("Key Engineering Projects:\n")
	sb.WriteString("- Distributed Job Processing System (Go, Redis, PostgreSQL, Docker, Prometheus): Built a concurrent job processing pipeline using a 5-goroutine worker pool and Redis with exponential backoff retries, handling load tests of 500 concurrent submissions; validated 98% of jobs completed in under 100ms via Prometheus and automated CI with GitHub Actions.\n")
	sb.WriteString("- Order & Inventory Management API (Java 21, Spring Boot, PostgreSQL, Docker): Designed a REST API with JWT authentication, RBAC, and an order lifecycle state machine; secured concurrent stock operations using pessimistic locking and implemented PostgreSQL integration tests via Testcontainers (~75% core domain line coverage).\n")
	sb.WriteString("- Capstone Project — IoT-Based Smart Indoor Plant Control System (React, Firebase, ESP32): Built a real-time IoT data pipeline and dashboard for sensor telemetry and remote irrigation, achieving >99% data delivery and ~500ms average end-to-end command latency.\n")

	sb.WriteString("Spoken Languages: English (Fluent C1/C2), Turkish (Native), German (Beginner).\n")

	return sb.String()
}

// truncateUTF8 safely truncates a string to a maximum number of runes (not bytes),
// preventing invalid UTF-8 characters at the boundary.
func truncateUTF8(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}

	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}

	return string(r[:maxRunes]) + "..."
}
