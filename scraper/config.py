"""
Config loader — Python scraper için.

Go ve Python aynı config.yaml'ı okur.
Python yalnızca 'candidate' ve 'sources' bölümlerini kullanır.
Secrets (.env) ayrı yüklenir.
"""

import os
from pathlib import Path
from typing import Any

import yaml


def load_config(path: str = "config.yaml") -> dict[str, Any]:
    config_path = Path(path)
    if not config_path.exists():
        raise FileNotFoundError(
            f"config.yaml not found at {config_path.resolve()}\n"
            f"Make sure you're running from the repo root: python -m scraper.main"
        )

    with open(config_path, encoding="utf-8") as f:
        return yaml.safe_load(f)


def get_redis_url() -> str:
    host     = os.getenv("REDIS_HOST", "localhost")
    port     = os.getenv("REDIS_PORT", "6379")
    password = os.getenv("REDIS_PASSWORD", "")

    if password:
        return f"redis://:{password}@{host}:{port}/0"
    return f"redis://{host}:{port}/0"