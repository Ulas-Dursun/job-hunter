"""
RedisProducer — Scraper'dan Redis Stream'e köprü.

Tek sorumluluğu: Job nesnesini alıp jobs:raw stream'ine yayınlamak.
Deduplication burada yapılır — aynı URL iki kez stream'e girmez.
"""

import logging
from redis import Redis
from scraper.models.job import Job

logger = logging.getLogger(__name__)


class RedisProducer:
    def __init__(self, client: Redis, stream_name: str, dedup_set_key: str):
        self._client       = client
        self._stream       = stream_name
        self._dedup_key    = dedup_set_key

    def publish(self, job: Job) -> bool:
        """
        İlanı stream'e yayınlar, daha önce görüldüyse atlar.

        SADD atomik bir işlemdir: set'e ekler ve yeni mi eklendiğini döner.
          → 1: yeni eklendi (duplicate değil) → publish et
          → 0: zaten vardı (duplicate)        → atla

        Returns:
            True  → yayınlandı
            False → duplicate, atlandı
        """
        hash_val = job.dedup_hash()
        is_new = self._client.sadd(self._dedup_key, hash_val) == 1

        if not is_new:
            logger.debug("duplicate skipped: %s — %s", job.company, job.title)
            return False

        self._client.xadd(self._stream, job.to_redis_fields())
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
            if self.publish(job):
                published += 1
            else:
                skipped += 1

        logger.info(
            "batch done  total=%d  published=%d  skipped=%d",
            len(jobs), published, skipped,
        )
        return published, skipped