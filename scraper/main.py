"""
Scraper Service — Entry Point

Step 1B: KariyerNetSource aktif, ilk gerçek ilanlar çekiliyor.
"""

import logging
import os
import time

import redis
from dotenv import load_dotenv

from scraper.config import load_config, get_redis_url
from scraper.publisher.redis_producer import RedisProducer
from scraper.sources.jobspy_source import JobSpySource
from scraper.sources.kariyer_net import KariyerNetSource
from scraper.sources.linkedin_rss import LinkedInRSSSource
from scraper.sources.indeed import IndeedSource

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
    datefmt="%Y-%m-%d %H:%M:%S",
)
logger = logging.getLogger("scraper")


def main() -> None:
    load_dotenv()

    cfg       = load_config("config.yaml")
    redis_cfg = cfg["redis"]

    logger.info("scraper starting — candidate: %s", cfg["candidate"]["name"])

    # ── Redis bağlantısı ──────────────────────────────────────────────────
    redis_client = redis.from_url(get_redis_url(), decode_responses=True)
    try:
        redis_client.ping()
        logger.info("redis connected")
    except redis.ConnectionError as exc:
        logger.error("redis connection failed: %s", exc)
        raise SystemExit(1)

    # ── Producer ──────────────────────────────────────────────────────────
    producer = RedisProducer(
        client=redis_client,
        stream_name=redis_cfg["stream_raw"],
        dedup_set_key=redis_cfg["dedup_set_key"],
    )

    # ── Sources ───────────────────────────────────────────────────────────
    sources     = []

    if cfg["sources"]["jobspy"].get("enabled", False):
        sources.append(JobSpySource(cfg["sources"]["jobspy"]))
    logger.info("jobspy source enabled (linkedin + indeed)")


    if not sources:
        logger.warning("no sources enabled — check config.yaml")

    logger.info("active sources: %d", len(sources))

    # ── Poll loop ─────────────────────────────────────────────────────────
    poll_interval = cfg["sources"]["jobspy"].get("poll_interval_minutes", 30) * 60

    while True:
        logger.info("─── poll cycle starting ───")

        for source in sources:
            source_cfg = cfg["sources"].get(source.name, {})
            try:
                jobs = source.fetch(
                    keywords=source_cfg.get("keywords", []),
                    location=source_cfg.get("location", "istanbul"),
                )
                published, skipped = producer.publish_batch(jobs)
                logger.info(
                    "cycle done  source=%s  fetched=%d  published=%d  skipped=%d",
                    source.name, len(jobs), published, skipped,
                )
            except Exception as exc:
                logger.error("source %s failed: %s", source.name, exc, exc_info=True)

        logger.info("─── poll cycle done, sleeping %d minutes ───", poll_interval // 60)
        time.sleep(poll_interval)


if __name__ == "__main__":
    main()