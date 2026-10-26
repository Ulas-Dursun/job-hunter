"""
KariyerNetSource — Kariyer.net Güncel Nuxt.js Scraper'ı.

Kariyer.net'in güncel SEO kategori sayfalarını (/is-ilanlari/yazilim-muhendisi,
/yazilim-gelistirme-uzmani, /devops-muhendisi, /veri-analisti, /veri-bilimci)
tarar; kart başlıklarını (_should_drop_kariyer_title) ile henüz liste aşamasında süzerek
yalnızca aday profiliyle uyumlu ilanların detay sayfasını (.job-detail-qualifications) çeker.
"""

import json
import logging
import re
import time

import requests
from bs4 import BeautifulSoup
from requests.adapters import HTTPAdapter
from urllib3.util.retry import Retry

from scraper.models.job import Job
from scraper.sources.base import BaseSource
from scraper.sources.filters import is_rejected_title

logger = logging.getLogger("scraper.kariyer")

_BASE_URL = "https://www.kariyer.net"

_DEFAULT_SLUGS = [
    "yazilim-muhendisi",
    "yazilim-gelistirme-uzmani",
    "devops-muhendisi",
    "veri-analisti",
    "veri-bilimci",
]

# Go CheapFilter ve scraper/sources/filters.py ile tam uyumlu ek Kariyer.net gürültü kelimeleri:
_KARIYER_EXTRA_REJECT = [
    ".net", "c#", "php", "laravel", "symfony", "wordpress", "drupal", "cobol", "abap",
    "flutter", "react native", "unity", "unreal", "ios developer", "android developer",
    "yazılım test", "manuel test", "test uzman", "qa ",
    "muhasebe", "insan kaynakları", "destek uzman", "helpdesk", "çağrı merkezi",
    "grafik", "ui/ux", "elektrik", "mekanik", "inşaat", "makine mühendis", "endüstri mühendis",
    "disabled candidate", "engelli", "başmühendis", "baş mühendis",
]


def _should_drop_kariyer_title(title: str) -> bool:
    """
    Başlığı önce ortak kelime-sınırlı is_rejected_title() ile, ardından Kariyer.net'e özgü
    stack/alan kara listesiyle kontrol eder. Go CheapFilter ile %100 uyumludur.
    """
    if not title or not title.strip():
        return True
    if is_rejected_title(title):
        return True
    low = title.lower().strip()
    for bad in _KARIYER_EXTRA_REJECT:
        if bad in low:
            return True
    return False


class KariyerNetSource(BaseSource):
    """Kariyer.net Nuxt.js kategori sayfalarından yazılım, veri ve bulut ilanlarını çeker."""

    def __init__(self, cfg: dict):
        super().__init__(name="kariyer_net")
        self.cfg = cfg
        self.slugs = cfg.get("category_slugs", _DEFAULT_SLUGS)
        self.max_details_per_cycle = int(cfg.get("max_details_per_cycle", 50))
        self.session = requests.Session()
        self.session.headers.update({
            "User-Agent": (
                "Mozilla/5.0 (Windows NT 10.0; Win64; x64) "
                "AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
            ),
            "Accept-Language": "tr-TR,tr;q=0.9,en-US;q=0.8,en;q=0.7",
        })
        retry_strategy = Retry(
            total=2,
            backoff_factor=0.6,
            status_forcelist=[500, 502, 503, 504],
            allowed_methods=["GET"],
        )
        adapter = HTTPAdapter(max_retries=retry_strategy)
        self.session.mount("https://", adapter)
        self.session.mount("http://", adapter)

    def fetch(self, keywords: list[str], location: str) -> list[Job]:
        jobs: list[Job] = []
        seen_ids: set[str] = set()
        details_fetched = 0

        for slug in self.slugs:
            if details_fetched >= self.max_details_per_cycle:
                break

            list_url = f"{_BASE_URL}/is-ilanlari/{slug}"
            try:
                resp = self.session.get(list_url, timeout=12)
                if resp.status_code in (429, 403):
                    logger.warning(
                        "kariyer_net rate-limited or blocked (status=%d) on slug=%s — backing off",
                        resp.status_code, slug,
                    )
                    time.sleep(2.0)
                    continue
                if resp.status_code != 200:
                    logger.warning("kariyer_net non-200 status=%d url=%s", resp.status_code, list_url)
                    continue

                soup = BeautifulSoup(resp.text, "html.parser")
                cards = soup.select("a.k-ad-card")
                if not cards:
                    # Nuxt DOM sınıfı değişirse yedek seçicileri dene
                    cards = soup.select('a[data-test="ad-card-item"], a[href*="/is-ilani/"]')
                if not cards:
                    logger.warning(
                        "kariyer_net DOM selector returned 0 cards for slug=%s — page structure may have changed",
                        slug,
                    )
                    continue

                for card in cards:
                    href = (card.get("href") or "").strip()
                    if not href or "/is-ilani/" not in href:
                        continue

                    ext_id = self._extract_id(href)
                    if ext_id in seen_ids:
                        continue
                    seen_ids.add(ext_id)

                    title_el = card.select_one('.k-ad-card-title, [data-test="ad-card-title"]')
                    comp_el = card.select_one('.subtitle, [data-test="subtitle"]')
                    loc_el = card.select_one('.location, [data-test="location"]')

                    title = " ".join(title_el.get_text(" ", strip=True).split()) if title_el else ""
                    company = " ".join(comp_el.get_text(" ", strip=True).split()) if comp_el else "Kariyer.net İşveren"
                    loc_text = " ".join(loc_el.get_text(" ", strip=True).split()) if loc_el else (location or "İstanbul, Turkey")

                    if not title or _should_drop_kariyer_title(title):
                        continue

                    if details_fetched >= self.max_details_per_cycle:
                        break

                    full_url = href if href.startswith("http") else f"{_BASE_URL}{href}"
                    description = self._fetch_detail_description(full_url)
                    details_fetched += 1
                    time.sleep(0.15)

                    if not description:
                        continue

                    jobs.append(
                        Job(
                            source="kariyer_net",
                            external_id=f"kariyer_{ext_id}",
                            url=full_url,
                            title=title,
                            company=company,
                            location=loc_text,
                            description=description,
                        )
                    )
            except Exception as exc:
                logger.warning("kariyer_net fetch failed for slug %s: %s", slug, exc)

        logger.info("kariyer_net fetched %d pre-filtered jobs (%d detail pages)", len(jobs), details_fetched)
        return jobs

    def _fetch_detail_description(self, url: str) -> str:
        try:
            resp = self.session.get(url, timeout=10)
            if resp.status_code in (429, 403):
                logger.warning("kariyer_net detail rate-limited/blocked status=%d url=%s", resp.status_code, url)
                return ""
            if resp.status_code != 200:
                logger.debug("kariyer_net detail non-200 status=%d url=%s", resp.status_code, url)
                return ""

            soup = BeautifulSoup(resp.text, "html.parser")
            node = (
                soup.select_one(".job-detail-qualifications")
                or soup.select_one(".job-detail-content")
                or soup.select_one(".job-sub-detail")
                or soup.select_one('[data-test="qualifications"]')
                or soup.select_one(".aligment-container")
            )
            if node:
                text = " ".join(node.get_text(" ", strip=True).split())
                if len(text) >= 40:
                    return text[:4000]

            # Yedek: schema.org JobPosting JSON-LD
            for ld in soup.find_all("script", {"type": "application/ld+json"}):
                if not ld.string:
                    continue
                try:
                    obj = json.loads(ld.string)
                    if isinstance(obj, dict) and obj.get("@type") == "JobPosting":
                        desc = obj.get("description", "")
                        if desc:
                            clean = " ".join(BeautifulSoup(desc, "html.parser").get_text(" ", strip=True).split())
                            if clean:
                                return clean[:4000]
                except Exception:
                    continue

            logger.debug("kariyer_net detail description selector matched empty for url=%s", url)
            return ""
        except Exception as exc:
            logger.debug("kariyer_net detail fetch error url=%s: %s", url, exc)
            return ""

    @staticmethod
    def _extract_id(href: str) -> str:
        m = re.search(r"-(\d+)(?:[/?#]|$)", href)
        if m:
            return m.group(1)
        return href.rstrip("/").split("/")[-1]
