"""
KariyerNetSource — Kariyer.net Güncel Nuxt.js Scraper'ı.

Kariyer.net'in güncel SEO kategori sayfalarını (/is-ilanlari/yazilim-muhendisi,
/yazilim-gelistirme-uzmani, /devops-muhendisi, /veri-analisti, /veri-bilimci)
tarar; kart başlıklarını (_should_drop_title) ile henüz liste aşamasında süzerek
yalnızca aday profiliyle uyumlu ilanların detay sayfasını (.job-detail-qualifications) çeker.
"""

import logging
import re
import time
from typing import Optional

import requests
from bs4 import BeautifulSoup

from scraper.models.job import Job
from scraper.sources.base import BaseSource

logger = logging.getLogger("scraper.kariyer")

_BASE_URL = "https://www.kariyer.net"

_DEFAULT_SLUGS = [
    "yazilim-muhendisi",
    "yazilim-gelistirme-uzmani",
    "devops-muhendisi",
    "veri-analisti",
    "veri-bilimci",
]

_REJECT_TITLE_KEYWORDS = [
    "senior", "sr.", "sr ", "kıdemli", "kidemli", "başmühendis", "baş mühendis",
    "lead", "takım lideri", "ekip lideri", "manager", "müdür", "yönetici",
    "director", "direktör", "architect", "mimar", "principal", "staff", "head of",
    "stajyer", "staj", "intern", "internship",
    ".net", "c#", "php", "laravel", "wordpress", "drupal", "cobol", "abap", "sap ",
    "ios ", "android ", "flutter", "react native", "unity", "unreal",
    "qa ", "test uzman", "yazılım test", "manuel test",
    "satış", "sales", "pazarlama", "marketing", "muhasebe", "insan kaynakları",
    "destek uzman", "helpdesk", "çağrı merkezi", "grafik", "ui/ux",
    "elektrik", "mekanik", "inşaat", "makine mühendis", "endüstri mühendis",
    "disabled candidate", "engelli",
]


def _should_drop_kariyer_title(title: str) -> bool:
    low = title.lower().strip()
    if not low:
        return True
    for bad in _REJECT_TITLE_KEYWORDS:
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

    def fetch(self, keywords: list[str], location: str) -> list[Job]:
        jobs: list[Job] = []
        seen_ids: set[str] = set()
        details_fetched = 0

        for slug in self.slugs:
            list_url = f"{_BASE_URL}/is-ilanlari/{slug}"
            try:
                resp = self.session.get(list_url, timeout=12)
                if resp.status_code != 200:
                    logger.warning("kariyer_net non-200 status=%d url=%s", resp.status_code, list_url)
                    continue

                soup = BeautifulSoup(resp.text, "html.parser")
                cards = soup.select("a.k-ad-card")

                for card in cards:
                    href = (card.get("href") or "").strip()
                    if not href or "/is-ilani/" not in href:
                        continue

                    ext_id = self._extract_id(href)
                    if ext_id in seen_ids:
                        continue
                    seen_ids.add(ext_id)

                    title_el = card.select_one(".k-ad-card-title")
                    comp_el = card.select_one(".subtitle")
                    loc_el = card.select_one(".location")

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
            if resp.status_code != 200:
                return ""
            soup = BeautifulSoup(resp.text, "html.parser")
            node = (
                soup.select_one(".job-detail-qualifications")
                or soup.select_one(".job-detail-content")
                or soup.select_one(".job-sub-detail")
            )
            if not node:
                return ""
            text = " ".join(node.get_text(" ", strip=True).split())
            return text[:4000]
        except Exception:
            return ""

    @staticmethod
    def _extract_id(href: str) -> str:
        m = re.search(r"-(\d+)(?:[/?#]|$)", href)
        if m:
            return m.group(1)
        return href.rstrip("/").split("/")[-1]
