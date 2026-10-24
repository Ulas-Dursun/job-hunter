"""
CoderspaceSource — coderspace.io Aktif Genç Yetenek, Hiring Challenge ve Bootcamp Çekicisi.

Yalnızca başvuruya açık (aktif) teknoloji programlarını çeker; 'Geçmiş etkinlikler'
bölümündeki veya 'Başvurular Tamamlandı' durumundaki etkinlikleri eler.
"""

import logging
import re
import requests
from bs4 import BeautifulSoup

from scraper.models.job import Job
from scraper.sources.base import BaseSource
from scraper.sources.filters import is_tech_title

logger = logging.getLogger("scraper.coderspace")

_CATEGORY_SLUGS = {
    "/etkinlikler/",
    "/etkinlikler/bootcamp/",
    "/etkinlikler/hackathon/",
    "/etkinlikler/hiring-challenge/",
    "/etkinlikler/meet-up/",
    "/etkinlikler/yetenek-programi/",
    "/etkinlikler/web3/",
}


class CoderspaceSource(BaseSource):
    BASE_URL = "https://coderspace.io"
    LIST_URLS = [
        "https://coderspace.io/etkinlikler/yetenek-programi/",
        "https://coderspace.io/etkinlikler/hiring-challenge/",
        "https://coderspace.io/etkinlikler/bootcamp/",
    ]
    _NOISE_WORDS = ("zirve", "zirvesi", "panel", "paneli", "summit", "meet-up", "meetup", "webinar")

    def __init__(self, cfg: dict):
        super().__init__(name="coderspace")
        self.cfg = cfg
        self.session = requests.Session()
        self.session.headers.update({
            "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
            "Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
        })

    def fetch(self, keywords: list[str], location: str) -> list[Job]:
        logger.info("fetching coderspace.io active programs...")
        jobs: list[Job] = []
        seen_slugs: set[str] = set()

        for list_url in self.LIST_URLS:
            try:
                r = self.session.get(list_url, timeout=12)
                if r.status_code != 200:
                    continue

                soup = BeautifulSoup(r.text, "html.parser")
                # 'Geçmiş etkinlikler' bölümünü tamamen çıkar ki kapanmış ilanlar taranmasın.
                for sec in soup.find_all("section"):
                    h = sec.find(["h1", "h2", "h3"])
                    if h and "geçmiş" in h.get_text(strip=True).lower():
                        sec.decompose()

                for a in soup.find_all("a", href=True):
                    href = a.get("href", "").strip()
                    if not href.startswith("/etkinlikler/") or href in _CATEGORY_SLUGS or href.count("/") != 3:
                        continue
                    slug = href.strip("/").split("/")[-1]
                    if not slug or slug in seen_slugs:
                        continue
                    seen_slugs.add(slug)

                    job = self._fetch_program_detail(slug, f"{self.BASE_URL}{href}")
                    if job:
                        jobs.append(job)
            except Exception as e:
                logger.warning("coderspace list error url=%s err=%s", list_url, e)

        logger.info("coderspace fetched %d active tech programs", len(jobs))
        return jobs

    def _fetch_program_detail(self, slug: str, url: str) -> Job | None:
        try:
            r = self.session.get(url, timeout=10)
            if r.status_code != 200 or "Başvurular Tamamlandı" in r.text:
                return None

            soup = BeautifulSoup(r.text, "html.parser")
            h1 = soup.find("h1")
            title = h1.get_text(" ", strip=True) if h1 else (soup.title.get_text(" ", strip=True) if soup.title else slug)
            if not title or any(w in title.lower() for w in self._NOISE_WORDS):
                return None

            main = soup.find("main") or soup.body
            desc = re.sub(r"\n{3,}", "\n\n", main.get_text("\n", strip=True)) if main else ""
            desc = desc[:4000]

            # Genç Yetenek / Hiring Challenge başlıklarında 'Genç Yetenek' sinyali doğrudan yoksa bile
            # yazılım/veri/teknoloji odaklıysa yumuşak filtre ile geçir.
            if not is_tech_title(f"{title} Genç Yetenek", desc):
                return None

            company = self._guess_company(title)
            normalized_title = title if "genç yetenek" in title.lower() or "graduate" in title.lower() or "developer" in title.lower() or "engineer" in title.lower() else f"{title} (Genç Yetenek Programı)"
            return Job(
                source="coderspace",
                external_id=f"cs:{slug}",
                url=url,
                title=normalized_title,
                company=company,
                location="Istanbul, Turkey",
                description=desc,
            )
        except Exception:
            return None

    @staticmethod
    def _guess_company(title: str) -> str:
        for sep in (" - ", " | ", ": "):
            if sep in title:
                first = title.split(sep, 1)[0].strip()
                if 2 <= len(first) <= 40:
                    return first
        words = title.split()
        if len(words) >= 2:
            return " ".join(words[:2])
        return "Coderspace Partner"
