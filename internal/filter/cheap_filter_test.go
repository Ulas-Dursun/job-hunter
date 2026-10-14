package filter_test

import (
	"testing"

	"github.com/ulasdursun/job-hunter/internal/config"
	"github.com/ulasdursun/job-hunter/internal/filter"
)

func testFilter() *filter.CheapFilter {
	cfg := &config.CandidateConfig{
		BlacklistKeywords: []string{
			".NET", "C#", "PHP", "Ruby on Rails",
			"QA Engineer", "Test Engineer",
			"iOS Developer", "Android Developer",
			"7+ years", "8+ years", "10+ years",
			"Senior", "Manager", "Lead", "Principal",
			"Sales", "Satış", "Üretim", "6+ years",
			"intern", "stajyer", "working student", "werkstudent",
		},
	}
	return filter.NewCheapFilter(cfg)
}

func TestCheapFilter_RejectsTitleBlacklist(t *testing.T) {
	f := testFilter()

	cases := []struct{ title, company string }{
		{"Senior .NET Developer", "Acme"},
		{"PHP Backend Developer", "Corp"},
		{"QA Engineer", "TestCo"},
		{"iOS Developer - Remote", "Apple"},
		{"Senior Developer", "Firm"},
		{"Sales Engineer", "SalesCo"},
		{"Software Engineer Intern", "BigCo"},
		{"Stajyer Yazılım Mühendisi", "Trendyol"},
		{"Werkstudent Backend", "Siemens"},
	}

	for _, tc := range cases {
		result := f.Check(tc.title, tc.company, "")
		if !result.Rejected {
			t.Errorf("expected rejection for title=%q, got passed", tc.title)
		}
	}
}

func TestCheapFilter_PassesRelevantTitles(t *testing.T) {
	f := testFilter()

	cases := []struct{ title, company string }{
		{"International Software Engineer", "GlobalCo"}, // "intern" içermesine rağmen tam kelime olmadığı için geçmeli
		{"Backend Developer", "Trendyol"},
		{"Go Developer", "Getir"},
		{"Java Software Engineer", "Insider"},
		{"Software Engineer (New Grad)", "Dream Games"},
		{"DevOps Engineer", "Hepsiburada"},
		{"Platform Engineer", "Papara"},
		{"Data Engineer", "LC Waikiki"},
		{"Cloud Engineer", "Gunvor"},
		{"Junior Backend Developer", "Startup"},
		{"Software Engineer, Backend (New Grad)", "Peak"},
		{"Backend Developer (Node.js / Python / Go / Java)", "Alpaco"},
	}

	for _, tc := range cases {
		result := f.Check(tc.title, tc.company, "")
		if result.Rejected {
			t.Errorf("expected pass for title=%q, got rejected: %s", tc.title, result.Reason)
		}
	}
}

func TestCheapFilter_RejectsNonTechRoles(t *testing.T) {
	f := testFilter()

	nonTechTitles := []string{
		"Physical Therapist",
		"Clinic Director",
		"Facharzt Allgemeine Innere Medizin",
		"Plumbing and Maintenance - Installer",
		"Commercial Director, AU",
		"Psychotherapeut:in in Weiterbildung",
		"Account Executive",
		"Chef / Cook",
		"Barista",
	}

	for _, title := range nonTechTitles {
		result := f.Check(title, "Acme Corp", "")
		if !result.Rejected {
			t.Errorf("expected rejection for non-tech role %q, but passed", title)
		}
		if result.Reason != "non_tech_role" {
			t.Errorf("expected reason 'non_tech_role' for %q, got %q", title, result.Reason)
		}
	}
}

func TestCheapFilter_EurocontrolException(t *testing.T) {
	f := testFilter()

	// Normalde "intern" kara listede olduğu için reddedilmeli
	resNormal := f.Check("CNS Engineering Intern", "AviationCorp", "")
	if !resNormal.Rejected {
		t.Errorf("expected normal intern to be rejected")
	}

	// Ancak eurocontrol kaynağı için staj/traineeship serbest olmalı
	resEuro := f.Check("CNS Engineering Trainee", "EUROCONTROL", "", "eurocontrol")
	if resEuro.Rejected {
		t.Errorf("expected EUROCONTROL trainee to pass, got rejected: %s", resEuro.Reason)
	}
}

func TestCheapFilter_RejectsEmptyCompany(t *testing.T) {
	f := testFilter()

	for _, company := range []string{"nan", "NaN", "NAN", "", "   "} {
		result := f.Check("Backend Developer", company, "")
		if !result.Rejected {
			t.Errorf("expected rejection for company=%q", company)
		}
	}
}

func TestCheapFilter_RejectsDescriptionStack(t *testing.T) {
	f := testFilter()

	cases := []struct {
		title       string
		description string
	}{
		{
			"Backend Developer",
			"Strong expertise in .NET Core, C# developer and Entity Framework required.",
		},
		{
			"Software Engineer",
			"We use ASP.NET and entity framework for all backend services.",
		},
		{
			"Backend Engineer",
			"Expertise in C# and .net framework is mandatory for this position.",
		},
	}

	for _, tc := range cases {
		result := f.Check(tc.title, "Acme", tc.description)
		if !result.Rejected {
			t.Errorf("expected rejection for description stack, title=%q reason would be empty", tc.title)
		}
	}
}

func TestCheapFilter_RejectsHighExperienceInDescription(t *testing.T) {
	f := testFilter()

	cases := []struct {
		description string
	}{
		{"Minimum 5 yıl deneyim gereklidir."},
		{"minimum 10 years of experience required."},
		{"en az 6 yıl yazılım geliştirme deneyimi."},
		{"5+ years of hands-on backend experience."},
		{"At least 7 years of software development experience."},
	}

	for _, tc := range cases {
		result := f.Check("Software Engineer", "Acme", tc.description)
		if !result.Rejected {
			t.Errorf("expected rejection for high experience desc=%q", tc.description)
		}
	}
}

func TestCheapFilter_PassesAcceptableExperience(t *testing.T) {
	f := testFilter()

	cases := []struct {
		description string
	}{
		{"0-2 years of experience preferred."},
		{"Junior or entry level candidates welcome."},
		{"New grad position, no experience required."},
		{"1-3 years of backend experience is ideal."},
		{"4 years of experience in Go or Java."},
	}

	for _, tc := range cases {
		result := f.Check("Backend Developer", "Trendyol", tc.description)
		if result.Rejected {
			t.Errorf("expected pass for desc=%q, got rejected: %s", tc.description, result.Reason)
		}
	}
}
