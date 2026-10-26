"""scraper.sources.filters, kariyer_source, jobspy_source ve config paritesi için stdlib unittest:
python -m unittest discover -s scraper/tests -t .
"""
from pathlib import Path
import unittest
import yaml

from scraper.sources.filters import is_rejected_title, is_tech_title
from scraper.sources.kariyer_source import _should_drop_kariyer_title
from scraper.sources.jobspy_source import resolve_indeed_country
from scraper.sources.ats_source import is_allowed_location


class TestFilters(unittest.TestCase):
    def test_passes_tech_and_data_roles(self):
        for t in ["Backend Developer", "Junior Data Analyst", "Business Intelligence Analyst",
                  "Machine Learning Engineer (Graduate)", "Veri Analisti", "Yazılım Uzmanı", "Yazılım Uzman Yardımcısı"]:
            self.assertTrue(is_tech_title(t, "Python SQL"), t)

    def test_rejects_corporate_noise(self):
        for t in ["Operational Risk Analyst", "FinCrime Analyst", "Deal Intelligence Analyst",
                  "Product Cost Analyst", "Stage: Software Developer", "Veri Merkezleri İşletme Müdürü",
                  "Senior Backend Engineer", "Ürün Yöneticisi"]:
            self.assertFalse(is_tech_title(t, "cloud data company"), t)

    def test_soft_entry_gate_needs_real_tech_terms(self):
        self.assertTrue(is_tech_title("Graduate Programme", "Python and SQL backend work"))
        self.assertFalse(is_tech_title("Graduate Programme", "sales and marketing rotation"))
        self.assertFalse(is_tech_title("Junior HR Associate", "recruiting, cloud-based tools"))

    def test_empty_title_rejected(self):
        self.assertTrue(is_rejected_title(""))

    def test_kariyer_title_filter(self):
        self.assertFalse(_should_drop_kariyer_title("Yazılım Mühendisi"))
        self.assertFalse(_should_drop_kariyer_title("Veri Analisti"))
        self.assertFalse(_should_drop_kariyer_title("Yazılım Uzman Yardımcısı"))
        self.assertTrue(_should_drop_kariyer_title("Kıdemli .Net Geliştirici"))
        self.assertTrue(_should_drop_kariyer_title("Yazılım Stajyeri"))
        self.assertTrue(_should_drop_kariyer_title("Software Engineer (Disabled Candidate)"))

    def test_denmark_copenhagen_support(self):
        self.assertEqual(resolve_indeed_country("Copenhagen, Denmark"), "Denmark")
        self.assertEqual(resolve_indeed_country("København, Denmark"), "Denmark")
        self.assertEqual(resolve_indeed_country("Brussels, Belgium"), "Belgium")
        self.assertEqual(resolve_indeed_country("Amsterdam, Netherlands"), "Netherlands")
        self.assertEqual(resolve_indeed_country("Berlin, Germany"), "Germany")
        self.assertEqual(resolve_indeed_country("Istanbul, Turkey"), "Turkey")
        self.assertTrue(is_allowed_location("Copenhagen, Denmark"))
        self.assertTrue(is_allowed_location("København"))

    def test_config_key_parity_and_copenhagen_presence(self):
        root = Path(__file__).resolve().parents[2]
        with open(root / "config.yaml", encoding="utf-8") as f:
            tpl = yaml.safe_load(f)

        self.assertIn("Copenhagen, Denmark", tpl["sources"]["jobspy"]["locations"])
        self.assertTrue(tpl["sources"]["kariyer_net"]["enabled"])
        self.assertTrue(tpl["sources"]["coderspace"]["enabled"])

        local_path = root / "config.local.yaml"
        if local_path.exists():
            with open(local_path, encoding="utf-8") as f:
                loc = yaml.safe_load(f)
            self.assertEqual(set(tpl.keys()), set(loc.keys()))
            self.assertEqual(set(tpl["candidate"].keys()), set(loc["candidate"].keys()))
            self.assertEqual(set(tpl["sources"].keys()), set(loc["sources"].keys()))
            self.assertIn("Copenhagen, Denmark", loc["sources"]["jobspy"]["locations"])


if __name__ == "__main__":
    unittest.main()
