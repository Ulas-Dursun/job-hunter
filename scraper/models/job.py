"""
Job — Pipeline'ın tek veri modeli.

Tüm scraper'lar (KariyerNet, LinkedIn, Indeed) bu modeli üretir.
Go orchestrator Redis'ten bu alanları okur.
Farklı kaynaklardan gelen verilerin normalizasyonu burada olur.
"""

import hashlib
from dataclasses import dataclass, field
from datetime import datetime, timezone
from typing import Optional
from urllib.parse import urlparse, urlunparse


@dataclass
class Job:
    # ── Zorunlu alanlar ──────────────────────────────────────────────────────
    source:      str   # "kariyer_net" | "linkedin" | "indeed"
    external_id: str   # Kaynağın kendi benzersiz ID'si
    url:         str   # İlana direkt link
    title:       str   # "Backend Developer"
    company:     str   # "Trendyol"

    # ── Opsiyonel alanlar ────────────────────────────────────────────────────
    location:    str            = ""
    description: str            = ""
    posted_at:   Optional[str]  = None  # ISO 8601 — kaynak sağlıyorsa

    # ── Otomatik alanlar ─────────────────────────────────────────────────────
    scraped_at: str = field(
        default_factory=lambda: datetime.now(timezone.utc).isoformat()
    )

    def dedup_hash(self) -> str:
        """
        URL bazlı 16-karakter deduplication hash'i.
        LinkedIn/Indeed URL'lerindeki tracking query parametreleri (utm_*, refId,
        trackingId, currentJobId vb.) temizlenir — aksi halde aynı ilan farklı
        keyword aramalarında farklı hash üretir ve dedup'tan kaçar.
        """
        parsed = urlparse(self.url)
        # Query string'i tamamen at — LinkedIn/Indeed'de path zaten ilanı tekil tanımlar
        clean_url = urlunparse((parsed.scheme, parsed.netloc, parsed.path, '', '', ''))

        return hashlib.sha256(clean_url.encode("utf-8")).hexdigest()[:16]

    def to_redis_fields(self) -> dict[str, str]:
        """
        Redis XADD için flat string dict üretir.
        Redis Streams tüm değerleri string olarak saklar.
        Description 2000 karakterle sınırlandırılır — tam metin Go'da işlenecek.
        """
        return {
            "source":       self.source,
            "external_id":  self.external_id,
            "url":          self.url,
            "title":        self.title,
            "company":      self.company,
            "location":     self.location,
            "description":  self.description[:2000],
            "posted_at":    self.posted_at or "",
            "scraped_at":   self.scraped_at,
            "dedup_hash":   self.dedup_hash(),
        }

    def __repr__(self) -> str:
        return f"Job(source={self.source!r}, company={self.company!r}, title={self.title!r})"