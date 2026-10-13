package intelligence

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SkillStat holds a skill's market frequency data.
type SkillStat struct {
	Skill     string
	Count     int
	Frequency float64 // 0.0–1.0 arası, toplam iş ilanı sayısına göre
}

// MarketReport holds aggregated job market analysis for a time period.
type MarketReport struct {
	GeneratedAt time.Time
	PeriodDays  int
	TotalJobs   int
	ApplyJobs   int // action = "apply"
	WatchJobs   int // action = "watch"

	// CV'de olan ve piyasada istenen skill'ler
	MatchingSkills []SkillStat

	// CV'de olmayan ama piyasada sık istenen skill'ler
	GapSkills []SkillStat

	// Piyasada en çok görülen tüm skill'ler (hem matching hem gap)
	TopMarketSkills []SkillStat

	// En çok ilan veren şirketler
	TopCompanies []CompanyStat
}

// CompanyStat holds per-company job statistics.
type CompanyStat struct {
	Company  string
	JobCount int
	AvgScore float64
}

// Analyzer produces market intelligence from PostgreSQL job data

// Analyzer produces market intelligence from PostgreSQL job data.
type Analyzer struct {
	pool            *pgxpool.Pool
	candidateSkills map[string]struct{} // CV'deki skill'ler (lowercase, hızlı lookup için)
}

func NewAnalyzer(pool *pgxpool.Pool, candidateSkills []string) *Analyzer {
	skillSet := make(map[string]struct{}, len(candidateSkills))
	for _, s := range candidateSkills {
		skillSet[strings.ToLower(s)] = struct{}{}
	}
	return &Analyzer{pool: pool, candidateSkills: skillSet}
}

// Analyze returns a MarketReport for the last N days of job data.
func (a *Analyzer) Analyze(ctx context.Context, periodDays int) (*MarketReport, error) {
	report := &MarketReport{
		GeneratedAt: time.Now(),
		PeriodDays:  periodDays,
	}

	// ── 1. Genel istatistikler ────────────────────────────────────────────────
	if err := a.fetchJobCounts(ctx, periodDays, report); err != nil {
		return nil, fmt.Errorf("fetching job counts: %w", err)
	}

	// ── 2. Skill frekans analizi ──────────────────────────────────────────────
	overlapStats, err := a.fetchSkillStats(ctx, periodDays, "skill_overlap")
	if err != nil {
		return nil, fmt.Errorf("fetching overlap stats: %w", err)
	}

	gapStats, err := a.fetchSkillStats(ctx, periodDays, "missing_skills")
	if err != nil {
		return nil, fmt.Errorf("fetching gap stats: %w", err)
	}

	// ── 3. CV diff ────────────────────────────────────────────────────────────
	report.TopMarketSkills = overlapStats
	report.MatchingSkills = a.filterMatching(overlapStats)
	report.GapSkills = a.filterGaps(gapStats)

	// ── 4. Şirket analizi ─────────────────────────────────────────────────────
	companies, err := a.fetchTopCompanies(ctx, periodDays)
	if err != nil {
		return nil, fmt.Errorf("fetching top companies: %w", err)
	}
	report.TopCompanies = companies

	return report, nil
}

func (a *Analyzer) fetchJobCounts(ctx context.Context, days int, report *MarketReport) error {
	row := a.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE ai_action = 'apply') AS apply_count,
			COUNT(*) FILTER (WHERE ai_action = 'watch') AS watch_count
		FROM jobs
		WHERE scraped_at > NOW() - ($1::integer * INTERVAL '1 day')
		  AND match_score IS NOT NULL
	`, days)

	return row.Scan(&report.TotalJobs, &report.ApplyJobs, &report.WatchJobs)
}

func (a *Analyzer) fetchSkillStats(ctx context.Context, days int, column string) ([]SkillStat, error) {
	var query string
	switch column {
	case "skill_overlap":
		query = `
			SELECT LOWER(TRIM(skill)) AS skill, COUNT(*) AS frequency
			FROM jobs, unnest(skill_overlap) AS skill
			WHERE scraped_at > NOW() - ($1::integer * INTERVAL '1 day')
			  AND match_score IS NOT NULL
			  AND TRIM(skill) != ''
			GROUP BY LOWER(TRIM(skill))
			ORDER BY frequency DESC
			LIMIT 30`
	case "missing_skills":
		query = `
			SELECT LOWER(TRIM(skill)) AS skill, COUNT(*) AS frequency
			FROM jobs, unnest(missing_skills) AS skill
			WHERE scraped_at > NOW() - ($1::integer * INTERVAL '1 day')
			  AND match_score IS NOT NULL
			  AND TRIM(skill) != ''
			GROUP BY LOWER(TRIM(skill))
			ORDER BY frequency DESC
			LIMIT 30`
	default:
		return nil, fmt.Errorf("unknown column: %s", column)
	}

	rows, err := a.pool.Query(ctx, query, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []SkillStat
	for rows.Next() {
		var s SkillStat
		if err := rows.Scan(&s.Skill, &s.Count); err != nil {
			return nil, err
		}
		stats = append(stats, s)
	}

	if len(stats) > 0 {
		maxCount := stats[0].Count
		for i := range stats {
			if maxCount > 0 {
				stats[i].Frequency = float64(stats[i].Count) / float64(maxCount)
			}
		}
	}

	return stats, rows.Err()
}

func (a *Analyzer) fetchTopCompanies(ctx context.Context, days int) ([]CompanyStat, error) {
	rows, err := a.pool.Query(ctx, `
    SELECT
        company,
        COUNT(*) AS job_count,
        ROUND(AVG(match_score)::numeric, 2) AS avg_score
    FROM jobs
    WHERE scraped_at > NOW() - ($1::integer * INTERVAL '1 day')
      AND match_score IS NOT NULL
      AND ai_action IN ('apply', 'watch')
    GROUP BY company
    ORDER BY job_count DESC, avg_score DESC
    LIMIT 10
`, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var companies []CompanyStat
	for rows.Next() {
		var c CompanyStat
		if err := rows.Scan(&c.Company, &c.JobCount, &c.AvgScore); err != nil {
			return nil, err
		}
		companies = append(companies, c)
	}
	return companies, rows.Err()
}

// filterMatching, candidate CV'sindeki skill'leri döndürür.
func (a *Analyzer) filterMatching(stats []SkillStat) []SkillStat {
	var result []SkillStat
	for _, s := range stats {
		if _, ok := a.candidateSkills[s.Skill]; ok {
			result = append(result, s)
		}
	}
	// Frekansa göre sırala
	sort.Slice(result, func(i, j int) bool {
		return result[i].Count > result[j].Count
	})
	if len(result) > 8 {
		result = result[:8]
	}
	return result
}

// filterGaps, CV'de olmayan ama piyasada istenen skill'leri döndürür.
func (a *Analyzer) filterGaps(stats []SkillStat) []SkillStat {
	var result []SkillStat
	for _, s := range stats {
		if _, ok := a.candidateSkills[s.Skill]; !ok {
			// Çok generic veya irrelevant skill'leri filtrele
			if !isNoiseSkill(s.Skill) {
				result = append(result, s)
			}
		}
	}
	if len(result) > 8 {
		result = result[:8]
	}
	return result
}

// isNoiseSkill, "communication", "teamwork" gibi generic skill'leri filtreler.
func isNoiseSkill(skill string) bool {
	// Exact noise words — bunlar skill değil, kategori veya bağlam
	exactNoise := []string{
		"communication", "teamwork", "problem solving",
		"english", "turkish", "agile", "scrum", "git",
		"bachelor", "degree", "university",
	}
	for _, noise := range exactNoise {
		if skill == noise {
			return true
		}
	}

	// Pattern noise — bu substring'leri içerenler skill değil kategori tanımı
	// Groq bazen "specific devops tools", "frontend skills" gibi şeyler üretiyor
	patternNoise := []string{
		"specific", "skills", "tools", " development",
		"it ops", "ui component", "frontend skill",
		"devops tool", "backend skill", "cloud service",
		"canonical",           // Ubuntu şirket adı
		"offset",              // baskı teknolojisi
		"telecomm",            // telekom sektörü
		"pipeline management", // kategori
		"data curation",       // veri kategori
		"ubuntu",              // OS değil, genellikle context noise
	}
	skillLower := strings.ToLower(skill)
	for _, pattern := range patternNoise {
		if strings.Contains(skillLower, pattern) {
			return true
		}
	}

	// Çok uzun string'ler — bunlar skill değil, cümle parçası
	if len(skill) > 25 {
		return true
	}

	return false
}
