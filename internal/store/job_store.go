package store

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ulasdursun/job-hunter/internal/analyzer"
)

// JobRecord, PostgreSQL jobs tablosuna yazılacak veriyi tutar.
type JobRecord struct {
	Source      string
	ExternalID  string
	URL         string
	Title       string
	Company     string
	Location    string
	Description string
	PostedAt    string
	Score       *analyzer.ScoreResult
}

// JobStore, jobs tablosuna yazma işlemlerini yönetir.
type JobStore struct {
	pool *pgxpool.Pool
}

func NewJobStore(pool *pgxpool.Pool) *JobStore {
	return &JobStore{pool: pool}
}

// Upsert, iş ilanını ve AI analiz sonucunu PostgreSQL'e yazar.
// ON CONFLICT: aynı ilan tekrar gelirse sadece score güncellenir.
func (s *JobStore) Upsert(ctx context.Context, rec JobRecord) error {
	var postedAt *time.Time
	if rec.PostedAt != "" && rec.PostedAt != "None" {
		if t, err := parsePostedAt(rec.PostedAt); err == nil {
			postedAt = &t
		}
	}

	var (
		matchScore    *float64
		skillOverlap  []string
		missingSkills []string
		redFlags      []string
		aiAction      *string
		aiReasoning   *string
		roleFamily    *string
	)

	if rec.Score != nil {
		matchScore = &rec.Score.MatchScore
		skillOverlap = rec.Score.SkillOverlap
		missingSkills = rec.Score.MissingSkills
		redFlags = rec.Score.RedFlags
		aiAction = &rec.Score.Action
		aiReasoning = &rec.Score.Reasoning
		roleFamily = &rec.Score.RoleFamily

		// Nil slice yerine boş slice — PostgreSQL NULL yerine {} yazar
		if skillOverlap == nil {
			skillOverlap = []string{}
		}
		if missingSkills == nil {
			missingSkills = []string{}
		}
		if redFlags == nil {
			redFlags = []string{}
		}
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO jobs (
			source, external_id, url, title, company, location,
			description, posted_at,
			match_score, skill_overlap, missing_skills, red_flags,
			ai_action, ai_reasoning, role_family
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8,
			$9, $10, $11, $12,
			$13, $14, $15
		)
		ON CONFLICT (source, external_id)
		DO UPDATE SET
			match_score    = EXCLUDED.match_score,
			skill_overlap  = EXCLUDED.skill_overlap,
			missing_skills = EXCLUDED.missing_skills,
			red_flags      = EXCLUDED.red_flags,
			ai_action      = EXCLUDED.ai_action,
			ai_reasoning   = EXCLUDED.ai_reasoning,
			role_family    = EXCLUDED.role_family,
			updated_at     = NOW()
		WHERE jobs.match_score IS NULL
	`,
		truncateRunes(rec.Source, 50),
		truncateRunes(rec.ExternalID, 255),
		rec.URL,
		truncateRunes(rec.Title, 255),
		truncateRunes(rec.Company, 255),
		truncateRunes(rec.Location, 255),
		rec.Description, postedAt,
		matchScore, skillOverlap, missingSkills, redFlags,
		aiAction, aiReasoning, roleFamily,
	)

	if err != nil {
		return fmt.Errorf("upserting job %s/%s: %w", rec.Source, rec.ExternalID, err)
	}

	return nil
}

// GetNotifiedAt, bir ilanın daha önce Telegram bildirimi alıp almadığını kontrol eder.
// nil dönerse hiç bildirim gönderilmemiş demektir.
func (s *JobStore) GetNotifiedAt(ctx context.Context, source, externalID string) (*time.Time, error) {
	var notifiedAt *time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT notified_at FROM jobs WHERE source = $1 AND external_id = $2`,
		source, externalID,
	).Scan(&notifiedAt)
	if err != nil {
		return nil, err
	}
	return notifiedAt, nil
}

// MarkNotified, bir ilanı "bildirim gönderildi" olarak işaretler.
// Bu, Redis dedup hafızası silinse bile duplicate Telegram bildirimini engeller —
// ikinci savunma hattı.
func (s *JobStore) MarkNotified(ctx context.Context, source, externalID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE jobs SET notified_at = NOW() WHERE source = $1 AND external_id = $2`,
		source, externalID,
	)
	return err
}

func parsePostedAt(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	// UNIX epoch in seconds (e.g. 1791988821)
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		if n > 1e11 { // milliseconds
			return time.UnixMilli(n), nil
		}
		return time.Unix(n, 0), nil
	}

	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		time.DateOnly,
		time.RFC1123,
		time.RFC822,
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unknown date format: %s", s)
}

// BuildPreferenceSummary, kullanıcının Kanban panosunda yaptığı gerçek seçimleri
// (applied/interview/offer vs skipped/rejected) özetleyerek AI prompt'una geri bildirim üretir.
func (s *JobStore) BuildPreferenceSummary(ctx context.Context) (string, error) {
	posRows, err := s.pool.Query(ctx, `
		SELECT title, company, COALESCE(location, ''), COALESCE(array_to_string(skill_overlap, ', '), '')
		FROM jobs
		WHERE status IN ('applied', 'interview', 'offer')
		ORDER BY updated_at DESC NULLS LAST, scraped_at DESC
		LIMIT 8
	`)
	if err != nil {
		return "", err
	}
	defer posRows.Close()

	var liked []string
	for posRows.Next() {
		var title, company, loc, overlap string
		if err := posRows.Scan(&title, &company, &loc, &overlap); err == nil {
			liked = append(liked, fmt.Sprintf("  + APPLIED/INTERVIEW: %q at %s (%s) [Skills: %s]", title, company, loc, overlap))
		}
	}

	negRows, err := s.pool.Query(ctx, `
		SELECT title, company, COALESCE(location, ''), COALESCE(array_to_string(red_flags, '; '), '')
		FROM jobs
		WHERE status IN ('skipped', 'rejected')
		ORDER BY updated_at DESC NULLS LAST, scraped_at DESC
		LIMIT 8
	`)
	if err != nil {
		return "", err
	}
	defer negRows.Close()

	var disliked []string
	for negRows.Next() {
		var title, company, loc, flags string
		if err := negRows.Scan(&title, &company, &loc, &flags); err == nil {
			disliked = append(disliked, fmt.Sprintf("  - MANUALLY SKIPPED: %q at %s (%s) [Flags: %s]", title, company, loc, flags))
		}
	}

	if len(liked) == 0 && len(disliked) == 0 {
		return "", nil
	}

	var sb strings.Builder
	if len(liked) > 0 {
		sb.WriteString("Roles the candidate actively chose to APPLY/INTERVIEW for (reward similar entry/junior roles +0.3):\n")
		sb.WriteString(strings.Join(liked, "\n") + "\n")
	}
	if len(disliked) > 0 {
		sb.WriteString("Roles the candidate manually SKIPPED/REJECTED (be more conservative on similar patterns):\n")
		sb.WriteString(strings.Join(disliked, "\n") + "\n")
	}
	return sb.String(), nil
}

// HasLowScoreTwin, aynı şirket + aynı başlıkta ve AYNI lokasyon/ülke kapsamında son 30 günde zaten düşük (<5.0)
// puanlanmış bir ilan olup olmadığını söyler.
// Farklı ülkelerdeki (örn. ABD/Asya) veya yerel dil bariyeri nedeniyle düşük puan alan bir ilanın,
// Amsterdam/Brüksel/İstanbul'daki İngilizce ikizini zehirlemesini engeller.
func (s *JobStore) HasLowScoreTwin(ctx context.Context, company, title string, location ...string) (bool, error) {
	incomingLoc := ""
	if len(location) > 0 {
		incomingLoc = strings.TrimSpace(location[0])
	}

	rows, err := s.pool.Query(ctx, `
		SELECT COALESCE(location, ''), COALESCE(array_to_string(red_flags, ' '), '')
		FROM jobs
		WHERE lower(company) = lower($1) AND lower(title) = lower($2)
		  AND match_score IS NOT NULL AND match_score < 5.0
		  AND scraped_at > NOW() - INTERVAL '30 days'
	`, company, title)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var existingLoc, flags string
		if err := rows.Scan(&existingLoc, &flags); err != nil {
			continue
		}
		if IsPoisonedByLanguageOrRegion(flags) {
			continue
		}
		if SameLocationScope(existingLoc, incomingLoc) {
			return true, nil
		}
	}
	return false, rows.Err()
}

// IsPoisonedByLanguageOrRegion, önceki düşük puanlı ilanın yerel dil bariyeri veya denizaşırı (US/APAC)
// konum sebebiyle elenip elenmediğini denetler; böylece aynı şirketin İngilizce/Avrupa ilanı bloklanmaz.
func IsPoisonedByLanguageOrRegion(redFlagsJoined string) bool {
	low := strings.ToLower(redFlagsJoined)
	for _, marker := range []string{
		"language", "german", "deutsch", "dutch", "nederlands", "french", "français", "danish", "dansk",
		"us on-site", "overseas", "apac",
	} {
		if strings.Contains(low, marker) {
			return true
		}
	}
	return false
}

// SameLocationScope, iki lokasyon metninin aynı ülke/bölge kapsamında olup olmadığını belirler.
// ABD/Asya veya başka bir Avrupa ülkesindeki ilanın farklı ülkedeki (örn. Amsterdam, Brüksel, İstanbul)
// ilanı "düşük puanlı ikiz" olarak zehirlemesini engeller.
func SameLocationScope(locA, locB string) bool {
	a := strings.ToLower(strings.TrimSpace(locA))
	b := strings.ToLower(strings.TrimSpace(locB))
	if a == "" || b == "" {
		return a == b
	}
	if a == b {
		return true
	}
	countryA := extractCountryScope(a)
	countryB := extractCountryScope(b)
	if countryA != "" && countryB != "" {
		return countryA == countryB
	}
	return false
}

func extractCountryScope(locLower string) string {
	scopes := []struct {
		country string
		markers []string
	}{
		{"tr", []string{"turkey", "türkiye", "istanbul", "ankara", "izmir", "bursa", "kocaeli", "antalya"}},
		{"be", []string{"belgium", "belgique", "belgië", "brussels", "bruxelles", "brüksel", "antwerp", "ghent"}},
		{"nl", []string{"netherlands", "hollanda", "amsterdam", "rotterdam", "eindhoven", "utrecht", "the hague"}},
		{"de", []string{"germany", "deutschland", "almanya", "berlin", "munich", "münchen", "frankfurt", "hamburg", "cologne", "stuttgart"}},
		{"dk", []string{"denmark", "danimarka", "copenhagen", "københavn", "kobenhavn", "aarhus", "odense"}},
		{"uk", []string{"united kingdom", "england", "scotland", "london", "manchester", "edinburgh", "cambridge", "bristol"}},
		{"ch", []string{"switzerland", "isviçre", "zurich", "zürich", "geneva", "genève", "basel", "bern"}},
		{"fr", []string{"france", "fransa", "paris", "lyon", "toulouse"}},
		{"us", []string{"united states", "usa", "u.s.", "new york", "san francisco", "seattle", "austin", "boston", "chicago", "california"}},
		{"apac", []string{"singapore", "tokyo", "japan", "australia", "sydney", "melbourne", "india", "bangalore", "bengaluru", "apac"}},
	}
	for _, sc := range scopes {
		for _, m := range sc.markers {
			if strings.Contains(locLower, m) {
				return sc.country
			}
		}
	}
	return ""
}

// AlreadyScored, aynı (source, external_id) veya son 30 günde aynı (company, title, location)
// kaydının veritabanında zaten puanlanmış olarak bulunup bulunmadığını kontrol eder.
// Redis TTL süresi dolduğunda veya farklı günlerde tekrar scrape edildiğinde mükerrer LLM harcamasını engeller.
func (s *JobStore) AlreadyScored(ctx context.Context, source, externalID, company, title, location string) (bool, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM jobs
		WHERE match_score IS NOT NULL
		  AND (
		      (source = $1 AND external_id = $2)
		      OR (
		          lower(company) = lower($3)
		          AND lower(title) = lower($4)
		          AND lower(COALESCE(location, '')) = lower($5)
		          AND scraped_at > NOW() - INTERVAL '30 days'
		      )
		  )
	`, truncateRunes(source, 50), truncateRunes(externalID, 255), company, title, location).Scan(&n)
	return n > 0, err
}

func truncateRunes(s string, maxRunes int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= maxRunes {
		return string(r)
	}
	return string(r[:maxRunes])
}
