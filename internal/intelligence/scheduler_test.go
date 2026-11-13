package intelligence

import (
	"strings"
	"testing"
	"time"
)

func TestShouldTriggerEveningDigest(t *testing.T) {
	loc := time.FixedZone("CET", 3600)
	day1Before := time.Date(2026, 11, 12, 19, 59, 0, 0, loc)
	day1At20 := time.Date(2026, 11, 12, 20, 0, 0, 0, loc)
	day1Wakeup := time.Date(2026, 11, 12, 21, 30, 0, 0, loc)
	day2At20 := time.Date(2026, 11, 13, 20, 5, 0, 0, loc)

	if shouldTriggerEveningDigest(day1Before, "") {
		t.Errorf("expected false before 20:00")
	}
	if !shouldTriggerEveningDigest(day1At20, "") {
		t.Errorf("expected true at 20:00 when not yet sent")
	}
	if !shouldTriggerEveningDigest(day1Wakeup, "") {
		t.Errorf("expected true at 21:30 after sleep wakeup when not yet sent today")
	}
	if shouldTriggerEveningDigest(day1Wakeup, "2026-11-12") {
		t.Errorf("expected false when already sent today")
	}
	if !shouldTriggerEveningDigest(day2At20, "2026-11-12") {
		t.Errorf("expected true on next day at 20:05")
	}
}

func TestShouldTriggerWeeklyReport(t *testing.T) {
	loc := time.FixedZone("CET", 3600)
	// 2026-11-09 Pazartesi
	sunday := time.Date(2026, 11, 8, 10, 0, 0, 0, loc)
	monBefore9 := time.Date(2026, 11, 9, 8, 59, 0, 0, loc)
	monAt9 := time.Date(2026, 11, 9, 9, 0, 0, 0, loc)
	monWakeup := time.Date(2026, 11, 9, 14, 30, 0, 0, loc)
	nextMon := time.Date(2026, 11, 16, 9, 5, 0, 0, loc)

	week1 := isoWeekKey(monAt9)

	if shouldTriggerWeeklyReport(sunday, "") {
		t.Errorf("expected false on Sunday")
	}
	if shouldTriggerWeeklyReport(monBefore9, "") {
		t.Errorf("expected false on Monday before 09:00")
	}
	if !shouldTriggerWeeklyReport(monAt9, "") {
		t.Errorf("expected true on Monday at 09:00")
	}
	if !shouldTriggerWeeklyReport(monWakeup, "") {
		t.Errorf("expected true on Monday at 14:30 when not yet sent this week")
	}
	if shouldTriggerWeeklyReport(monWakeup, week1) {
		t.Errorf("expected false when already sent in ISO week %s", week1)
	}
	if !shouldTriggerWeeklyReport(nextMon, week1) {
		t.Errorf("expected true on next Monday for new ISO week")
	}
}

func TestFormatTelegramReportHTMLEscape(t *testing.T) {
	report := &MarketReport{
		PeriodDays:  7,
		GeneratedAt: time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC),
		TotalJobs:   10,
		ApplyJobs:   2,
		WatchJobs:   3,
		MatchingSkills: []SkillStat{
			{Skill: "c++ <20> & boost", Frequency: 0.5},
		},
		TopCompanies: []CompanyStat{
			{Company: "Acme <Tech> & Co", JobCount: 4, AvgScore: 7.5},
		},
	}

	out := FormatTelegramReport(report)
	if !strings.Contains(out, "Acme &lt;Tech&gt; &amp; Co") {
		t.Errorf("expected escaped company name, got:\n%s", out)
	}
	if !strings.Contains(out, "C++ &lt;20&gt; &amp; boost") {
		t.Errorf("expected escaped skill name, got:\n%s", out)
	}
}
