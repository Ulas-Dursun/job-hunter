"""
ATSSource — Greenhouse, Lever ve SmartRecruiters genel API çekicisi.

Şirketlerin kariyer sayfalarının arka planındaki halka açık JSON API'lerini kullanır.
Scraping, captcha veya ban riski yoktur; doğrudan güncel ilanları çeker.
"""

import html
import logging
import re
from typing import Optional
from urllib.parse import urlparse
import requests
from bs4 import BeautifulSoup

from scraper.models.job import Job
from scraper.sources.base import BaseSource

logger = logging.getLogger("scraper.ats")


def clean_html(text: str) -> str:
    """HTML etiketlerini temizler ve düz metne dönüştürür."""
    if not text:
        return ""
    text = html.unescape(text)
    soup = BeautifulSoup(text, "html.parser")
    # Fazla boşlukları düzenle
    return re.sub(r"\n{3,}", "\n\n", soup.get_text("\n")).strip()


class ATSSource(BaseSource):
    def __init__(self, cfg: dict):
        super().__init__(name="ats")
        self.cfg = cfg
        self.session = requests.Session()
        self.session.headers.update({
            "User-Agent": "JobHunter-ATS-Collector/1.0",
            "Accept": "application/json",
        })

    def fetch(self, keywords: list[str], location: str) -> list[Job]:
        jobs: list[Job] = []

        # ── 1. Greenhouse ─────────────────────────────────────────────────────
        greenhouse_boards = self.cfg.get("greenhouse", [])
        for board in greenhouse_boards:
            name = board.get("name", "Unknown")
            token = board.get("token")
            if not token:
                continue
            jobs.extend(self._fetch_greenhouse(name, token))

        # ── 2. Lever ──────────────────────────────────────────────────────────
        lever_companies = self.cfg.get("lever", [])
        for comp in lever_companies:
            name = comp.get("name", "Unknown")
            slug = comp.get("slug")
            if not slug:
                continue
            jobs.extend(self._fetch_lever(name, slug))

        # ── 3. SmartRecruiters ────────────────────────────────────────────────
        smart_companies = self.cfg.get("smartrecruiters", [])
        for comp in smart_companies:
            name = comp.get("name", "Unknown")
            comp_id = comp.get("company_id")
            if not comp_id:
                continue
            jobs.extend(self._fetch_smartrecruiters(name, comp_id))

        logger.info("ats source fetched total %d jobs across configured boards", len(jobs))
        return jobs

    def _fetch_greenhouse(self, company_name: str, token: str) -> list[Job]:
        url = f"https://boards-api.greenhouse.io/v1/boards/{token}/jobs?content=true"
        jobs: list[Job] = []
        try:
            r = self.session.get(url, timeout=12)
            if r.status_code != 200:
                logger.warning("greenhouse board %s returned status %d", token, r.status_code)
                return jobs
            data = r.json()
            for item in data.get("jobs", []):
                title = item.get("title", "").strip()
                ext_id = str(item.get("id", ""))
                job_url = item.get("absolute_url", "")
                loc = item.get("location", {}).get("name", "") if isinstance(item.get("location"), dict) else ""
                desc = clean_html(item.get("content", ""))
                posted_at = item.get("updated_at")

                if not title or not job_url:
                    continue

                jobs.append(Job(
                    source="greenhouse",
                    external_id=f"gh:{token}:{ext_id}",
                    url=job_url,
                    title=title,
                    company=company_name,
                    location=loc,
                    description=desc,
                    posted_at=posted_at,
                ))
            logger.info("greenhouse %s (%s): %d jobs found", company_name, token, len(jobs))
        except Exception as e:
            logger.error("greenhouse fetch error for %s (%s): %s", company_name, token, e)
        return jobs

    def _fetch_lever(self, company_name: str, slug: str) -> list[Job]:
        url = f"https://api.lever.co/v0/postings/{slug}?mode=json"
        jobs: list[Job] = []
        try:
            r = self.session.get(url, timeout=12)
            if r.status_code != 200:
                logger.warning("lever slug %s returned status %d", slug, r.status_code)
                return jobs
            data = r.json()
            if not isinstance(data, list):
                return jobs
            for item in data:
                title = item.get("text", "").strip()
                ext_id = str(item.get("id", ""))
                job_url = item.get("hostedUrl", "")
                categories = item.get("categories", {})
                loc = categories.get("location", "") if isinstance(categories, dict) else ""
                desc = clean_html(item.get("descriptionPlain") or item.get("description", ""))
                posted_at = item.get("createdAt")

                if not title or not job_url:
                    continue

                jobs.append(Job(
                    source="lever",
                    external_id=f"lever:{slug}:{ext_id}",
                    url=job_url,
                    title=title,
                    company=company_name,
                    location=loc,
                    description=desc,
                    posted_at=str(posted_at) if posted_at else None,
                ))
            logger.info("lever %s (%s): %d jobs found", company_name, slug, len(jobs))
        except Exception as e:
            logger.error("lever fetch error for %s (%s): %s", company_name, slug, e)
        return jobs

    def _fetch_smartrecruiters(self, company_name: str, company_id: str) -> list[Job]:
        url = f"https://api.smartrecruiters.com/v1/companies/{company_id}/postings"
        jobs: list[Job] = []
        try:
            r = self.session.get(url, timeout=12)
            if r.status_code != 200:
                logger.warning("smartrecruiters %s returned status %d", company_id, r.status_code)
                return jobs
            data = r.json()
            items = data.get("content", [])
            for item in items:
                title = item.get("name", "").strip()
                ext_id = str(item.get("id", ""))
                loc_data = item.get("location", {})
                loc = f"{loc_data.get('city', '')}, {loc_data.get('country', '')}".strip(", ")
                job_url = f"https://jobs.smartrecruiters.com/{company_id}/{ext_id}"

                if not title or not ext_id:
                    continue

                jobs.append(Job(
                    source="smartrecruiters",
                    external_id=f"sr:{company_id}:{ext_id}",
                    url=job_url,
                    title=title,
                    company=company_name,
                    location=loc,
                    description="",  # SmartRecruiters liste uç noktası özet verir, detay Go veya filter'da zenginleşebilir
                    posted_at=item.get("releasedDate"),
                ))
            logger.info("smartrecruiters %s (%s): %d jobs found", company_name, company_id, len(jobs))
        except Exception as e:
            logger.error("smartrecruiters fetch error for %s (%s): %s", company_name, company_id, e)
        return jobs
