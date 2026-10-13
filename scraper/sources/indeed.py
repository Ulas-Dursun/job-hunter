"""
IndeedSource — Indeed Turkey scraper.

Indeed'in arama sayfası JSON-LD JobPosting verileri içeriyor.
curl_cffi ile bot tespitini bypass ediyoruz.
"""

import json
import logging
import re
import time
import random
from datetime import datetime, timezone
from typing import Optional

from curl_cffi.requests import Session, BrowserType

from scraper.models.job import Job
from scraper.sources.base import BaseSource

logger = logging.getLogger(__name__)

_BASE_URL   = "https://tr.indeed.com"
_SEARCH_URL = f"{_BASE_URL}/jobs"


class IndeedSource(BaseSource):

    def __init__(self, source_config: dict):
        super().__init__("indeed")

    def fetch(self, keywords: list[str], location: str) -> list[Job]:
        all_jobs: list[Job] = []

        with Session(impersonate=BrowserType.chrome120) as session:
            # Warm-up
            try:
                session.get(_BASE_URL, timeout=15)
                time.sleep(random.uniform(1.5, 3.0))
            except Exception:
                pass

            for keyword in keywords:
                logger.info("fetching indeed  keyword=%r", keyword)
                try:
                    jobs = self._fetch_keyword(session, keyword, location)
                    all_jobs.extend(jobs)
                    logger.info(
                        "indeed result  keyword=%r  found=%d",
                        keyword, len(jobs),
                    )
                except Exception as exc:
                    logger.error(
                        "indeed error  keyword=%r  error=%s",
                        keyword, exc,
                    )
                time.sleep(random.uniform(3.0, 6.0))

        return all_jobs

    def _fetch_keyword(self, session: Session, keyword: str, location: str) -> list[Job]:
        params = {
            "q": keyword,
            "l": location,
        }
        try:
            resp = session.get(_SEARCH_URL, params=params, timeout=20)
            logger.debug("indeed  status=%d  url=%s", resp.status_code, resp.url)

            if resp.status_code != 200:
                logger.warning("indeed HTTP %d  keyword=%r", resp.status_code, keyword)
                return []

            return self._parse(resp.text)

        except Exception as exc:
            logger.warning("indeed request error  keyword=%r  %s", keyword, exc)
            return []

    def _parse(self, html: str) -> list[Job]:
        """
        Indeed HTML'inden ilanları parse eder.

        Indeed sayfası <script type="application/ld+json"> içinde
        JobPosting nesneleri barındırır.
        """
        jobs = self._parse_json_ld(html)
        if jobs:
            return jobs

        # Fallback: window.mosaic gömülü JSON
        jobs = self._parse_mosaic(html)
        return jobs

    def _parse_json_ld(self, html: str) -> list[Job]:
        """schema.org JSON-LD JobPosting parse eder."""
        jobs = []
        pattern = re.compile(
            r'<script[^>]+type=["\']application/ld\+json["\'][^>]*>(.*?)</script>',
            re.DOTALL | re.IGNORECASE,
            )

        for match in pattern.finditer(html):
            try:
                obj = json.loads(match.group(1).strip())

                if isinstance(obj, dict) and obj.get("@type") == "JobPosting":
                    job = self._jsonld_to_job(obj)
                    if job:
                        jobs.append(job)
                elif isinstance(obj, list):
                    for item in obj:
                        if isinstance(item, dict) and item.get("@type") == "JobPosting":
                            job = self._jsonld_to_job(item)
                            if job:
                                jobs.append(job)
            except (json.JSONDecodeError, Exception):
                continue

        logger.debug("JSON-LD found %d jobs", len(jobs))
        return jobs

    def _parse_mosaic(self, html: str) -> list[Job]:
        """Indeed'in window.mosaic gömülü JSON'unu parse eder."""
        jobs = []

        match = re.search(
            r'window\.mosaic\.providerData\["mosaic-provider-jobcards"\]\s*=\s*({.*?});',
            html,
            re.DOTALL,
        )
        if not match:
            return []

        try:
            data     = json.loads(match.group(1))
            results  = data.get("metaData", {}).get("mosaicProviderJobCardsModel", {})
            postings = results.get("results", [])

            for posting in postings:
                job = self._mosaic_to_job(posting)
                if job:
                    jobs.append(job)

            logger.debug("mosaic found %d jobs", len(jobs))
        except Exception as exc:
            logger.warning("mosaic parse error: %s", exc)

        return jobs

    def _jsonld_to_job(self, obj: dict) -> Optional[Job]:
        title = obj.get("title", "").strip()
        url   = obj.get("url") or obj.get("@id") or ""
        if not title or not url:
            return None

        company = ""
        org = obj.get("hiringOrganization", {})
        if isinstance(org, dict):
            company = org.get("name", "").strip()

        external_id = url.rstrip("/").split("/")[-1] or url

        location = "İstanbul"
        loc = obj.get("jobLocation", {})
        if isinstance(loc, dict):
            addr = loc.get("address", {})
            if isinstance(addr, dict):
                location = addr.get("addressLocality", "İstanbul")

        return Job(
            source="indeed",
            external_id=external_id,
            url=url if url.startswith("http") else f"{_BASE_URL}{url}",
            title=title,
            company=company or "Bilinmiyor",
            location=location,
            description=obj.get("description", "")[:2000],
        )

    def _mosaic_to_job(self, posting: dict) -> Optional[Job]:
        job_id  = posting.get("jobkey") or posting.get("mosobjId") or ""
        title   = (posting.get("title") or "").strip()
        company = (posting.get("company") or "").strip()

        if not job_id or not title:
            return None

        return Job(
            source="indeed",
            external_id=job_id,
            url=f"{_BASE_URL}/rc/clk?jk={job_id}",
            title=title,
            company=company or "Bilinmiyor",
            location=posting.get("formattedLocation") or "İstanbul",
        )

    def close(self) -> None:
        pass