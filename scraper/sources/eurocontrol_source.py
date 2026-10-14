"""
EurocontrolSource — EUROCONTROL Resmi Kariyer Portalı (jobs.eurocontrol.int) Çekicisi.

Brüksel ve Maastricht merkezli havacılık, hava trafik yönetimi (ATM),
platform, sistem, yazılım ve staj ilanlarını doğrudan çeker.
"""

import logging
import re
from urllib.parse import urljoin
import requests
from bs4 import BeautifulSoup

from scraper.models.job import Job
from scraper.sources.base import BaseSource

logger = logging.getLogger("scraper.eurocontrol")


class EurocontrolSource(BaseSource):
    BASE_URL = "https://jobs.eurocontrol.int"
    AJAX_URL = "https://jobs.eurocontrol.int/wp-admin/admin-ajax.php"

    def __init__(self, cfg: dict):
        super().__init__(name="eurocontrol")
        self.cfg = cfg
        self.session = requests.Session()
        self.session.headers.update({
            "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
            "Accept": "application/json, text/javascript, */*; q=0.01",
        })

    def fetch(self, keywords: list[str], location: str) -> list[Job]:
        logger.info("fetching official EUROCONTROL vacancies...")
        jobs: list[Job] = []

        try:
            # Sayfa 1 ve 2'yi çek (genellikle 10-25 arası aktif ilan olur)
            for page in [1, 2]:
                page_jobs = self._fetch_page(page)
                jobs.extend(page_jobs)
                if len(page_jobs) < 10:
                    break
        except Exception as e:
            logger.error("eurocontrol fetch error: %s", e)

        logger.info("eurocontrol fetched %d official vacancies", len(jobs))
        return jobs

    def _fetch_page(self, page_num: int) -> list[Job]:
        jobs: list[Job] = []
        payload = {
            "action": "lumesse_ajax_modern_list",
            "lanugage": "1",
            "sendEvent": "false",
        }
        if page_num > 1:
            payload["page_job"] = str(page_num)

        r = self.session.post(self.AJAX_URL, data=payload, timeout=15)
        if r.status_code != 200:
            logger.warning("eurocontrol ajax returned status %d", r.status_code)
            return jobs

        data = r.json()
        html_content = data.get("advertsHtml", "")
        if not html_content:
            return jobs

        soup = BeautifulSoup(html_content, "html.parser")
        seen_urls = set()

        for a in soup.find_all("a"):
            title = a.get_text(strip=True)
            href = a.get("href", "")

            if not href or "/jobs/" not in href or title in ["Read more", "", "Apply Now"]:
                continue

            full_url = urljoin(self.BASE_URL, href)
            if full_url in seen_urls:
                continue
            seen_urls.add(full_url)

            # URL'den referans ID çıkar (örn: platform-expert-noc-ma-2026-ad-1161-1)
            ext_id = href.rstrip("/").split("/")[-1]

            # İlan detay sayfasından açıklama ve lokasyon çek
            desc, loc = self._fetch_job_detail(full_url)

            jobs.append(Job(
                source="eurocontrol",
                external_id=f"ec:{ext_id}",
                url=full_url,
                title=title,
                company="EUROCONTROL",
                location=loc or "Brussels, Belgium",
                description=desc,
            ))

        return jobs

    def _fetch_job_detail(self, url: str) -> tuple[str, str]:
        """İlan detay sayfasını çekip açıklama ve lokasyonu ayıklar."""
        try:
            r = self.session.get(url, timeout=10)
            if r.status_code != 200:
                return "", "Brussels, Belgium"

            soup = BeautifulSoup(r.text, "html.parser")
            main = soup.find("div", class_="cs-job-detail") or soup.find("div", class_="rich-text-block")
            if not main:
                return "", "Brussels, Belgium"

            raw_text = main.get_text("\n")
            cleaned_desc = re.sub(r"\n{3,}", "\n\n", raw_text).strip()

            # Lokasyon tespiti
            loc = "Brussels, Belgium"
            if "Maastricht" in cleaned_desc[:500]:
                loc = "Maastricht, Netherlands"
            elif "Brétigny" in cleaned_desc[:500]:
                loc = "Brétigny-sur-Orge, France"
            elif "Luxembourg" in cleaned_desc[:500]:
                loc = "Luxembourg"

            return cleaned_desc, loc
        except Exception as e:
            logger.debug("could not fetch detail for %s: %s", url, e)
            return "", "Brussels, Belgium"
