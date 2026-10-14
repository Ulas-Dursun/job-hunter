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
// Description 3500 rune ile sınırlandırılır — Gemini Flash kotasında tam gereksinimleri kapsar.
func (p *PromptBuilder) Build(title, company, location, description string) string {
	desc := truncateUTF8(strings.TrimSpace(description), 3500)

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
  "red_flags": [<concerns: wrong stack, too senior, irrelevant domain>],
  "action": "<apply|watch|skip>",
  "reasoning": "<2 sentence explanation>"
}

CRITICAL RULES (MUST OBEY):

1. EXPERIENCE & SCORING MATRIX:
   - 0-2 Years Required: If the candidate's core stack aligns with any pillar and fundamentals are present, give 8.5 - 10.0. Top priority.
   - 3 Years Required: If the candidate's tech stack matches well, give 7.5 - 8.4.
   - 4-5 Years Required WITH strong tech stack match: Give 6.0 - 6.9. This is a stretch — worth trying (watch) but not a guaranteed match.
   - 4-5 Years Required WITHOUT strong tech stack match: Give below 5.0 (skip).
   - 6+ Years Required OR explicitly matching any role in the "Explicitly Rejected Roles" list: You MUST set match_score below 4.0. Add "Too senior" to red_flags.

2. NEW GRAD / JUNIOR FLEXIBILITY:
   - If the job is explicitly for "New Grad", "Junior", "Associate", "Graduate", or "Trainee", prioritize engineering fundamentals over exact syntax or exact years.
   - Strong fundamentals include: REST, HTTP, APIs, databases, SQL/NoSQL, OOP, Git, Docker, cloud basics, testing, concurrency.
   - Do not penalize heavily for missing years if the role is clearly entry-level and the candidate shows learning potential plus adjacent stack fit.

2b. TOOL GAP FLEXIBILITY:
   If the candidate is missing only 1-2 secondary infrastructure/tool requirements
   (e.g., deep Kubernetes production experience, Terraform, a specific cloud provider nuance)
   but core fundamentals AND primary tech stack (Go/Java/Python/Rust, databases,
   concurrency, APIs, containerization basics with Docker) align well:
   - Do NOT treat this as disqualifying. The candidate is actively open to learning and has demonstrated fast adaptation across multiple backend stacks.
   - Score in the 7.0-8.5 range depending on overall fit.
   - List the specific gap in missing_skills — do not skip solely because of it.

2c. SOFTWARE ENGINEER / FULLSTACK FLEXIBILITY:
   If a role is titled "Software Engineer", "Associate Software Engineer", or "Full Stack Developer",
   and the core workload is fundamentally backend, APIs, databases, or cloud infrastructure (with minor secondary
   frontend tasks like React/TypeScript):
   - Do NOT disqualify. Candidate has foundational React/HTML/CSS exposure from capstone IoT project.
   - Disqualify only if the role is exclusively/primarily frontend UI design (e.g., pure React/Angular/Vue/CSS developer).

3. HARD FILTERS:
   - If the role title explicitly matches the "Explicitly Rejected Roles", it is an immediate skip.
   - If the role is clearly unrelated to any of the 4 target pillars (e.g. sales, marketing, HR, legal, mobile-only), set action to "skip" and keep the score low (< 3.5).
   - If the posting contains a senior title but the description is junior-sounding, use the stricter interpretation.

4. ROLE FAMILY & THE 4 TARGET PILLARS:
   - "primary" for:
     * Backend Engineering: Backend Developer, Software Engineer, Go/Java/Python Engineer.
     * Cloud / DevOps / SRE: Platform Engineer, Cloud Engineer, DevOps Engineer, Site Reliability Engineer, Systems Engineer.
     * Data Engineering & Analytics: Data Engineer, Data Analyst, Data Scientist, Analytics Engineer.
     * Aerospace & Air Traffic Management Systems: Surveillance data, ATM systems, CNS engineering.
   - If entry-level/junior in ANY of these primary tracks, score high (7.5 - 9.5) and action "apply".
   - "secondary" for BI Developer, ML Research, Pipeline Developer, or adjacent technical support roles.

5. DECISION TIE-BREAKERS:
   - If the role is junior and the stack is adjacent, prefer "watch" over "skip".
   - If the role is senior and the stack is close, keep the score low.
   - If the evidence is mixed, choose the more conservative score.
   - Do not reward buzzwords without concrete evidence.

6. DOMAIN SPECIALIZATION CHECK:
   Do not inflate the score just because a single language matches (e.g., "Rust" matches but the role requires quantitative finance, algorithmic trading, or computer vision). Cap at 6.0 unless it aligns with candidate's actual projects.

7. UNRELATED FIELD CHECK:
   If the posting is from an unrelated engineering discipline (mechanical, civil, chemical, biomedical) that merely contains the word "engineer" in the title, set match_score below 3.0 and action "skip".

8. LOCATION & LANGUAGE ADVANTAGE:
   - The candidate is based in Brussels, Belgium (Software Engineering Trainee at EUROCONTROL) and Istanbul, Turkey.
   - Positions in Belgium (Brussels), Netherlands (Amsterdam), Germany (Berlin/Munich), Switzerland, UK, or Remote EU are prime local/regional targets.
   - If the job explicitly mandates fluent French ("Français obligatoire / courant") or Dutch ("Nederlands vereist") and does NOT accept English as primary working language, deduct 2.5 from match_score and flag "Requires French/Dutch" in red_flags.
   - If English is accepted or the company operates in English, treat the candidate as fully language-eligible (Fluent English C1/C2).

9. STEALTH SENIORITY & SCOPE DETECTION:
   - If a job does not explicitly state a numerical years requirement, but lists expectations such as:
     * Leading architectural design, system architecture, or tech roadmap
     * Mentoring mid/senior engineers or direct team leadership
     * Ownership of company-wide production infrastructure or enterprise strategies
     * Extensive multi-year track record in high-scale distributed systems in production
   - This role is implicitly Senior/Lead/Architect. You MUST set match_score below 4.5, set action to "skip", and add "Implicit senior scope / Architectural ownership" to red_flags.
   - Do NOT let a job pass as junior just because it omitted a numerical year requirement.

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
