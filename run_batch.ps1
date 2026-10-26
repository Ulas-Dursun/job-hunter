# Job Hunter — Tek Komutla Uçtan Uca Çalıştırma & Batch Tarama Betiği (Windows / PowerShell)
# 1) Yerel profil (config.local.yaml) kontrolü yapar
# 2) PostgreSQL ve Redis (Docker) servislerini başlatır/kontrol eder
# 3) Go Orchestrator çalışmıyorsa derleyip arka planda başlatır ve hazır olmasını bekler
# 4) Python Scraper ile tüm aktif kaynaklardan tek seferlik (--once) tarama yapar

$ErrorActionPreference = "Stop"

Write-Host "══════════════════════════════════════════════════" -ForegroundColor Cyan
Write-Host "  JOB HUNTER — UÇTAN UCA BATCH & ORCHESTRATOR     " -ForegroundColor Cyan
Write-Host "══════════════════════════════════════════════════" -ForegroundColor Cyan

# 0. Şablon profil / config.local.yaml kontrolü
if (-not (Test-Path ".\config.local.yaml")) {
    $cfgContent = Get-Content ".\config.yaml" -Raw -ErrorAction SilentlyContinue
    if ($cfgContent -match 'name:\s*"Your Name"') {
        Write-Host "`n[!] HATA: Varsayılan şablon profil ('Your Name') tespit edildi. Lütfen 'config.local.yaml' dosyanızı oluşturup gerçek profil bilgilerinizi girin." -ForegroundColor Red
        Write-Host "    Örnek: Copy-Item config.yaml config.local.yaml`n" -ForegroundColor Yellow
        exit 1
    }
}

# 1. Altyapı kontrolü (PostgreSQL & Redis)
Write-Host "`n→ [1/3] PostgreSQL ve Redis başlatılıyor/kontrol ediliyor..." -ForegroundColor Cyan
docker compose up -d postgres redis

# 2. Go Orchestrator kontrolü ve arka planda başlatma
$dashboardUrl = "http://127.0.0.1:8080/api/stats"
$orchestratorRunning = $false

try {
    $resp = Invoke-WebRequest -Uri $dashboardUrl -UseBasicParsing -TimeoutSec 2 -ErrorAction Stop
    if ($resp.StatusCode -eq 200) {
        $orchestratorRunning = $true
    }
} catch {
    $orchestratorRunning = $false
}

if ($orchestratorRunning) {
    Write-Host "`n→ [2/3] Go Orchestrator zaten aktif (http://127.0.0.1:8080)" -ForegroundColor Green
} else {
    Write-Host "`n→ [2/3] Go Orchestrator çalışmıyor; derlenip arka planda başlatılıyor..." -ForegroundColor Cyan
    if (-not (Test-Path ".\bin")) {
        New-Item -ItemType Directory -Path ".\bin" | Out-Null
    }
    go build -o .\bin\orchestrator.exe .\cmd\orchestrator
    if ($LASTEXITCODE -ne 0) {
        Write-Host "[!] Go Orchestrator derlenemedi!" -ForegroundColor Red
        exit 1
    }

    Start-Process -FilePath ".\bin\orchestrator.exe" -WorkingDirectory (Get-Location).Path -WindowStyle Minimized

    # Orchestrator'ın 8080 portunu açmasını bekle (en fazla 15 saniye)
    $ready = $false
    for ($i = 0; $i -lt 15; $i++) {
        Start-Sleep -Seconds 1
        try {
            $check = Invoke-WebRequest -Uri $dashboardUrl -UseBasicParsing -TimeoutSec 1 -ErrorAction Stop
            if ($check.StatusCode -eq 200) {
                $ready = $true
                break
            }
        } catch {}
    }

    if ($ready) {
        Write-Host "✓ Go Orchestrator arka planda başlatıldı ve hazır!" -ForegroundColor Green
    } else {
        Write-Host "⚠ Go Orchestrator başlatıldı ancak henüz yanıt vermedi; logları kontrol edin." -ForegroundColor Yellow
    }
}

# 3. Python Scraper (Tek seferlik tarama)
Write-Host "`n→ [3/3] Scraper başlatılıyor (son ilanlar çekiliyor)..." -ForegroundColor Cyan
$env:PYTHONPATH = "."
if (Test-Path ".\.venv\Scripts\python.exe") {
    .\.venv\Scripts\python.exe -m scraper.main --once
} else {
    python -m scraper.main --once
}

Write-Host "`n✓ Tarama tamamlandı!" -ForegroundColor Green
Write-Host "  Orchestrator arka planda ilanları AI ile puanlayıp Telegram'a bildirim gönderecek."
Write-Host "  📊 Dashboard: http://localhost:8080" -ForegroundColor Yellow
Write-Host "══════════════════════════════════════════════════`n" -ForegroundColor Cyan
