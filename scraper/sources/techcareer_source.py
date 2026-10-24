"""
TechcareerSource — techcareer.net Türkiye Teknoloji İlanları Çekicisi.

Türkiye'deki yazılım, backend, devops, veri ve yeni mezun teknoloji ilanlarını
Next.js SSR veri yapısı üzerinden doğrudan ve temiz bir şekilde çeker.
"""

import html
import json
import logging
import re
import requests
from bs4 import BeautifulSoup

from scraper.models.job import Job
from scraper.sources.base import BaseSource
from scraper.sources.filters import is_tech_title

logger = logging.getLogger("scraper.techcareer")


def clean_html(text: str) -> str:
    if not text:
        return ""
    text = html.unescape(text)
    soup = BeautifulSoup(text, "html.parser")
    return re.sub(r"\n{3,}", "\n\n", soup.get_text("\n")).strip()


class TechcareerSource(BaseSource):
    BASE_URL = "https://www.techcareer.net"
    JOBS_URL = "https://www.techcareer.net/jobs"

    def __init__(self, cfg: dict):
        super().__init__(name="techcareer")
        self.cfg = cfg
        self.session = requests.Session()
        self.session.headers.update({
            "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
            "Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
        })

    def fetch(self, keywords: list[str], location: str) -> list[Job]:
        logger.info("fetching techcareer.net jobs...")
        jobs: list[Job] = []

        try:
            r = self.session.get(self.JOBS_URL, timeout=15)
            if r.status_code != 200:
                logger.warning("techcareer returned status %d", r.status_code)
                return jobs

            m = re.search(r'<script id="__NEXT_DATA__" type="application/json">(.*?)</script>', r.text)
            if not m:
                logger.warning("techcareer __NEXT_DATA__ not found")
                return jobs

            data = json.loads(m.group(1))
            items = data.get("props", {}).get("pageProps", {}).get("initialJobList", {}).get("jobListItems", [])

            for item in items:
                title = item.get("title", "").strip()
                slug = item.get("slug", "")
                ext_id = str(item.get("id", ""))
                if not title or not slug:
                    continue

                combined_title = f"{title} {item.get('jobTitleEn') or ''}".strip()
                if not is_tech_title(combined_title):
                    continue

                owner = item.get("owner") or {}
                comp_name = owner.get("name") if isinstance(owner, dict) else "Gizli Şirket"

                loc_data = item.get("location")
                if isinstance(loc_data, str):
                    city = loc_data.strip()
                elif isinstance(loc_data, dict):
                    city = (loc_data.get("name") or "").strip()
                else:
                    city = ""

                work_places = item.get("workPlaces") or []
                wp_parts = []
                for wp in work_places:
                    if isinstance(wp, str) and wp.strip():
                        wp_parts.append(wp.strip())
                    elif isinstance(wp, dict) and wp.get("name"):
                        wp_parts.append(str(wp.get("name")).strip())
                wp_text = ", ".join(wp_parts)
                loc = f"{city} ({wp_text})" if wp_text and city else (city or wp_text or "Turkey")

                job_url = f"{self.BASE_URL}/jobs/detail/{slug}"
                desc = self._fetch_detail_description(job_url)

                jobs.append(Job(
                    source="techcareer",
                    external_id=f"tc:{ext_id}",
                    url=job_url,
                    title=title,
                    company=comp_name,
                    location=loc,
                    description=desc,
                ))

            logger.info("techcareer fetched %d jobs", len(jobs))
        except Exception as e:
            logger.error("techcareer fetch error: %s", e)

        return jobs

    def _fetch_detail_description(self, job_url: str) -> str:
        try:
            r = self.session.get(job_url, timeout=10)
            if r.status_code != 200:
                return ""
            m = re.search(r'<script id="__NEXT_DATA__" type="application/json">(.*?)</script>', r.text)
            if not m:
                return ""
            data = json.loads(m.group(1))
            page_props = data.get("props", {}).get("pageProps", {})
            detail = page_props.get("jobDetail") or page_props.get("initialJobDetail") or {}
            content = detail.get("content") or {}
            raw_html = content.get("description", "") if isinstance(content, dict) else ""
            return clean_html(raw_html)
        except Exception:
            return ""
