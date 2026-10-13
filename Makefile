.PHONY: help setup infra-up infra-down run-orchestrator run-scraper \
        test logs clean redis-cli psql check-env

# ── Yardım ──────────────────────────────────────────────────────────────────
help:
	@echo ""
	@echo "  Job Hunter — Komutlar"
	@echo "  ─────────────────────────────────────────────"
	@echo "  make setup              Tüm bağımlılıkları kur"
	@echo "  make infra-up           PG + Redis + Prometheus + Grafana başlat"
	@echo "  make infra-down         Infrastructure'ı durdur"
	@echo "  make run-orchestrator   Go orchestrator'ı çalıştır"
	@echo "  make run-scraper        Python scraper'ı çalıştır"
	@echo "  make test               Go testlerini çalıştır"
	@echo "  make logs               Infrastructure loglarını izle"
	@echo "  make redis-cli          Redis CLI'ya bağlan"
	@echo "  make psql               PostgreSQL'e bağlan"
	@echo "  make clean              Tüm container ve volume'ları sil"
	@echo ""

# ── Kurulum ──────────────────────────────────────────────────────────────────
setup:
	@echo "→ Go bağımlılıkları yükleniyor..."
	go mod download
	@echo "→ Python bağımlılıkları yükleniyor..."
	pip install -r scraper/requirements.txt
	@echo "→ .env dosyası oluşturuluyor..."
	@cp -n .env.example .env 2>/dev/null && echo "  .env oluşturuldu" || echo "  .env zaten mevcut, atlandı"
	@echo ""
	@echo "✓ Setup tamamlandı."
	@echo "  .env dosyasını açıp API key'lerini gir, sonra: make infra-up"
	@echo ""

# ── Infrastructure ───────────────────────────────────────────────────────────
infra-up:
	@echo "→ Infrastructure başlatılıyor..."
	docker compose up -d postgres redis prometheus grafana
	@echo "→ Servislerin hazır olması bekleniyor..."
	@sleep 4
	@echo ""
	@echo "✓ Infrastructure çalışıyor:"
	@echo "  PostgreSQL  → localhost:5432"
	@echo "  Redis       → localhost:6379"
	@echo "  Prometheus  → http://localhost:9090"
	@echo "  Grafana     → http://localhost:3000  (admin / admin)"
	@echo ""

infra-down:
	docker compose down
	@echo "✓ Infrastructure durduruldu."

# ── Servisler ────────────────────────────────────────────────────────────────
run-orchestrator:
	go run ./cmd/orchestrator/...

run-scraper:
	PYTHONPATH=. python -m scraper.main

# ── Testler ──────────────────────────────────────────────────────────────────
test:
	go test ./...

test-verbose:
	go test -v -race ./...

# ── Debug Araçları ───────────────────────────────────────────────────────────
logs:
	docker compose logs -f postgres redis

redis-cli:
	docker exec -it jobhunter_redis redis-cli

psql:
	docker exec -it jobhunter_postgres psql -U jobhunter -d jobhunter

# Stream içeriğini kontrol et (debug için)
redis-stream-check:
	docker exec jobhunter_redis redis-cli XLEN jobs:raw
	docker exec jobhunter_redis redis-cli XLEN jobs:filtered
	docker exec jobhunter_redis redis-cli XLEN jobs:failed

# ── Temizlik ──────────────────────────────────────────────────────────────────
clean:
	docker compose down -v
	@echo "✓ Tüm container ve volume'lar silindi."