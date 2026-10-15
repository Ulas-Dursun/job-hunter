package analyzer

import (
	"fmt"
	"strings"

	"github.com/ulasdursun/job-hunter/internal/config"
)

// PromptBuilder, aday profili ile iş ilanını birleştiren Gemini/Groq prompt'u üretir.
// Her çağrıda aynı profil kullanılır — bir kez build edilip cache'lenir.
type PromptBuilder struct {
	profileText string
}

func NewPromptBuilder(cfg *config.CandidateConfig) *PromptBuilder {
	return &PromptBuilder{profileText: buildProfile(cfg)}
}

// Build, tek bir iş ilanı için tam prompt'u üretir.
// Description 4000 rune ile sınırlandırılır — Go ve Python tarafındaki limitlerle tam uyumludur.
func (p *PromptBuilder) Build(title, company, location, description string) string {
	desc := truncateUTF8(strings.TrimSpace(description), 4000)

	return fmt.Sprintf(`You are an elite technical talent partner and hiring advisor evaluating career opportunities for a versatile Software Engineering graduate (Ulaş Dursun).
Analyze this job posting and return ONLY a valid JSON object. No markdown. No explanation. No extra text.

CANDIDATE PROFILE:
%s

=== BEGIN JOB POSTING ===
Title: %s
Company: %s
Location: %s
Description: %s
=== END JOB POSTING ===

Your task:
Determine whether this role is a strong career match for the candidate across his 4 core target pillars:
1. Backend & Systems Software: Go, Java (Spring Boot), Python, Distributed Systems, Concurrency, REST APIs, Microservices.
2. Cloud, Platform & DevOps / SRE: Docker, Linux (Bash), AWS, CI/CD (GitHub Actions), Prometheus, Grafana.
3. Data Engineering & Analytics: SQL, Python, PostgreSQL, Data Pipelines, Analytics, Business Intelligence.
4. Systems & Aerospace Software: Surveillance data decoding, ASTERIX E-ARCO, JSON validation, TCP/UDP sockets, C++/Go.

Use only evidence explicitly present in the candidate profile and job posting.
Do not hallucinate skills, experience, or projects.
Prefer "watch" over "skip" when the role is entry/junior level or an adjacent growth opportunity.
Prefer "apply" when there is strong alignment with any of the 4 pillars.

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

1. EXPERIENCE & SCORING MATRIX (STRICT CALIBRATION):
   - 8.5 - 10.0 (JACKPOT MATCH — Triggers Real-Time Notification):
     * EXCLUSIVELY for top-tier entry-level alignments: explicitly Junior, New Grad, Associate, or 0-2 years.
     * Core tech stack aligns 80%%+ with candidate's primary skills (Go, Java/Spring Boot, Python data engineering, Docker/Linux/APIs, or EUROCONTROL/aviation surveillance systems).
     * No major missing requirements, no visa/location blockers.
   - 7.0 - 8.4 (STRONG FIT / APPLY):
     * Entry-level or junior role (0-2 years) with solid fundamentals, but candidate has 1-2 secondary tool gaps (e.g., specific cloud nuances, Kubernetes in production) that can be learned on the job.
     * Or ~3 years required where candidate's strong project portfolio makes it a viable high-probability application.
   - 5.0 - 6.9 (WATCH / STRETCH OPPORTUNITY):
     * 2-4 years required with strong stack match, OR junior roles requiring an adjacent stack transition (e.g. Node.js or C# backend), OR mid-level roles that represent a significant stretch.
   - Below 5.0 (SKIP):
     * Incompatible roles, 4+ years required without match, 5+ years required in general, non-engineering disciplines, gig/labeling work, or unresolvable visa/location barriers.

2. NEW GRAD / JUNIOR FLEXIBILITY:
   - If the job is explicitly for "New Grad", "Junior", "Associate", "Graduate", or "Trainee", prioritize engineering fundamentals over exact syntax or exact years.
   - Strong fundamentals include: REST, HTTP, APIs, databases, SQL/NoSQL, OOP, Git, Docker, cloud basics, testing, concurrency.
   - Do not penalize heavily for missing years if the role is clearly entry-level and the candidate shows learning potential plus adjacent stack fit.

2b. TOOL GAP FLEXIBILITY:
   If the candidate is missing only 1-2 secondary infrastructure/tool requirements
   (e.g., deep Kubernetes production experience, Terraform, a specific cloud provider nuance)
   but core fundamentals AND primary tech stack (Go/Java/Python/Rust, databases,
   concurrency, APIs, containerization basics with Docker) align well:
   - Do NOT treat this as disqualifying.
   - Score in the 7.0-8.4 range depending on overall fit.
   - List the specific gap in missing_skills — do not skip solely because of it.

2c. SOFTWARE ENGINEER / FULLSTACK FLEXIBILITY:
   If a role is titled "Software Engineer", "Associate Software Engineer", or "Full Stack Developer",
   and the core workload is fundamentally backend, APIs, databases, or cloud infrastructure (with minor secondary
   frontend tasks like React/TypeScript):
   - Do NOT disqualify. Candidate has foundational React/HTML/CSS exposure from capstone IoT project.
   - Disqualify only if the role is exclusively/primarily frontend UI design (e.g., pure React/Angular/Vue/CSS developer).

3. HARD FILTERS:
   - If the role title explicitly matches the "Explicitly Rejected Roles", it is an immediate skip (< 4.0).
   - If the role is clearly unrelated to any of the 4 target pillars (e.g. sales, marketing, HR, legal, mobile-only), set action to "skip" and keep the score low (< 3.5).
   - If the posting contains a senior title but the description is junior-sounding, use the stricter interpretation.

4. ROLE FAMILY & THE 4 TARGET PILLARS:
   - "primary" for:
     * Backend Engineering: Backend Developer, Software Engineer, Go/Java/Python Engineer.
     * Cloud / DevOps / SRE: Platform Engineer, Cloud Engineer, DevOps Engineer, Site Reliability Engineer, Systems Engineer.
     * Data Engineering & Analytics: Data Engineer, Data Analyst, Data Scientist, Analytics Engineer.
     * Aerospace & Air Traffic Management Systems: Surveillance data, ATM systems, CNS engineering.
   - "secondary" for BI Developer, ML Research, Pipeline Developer, or adjacent technical support roles.

5. DECISION TIE-BREAKERS:
   - If the role is junior and the stack is adjacent, prefer "watch" over "skip".
   - If the role is senior and the stack is close, keep the score low (< 5.0).
   - If the evidence is mixed, choose the more conservative score.
   - Do not reward buzzwords without concrete evidence.

6. DOMAIN SPECIALIZATION CHECK:
   Do not inflate the score just because a single language matches (e.g., "Rust" matches but the role requires quantitative finance, algorithmic trading, or computer vision). Cap at 6.0 unless it aligns with candidate's actual projects.

7. UNRELATED FIELD CHECK:
   If the posting is from an unrelated engineering discipline (mechanical, civil, chemical, biomedical) that merely contains the word "engineer" in the title, set match_score below 3.0 and action "skip".

8. VISA, CITIZENSHIP & LOCATION ELIGIBILITY:
   - The candidate is a Turkish citizen (non-EU national). He will be based in Brussels (Software Engineering Trainee at EUROCONTROL) and Istanbul.
   - TARGET REGIONS: Turkey, Belgium (Brussels), Netherlands (Amsterdam), Germany (Berlin/Munich), Switzerland, UK, or Remote (EU/Global).
   - STRICT VISA/CITIZENSHIP FILTER:
     * If the posting explicitly mandates "EU citizenship required / must hold EU passport", "Only open to EU/EEA nationals", "No visa sponsorship provided", or "Must already hold unrestricted local work permit with no sponsorship":
     * Candidate cannot legally accept the job. You MUST set match_score below 4.0, set action to "skip", and flag "No visa sponsorship / EU citizenship required" in red_flags.
     * Exception: International organizations where Turkey is a member state (e.g. EUROCONTROL), companies explicitly stating "Visa sponsorship available" or marked "[Visa Sponsored]", or roles open to remote contract from Turkey.
   - STRICT US / OVERSEAS ON-SITE FILTER:
     * Postings located in the US, Canada, Australia, etc., that require physical on-site presence or local US work authorization (Green Card / US Citizen / H1-B holder) are impossible for a fresh graduate abroad.
     * You MUST set match_score below 3.0, set action to "skip", and flag "Ineligible location: US/Overseas on-site without remote option" in red_flags.
     * Remote roles from US/international companies open to global/EU remote contractors are acceptable.
   - LANGUAGE CHECK:
     * If the job explicitly mandates fluent French ("Français obligatoire / courant") or Dutch ("Nederlands vereist") as primary working language and does NOT accept English, deduct 2.5 from match_score and flag "Requires French/Dutch" in red_flags.
     * If English is accepted or the company operates in English, treat the candidate as fully language-eligible (Fluent English C1/C2).

9. STEALTH SENIORITY & SCOPE DETECTION:
   - If a job does not explicitly state a numerical years requirement, but lists expectations such as:
     * Leading architectural design, system architecture, or tech roadmap
     * Mentoring mid/senior engineers or direct team leadership
     * Ownership of company-wide production infrastructure or enterprise strategies
     * Extensive multi-year track record in high-scale distributed systems in production
   - This role is implicitly Senior/Lead/Architect. You MUST set match_score below 4.5, set action to "skip", and add "Implicit senior scope / Architectural ownership" to red_flags.
   - Do NOT let a job pass as junior just because it omitted a numerical year requirement.

10. GIG WORK, DATA LABELING & NON-ENGINEERING NOISE FILTER:
   - Postings titled or describing:
     * "AI Trainer", "AI Data Annotation Specialist", "AI Tutor", "Prompt Evaluation Contractor", "Data Labeler" (e.g. Alignerr, Outlier, Remotasks, Scale AI, Appen)
     * "IT Support", "Helpdesk", "Desktop Support Technician", "Customer Support Engineer"
     * Mid/Senior DevOps roles that require 4+ years on-call production infrastructure ownership
   - These are gig work or IT support, NOT software/data engineering career paths.
   - You MUST set match_score below 3.5, set action to "skip", and add "Gig work / AI data labeling or IT support role" to red_flags.

OUTPUT RULES:
- Return ONLY the JSON object.
- Use double quotes for all strings.
- Do not wrap in markdown code fences.
- Keep reasoning to exactly 2 sentences.
- skill_overlap and missing_skills must be concise and evidence-based.
- red_flags must include only real issues from the posting.

IMPORTANT:
The output must be valid JSON and must match the schema exactly.`,
		p.profileText, title, company, location, desc,
	)
}

// buildProfile, config'den prompt'a eklenecek aday profil metnini oluşturur.
func buildProfile(cfg *config.CandidateConfig) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Name: %s\n", cfg.Name))
	sb.WriteString(fmt.Sprintf("Status: %s, Graduation: %d\n", cfg.Status, cfg.GraduationYear))
	sb.WriteString(fmt.Sprintf("Location: %s (Open to Turkey & Brussels/Belgium/Europe)\n", cfg.Location))
	sb.WriteString(fmt.Sprintf("Languages: %s\n", strings.Join(cfg.Skills.Languages, ", ")))
	sb.WriteString(fmt.Sprintf("Frameworks: %s\n", strings.Join(cfg.Skills.Frameworks, ", ")))
	sb.WriteString(fmt.Sprintf("Databases: %s\n", strings.Join(cfg.Skills.Databases, ", ")))
	sb.WriteString(fmt.Sprintf("Infrastructure: %s\n", strings.Join(cfg.Skills.Infra, ", ")))
	sb.WriteString(fmt.Sprintf("Cloud & Certs: %s\n", strings.Join(cfg.Skills.Cloud, ", ")))
	sb.WriteString(fmt.Sprintf("Concepts: %s\n", strings.Join(cfg.Skills.Concepts, ", ")))
	sb.WriteString(fmt.Sprintf("OS & Tools: %s\n", strings.Join(cfg.Skills.OS, ", ")))
	sb.WriteString(fmt.Sprintf("Primary roles: %s\n", strings.Join(cfg.TargetRoles.Primary, ", ")))
	sb.WriteString(fmt.Sprintf("Secondary roles: %s\n", strings.Join(cfg.TargetRoles.Secondary, ", ")))
	sb.WriteString(fmt.Sprintf("Preferred seniority: %s\n", strings.Join(cfg.Seniority.Preferred, ", ")))
	sb.WriteString(fmt.Sprintf("Acceptable seniority: %s\n", strings.Join(cfg.Seniority.Acceptable, ", ")))

	rejectedStr := strings.Join(cfg.Seniority.Rejected, ", ")
	sb.WriteString(fmt.Sprintf("Explicitly Rejected Roles: %s (CRITICAL: The candidate must NEVER be matched to these roles, regardless of how well the tech stack aligns.)\n", rejectedStr))

	sb.WriteString("Work Experience:\n")
	sb.WriteString("- EUROCONTROL (Brussels, Belgium | Nov 2026 – Apr 2027): Software Engineering Trainee (NMD/INF/CNS Unit). Contributing to ASTERIX E-ARCO surveillance data library (C++, Python client, JSON validation, test automation within CI/CD).\n")
	sb.WriteString("- Sanlab (Istanbul | Aug 2024 – Oct 2024): Software Engineering Internship. Web-based market management system integrating JWT-authenticated Go API with Rust REST API across 12 endpoints (MongoDB, atomic sequence ID generation), TCP/UDP socket communication in Rust, Agile/Scrum.\n")

	sb.WriteString("Key Projects:\n")
	sb.WriteString("- Distributed Job Processing System: Go, Redis worker pool with exponential backoff retries, Prometheus metrics, Docker, CI/CD with GitHub Actions.\n")
	sb.WriteString("- Order & Inventory Management API: Java 21, Spring Boot, REST API, JWT auth, RBAC, order state machine, pessimistic locking for concurrency safety, Testcontainers integration tests against PostgreSQL.\n")
	sb.WriteString("- IoT-Based Smart Indoor Plant Control System (Capstone): ESP32 sensor system, React/Firebase real-time dashboard, remote irrigation, >99% successful data delivery, ~500ms latency.\n")

	sb.WriteString("Certifications & Languages: AWS Certified AI Practitioner (2026). English: Fluent (C1/C2 professional working proficiency), Turkish: Native, German: Beginner.\n")

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
