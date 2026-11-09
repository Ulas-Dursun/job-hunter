#!/usr/bin/env bash
set -e

cd "$(dirname "$0")"

echo "══════════════════════════════════════════════════"
echo "  JOB HUNTER — MACBOOK UÇTAN UCA BAŞLATICI        "
echo "══════════════════════════════════════════════════"

# 1. Docker servislerini başlat (PostgreSQL & Redis)
echo ""
echo "→ [1/4] PostgreSQL ve Redis (Docker) başlatılıyor..."
docker compose up -d postgres redis

# 2. Python sanal ortamı (.venv) yoksa otomatik kur
if [ ! -f ".venv/bin/python" ]; then
    echo ""
    echo "→ [2/4] MacBook için Python sanal ortamı (.venv) ilk kez kuruluyor..."
    python3 -m venv .venv
    ./.venv/bin/pip install --upgrade pip
    ./.venv/bin/pip install -r scraper/requirements.txt
else
    echo ""
    echo "→ [2/4] Python sanal ortamı (.venv) hazır."
fi

# 3. Go Orchestrator kontrolü ve arka planda başlatma
DASHBOARD_URL="http://127.0.0.1:8080/api/stats"
if curl -fsS --max-time 2 "$DASHBOARD_URL" >/dev/null 2>&1; then
    echo ""
    echo "→ [3/4] Go Orchestrator zaten aktif (http://localhost:8080)"
else
    echo ""
    echo "→ [3/4] Go Orchestrator MacBook için derlenip başlatılıyor..."
    mkdir -p bin
    go build -o ./bin/orchestrator ./cmd/orchestrator
    nohup ./bin/orchestrator > ./bin/orchestrator.log 2>&1 &

    READY=0
    for i in $(seq 1 20); do
        sleep 1
        if curl -fsS --max-time 1 "$DASHBOARD_URL" >/dev/null 2>&1; then
            READY=1
            break
        fi
    done

    if [ "$READY" -eq 1 ]; then
        echo "✓ Go Orchestrator hazır!"
    else
        echo "[!] HATA: Go Orchestrator başlatılamadı. Log: ./bin/orchestrator.log"
        exit 1
    fi
fi

# Eğer veritabanı yeni kurulduysa (0 ilan varsa), önceki başvurduğun (applied) 4 ilanı AI hafızası için yükle
JOB_COUNT=$(docker exec jobhunter_postgres psql -U jobhunter -d jobhunter -Atc "SELECT COUNT(*) FROM jobs;" 2>/dev/null || echo "0")
if [ "$JOB_COUNT" = "0" ] && [ -f "./backups/applied_history_seed.sql" ]; then
    echo "→ Önceki başvurduğun (applied) ilan hafızası yükleniyor (temiz başlangıç + AI hafızası)..."
    docker exec -i jobhunter_postgres psql -U jobhunter -d jobhunter < ./backups/applied_history_seed.sql >/dev/null 2>&1 || true
fi

# Tarayıcıda arayüzü aç
open "http://localhost:8080" >/dev/null 2>&1 || true

# 4. Python Scraper (Tek seferlik tarama)
echo ""
echo "→ [4/4] Scraper başlatılıyor (güncel Avrupa & TR ilanları çekiliyor)..."
PYTHONPATH="." ./.venv/bin/python -m scraper.main --once

echo ""
echo "✓ Tarama tamamlandı! Arayüz: http://localhost:8080"
echo "══════════════════════════════════════════════════"