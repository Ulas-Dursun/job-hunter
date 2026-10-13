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
// Description 1200 rune ile sınırlandırılır — token maliyeti kontrolü.
func (p *PromptBuilder) Build(title, company, location, description string) string {
	desc := truncateUTF8(strings.TrimSpace(description), 1200)

	return fmt.Sprintf(`You are a strict job matching assistant for a NEW GRADUATE / JUNIOR backend engineer.
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
Compare the job posting against the candidate profile and determine whether this role is a strong fit for a NEW GRADUATE / JUNIOR backend engineer.
Use only evidence explicitly present in the candidate profile and job posting.
Do not hallucinate skills, experience, or projects.
Prefer "watch" over "skip" when the role is plausible but not clearly aligned.
Prefer "apply" only when there is strong evidence of fit.

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
   - 0-2 Years Required: If the candidate's core stack aligns and backend fundamentals are present, give 8.5 - 10.0. First priority.
   - 3 Years Required: If the candidate's tech stack matches well, give 7.5 - 8.4.
   - 4-5 Years Required WITH strong tech stack match: Give 6.0 - 6.9. This is a stretch — worth trying but should NOT be treated as a high-confidence match.
   - 4-5 Years Required WITHOUT strong tech stack match: Give below 5.0.
   - 6+ Years Required OR explicitly matching any role in the "Explicitly Rejected Roles" list: You MUST set match_score below 4.0. Add "Too senior" to red_flags.
2. NEW GRAD / JUNIOR FLEXIBILITY:
   - If the job is explicitly for "New Grad", "Junior", or "Entry Level", prioritize backend fundamentals over exact syntax or exact years.
   - Strong fundamentals include: REST, HTTP, APIs, databases, SQL/NoSQL, OOP, Git, Docker, cloud, testing, concurrency.
   - Do not penalize heavily for missing years if the role is clearly entry-level and the candidate shows learning potential plus adjacent stack fit.
   - Do not inflate the score unless there is clear evidence of fundamentals and role alignment.

2b. TOOL GAP FLEXIBILITY:
   If the candidate is missing only 1-2 secondary infrastructure/tool requirements
   (e.g., Kubernetes production experience, Terraform, a specific cloud certification)
   but core backend fundamentals AND primary tech stack (languages, databases,
   concurrency, APIs, containerization basics) align well:
   - Do NOT treat this as disqualifying.
   - Assume the candidate can learn additional tools quickly given demonstrated
     fundamentals and hands-on project experience.
   - Score in the 7.0-8.5 range depending on overall fit.
   - List the specific gap in missing_skills — do not skip solely because of it.

3. HARD FILTERS:
   - If the role title explicitly matches the "Explicitly Rejected Roles", it is an immediate skip.
   - If the role is clearly unrelated to backend/software/cloud/platform/devops/sre/infrastructure/data engineering, set action to "skip" and keep the score low.
   - If the posting contains a senior title but the description is junior-sounding, use the stricter interpretation.

4. ROLE FAMILY:
   - "primary" for Backend / Software / Cloud / DevOps / Platform / SRE / Infrastructure
   - "secondary" for Data Engineer / Analytics Engineer / Data Platform / Data Analyst

5. DECISION TIE-BREAKERS:
   - If the role is junior and the stack is adjacent, prefer "watch" over "skip".
   - If the role is senior and the stack is close, keep the score low.
   - If the evidence is mixed, choose the more conservative score.
   - Do not reward buzzwords without concrete evidence.

6. DOMAIN SPECIALIZATION CHECK:
   Do not inflate the score just because a single language/tool matches
   (e.g., "Rust" matches but the role requires deep domain expertise like
   quantitative trading, algorithmic finance, embedded systems, or robotics).
   If the role requires specialized domain knowledge the candidate has no
   evidence of, cap the score at 6.0 even if the programming language matches.

7. UNRELATED FIELD CHECK:
   If the posting is from an unrelated engineering discipline (mechanical,
   chemical, civil, electrical, biotech/process engineering) that merely
   contains the word "engineer" or "developer" in the title, this is NOT
   a software role. Set match_score below 3.0 and action "skip", regardless
   of any coincidental keyword overlap.

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
// buildProfile, config'den prompt'a eklenecek aday profil metnini oluşturur.
func buildProfile(cfg *config.CandidateConfig) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Name: %s\n", cfg.Name))
	sb.WriteString(fmt.Sprintf("Status: %s, Graduation: %d\n", cfg.Status, cfg.GraduationYear))
	sb.WriteString(fmt.Sprintf("Location: %s\n", cfg.Location))
	sb.WriteString(fmt.Sprintf("Languages: %s\n", strings.Join(cfg.Skills.Languages, ", ")))
	sb.WriteString(fmt.Sprintf("Frameworks: %s\n", strings.Join(cfg.Skills.Frameworks, ", ")))
	sb.WriteString(fmt.Sprintf("Databases: %s\n", strings.Join(cfg.Skills.Databases, ", ")))
	sb.WriteString(fmt.Sprintf("Infrastructure: %s\n", strings.Join(cfg.Skills.Infra, ", ")))
	sb.WriteString(fmt.Sprintf("Cloud: %s\n", strings.Join(cfg.Skills.Cloud, ", ")))
	sb.WriteString(fmt.Sprintf("Concepts: %s\n", strings.Join(cfg.Skills.Concepts, ", ")))
	sb.WriteString(fmt.Sprintf("OS & Tools: %s\n", strings.Join(cfg.Skills.OS, ", ")))
	sb.WriteString(fmt.Sprintf("Primary roles: %s\n", strings.Join(cfg.TargetRoles.Primary, ", ")))
	sb.WriteString(fmt.Sprintf("Secondary roles: %s\n", strings.Join(cfg.TargetRoles.Secondary, ", ")))
	sb.WriteString(fmt.Sprintf("Preferred seniority: %s\n", strings.Join(cfg.Seniority.Preferred, ", ")))
	sb.WriteString(fmt.Sprintf("Acceptable seniority: %s\n", strings.Join(cfg.Seniority.Acceptable, ", ")))

	rejectedStr := strings.Join(cfg.Seniority.Rejected, ", ")
	sb.WriteString(fmt.Sprintf("Explicitly Rejected Roles: %s (CRITICAL: The candidate must NEVER be matched to these roles, regardless of how well the tech stack aligns.)\n", rejectedStr))

	sb.WriteString("Work Experience: 3-month internship at Sanlab — built Go API with JWT auth, RBAC, MongoDB persistence, and Rust backend integration with TCP/UDP communication layer.\n")
	sb.WriteString("Key Projects: (1) Distributed Job Processing System — Go worker pool with Redis-backed queue, exponential backoff retry, Prometheus metrics, Dockerized with CI/CD. (2) Order & Inventory Management API — Java Spring Boot REST API with JWT auth, RBAC, order state machine, pessimistic locking for concurrency safety, Testcontainers integration tests. (3) AWS cloud deployment project — hands-on experience deploying and configuring AWS services (EC2, S3, IAM).\n")

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
