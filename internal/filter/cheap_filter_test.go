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
