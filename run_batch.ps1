# Job Hunter — Günlük Batch Tarama Scripti (Windows / PowerShell)
# Bu script infrastructure'ı kontrol eder ve tek seferlik güncel ilan taraması yapar.

Write-Host "══════════════════════════════════════════════════" -ForegroundColor Cyan
Write-Host "  JOB HUNTER — GÜNLÜK BATCH TARAMA                " -ForegroundColor Cyan
Write-Host "══════════════════════════════════════════════════" -ForegroundColor Cyan

# 1. Altyapı kontrolü
Write-Host "`n→ PostgreSQL ve Redis başlatılıyor/kontrol ediliyor..." -ForegroundColor Cyan
docker compose up -d postgres redis

# 2. Python Scraper (Tek seferlik tarama)
Write-Host "`n→ Scraper başlatılıyor (son ilanlar çekiliyor)..." -ForegroundColor Cyan
$env:PYTHONPATH = "."
if (Test-Path ".\.venv\Scripts\python.exe") {
    .\.venv\Scripts\python.exe -m scraper.main --once
} else {
    python -m scraper.main --once
}

Write-Host "`n✓ Tarama tamamlandı!" -ForegroundColor Green
Write-Host "  Orchestrator ilanları AI ile puanlayıp Telegram'a bildirim gönderecek."
Write-Host "  📊 Dashboard: http://localhost:8080" -ForegroundColor Yellow
Write-Host "══════════════════════════════════════════════════`n" -ForegroundColor Cyan
