# MacBook Antigravity İlk Başlatma Promptu

MacBook'a geçip Antigravity'yi (veya Claude / Cursor / VS Code Agent) bu proje klasöründe ilk kez açtığında aşağıdaki metni ilk mesaj olarak yapıştırman yeterlidir:

```text
Merhaba! Bu projeye Windows bilgisayarımdan MacBook'a (macOS / Apple Silicon) geçerek devam ediyorum.
Lütfen başlamadan önce AGENTS.md dosyasını oku.

Önemli Sistem Bağlamı:
1. Ortam artık macOS (zsh). Terminal komutlarını POSIX zsh uyumlu yaz.
   - Python sanal ortamı: ./.venv/bin/python
   - Tüm sistemi (Docker + Go Orchestrator + Python Scraper) başlatmak için: bash run_mac.sh
   - Go testleri için: go test -count=1 ./...
   - Python testleri için: ./.venv/bin/python -m unittest discover -s scraper/tests -t .
2. Gizlilik: Kişisel profilim config.local.yaml içinde, API anahtarlarım .env içinde yer alıyor. Bunları ASLA git'e ekleme veya commit'leme.
3. Altyapı: PostgreSQL portu 5433 (POSTGRES_PORT=5433), Redis portu 6379 olarak Docker Compose ile çalışır.
4. AI Scoring: Model Chain-of-Thought sırasıyla nesnel score_breakdown (tech_fit, seniority_fit, domain_fit, location_fit) ve ardından match_score üretir.
5. Failover: Gemini ve Groq API anahtarları tek havuzdadır; 429/503 hatasında otomatik sonraki anahtara geçer.

Lütfen önce ortamı (git status, go version, python3 --version, docker info) salt okunur olarak kontrol et ve sistemin hazır olduğunu Türkçe olarak kısaca bildir.
```
