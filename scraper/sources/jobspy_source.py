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
        self._country_indeed = source_config.get("country_indeed", "Turkey")

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
        # Lokasyona göre Indeed ülke kodunu belirle
        loc_lower = (location or "").lower()
        if any(w in loc_lower for w in ["belgium", "brussels", "bruxelles", "brüksel"]):
            country = "Belgium"
        elif any(w in loc_lower for w in ["germany", "berlin", "munich", "almanya"]):
            country = "Germany"
        elif any(w in loc_lower for w in ["netherlands", "amsterdam", "hollanda"]):
            country = "Netherlands"
        else:
            country = self._country_indeed

        try:
            # scrape_jobs pandas DataFrame döndürür
            df = scrape_jobs(
                site_name=["linkedin", "indeed"],
                search_term=keyword,
                location=location,
                results_wanted=self._results_per_keyword,
                hours_old=self._max_age_days * 24,
                country_indeed=country,
                linkedin_fetch_description=True,  # 🟢 LinkedIn tam ilan açıklamasını çek
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

        # external_id: jobspy'ın ürettiği id varsa onu kullan (örn: in-xxx, li-xxx)
        raw_id = str(row.get("id") or "").strip()
        if raw_id and raw_id != "nan":
            external_id = raw_id
        else:
            from urllib.parse import urlparse, parse_qs
            parsed = urlparse(url)
            qs = parse_qs(parsed.query)
            jk = qs.get("jk", [None])[0] or qs.get("vjk", [None])[0]
            if jk:
                external_id = f"in-{jk}"
            elif "linkedin" in url:
                path_parts = [p for p in parsed.path.split("/") if p]
                external_id = f"li-{path_parts[-1]}" if path_parts else url
            else:
                external_id = url.rstrip("/").split("/")[-1] or url

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