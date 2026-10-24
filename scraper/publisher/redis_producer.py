"""
RedisProducer — Scraper'dan Redis Stream'e köprü.

Tek sorumluluğu: Job nesnesini alıp jobs:raw stream'ine yayınlamak.
Deduplication burada yapılır:
  1) URL/ID bazlı  : "<dedup_key>:<hash>"       (TTL'li, dedup_ttl_days)
  2) İçerik bazlı  : "<dedup_key>:c:<content>"   (TTL'li, çapraz-site duplicate: LinkedIn ↔ Indeed)
Eski sürümde kullanılan "<dedup_key>" Redis SET'i geriye dönük uyumluluk için hâlâ okunur
(yeni kayıt yazılmaz); böylece mevcut 3000+ üye silinmeden devre dışı kalır.
"""

import logging
from redis import Redis
from scraper.models.job import Job

logger = logging.getLogger(__name__)

SECONDS_PER_DAY = 86400
CONTENT_DEDUP_DAYS = 14


class RedisProducer:
    def __init__(
        self,
        client: Redis,
        stream_name: str,
        dedup_set_key: str,
        ttl_days: int = 30,
    ):
        self._client       = client
        self._stream       = stream_name
        self._dedup_key    = dedup_set_key
        self._ttl          = max(1, int(ttl_days)) * SECONDS_PER_DAY
        self._content_ttl  = CONTENT_DEDUP_DAYS * SECONDS_PER_DAY

    def _is_duplicate(self, job: Job) -> tuple[bool, list[str]]:
        """
        Duplicate mi? Ayrıca bu çağrıda yazılan anahtarları döner (XADD hatasında geri almak için).
        SET NX EX atomiktir: iki eşzamanlı yayıncı aynı ilanı iki kez geçiremez.
        """
        written: list[str] = []
        h = job.dedup_hash()

        # Eski (TTL'siz) SET ile geriye dönük uyumluluk
        if self._client.sismember(self._dedup_key, h):
            return True, written

        url_key = f"{self._dedup_key}:{h}"
        if not self._client.set(url_key, 1, nx=True, ex=self._ttl):
            return True, written
        written.append(url_key)

        content_key = f"{self._dedup_key}:c:{job.content_hash()}"
        if not self._client.set(content_key, 1, nx=True, ex=self._content_ttl):
            # Aynı ilan başka bir siteden zaten geldi → URL anahtarı kalsın (tekrar değerlendirilmesin)
            return True, written
        written.append(content_key)
        return False, written

    def publish(self, job: Job) -> bool:
        """
        İlanı stream'e yayınlar, daha önce görüldüyse atlar.

        Returns:
            True  → yayınlandı
            False → duplicate, atlandı
        """
        duplicate, written = self._is_duplicate(job)
        if duplicate:
            logger.debug("duplicate skipped: %s — %s", job.company, job.title)
            return False

        try:
            self._client.xadd(self._stream, job.to_redis_fields(), maxlen=5000, approximate=True)
        except Exception:
            # XADD başarısızsa ilan "görüldü" olarak kalmasın; bir sonraki taramada tekrar denensin.
            if written:
                self._client.delete(*written)
            raise

        logger.info(
            "published  source=%-12s  company=%-20s  title=%s",
            job.source, job.company, job.title,
        )
        return True

    def publish_batch(self, jobs: list[Job]) -> tuple[int, int]:
        """
        Job listesini toplu yayınlar.

        Returns:
            (published_count, skipped_count)
        """
        published = skipped = 0
        for job in jobs:
            try:
                ok = self.publish(job)
            except Exception as exc:
                logger.error("publish failed for %s — %s: %s", job.company, job.title, exc)
                skipped += 1
                continue
            if ok:
                published += 1
            else:
                skipped += 1

        logger.info(
            "batch done  total=%d  published=%d  skipped=%d",
            len(jobs), published, skipped,
        )
        return published, skipped
