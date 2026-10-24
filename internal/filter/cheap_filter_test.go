package filter_test

import (
	"strings"
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
			"intern", "stajyer", "working student", "werkstudent", "praktikum",
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
		{"3 years of experience in Go or Java."},
		{"Over 20 years of experience in the industry, we build great software."},
	}

	for _, tc := range cases {
		result := f.Check("Backend Developer", "Trendyol", tc.description)
		if result.Rejected {
			t.Errorf("expected pass for desc=%q, got rejected: %s", tc.description, result.Reason)
		}
	}
}

func TestCheapFilter_PraktikumRejected(t *testing.T) {
	f := testFilter()
	if r := f.Check("Praktikum Backend Developer (m/w/d)", "Stella", ""); !r.Rejected {
		t.Error("Praktikum ilanı elenmeliydi")
	}
}

func TestCheapFilter_MLAndDataTitlesPass(t *testing.T) {
	f := testFilter()
	for _, title := range []string{
		"Junior Data Scientist",
		"Machine Learning Engineer (Graduate)",
		"Veri Bilimci",
		"AI Engineer - New Grad",
		"Data Analyst",
		"Business Intelligence Analyst",
		"Veri Analisti",
	} {
		if r := f.Check(title, "Acme", "Python and SQL"); r.Rejected {
			t.Errorf("%q geçmeliydi, red: %s", title, r.Reason)
		}
	}
}

func TestCheapFilter_RejectsCorporateNoiseAndStage(t *testing.T) {
	f := testFilter()
	for _, title := range []string{
		"Operational Risk Analyst",
		"FinCrime Analyst",
		"Deal Intelligence Analyst",
		"Product Cost Analyst",
		"Stage: Software Developer",
		"Veri Merkezleri İşletme Müdürü",
		"Technical Account Management Team Leader France",
	} {
		if r := f.Check(title, "Acme", "We are a fast-growing cloud company."); !r.Rejected {
			t.Errorf("%q elenmeliydi ama geçti", title)
		}
	}
}

func TestCheapFilter_EntrySignalGivesChance(t *testing.T) {
	f := testFilter()
	// Başlıkta engineer/developer yok ama new-grad sinyali + teknik açıklama → AI'ya gitmeli.
	desc := "Graduate programme: you will work with Python, SQL and backend APIs."
	if r := f.Check("Graduate Programme 2026", "Acme", desc); r.Rejected {
		t.Errorf("giriş seviyesi + teknik açıklama geçmeliydi, red: %s", r.Reason)
	}
	// Teknik içerik yoksa yine elenmeli.
	if r := f.Check("Graduate Programme 2026", "Acme", "Join our sales and marketing rotation."); !r.Rejected {
		t.Error("teknik olmayan graduate programı elenmeliydi")
	}
	// Sinyal yoksa katı kalmalı.
	if r := f.Check("Office Assistant", "Acme", "Python SQL backend software api"); !r.Rejected {
		t.Error("sinyalsiz alakasız başlık elenmeliydi")
	}
}

func TestCheapFilter_RejectsFourPlusYears(t *testing.T) {
	f := testFilter()
	for _, d := range []string{
		"You have 4+ years of experience in Go or Java.",
		"Experience of at least 5 years building backend services.",
		"4-6 years of professional experience required.",
		"Mindestens 4 Jahre Berufserfahrung als Entwickler.",
	} {
		if r := f.Check("Backend Developer", "Acme", d); !r.Rejected {
			t.Errorf("%q elenmeliydi", d)
		}
	}
	// Başlıkta açık giriş sinyali varsa yıl şartı AI'ya bırakılır.
	if r := f.Check("Junior Backend Developer", "Acme", "4+ years of experience preferred"); r.Rejected {
		t.Errorf("junior başlık yıl kapısına takılmamalı: %s", r.Reason)
	}
}

func TestCheapFilter_LanguageBarrier(t *testing.T) {
	f := testFilter()
	for _, d := range []string{
		"Fluent German (C1) is required for daily communication with customers.",
		"Sie bringen sehr gute Deutschkenntnisse mit.",
		"Vloeiend Nederlands is vereist voor deze rol.",
	} {
		if r := f.Check("Software Engineer", "Acme", d); !r.Rejected {
			t.Errorf("%q dil bariyeri olarak elenmeliydi", d)
		}
	}
	for _, d := range []string{
		"German is a plus but not required. We work in English.",
		"Deutschkenntnisse von Vorteil, Arbeitssprache Englisch.",
	} {
		if r := f.Check("Software Engineer", "Acme", d); r.Rejected {
			t.Errorf("%q yumuşak dil şartı, elenmemeli: %s", d, r.Reason)
		}
	}
}

func TestCheapFilter_NonEnglishPosting(t *testing.T) {
	f := testFilter()
	nl := strings.Repeat("Wij zoeken een ervaren developer voor ons team met een leuke sfeer bij een mooi bedrijf. ", 8)
	if r := f.Check("Software Engineer", "Acme", nl); !r.Rejected {
		t.Error("Felemenkçe ilan elenmeliydi")
	}
	en := strings.Repeat("We are looking for a developer to join our team and build the platform with you. ", 8)
	if r := f.Check("Software Engineer", "Acme", en); r.Rejected {
		t.Errorf("İngilizce ilan elenmemeli: %s", r.Reason)
	}
	friendly := nl + " Dutch is not required, we are an English-speaking team."
	if r := f.Check("Software Engineer", "Acme", friendly); r.Rejected {
		t.Errorf("İngilizce-dostu ilan elenmemeli: %s", r.Reason)
	}
}
