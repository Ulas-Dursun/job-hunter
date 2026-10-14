"""
Scraper Service — Entry Point

Step 1B: KariyerNetSource aktif, ilk gerçek ilanlar çekiliyor.
"""

import argparse
import logging
import os
import time

import redis
from dotenv import load_dotenv

from scraper.config import load_config, get_redis_url
from scraper.publisher.redis_producer import RedisProducer
from scraper.sources.jobspy_source import JobSpySource
from scraper.sources.ats_source import ATSSource
from scraper.sources.eurocontrol_source import EurocontrolSource
from scraper.sources.techcareer_source import TechcareerSource
from scraper.sources.arbeitnow_source import ArbeitnowSource

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
    datefmt="%Y-%m-%d %H:%M:%S",
)
logger = logging.getLogger("scraper")


def run_cycle(cfg: dict, sources: list, producer: RedisProducer) -> tuple[int, int]:
    """Tek bir tarama döngüsü yürütür ve (yayınlanan, atlanan) sayılarını döner."""
    total_published = 0
    total_skipped = 0

    for source in sources:
        source_cfg = cfg["sources"].get(source.name, {})
        try:
            jobs = source.fetch(
                keywords=source_cfg.get("keywords", []),
                location=source_cfg.get("location", "Istanbul, Turkey"),
            )
            published, skipped = producer.publish_batch(jobs)
            total_published += published
            total_skipped += skipped
            logger.info(
                "cycle done  source=%s  fetched=%d  published=%d  skipped=%d",
                source.name, len(jobs), published, skipped,
            )
        except Exception as exc:
            logger.error("source %s failed: %s", source.name, exc, exc_info=True)

    return total_published, total_skipped


def main() -> None:
    parser = argparse.ArgumentParser(description="Job Hunter Scraper Service")
    parser.add_argument(
        "--once",
        action="store_true",
        help="Sonsuz döngü yerine tek bir tarama döngüsü çalıştırıp çıkar (batch modu)",
    )
    args = parser.parse_args()

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
    sources = []
    if cfg["sources"].get("jobspy", {}).get("enabled", False):
        sources.append(JobSpySource(cfg["sources"]["jobspy"]))
        logger.info("jobspy source enabled (linkedin + indeed)")

    if cfg["sources"].get("ats", {}).get("enabled", False):
        sources.append(ATSSource(cfg["sources"]["ats"]))
        logger.info("ats source enabled (greenhouse + lever + smartrecruiters)")

    if cfg["sources"].get("eurocontrol", {}).get("enabled", False):
        sources.append(EurocontrolSource(cfg["sources"]["eurocontrol"]))
        logger.info("eurocontrol source enabled (official brussels portal)")

    if cfg["sources"].get("techcareer", {}).get("enabled", False):
        sources.append(TechcareerSource(cfg["sources"]["techcareer"]))
        logger.info("techcareer source enabled (techcareer.net)")

    if cfg["sources"].get("arbeitnow", {}).get("enabled", False):
        sources.append(ArbeitnowSource(cfg["sources"]["arbeitnow"]))
        logger.info("arbeitnow source enabled (eu tech jobs)")

    if not sources:
        logger.warning("no sources enabled — check config.yaml")

    logger.info("active sources: %d", len(sources))

    # ── Batch Modu (--once) ────────────────────────────────────────────────
    if args.once:
        logger.info("─── batch mode (--once) starting ───")
        pub, skip = run_cycle(cfg, sources, producer)
        logger.info("─── batch mode done  published=%d  skipped=%d ───", pub, skip)
        return

    # ── Sürekli Döngü Modu ────────────────────────────────────────────────
    poll_interval = cfg["sources"]["jobspy"].get("poll_interval_minutes", 30) * 60

    while True:
        logger.info("─── poll cycle starting ───")
        run_cycle(cfg, sources, producer)
        logger.info("─── poll cycle done, sleeping %d minutes ───", poll_interval // 60)
        time.sleep(poll_interval)


if __name__ == "__main__":
    main()