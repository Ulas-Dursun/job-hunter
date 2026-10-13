"""
BaseSource — Tüm scraper'ların uyması gereken sözleşme.

Her kaynak (KariyerNet, LinkedIn, Indeed) bu sınıftan türer ve
fetch() metodunu implemente eder. Scheduler bunları polymorphic olarak çağırır.
"""

from abc import ABC, abstractmethod
from scraper.models.job import Job


class BaseSource(ABC):
    def __init__(self, name: str):
        # name, config.yaml'daki sources key'iyle eşleşmeli: "kariyer_net", "linkedin_rss"
        self.name = name

    @abstractmethod
    def fetch(self, keywords: list[str], location: str) -> list[Job]:
        """
        Verilen keyword ve lokasyon için iş ilanlarını çeker.

        Implementasyon kuralları:
        - max_age_days filtresi kaynak içinde uygulanır
        - Kısmi hatalarda exception fırlatma, log'la ve devam et
        - Tam hata durumunda boş liste döndür — asla crash etme
        - Dönen Job nesneleri normalize edilmiş ve eksiksiz olmalı
        """
        ...

    def __repr__(self) -> str:
        return f"{self.__class__.__name__}(name={self.name!r})"