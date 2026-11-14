"""
Faz 2A Scraper P0 düzeltmeleri için izole (mock destekli) birim testleri:
python3 -m unittest scraper/tests/test_sources.py
"""
import re
import sys
import types
import unittest
from pathlib import Path
from unittest.mock import MagicMock, patch


def _install_optional_stubs() -> None:
    """Ortamda kurulu olmayan 3. parti paketler için hafif test stub'ları yükler."""
    if "requests" not in sys.modules:
        try:
            import requests  # noqa: F401
        except ImportError:
            req_mod = types.ModuleType("requests")

            class _DummySession:
                def __init__(self):
                    self.headers = {}

                def get(self, *args, **kwargs):
                    raise NotImplementedError("mock me")

                def post(self, *args, **kwargs):
                    raise NotImplementedError("mock me")

            req_mod.Session = _DummySession
            sys.modules["requests"] = req_mod

    if "bs4" not in sys.modules:
        try:
            import bs4  # noqa: F401
        except ImportError:
            bs4_mod = types.ModuleType("bs4")

            class _DummyTag:
                def __init__(self, text: str):
                    self._text = text

                def get_text(self, sep: str = "") -> str:
                    clean = re.sub(r"<[^>]+>", sep if sep else " ", self._text)
                    return re.sub(r"[ \t]+", " ", clean).strip()

            class _DummySoup:
                def __init__(self, html_text: str, parser: str = "html.parser"):
                    self.html_text = html_text

                def find(self, tag: str, class_: str = ""):
                    if not class_:
                        return None
                    pattern = rf"<{tag}[^>]*class=[\"'][^\"']*\b{re.escape(class_)}\b[^\"']*[\"'][^>]*>(.*?)</{tag}>"
                    m = re.search(pattern, self.html_text, re.DOTALL | re.IGNORECASE)
                    return _DummyTag(m.group(1)) if m else None

                def get_text(self, sep: str = "") -> str:
                    return _DummyTag(self.html_text).get_text(sep)

            bs4_mod.BeautifulSoup = _DummySoup
            sys.modules["bs4"] = bs4_mod

    if "jobspy" not in sys.modules:
        try:
            import jobspy  # noqa: F401
        except ImportError:
            jobspy_mod = types.ModuleType("jobspy")
            jobspy_mod.scrape_jobs = MagicMock(return_value=None)
            sys.modules["jobspy"] = jobspy_mod


_install_optional_stubs()

from scraper.models.job import Job  # noqa: E402
from scraper.sources.ats_source import ATSSource, is_allowed_location  # noqa: E402
from scraper.sources.eurocontrol_source import EurocontrolSource  # noqa: E402
from scraper.sources.filters import is_target_location, is_tech_title  # noqa: E402
from scraper.sources.jobspy_source import JobSpySource, resolve_indeed_country  # noqa: E402


class TestPhase2ASources(unittest.TestCase):
    def test_ats_belgium_and_luxembourg_indicators(self):
        for loc in [
            "Brussels, Belgium",
            "Leuven",
            "Zaventem",
            "Liège",
            "Liege",
            "Luxembourg",
            "Antwerpen",
            "Gent",
            "Mechelen",
            "Louvain-la-Neuve",
            "België",
            "Belgie",
            "Belgique",
        ]:
            self.assertTrue(is_allowed_location(loc), f"expected allowed: {loc}")

        self.assertFalse(is_allowed_location("Remote, KSA"))
        self.assertFalse(is_allowed_location("San Francisco, CA (Remote)"))
        self.assertFalse(is_target_location("San Francisco, CA (Remote)"))
        self.assertTrue(is_allowed_location("Home Based - Americas; Home based - EMEA"))

    def test_filters_new_entry_signals_and_tech_terms(self):
        self.assertTrue(is_tech_title("Starter Programme", "Build REST API services"))
        self.assertTrue(is_tech_title("V.I.E Opportunity", "Software engineering with database"))
        self.assertTrue(is_tech_title("VIE Track", "Data pipeline development"))
        self.assertTrue(is_tech_title("Young Potential", "Backend API work"))
        self.assertTrue(is_tech_title("Jr Specialist", "SQL database"))
        self.assertTrue(is_tech_title("Graduates 2026", "REST service"))
        self.assertTrue(is_tech_title("Traineeship", "Software testing and pipeline"))
        self.assertTrue(is_tech_title("Juniors Welcome", "API integration"))
        self.assertFalse(is_tech_title("Young Potential", "HR and recruiting"))

    def test_eurocontrol_new_and_fallback_html_and_maastricht_detection(self):
        src = EurocontrolSource({})

        # 1) Yeni HTML: div.rich-editor-text + URL'de noc-ma -> Maastricht, Netherlands
        resp_new = MagicMock(status_code=200, text='<div class="rich-editor-text"><p>Cloud platform role</p></div>')
        with patch.object(src.session, "get", return_value=resp_new):
            desc, loc = src._fetch_job_detail("https://jobs.eurocontrol.int/jobs/platform-noc-ma-2026/", title="Platform Expert")
            self.assertIn("Cloud platform role", desc)
            self.assertEqual(loc, "Maastricht, Netherlands")

        # 2) Yeni HTML: div.jobs-detail-listing + metinde Maastricht -> Maastricht, Netherlands
        resp_listing = MagicMock(status_code=200, text='<div class="jobs-detail-listing"><p>Located in Maastricht center</p></div>')
        with patch.object(src.session, "get", return_value=resp_listing):
            desc, loc = src._fetch_job_detail("https://jobs.eurocontrol.int/jobs/atm-engineer-2026/", title="ATM Engineer")
            self.assertIn("Maastricht", desc)
            self.assertEqual(loc, "Maastricht, Netherlands")

        # 3) Eski HTML fallback: div.cs-job-detail -> Brussels, Belgium
        resp_old = MagicMock(status_code=200, text='<div class="cs-job-detail"><p>Headquarters role</p></div>')
        with patch.object(src.session, "get", return_value=resp_old):
            desc, loc = src._fetch_job_detail("https://jobs.eurocontrol.int/jobs/software-2026/", title="Software Engineer")
            self.assertIn("Headquarters role", desc)
            self.assertEqual(loc, "Brussels, Belgium")

    def test_jobspy_resolve_indeed_country_remote_eu_not_turkey(self):
        for loc in ["Remote", "Europe", "EU", "EMEA", "Remote - Europe"]:
            country = resolve_indeed_country(loc, default="Turkey")
            self.assertNotEqual(country, "Turkey", f"unexpected Turkey for {loc}")
            self.assertEqual(country, "Netherlands")

        self.assertEqual(resolve_indeed_country("Istanbul, Turkey"), "Turkey")
        self.assertEqual(resolve_indeed_country("Brussels, Belgium"), "Belgium")

    def test_jobspy_row_to_job_fallback_location_and_nat(self):
        src = JobSpySource({})
        row = {
            "title": "Junior Backend Engineer",
            "company": "Acme EU",
            "job_url": "https://example.com/jobs/123",
            "site": "linkedin",
            "id": "123",
            "location": float("nan"),
            "description": "Python and Go",
            "date_posted": "NaT",
        }
        job = src._row_to_job(row, fallback_location="Brussels, Belgium")
        self.assertIsNotNone(job)
        self.assertEqual(job.location, "Brussels, Belgium")
        self.assertIsNone(job.posted_at)

        row_empty_loc = dict(row, location="", date_posted="None")
        job2 = src._row_to_job(row_empty_loc, fallback_location="Amsterdam, Netherlands")
        self.assertEqual(job2.location, "Amsterdam, Netherlands")
        self.assertIsNone(job2.posted_at)

    def test_jobspy_fetch_sorts_turkey_locations_last(self):
        src = JobSpySource({
            "locations": ["Istanbul, Turkey", "Brussels, Belgium", "Ankara, Turkey", "Amsterdam, Netherlands"],
            "country_indeed": "Turkey",
        })
        visited = []

        def _fake_fetch_kw(keyword, location):
            visited.append(location)
            return []

        with patch.object(src, "_fetch_keyword", side_effect=_fake_fetch_kw), patch("scraper.sources.jobspy_source.time.sleep"):
            src.fetch(["backend engineer"], "Brussels, Belgium")

        self.assertEqual(
            visited,
            ["Brussels, Belgium", "Amsterdam, Netherlands", "Istanbul, Turkey", "Ankara, Turkey"],
        )

    def test_main_default_location_and_source_order(self):
        main_path = Path(__file__).resolve().parents[1] / "main.py"
        content = main_path.read_text(encoding="utf-8")
        self.assertIn('source_cfg.get("location", "Brussels, Belgium")', content)

        order = [
            'cfg["sources"].get("eurocontrol"',
            'cfg["sources"].get("ats"',
            'cfg["sources"].get("arbeitnow"',
            'cfg["sources"].get("techcareer"',
            'cfg["sources"].get("coderspace"',
            'cfg["sources"].get("kariyer_net"',
            'cfg["sources"].get("jobspy"',
        ]
        positions = [content.index(token) for token in order]
        self.assertEqual(positions, sorted(positions))

    def test_jobspy_secondary_keywords_limit_and_on_batch_streaming(self):
        src = JobSpySource({
            "locations": ["Brussels, Belgium", "Berlin, Germany", "Antwerp, Belgium", "Istanbul, Turkey"],
            "secondary_keywords_limit": 2,
            "country_indeed": "Turkey",
        })
        keywords = ["software engineer", "backend engineer", "go developer", "python developer"]
        queried_pairs = []
        streamed_batches = []

        def _fake_fetch_kw(keyword, location):
            queried_pairs.append((location, keyword))
            return [
                Job(
                    source="linkedin",
                    external_id=f"{location}:{keyword}",
                    url=f"https://example.com/{keyword}",
                    title=keyword,
                    company="Acme",
                    location=location,
                )
            ]

        with patch.object(src, "_fetch_keyword", side_effect=_fake_fetch_kw), patch(
            "scraper.sources.jobspy_source.time.sleep"
        ):
            all_jobs = src.fetch(keywords, "Brussels, Belgium", on_batch=lambda batch: streamed_batches.append(list(batch)))

        # Primary lokasyonlarda (Brussels, Istanbul) 4 keyword'ün tamamı; ikincil lokasyonlarda (Berlin, Antwerp) ilk 2 keyword aranmalı
        brussels_kws = [kw for loc, kw in queried_pairs if loc == "Brussels, Belgium"]
        istanbul_kws = [kw for loc, kw in queried_pairs if loc == "Istanbul, Turkey"]
        berlin_kws = [kw for loc, kw in queried_pairs if loc == "Berlin, Germany"]
        antwerp_kws = [kw for loc, kw in queried_pairs if loc == "Antwerp, Belgium"]

        self.assertEqual(brussels_kws, keywords)
        self.assertEqual(istanbul_kws, keywords)
        self.assertEqual(berlin_kws, keywords[:2])
        self.assertEqual(antwerp_kws, keywords[:2])

        # Toplam 4 + 2 + 2 + 4 = 12 alt sorgu -> her birinde on_batch anında çağrılmalı ve all_jobs da 12 eleman dönmeli
        self.assertEqual(len(streamed_batches), 12)
        self.assertEqual(len(all_jobs), 12)


class TestPhase2BSources(unittest.TestCase):
    def test_job_smart_truncate_short_and_long_description(self):
        short_desc = "Short description with Requirements: Python"
        short_job = Job(
            source="ashby",
            external_id="ashby:alan:1",
            url="https://jobs.ashbyhq.com/alan/1",
            title="Backend Engineer",
            company="Alan",
            description=short_desc,
        )
        self.assertEqual(short_job.to_redis_fields()["description"], short_desc)

        head_part = "H" * 1500
        mid_part = "M" * 1200
        tail_part = ("R" * 2420) + "Requirements: Python, English"
        long_desc = head_part + mid_part + tail_part
        self.assertGreater(len(long_desc), 4000)

        long_job = Job(
            source="ashby",
            external_id="ashby:alan:2",
            url="https://jobs.ashbyhq.com/alan/2",
            title="Backend Engineer",
            company="Alan",
            description=long_desc,
        )
        truncated = long_job.to_redis_fields()["description"]
        self.assertEqual(len(truncated), 1500 + 5 + 2450)
        self.assertTrue(truncated.startswith(head_part))
        self.assertIn("\n...\n", truncated)
        self.assertTrue(truncated.endswith("Requirements: Python, English"))

    def test_ats_ashby_parse_and_filters(self):
        src = ATSSource({
            "ashby": [{"name": "Alan", "slug": "alan"}],
        })
        payload = {
            "jobs": [
                {
                    "id": "job-eu-1",
                    "title": "Software Engineer - Backend",
                    "location": "Brussels, Belgium",
                    "jobUrl": "https://jobs.ashbyhq.com/alan/job-eu-1",
                    "descriptionHtml": "<p>Build Python &amp; Go microservices.</p>",
                    "publishedAt": "2026-11-10T10:00:00Z",
                    "isListed": True,
                },
                {
                    "id": "job-us-2",
                    "title": "Software Engineer - Backend",
                    "location": "San Francisco, United States",
                    "jobUrl": "https://jobs.ashbyhq.com/alan/job-us-2",
                    "descriptionPlain": "Build Python services.",
                },
                {
                    "id": "job-hr-3",
                    "title": "People Partner",
                    "location": "Brussels, Belgium",
                    "jobUrl": "https://jobs.ashbyhq.com/alan/job-hr-3",
                    "descriptionPlain": "Recruiting and HR operations.",
                },
            ]
        }
        resp = MagicMock(status_code=200)
        resp.json.return_value = payload

        with patch.object(src.session, "get", return_value=resp) as mock_get:
            jobs = src.fetch(["backend"], "Brussels, Belgium")

        mock_get.assert_called_once_with(
            "https://api.ashbyhq.com/posting-api/job-board/alan",
            timeout=12,
        )
        self.assertEqual(len(jobs), 1)
        self.assertEqual(jobs[0].source, "ashby")
        self.assertEqual(jobs[0].external_id, "ashby:alan:job-eu-1")
        self.assertEqual(jobs[0].company, "Alan")
        self.assertEqual(jobs[0].location, "Brussels, Belgium")
        self.assertIn("Build Python & Go microservices.", jobs[0].description)

    def test_ats_ashby_404_and_network_error_resilience(self):
        src = ATSSource({
            "ashby": [{"name": "Payflip", "slug": "payflip"}],
        })

        resp_404 = MagicMock(status_code=404)
        with patch.object(src.session, "get", return_value=resp_404):
            self.assertEqual(src.fetch(["backend"], "Brussels, Belgium"), [])

        with patch.object(src.session, "get", side_effect=RuntimeError("network timeout")):
            self.assertEqual(src.fetch(["backend"], "Brussels, Belgium"), [])

    def test_ats_smartrecruiters_iso_country_codes(self):
        # Yeni eklenen ISO ülke kodlarının (cz, ro, hu, bg, ee, gr, uk) doğrudan kabul edildiğini doğrula
        for code in ("cz", "ro", "hu", "bg", "ee", "gr", "uk"):
            self.assertTrue(
                is_allowed_location("Unknown City", country_code=code),
                f"expected country_code={code!r} to be allowed",
            )

        # SmartRecruiters payload'ında EU_TR_INDICATORS'da olmayan şehir + ISO ülke kodu ("cz", "ro") akışı
        src = ATSSource({
            "smartrecruiters": [{"name": "TestCorp", "company_id": "testcorp"}],
        })
        list_resp = MagicMock(status_code=200)
        list_resp.json.return_value = {
            "totalFound": 2,
            "content": [
                {
                    "id": "sr-cz-1",
                    "name": "Backend Engineer",
                    "location": {"city": "Brno", "country": "cz", "remote": False},
                    "releasedDate": "2026-11-12T08:00:00Z",
                },
                {
                    "id": "sr-ro-2",
                    "name": "Software Engineer",
                    "location": {"city": "Timisoara", "country": "ro", "remote": False},
                    "releasedDate": "2026-11-12T09:00:00Z",
                },
            ],
        }
        detail_resp = MagicMock(status_code=200)
        detail_resp.json.return_value = {
            "jobAd": {
                "sections": {
                    "jobDescription": {"text": "<p>Build Python and Go services.</p>"}
                }
            }
        }

        with patch.object(src.session, "get", side_effect=[list_resp, detail_resp, detail_resp]):
            jobs = src.fetch(["backend"], "Brussels, Belgium")

        self.assertEqual(len(jobs), 2)
        self.assertEqual([j.external_id for j in jobs], ["sr:testcorp:sr-cz-1", "sr:testcorp:sr-ro-2"])
        self.assertEqual([j.location for j in jobs], ["Brno, cz", "Timisoara, ro"])


if __name__ == "__main__":
    unittest.main()
