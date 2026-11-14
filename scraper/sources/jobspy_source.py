"""
JobSpySource — python-jobspy üzerinden LinkedIn + Indeed scraper.

python-jobspy anti-bot bypass dahil, aktif olarak maintain edilen
bir scraping kütüphanesidir. LinkedIn ve Indeed'i handle eder.
"""

import hashlib
import logging
import random
import time
from typing import Callable, Optional
from urllib.parse import parse_qs, urlparse

from jobspy import scrape_jobs

from scraper.models.job import Job
from scraper.sources.base import BaseSource
from scraper.sources.filters import has_word

logger = logging.getLogger(__name__)

# Go tarafındaki prompt (truncateUTF8 = 4000 rune) ve Job.to_dict() (4000) ile uyumlu.
MAX_DESCRIPTION_CHARS = 4000

_INDEED_COUNTRY_MAP: list[tuple[tuple[str, ...], str]] = [
    (("turkey", "türkiye", "istanbul", "ankara", "izmir", "bursa", "kocaeli"), "Turkey"),
    (("belgium", "brussels", "bruxelles", "brüksel", "antwerp", "ghent"), "Belgium"),
    (("netherlands", "amsterdam", "rotterdam", "eindhoven", "utrecht", "hollanda"), "Netherlands"),
    (("germany", "deutschland", "berlin", "munich", "münchen", "frankfurt", "hamburg", "almanya"), "Germany"),
    (("denmark", "copenhagen", "københavn", "kobenhavn", "aarhus", "odense", "danimarka"), "Denmark"),
    (("switzerland", "zurich", "zürich", "geneva", "genève", "basel", "bern", "isviçre"), "Switzerland"),
    (("united kingdom", "uk", "england", "scotland", "london", "manchester", "edinburgh", "cambridge"), "UK"),
    (("sweden", "stockholm", "gothenburg", "malmö", "isveç"), "Sweden"),
    (("austria", "vienna", "wien", "graz", "avusturya"), "Austria"),
    (("ireland", "dublin", "cork", "irlanda"), "Ireland"),
    (("france", "paris", "lyon", "toulouse", "fransa"), "France"),
    (("poland", "warsaw", "krakow", "wroclaw", "polonya"), "Poland"),
    (("spain", "madrid", "barcelona", "ispanya"), "Spain"),
]


def resolve_indeed_country(location: str, default: str = "Turkey") -> str:
    """Hedef şehir/ülke metnini python-jobspy Country enum karşılığına eşler."""
    loc_lower = (location or "").lower()
    for markers, country_name in _INDEED_COUNTRY_MAP:
        if any(m in loc_lower for m in markers):
            return country_name
    # Remote/Europe/EU/EMEA aramaları varsayılan Turkey yerine AB merkezi (Netherlands) olarak çözümlenir
    if any(has_word(loc_lower, kw) for kw in ("remote", "europe", "eu", "emea")):
        return "Netherlands"
    return default


class JobSpySource(BaseSource):

    def __init__(self, source_config: dict):
        super().__init__("jobspy")
        self._max_age_days = source_config.get("max_age_days", 7)
        self._results_per_keyword = source_config.get("results_per_keyword", 20)
        self._country_indeed = source_config.get("country_indeed", "Turkey")
        self._locations = source_config.get("locations", [])
        self._secondary_keywords_limit = source_config.get("secondary_keywords_limit", 3)
        self._primary_locations = source_config.get(
            "primary_locations",
            [
                "Brussels, Belgium",
                "Leuven, Belgium",
                "Amsterdam, Netherlands",
                "Luxembourg",
                "Istanbul, Turkey",
                "Remote",
            ],
        )

    def _is_primary_location(self, loc: str) -> bool:
        """Birincil merkezlerde (Brüksel, Leuven, Amsterdam, Lüksemburg, İstanbul, Remote) tüm keywordler aranır."""
        loc_lower = (loc or "").lower()
        primary_markers = {"brussels", "leuven", "amsterdam", "luxembourg", "istanbul", "remote"}
        for pl in self._primary_locations:
            if not pl:
                continue
            pl_lower = pl.strip().lower()
            primary_markers.add(pl_lower)
            head = pl_lower.split(",")[0].strip()
            if head:
                primary_markers.add(head)
        return any(marker in loc_lower for marker in primary_markers)

    def fetch(
        self,
        keywords: list[str],
        location: str,
        on_batch: Optional[Callable[[list[Job]], None]] = None,
    ) -> list[Job]:
        all_jobs: list[Job] = []
        raw_locations = self._locations if self._locations else [location]
        # Türkiye lokasyonlarını en sona sırala: önce Brüksel/Benelüks/AB taransın
        target_locations = sorted(
            raw_locations,
            key=lambda loc: resolve_indeed_country(loc, self._country_indeed) == "Turkey",
        )

        for loc in target_locations:
            is_tr = resolve_indeed_country(loc, self._country_indeed) == "Turkey"
            loc_keywords = (
                keywords
                if self._is_primary_location(loc) or self._secondary_keywords_limit <= 0
                else keywords[: self._secondary_keywords_limit]
            )

            for keyword in loc_keywords:
                # Yurt dışı (örn: Brüksel, Kopenhag) aranırken Türkçe anahtar kelimeleri atlayarak API limitlerini koru
                if not is_tr and any(
                    tw in keyword.lower()
                    for tw in ["yazılım", "mühendis", "geliştirici", "yeni mezun", "genç yetenek", "uzman", "programı"]
                ):
                    continue

                logger.info("fetching jobspy  keyword=%r  location=%r", keyword, loc)
                try:
                    jobs = self._fetch_keyword(keyword, loc)
                    all_jobs.extend(jobs)
                    if jobs and on_batch is not None:
                        on_batch(jobs)
                    logger.info(
                        "jobspy result  keyword=%r  location=%r  found=%d",
                        keyword, loc, len(jobs),
                    )
                except Exception as exc:
                    logger.error(
                        "jobspy error  keyword=%r  location=%r  error=%s",
                        keyword, loc, exc,
                    )
                time.sleep(random.uniform(3.0, 5.0))

        return all_jobs

    def _fetch_keyword(self, keyword: str, location: str) -> list[Job]:
        country = resolve_indeed_country(location, self._country_indeed)

        try:
            df = scrape_jobs(
                site_name=["linkedin", "indeed"],
                search_term=keyword,
                location=location,
                results_wanted=self._results_per_keyword,
                hours_old=self._max_age_days * 24,
                country_indeed=country,
                linkedin_fetch_description=True,
                verbose=0,
            )
        except Exception as exc:
            logger.warning("scrape_jobs failed  keyword=%r  location=%r  error=%s", keyword, location, exc)
            return []

        if df is None or df.empty:
            logger.debug("empty dataframe  keyword=%r  location=%r", keyword, location)
            return []

        jobs = []
        for _, row in df.iterrows():
            job = self._row_to_job(row, fallback_location=location)
            if job:
                jobs.append(job)

        return jobs

    def _row_to_job(self, row, fallback_location: str = "Brussels, Belgium") -> Optional[Job]:
        """DataFrame row'unu Job nesnesine dönüştürür."""
        title   = str(row.get("title") or "").strip()
        company = str(row.get("company") or "").strip()
        url     = str(row.get("job_url") or "").strip()
        site    = str(row.get("site") or "jobspy").strip()

        if not title or not url or url == "nan":
            return None

        raw_id = str(row.get("id") or "").strip()
        if raw_id and raw_id != "nan":
            external_id = raw_id
        else:
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
            external_id = hashlib.sha256(url.encode()).hexdigest()[:20]

        location = str(row.get("location") or "").strip()
        if not location or location.lower() in ("nan", "none", "nat"):
            location = fallback_location

        description = str(row.get("description") or "").strip()
        if description == "nan":
            description = ""

        posted_at = None
        date_posted = row.get("date_posted")
        if date_posted is not None and str(date_posted).strip() not in ("nan", "NaT", "None", ""):
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
            description=description[:MAX_DESCRIPTION_CHARS],
            posted_at=posted_at,
        )

    def close(self) -> None:
        pass
