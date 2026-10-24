"""scraper.sources.filters için stdlib unittest (ek bağımlılık gerekmez):
python -m unittest discover -s scraper/tests -t .
"""
import unittest

from scraper.sources.filters import is_rejected_title, is_tech_title


class TestFilters(unittest.TestCase):
    def test_passes_tech_and_data_roles(self):
        for t in ["Backend Developer", "Junior Data Analyst", "Business Intelligence Analyst",
                  "Machine Learning Engineer (Graduate)", "Veri Analisti", "Yazılım Uzmanı"]:
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


if __name__ == "__main__":
    unittest.main()
