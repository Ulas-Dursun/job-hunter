---
name: runtime-readonly-debug
description: >-
  Windows ve Docker üzerinde çalışan altyapıyı (loglar, container durumları, PostgreSQL ve Redis)
  salt okunur (read-only) olarak incelemek ve hata ayıklamak için bu skill'i kullanın.
  Veri değiştiren komutlar veya servis yeniden başlatma yapmadan çalışma zamanı durumunu denetler.
---

# Runtime Read-Only Debug Skill

Bu skill, Job Hunter projesinde Windows ve Docker ortamında koşan altyapı servislerinin (PostgreSQL, Redis, Prometheus, Grafana) çalışma zamanı durumunu ve loglarını **salt okunur (read-only)** şekilde incelemek ve hata ayıklamak için kullanılır.

## Kesin Yasaklar ve Güvenlik Kuralları

1. **Durum ve Veri Değiştirme Yasağı:**
   Aşağıdaki işlemler açıkça talep edilmedikçe **asla** çalıştırılmamalıdır:
   - `docker compose down -v` veya volume silme komutları (`docker volume rm`).
   - Servisleri durdurma veya yeniden başlatma (`docker restart`, `docker stop`, `docker compose restart`, `docker compose down`).
   - Veritabanında veri veya şema değiştiren işlemler: `DROP`, `TRUNCATE`, `DELETE`, `UPDATE`, `INSERT`, `ALTER`, manuel migration çalıştırma.
   - Redis'te veri değiştiren veya silen komutlar: `FLUSHALL`, `FLUSHDB`, `DEL`, `SET`, `XADD`, `XDEL`.
2. **Docker Çalışma Ön Koşulu (Engel Tespiti):**
   - Docker servisi veya ilgili container'lar çalışmıyorsa **asla doğrudan `docker exec` ile veritabanı veya Redis sorgulaması yapmaya çalışma.**
   - Container'ların kapalı veya Docker daemon'ın erişilemez olması durumunu kullanıcıya açık bir **çalışma engeli (blocker)** olarak raporla ve incelemeyi orada sınırla.
3. **Gizlilik:**
   - `.env` içerisindeki şifre ve anahtarları çıktılara dahil etme.

---

## Doğrulanmış Altyapı Referansı (`docker-compose.yml`)

Bu projede tanımlı servisler, container isimleri ve portlar şunlardır:
- **`postgres`:** Container adı `jobhunter_postgres` (Port: `5433:5432`, Veritabanı: `jobhunter`, Kullanıcı: `jobhunter`)
  - **Doğrulanmış Tablo:** `jobs` (İlan kayıtları tablosu)
- **`redis`:** Container adı `jobhunter_redis` (Port: `6379:6379`)
  - **Doğrulanmış Stream'ler:** `jobs:raw`, `jobs:filtered`, `jobs:failed`
  - **Doğrulanmış Set:** `jobs:seen` (URL deduplication)
- **`prometheus`:** Container adı `jobhunter_prometheus` (Port: `9090:9090`)
- **`grafana`:** Container adı `jobhunter_grafana` (Port: `3000:3000`)

---

## Güvenli İnceleme Adımları (Windows PowerShell)

Tüm komutlar PowerShell üzerinde sırayla ve yalnızca okuma amacıyla yürütülür.

### Adım 1: Docker ve Container Durum Kontrolü (Ön Koşul)
İlk olarak container'ların aktif olup olmadığını kontrol et:
```powershell
docker compose ps
```
veya
```powershell
docker ps --filter "name=jobhunter"
```
- **Karar Noktası:** Eğer ilgili container (`jobhunter_postgres` veya `jobhunter_redis`) çalışır durumda (`Up` / `healthy`) **değilse**, Adım 3 ve 4'e geçme; "Container çalışmıyor, veri sorgulaması yapılamadı" şeklinde engel bildir.

### Adım 2: Log İncelemeleri (Salt Okunur)
Container'lar mevcutsa logları son satırlarla sınırlandırarak oku:
```powershell
# PostgreSQL logları
docker compose logs --tail=100 postgres

# Redis logları
docker compose logs --tail=100 redis

# Prometheus logları
docker compose logs --tail=100 prometheus
```

### Adım 3: PostgreSQL Salt Okunur Sorgulama (Yalnızca Container Çalışıyorsa)
Doğrulanmış `jobs` tablosu üzerinde yalnızca okuma sorguları çalıştır:
```powershell
# Tablo listesi kontrolü
docker exec jobhunter_postgres psql -U jobhunter -d jobhunter -c "\dt"

# jobs tablosu şeması
docker exec jobhunter_postgres psql -U jobhunter -d jobhunter -c "\d jobs"

# Son eklenen ilanlar (LIMIT ile sınırlı)
docker exec jobhunter_postgres psql -U jobhunter -d jobhunter -c "SELECT id, source, title, company, status, created_at FROM jobs ORDER BY created_at DESC LIMIT 5;"
```

### Adım 4: Redis Stream ve Key Salt Okunur Kontrolü (Yalnızca Container Çalışıyorsa)
Doğrulanmış stream uzunluklarını ve set boyutunu oku:
```powershell
# Redis sunucu ve bellek durumu
docker exec jobhunter_redis redis-cli INFO server
docker exec jobhunter_redis redis-cli INFO memory

# Stream uzunlukları (Kuyruk birikme kontrolü)
docker exec jobhunter_redis redis-cli XLEN jobs:raw
docker exec jobhunter_redis redis-cli XLEN jobs:filtered
docker exec jobhunter_redis redis-cli XLEN jobs:failed

# Tekilleştirme setindeki eleman sayısı
docker exec jobhunter_redis redis-cli SCARD jobs:seen

# Son stream mesajları (XREVRANGE ile sınırlı)
docker exec jobhunter_redis redis-cli XREVRANGE jobs:raw + - COUNT 2
```

---

## Analiz ve Raporlama

İnceleme sonucunda kullanıcıya:
1. Altyapı servislerinin erişilebilirlik durumu (çalışıyor mu, durmuş mu, engeller neler?).
2. Varsa loglardaki hata/uyarı özetleri.
3. PostgreSQL `jobs` tablosu ve Redis `jobs:*` stream'lerindeki mevcut metrikler ve kuyruk durumları aktarılır.
