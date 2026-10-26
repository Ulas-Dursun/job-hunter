"""
Ortak başlık/lokasyon filtreleri — ATS ve Arbeitnow kaynakları aynı kuralları paylaşır.

Tasarım prensibi:
- Yazılım mühendisi aday (0-2 yıl / new grad) Backend, Cloud/DevOps, Data/ML, Analitik,
  Sistem/Havacılık ve geniş teknoloji/çözüm rollerine başvurabilir.
- Bariz Senior/Lead/Manager veya Staj/QA/Mobile başlıkları anında elenir (performans ve kota koruması).
- Pozitif kapı yumuşaktır: Başlıkta geniş teknoloji/analitik terimi varsa VEYA (başlık/açıklamada
  0-2 yıl / junior / graduate sinyali + en az 1 teknik terim varsa) ilan AI'ya geçer.
"""

import re

TECH_TITLE_KEYWORDS = [
    "developer", "engineer", "software", "backend", "frontend", "fullstack", "full-stack", "full stack",
    "programmer", "geliştirici", "mühendis", "yazılım", "bilişim",
    "devops", "cloud", "platform", "sre", "site reliability",
    "infrastructure", "sysadmin", "systems",
    "data", "data analyst", "analytics", "business intelligence", "bi analyst", "bi developer",
    "technical analyst", "systems analyst", "security analyst", "soc analyst", "database", "dba",
    "golang", "python", "java", "rust", "c++", "sql",
    "cns", "avionics", "atm", "asterix", "surveillance",
    "genç yetenek", "mt programı", "management trainee", "uzman yardımcısı",
    "security", "cybersecurity", "siber güvenlik",
    "scientist", "machine learning", "ml", "mlops", "ai",
    "artificial intelligence", "veri", "veri analisti", "veri bilimci", "iş zekası", "sistem analisti", "yapay zeka",
    "solutions engineer", "integration", "automation",
]

ENTRY_SIGNALS = [
    "graduate", "new grad", "entry level", "entry-level", "junior", "trainee", "associate",
    "yeni mezun", "genç yetenek", "mt programı", "management trainee", "uzman yardımcısı",
    "0-2 years", "0-1 years", "0-2 yıl", "0-1 yıl", "1-2 years", "1-3 years", "early career", "young graduate",
]

# Yumuşak filtrede açıklamanın gerçekten yazılım/veri işi olduğunu kanıtlayan somut araç/teknoloji terimleri
TECH_DESC_TERMS = [
    "python", "java", "golang", "sql", "backend", "software engineering", "computer science",
    "rest api", "docker", "kubernetes", "machine learning", "aws", "linux", "git",
    "yazılım", "veri tabanı", "veritabanı", "postgresql", "redis", "mongodb", "c++", "rust",
    "microservices", "data pipeline", "etl", "pandas", "spark", "power bi", "tableau",
    "bilgisayar mühendisliği", "yazılım mühendisliği",
]

_TECH_RE = re.compile(
    r"(?<![\w])(?:" + "|".join(re.escape(k) for k in TECH_TITLE_KEYWORDS) + r")(?![\w])"
)

_ENTRY_RE = re.compile(
    r"(?<![\w])(?:" + "|".join(re.escape(k) for k in ENTRY_SIGNALS) + r")(?![\w])"
)

_DESC_TECH_RE = re.compile(
    r"(?<![\w])(?:" + "|".join(re.escape(k) for k in TECH_DESC_TERMS) + r")(?![\w])"
)

# Go cheap_filter ile uyumlu bariz kıdem / staj / alakasız departman red kelimeleri:
REJECT_TITLE_KEYWORDS = [
    "senior", "sr", "staff", "principal", "manager", "lead", "team leader", "director", "head of",
    "vp", "vice president", "chief", "architect", "mimar", "mimarı", "kıdemli", "kidemli",
    "takım lideri", "ekip lideri", "team lead", "tech lead", "engineering manager", "müdür", "müdürü", "yöneticisi",
    "ürün yöneticisi", "product manager", "project manager", "scrum master", "delivery manager",
    "intern", "internship", "stajyer", "stajyeri", "staj", "stajı", "working student", "werkstudent",
    "praktikum", "praktikant", "praktikantin", "stagiair", "stagiaire", "offre de stage", "ausbildung",
    "qa engineer", "test engineer", "ios developer", "android developer",
    "sales", "satış", "account executive", "account management", "account manager",
    "customer success", "customer support", "recruiter", "talent acquisition", "human resources",
    "marketing", "pazarlama", "dijital pazarlama", "seo", "legal", "counsel", "paralegal",
    "finance manager", "accountant", "fincrime", "financial crime", "risk analyst", "operational risk",
    "credit risk", "deal intelligence", "cost analyst", "strategic finance", "investor relations",
    "workday", "mikro erp", "field engineer",
]

_REJECT_RE = re.compile(
    r"(?<![\w])(?:" + "|".join(re.escape(k) for k in REJECT_TITLE_KEYWORDS) + r")(?![\w])"
)


def is_rejected_title(title: str) -> bool:
    """Başlık bariz Senior/Lead/Manager, Staj veya Satış/İK/Hukuk ise True döner."""
    if not title:
        return True
    return _REJECT_RE.search(title.lower()) is not None


def is_tech_title(title: str, description: str = "") -> bool:
    """
    Yumuşak filtre:
    1) Bariz red (Senior/Lead/Manager/Staj/Satış/İK) varsa eler.
    2) Başlıkta geniş teknoloji/analitik/giriş sinyali varsa:
       - Eğer başlık sadece genel bir kelimeyse ('associate', 'graduate', 'trainee', 'junior', 'analyst', 'solutions')
         ve açıklama verilmişse, açıklamada en az 1 teknik/veri terimi aranır (örn. 'Junior HR Associate' sızmasın diye).
       - Doğrudan mühendislik/yazılım/veri başlığıysa ('software', 'backend', 'data', 'devops' vb.) doğrudan geçer.
    """
    if not title or is_rejected_title(title):
        return False
    t = title.lower()
    d = (description or "").lower()

    # 1) Başlıkta doğrudan teknoloji / veri / analitik / bilişim terimi varsa geçer
    if _TECH_RE.search(t):
        return True

    # 2) Yumuşak 0-2 yıl kapısı: Başlıkta veya açıklamada Junior/Graduate/Associate/0-2 yıl sinyali
    # varsa VE açıklamada somut yazılım/veri araçları (Python, SQL, Java, Docker, AWS vb.) geçiyorsa geçer.
    if d and (_ENTRY_RE.search(t) or _ENTRY_RE.search(d)) and _DESC_TECH_RE.search(d):
        return True

    return False


def has_word(text: str, token: str) -> bool:
    """Kısa token'ların ('uk', 'us') başka kelimelerin içinde eşleşmesini önleyen sınırlı arama."""
    return re.search(r"(?<![a-z0-9])" + re.escape(token) + r"(?![a-z0-9])", text) is not None
