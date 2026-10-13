"""
KariyerNetSource — curl_cffi tabanlı scraper.

Kariyer.net PerimeterX bot koruması kullanıyor.
curl_cffi Chrome'un TLS fingerprint'ini birebir taklit eder.
Bu yöntem PerimeterX'i bypass eder.
"""

import json
import logging
import random
import re
import time
from datetime import datetime, timezone, timedelta
from typing import Optional

from curl_cffi.requests import Session, BrowserType

from scraper.models.job import Job
from scraper.sources.base import BaseSource

logger = logging.getLogger(__name__)

_BASE_URL = "https://www.kariyer.net"


class KariyerNetSource(BaseSource):

    def __init__(self, source_config: dict):
        super().__init__("kariyer_net")
        self._max_age_days = source_config.get("max_age_days", 7)

    def fetch(self, keywords: list[str], location: str) -> list[Job]:
        all_jobs: list[Job] = []

        # Session tüm keyword'ler için paylaşılır — cookie state korunur
        with Session(impersonate=BrowserType.chrome120) as session:

            # Önce ana sayfayı ziyaret et — cookie al
            self._warm_up(session)

            for keyword in keywords:
                logger.info("fetching kariyer.net  keyword=%r", keyword)
                try:
                    jobs = self._fetch_keyword(session, keyword)
                    all_jobs.extend(jobs)
                    logger.info(
                        "kariyer.net result  keyword=%r  found=%d",
                        keyword, len(jobs),
                    )
                except Exception as exc:
                    logger.error(
                        "kariyer.net error  keyword=%r  error=%s",
                        keyword, exc,
                    )
                self._polite_delay()

        return all_jobs

    def _warm_up(self, session: Session) -> None:
        """
        Ana sayfayı ziyaret ederek PerimeterX cookie'sini alır.
        Bu olmadan search sayfası 403 verir.
        """
        try:
            resp = session.get(_BASE_URL, timeout=15)
            logger.debug("warm-up  status=%d  cookies=%s",
                         resp.status_code, list(resp.cookies.keys()))
            time.sleep(random.uniform(1.5, 3.0))
        except Exception as exc:
            logger.warning("warm-up failed: %s", exc)

    def _fetch_keyword(self, session: Session, keyword: str) -> list[Job]:
        jobs: list[Job] = []
        for page in range(1, 3):
            page_jobs = self._fetch_page(session, keyword, page)
            if not page_jobs:
                break
            jobs.extend(page_jobs)
        return jobs

    def _fetch_page(self, session: Session, keyword: str, page: int) -> list[Job]:
        url = f"{_BASE_URL}/is-ilanlari/ara"
        params = {
            "q":        keyword,
            "sehirler": "39",   # İstanbul
            "st":       "1",    # Tarihe göre sırala
            "sayfa":    str(page),
        }

        try:
            resp = session.get(url, params=params, timeout=20)
            logger.debug("search  status=%d  url=%s", resp.status_code, resp.url)

            if resp.status_code == 403:
                logger.warning("403 on page %d  keyword=%r", page, keyword)
                return []

            if resp.status_code != 200:
                logger.warning("HTTP %d  keyword=%r", resp.status_code, keyword)
                return []

            return self._parse(resp.text, keyword)

        except Exception as exc:
            logger.warning("fetch error  keyword=%r  %s", keyword, exc)
            return []

    def _parse(self, html: str, keyword: str) -> list[Job]:
        """
        HTML'den iş ilanlarını parse eder.

        Kariyer.net Next.js kullanıyor — __NEXT_DATA__ script tag'ine
        tüm sayfa verisi JSON olarak gömülü geliyor.
        Bu yöntem JavaScript render gerektirmez.
        """
        jobs = self._parse_next_data(html)
        if jobs:
            return jobs

        jobs = self._parse_json_ld(html)
        if jobs:
            return jobs

        logger.warning("no jobs parsed from HTML  keyword=%r", keyword)
        return []

    def _parse_next_data(self, html: str) -> list[Job]:
        """
        Next.js __NEXT_DATA__ JSON'undan ilanları çıkarır.
        Kariyer.net Next.js kullandığı için bu yöntem en güvenilir.
        """
        match = re.search(
            r'<script id="__NEXT_DATA__"[^>]*>(.*?)</script>',
            html,
            re.DOTALL,
        )
        if not match:
            logger.debug("__NEXT_DATA__ not found in HTML")
            return []

        try:
            data = json.loads(match.group(1))
        except json.JSONDecodeError as exc:
            logger.warning("__NEXT_DATA__ JSON parse error: %s", exc)
            return []

        # Next.js page props içinde ilanları bul
        # Farklı sayfa versiyonlarında farklı path'te olabilir
        postings = (
                self._deep_get(data, "props", "pageProps", "jobPostings") or
                self._deep_get(data, "props", "pageProps", "jobs") or
                self._deep_get(data, "props", "pageProps", "data", "jobPostings") or
                self._deep_get(data, "props", "pageProps", "initialData", "jobPostings") or
                []
        )

        if not postings:
            # Tüm key'leri logla — debug için
            all_keys = self._find_keys_with_list(data)
            logger.debug("__NEXT_DATA__ found but no postings. List keys: %s", all_keys[:10])

        jobs = []
        for posting in postings:
            job = self._to_job(posting)
            if job:
                jobs.append(job)

        logger.debug("__NEXT_DATA__ parse: found %d jobs", len(jobs))
        return jobs

    def _parse_json_ld(self, html: str) -> list[Job]:
        """Fallback: schema.org JSON-LD JobPosting verisi."""
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

        logger.debug("JSON-LD parse: found %d jobs", len(jobs))
        return jobs

    def _to_job(self, posting: dict) -> Optional[Job]:
        """API/Next.js posting dict → Job nesnesi."""
        job_id  = str(posting.get("id") or posting.get("jobPostingId") or "")
        title   = (posting.get("jobPositionName") or posting.get("title") or "").strip()
        company = (posting.get("companyName") or posting.get("company") or "").strip()

        if not job_id or not title or not company:
            return None

        published_at = self._parse_date(
            posting.get("publishedDate") or posting.get("publishDate") or ""
        )
        if published_at and self._is_too_old(published_at):
            return None

        seo_url = posting.get("seoFriendlyUrl") or posting.get("urlName") or ""
        url = (
            f"{_BASE_URL}/is-ilani/{seo_url}"
            if seo_url
            else f"{_BASE_URL}/is-ilani/{job_id}"
        )

        return Job(
            source="kariyer_net",
            external_id=job_id,
            url=url,
            title=title,
            company=company,
            location=posting.get("cityName") or "İstanbul",
            description=(posting.get("jobDescription") or "")[:2000],
            posted_at=published_at.isoformat() if published_at else None,
        )

    def _jsonld_to_job(self, obj: dict) -> Optional[Job]:
        """schema.org JobPosting → Job nesnesi."""
        title = obj.get("title", "").strip()
        url   = obj.get("url") or obj.get("@id") or ""

        if not title or not url:
            return None

        company = ""
        org = obj.get("hiringOrganization", {})
        if isinstance(org, dict):
            company = org.get("name", "").strip()

        external_id = url.rstrip("/").split("/")[-1] or url
        posted_at   = self._parse_date(obj.get("datePosted", ""))

        if posted_at and self._is_too_old(posted_at):
            return None

        location = "İstanbul"
        loc = obj.get("jobLocation", {})
        if isinstance(loc, dict):
            addr = loc.get("address", {})
            if isinstance(addr, dict):
                location = addr.get("addressLocality", "İstanbul")

        return Job(
            source="kariyer_net",
            external_id=external_id,
            url=url,
            title=title,
            company=company or "Bilinmiyor",
            location=location,
            description=obj.get("description", "")[:2000],
            posted_at=posted_at.isoformat() if posted_at else None,
        )

    # ── Yardımcı metodlar ─────────────────────────────────────────────────────

    def _deep_get(self, obj: dict, *keys):
        """Nested dict'ten güvenli değer okur."""
        for key in keys:
            if not isinstance(obj, dict):
                return None
            obj = obj.get(key)
        return obj

    def _find_keys_with_list(self, obj, prefix="", depth=0) -> list[str]:
        """__NEXT_DATA__ içinde list değer içeren tüm key path'lerini bulur."""
        if depth > 5:
            return []
        results = []
        if isinstance(obj, dict):
            for k, v in obj.items():
                path = f"{prefix}.{k}" if prefix else k
                if isinstance(v, list) and len(v) > 0:
                    results.append(f"{path}[{len(v)}]")
                elif isinstance(v, dict):
                    results.extend(self._find_keys_with_list(v, path, depth + 1))
        return results

    def _parse_date(self, date_str: str) -> Optional[datetime]:
        if not date_str:
            return None
        for fmt in ("%Y-%m-%dT%H:%M:%S", "%Y-%m-%dT%H:%M:%SZ", "%Y-%m-%d", "%d.%m.%Y"):
            try:
                return datetime.strptime(date_str[:19], fmt).replace(tzinfo=timezone.utc)
            except ValueError:
                continue
        return None

    def _is_too_old(self, posted_at: datetime) -> bool:
        return posted_at < datetime.now(timezone.utc) - timedelta(days=self._max_age_days)

    def _polite_delay(self) -> None:
        time.sleep(random.uniform(2.0, 4.0))

    def close(self) -> None:
        pass