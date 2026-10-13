---
name: project-audit
description: >-
  Projedeki kod tabanını, mimariyi, servis akışlarını ve bağımlılıkları salt okunur şekilde
  incelemek, analiz etmek ve bulguları dosya/fonksiyon referanslarıyla raporlamak için bu skill'i kullanın.
  Kullanıcı kod incelemesi, mimari analiz veya sistem keşfi istediğinde devreye girer.
---

# Project Audit Skill

Bu skill, Job Hunter projesinde kod tabanını, mimari bileşenleri ve servisler arası veri akışını **salt okunur (read-only)** şekilde denetlemek ve analiz etmek için kullanılır.

## Temel İlkeler ve Güvenlik Kuralları

1. **Kesinlikle Durum Değiştirmeme (Read-Only Prensibi):**
   - Açık ve net bir kullanıcı talimatı olmadıkça hiçbir kaynak kod dosyasını, konfigürasyonu veya ortam dosyasını (`.env`) değiştirme ya da silme.
   - Veritabanında (PostgreSQL) veya Redis üzerinde hiçbir veri oluşturma, güncelleme veya silme işlemi yapma.
   - Container ve servis durumlarını (`restart`, `stop`, `down` vb.) değiştirme.
2. **Doğrulanmamış Varsayım Yapmama:**
   - Hiçbir dosya yolu, Go paketi, Python scripti, Docker servis adı, veritabanı tablosu veya Redis stream adını doğrulamadan varsayma. Kod tabanından ve yapılandırma dosyalarından kanıt topla.
3. **Ön Koşul ve Ortam Kontrolleri:**
   - **Git Durumu:** `git status` çalıştırıldığında hata alınıyorsa (örneğin `.git` dizini henüz başlatılmamışsa), depoda Git varmış gibi davranma; durumu raporda belirt ve doğrudan yerel dosya sistemini incele.
   - **Docker Durumu:** Docker daemon veya ilgili container'lar çalışmıyorsa veri tabanı/Redis sorgulaması yapmaya çalışma. Servislerin kapalı olduğunu açık bir **engel (blocker)** olarak raporla.
4. **Kanıta Dayalı Raporlama ve Gizlilik:**
   - Bulguları sunarken mutlaka somut dosya yolları, satır aralıkları, fonksiyon veya struct adlarını referans göster.
   - `.env` içerisindeki veya sistem çıktılarındaki hassas anahtar, şifre ve token bilgilerini asla okuma, loglama veya yanıtlara ekleme.

---

## Proje Bileşenleri ve Denetim Kapsamı (Doğrulanmış Mimari)

İncelemelerde aşağıdaki somut ve doğrulanmış proje yapısını referans al:

### 1. Go Bileşenleri (`cmd/`, `internal/`)
- **Giriş Noktası:** `cmd/orchestrator/main.go`
- **İç Paketler:**
  - `internal/analyzer`: Gemini prompt ve iş ilanı analiz mantığı
  - `internal/api`: Web HTTP sunucusu ve handler'lar
  - `internal/config`: YAML ve ortam değişkeni konfigürasyon yönetimi
  - `internal/consumer`: `jobs:raw` stream'ini dinleyen ucuz filtreleme (cheap filter) consumer'ı
  - `internal/filter`: İlan filtreleme kuralları
  - `internal/intelligence`: Puanlama ve akıllı analiz katmanı
  - `internal/metrics`: Prometheus metrik tanımları
  - `internal/notifier`: Telegram ve bildirim göndericileri
  - `internal/store`: PostgreSQL bağlantısı, sorguları ve `internal/store/migrations/001_create_jobs.sql`
  - `internal/worker`: `jobs:filtered` stream'inden ilanları işleyen worker havuzu

### 2. Python Scraper Bileşenleri (`scraper/`)
- **Giriş Noktası:** `scraper/main.py`
- **Konfigürasyon:** `scraper/config.py`
- **Veri Modeli:** `scraper/models/job.py` (`Job` dataclass)
- **Kuyruk Yayıncısı:** `scraper/publisher/redis_producer.py` (Redis Streams `XADD` ve tekilleştirme)
- **Kaynaklar:** `scraper/sources/` (`kariyer_net.py`, `linkedin_rss.py`, `indeed.py`, `jobspy_source.py`)

### 3. Altyapı ve Veri Depoları (`docker-compose.yml`, `config.yaml`)
- **Docker Servisleri & Container İsimleri:**
  - `postgres` (Container: `jobhunter_postgres`, Port: `5433:5432`)
  - `redis` (Container: `jobhunter_redis`, Port: `6379:6379`)
  - `prometheus` (Container: `jobhunter_prometheus`, Port: `9090:9090`)
  - `grafana` (Container: `jobhunter_grafana`, Port: `3000:3000`)
- **Doğrulanmış Veritabanı Tablosu:**
  - `jobs` tablosu (`internal/store/migrations/001_create_jobs.sql` içinde tanımlı)
- **Doğrulanmış Redis Stream & Key Yapısı:**
  - `jobs:raw`: Scraper tarafından toplanan ham ilanlar
  - `jobs:filtered`: Go consumer'dan geçen filtrelenmiş ilanlar
  - `jobs:failed`: Hata alan ilanlar
  - `jobs:seen`: Scraper tekilleştirme Redis Set anahtarı

---

## Denetim Raporlama Formatı

Denetim tamamlandığında raporu şu başlıklarla Türkçe olarak sun:
1. **Ortam ve Ön Koşul Durumu:** Git deposu durumu, Docker servislerinin erişilebilirliği veya tespit edilen engeller.
2. **Mimari Uyumluluk:** İlgili kod bileşeninin gerçek akışla (`scraper` → `jobs:raw` → `consumer` → `jobs:filtered` → `worker` → `PostgreSQL: jobs`) uyumu.
3. **Bulgular & Referanslar:** Dosya yolu, fonksiyon ve satır numarası verilerek somut tespitler.
4. **Riskler ve İyileştirme Önerileri:** Mevcut mimariyi bozmadan yapılabilecek minimal öneriler.
