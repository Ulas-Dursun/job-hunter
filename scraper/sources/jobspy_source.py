"""
JobSpySource — python-jobspy üzerinden LinkedIn + Indeed scraper.

python-jobspy anti-bot bypass dahil, aktif olarak maintain edilen
bir scraping kütüphanesidir. LinkedIn ve Indeed'i handle eder.
"""

import logging
import time
import random
from datetime import datetime, timezone, timedelta
from typing import Optional

from jobspy import scrape_jobs

from scraper.models.job import Job
from scraper.sources.base import BaseSource

logger = logging.getLogger(__name__)


class JobSpySource(BaseSource):

    def __init__(self, source_config: dict):
        super().__init__("jobspy")
        self._max_age_days = source_config.get("max_age_days", 7)
        self._results_per_keyword = source_config.get("results_per_keyword", 20)

    def fetch(self, keywords: list[str], location: str) -> list[Job]:
        all_jobs: list[Job] = []

        for keyword in keywords:
            logger.info("fetching jobspy  keyword=%r  location=%r", keyword, location)
            try:
                jobs = self._fetch_keyword(keyword, location)
                all_jobs.extend(jobs)
                logger.info(
                    "jobspy result  keyword=%r  found=%d",
                    keyword, len(jobs),
                )
            except Exception as exc:
                logger.error(
                    "jobspy error  keyword=%r  error=%s",
                    keyword, exc,
                )
            time.sleep(random.uniform(3.0, 6.0))

        return all_jobs

    def _fetch_keyword(self, keyword: str, location: str) -> list[Job]:
        try:
            # scrape_jobs pandas DataFrame döndürür
            df = scrape_jobs(
                site_name=["linkedin", "indeed"],
                search_term=keyword,
                location=location,
                results_wanted=self._results_per_keyword,
                hours_old=self._max_age_days * 24,
                country_indeed="Turkey",
                verbose=0,  # Log spam'ı kapat
            )
        except Exception as exc:
            logger.warning("scrape_jobs failed  keyword=%r  error=%s", keyword, exc)
            return []

        if df is None or df.empty:
            logger.debug("empty dataframe  keyword=%r", keyword)
            return []

        jobs = []
        for _, row in df.iterrows():
            job = self._row_to_job(row)
            if job:
                jobs.append(job)

        return jobs

    def _row_to_job(self, row) -> Optional[Job]:
        """DataFrame row'unu Job nesnesine dönüştürür."""

        title   = str(row.get("title") or "").strip()
        company = str(row.get("company") or "").strip()
        url     = str(row.get("job_url") or "").strip()
        site    = str(row.get("site") or "jobspy").strip()

        if not title or not url or url == "nan":
            return None

        # external_id: URL'den üret
        external_id = url.rstrip("/").split("/")[-1] or url
        # Çok uzun ise hash al
        if len(external_id) > 200:
            import hashlib
            external_id = hashlib.sha256(url.encode()).hexdigest()[:20]

        # Konum
        location = str(row.get("location") or "İstanbul").strip()
        if location == "nan":
            location = "İstanbul"

        # Açıklama
        description = str(row.get("description") or "").strip()
        if description == "nan":
            description = ""

        # Yayınlanma tarihi
        posted_at = None
        date_posted = row.get("date_posted")
        if date_posted and str(date_posted) != "nan":
            try:
                if hasattr(date_posted, "isoformat"):
                    posted_at = date_posted.isoformat()
                else:
                    posted_at = str(date_posted)
            except Exception:
                pass

        return Job(
            source=site,
            external_id=external_id,
            url=url,
            title=title,
            company=company or "Bilinmiyor",
            location=location,
            description=description[:2000],
            posted_at=posted_at,
        )

    def close(self) -> None:
        pass