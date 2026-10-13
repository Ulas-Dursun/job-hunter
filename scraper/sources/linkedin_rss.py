"""
LinkedInRSSSource — LinkedIn RSS feed scraper.

LinkedIn'in public RSS endpoint'i auth gerektirmez.
URL: https://www.linkedin.com/jobs/search/?keywords=...&location=Istanbul
RSS: https://www.linkedin.com/jobs/search.rss?keywords=...&location=Istanbul

RSS feed'i doğrudan httpx ile çekip XML parse ediyoruz.
"""

import logging
import time
import random
import re
from datetime import datetime, timezone
from typing import Optional
from urllib.parse import urlencode

import httpx

from scraper.models.job import Job
from scraper.sources.base import BaseSource

logger = logging.getLogger(__name__)

_RSS_URL = "https://www.linkedin.com/jobs/search.rss"

_HEADERS = {
    "User-Agent": (
        "Mozilla/5.0 (Windows NT 10.0; Win64; x64) "
        "AppleWebKit/537.36 (KHTML, like Gecko) "
        "Chrome/120.0.0.0 Safari/537.36"
    ),
    "Accept": "application/rss+xml, application/xml, text/xml, */*",
    "Accept-Language": "tr-TR,tr;q=0.9,en;q=0.8",
}


class LinkedInRSSSource(BaseSource):

    def __init__(self, source_config: dict):
        super().__init__("linkedin_rss")
        self._client = httpx.Client(
            headers=_HEADERS,
            timeout=15.0,
            follow_redirects=True,
        )

    def fetch(self, keywords: list[str], location: str) -> list[Job]:
        all_jobs: list[Job] = []

        for keyword in keywords:
            logger.info("fetching linkedin rss  keyword=%r", keyword)
            try:
                jobs = self._fetch_keyword(keyword, location)
                all_jobs.extend(jobs)
                logger.info(
                    "linkedin rss result  keyword=%r  found=%d",
                    keyword, len(jobs),
                )
            except Exception as exc:
                logger.error(
                    "linkedin rss error  keyword=%r  error=%s",
                    keyword, exc,
                )
            time.sleep(random.uniform(2.0, 4.0))

        return all_jobs

    def _fetch_keyword(self, keyword: str, location: str) -> list[Job]:
        params = {
            "keywords": keyword,
            "location": f"{location}, Turkey",
            "f_TPR":    "r604800",   # Son 7 gün
            "sortBy":   "DD",        # Tarihe göre
        }

        try:
            resp = self._client.get(_RSS_URL, params=params)
            logger.debug(
                "linkedin rss  status=%d  url=%s",
                resp.status_code, resp.url,
            )

            if resp.status_code != 200:
                logger.warning(
                    "linkedin rss HTTP %d  keyword=%r",
                    resp.status_code, keyword,
                )
                return []

            return self._parse_rss(resp.text)

        except Exception as exc:
            logger.warning("linkedin rss request error: %s", exc)
            return []

    def _parse_rss(self, xml: str) -> list[Job]:
        """
        RSS XML'den iş ilanlarını parse eder.
        Standart RSS 2.0 formatı — her <item> bir ilan.
        """
        jobs = []

        # XML parser yerine regex — lxml bağımlılığından kaçınmak için
        # RSS item'ları <item>...</item> arasında
        items = re.findall(r"<item>(.*?)</item>", xml, re.DOTALL)

        for item in items:
            job = self._item_to_job(item)
            if job:
                jobs.append(job)

        return jobs

    def _item_to_job(self, item_xml: str) -> Optional[Job]:
        """Tek RSS item XML'ini Job nesnesine dönüştürür."""

        title   = self._extract_tag(item_xml, "title")
        link    = self._extract_tag(item_xml, "link")
        pub_date = self._extract_tag(item_xml, "pubDate")

        if not title or not link:
            return None

        # LinkedIn title formatı: "Backend Developer at Trendyol"
        company = ""
        if " at " in title:
            parts   = title.rsplit(" at ", 1)
            title   = parts[0].strip()
            company = parts[1].strip()

        # URL'den external_id üret
        # LinkedIn URL formatı: /jobs/view/1234567890/
        id_match = re.search(r"/jobs/view/(\d+)", link)
        external_id = id_match.group(1) if id_match else link

        posted_at = self._parse_rss_date(pub_date)

        return Job(
            source="linkedin_rss",
            external_id=external_id,
            url=link,
            title=title,
            company=company or "Bilinmiyor",
            location="İstanbul",
            posted_at=posted_at.isoformat() if posted_at else None,
        )

    def _extract_tag(self, xml: str, tag: str) -> str:
        """XML tag içeriğini çıkarır. CDATA destekler."""
        # CDATA: <title><![CDATA[...]]></title>
        match = re.search(
            rf"<{tag}[^>]*>\s*(?:<!\[CDATA\[)?(.*?)(?:\]\]>)?\s*</{tag}>",
            xml,
            re.DOTALL,
        )
        return match.group(1).strip() if match else ""

    def _parse_rss_date(self, date_str: str) -> Optional[datetime]:
        """RSS date formatını parse eder: 'Mon, 01 Jan 2024 00:00:00 GMT'"""
        if not date_str:
            return None
        try:
            from email.utils import parsedate_to_datetime
            return parsedate_to_datetime(date_str)
        except Exception:
            return None

    def close(self) -> None:
        self._client.close()