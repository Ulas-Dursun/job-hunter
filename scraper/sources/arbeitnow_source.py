"""
ArbeitnowSource — Avrupa & Almanya İngilizce Teknoloji İlanları Çekicisi.

Arbeitnow, vize sponsorluğu ve uzaktan çalışma etiketleri sunan,
özellikle Avrupa ve Almanya'daki İngilizce konuşan yazılım pozisyonları için
ücretsiz ve resmi bir JSON API sağlar.
"""

import html
import logging
import re
import requests
from bs4 import BeautifulSoup

from scraper.models.job import Job
from scraper.sources.base import BaseSource

logger = logging.getLogger("scraper.arbeitnow")


def clean_html(text: str) -> str:
    if not text:
        return ""
    text = html.unescape(text)
    soup = BeautifulSoup(text, "html.parser")
    return re.sub(r"\n{3,}", "\n\n", soup.get_text("\n")).strip()


class ArbeitnowSource(BaseSource):
    API_URL = "https://www.arbeitnow.com/api/job-board-api"

    def __init__(self, cfg: dict):
        super().__init__(name="arbeitnow")
        self.cfg = cfg
        self.session = requests.Session()
        self.session.headers.update({
            "User-Agent": "JobHunter-EU-Collector/1.0",
            "Accept": "application/json",
        })

    def fetch(self, keywords: list[str], location: str) -> list[Job]:
        logger.info("fetching Arbeitnow EU tech jobs...")
        jobs: list[Job] = []

        try:
            r = self.session.get(self.API_URL, timeout=15)
            if r.status_code != 200:
                logger.warning("arbeitnow returned status %d", r.status_code)
                return jobs

            data = r.json()
            items = data.get("data", [])

            for item in items:
                title = item.get("title", "").strip()
                company = item.get("company_name", "").strip()
                job_url = item.get("url", "")
                slug = item.get("slug", "")
                loc = item.get("location", "")
                remote = item.get("remote", False)
                visa = item.get("visa_sponsorship", False)
                desc = clean_html(item.get("description", ""))
                posted_at = item.get("created_at")

                if not title or not job_url:
                    continue

                if remote and not loc:
                    loc = "Remote (EU)"
                elif remote and loc:
                    loc = f"{loc} (Remote)"

                # Eğer vize sponsorluğu varsa açıklamaya veya konuma not düş
                if visa:
                    loc = f"{loc} [Visa Sponsored]"

                jobs.append(Job(
                    source="arbeitnow",
                    external_id=f"an:{slug}",
                    url=job_url,
                    title=title,
                    company=company,
                    location=loc,
                    description=desc,
                    posted_at=str(posted_at) if posted_at else None,
                ))

            logger.info("arbeitnow fetched %d jobs", len(jobs))
        except Exception as e:
            logger.error("arbeitnow fetch error: %s", e)

        return jobs
