# AGENTS.md — Job Hunter

## 1. İletişim
- Açıklamaları, analizleri ve önemli kod yorumlarını Türkçe yap.
- Teknik terimleri gerektiğinde İngilizce kullan.
- Dosya değiştirmeden önce hangi dosyalara neden dokunacağını kısaca açıkla.

## 2. Çalışma Ortamı
- İşletim sistemi Windows. Terminal komutlarını etkin kabuğa (PowerShell/CMD) uygun yaz.
- Linux komutlarını Windows terminalinde varsayma.
- Projede Go, Python, PostgreSQL ve Redis/Docker kullanılıyor. Gerçek mimariyi ve mevcut dosyaları incelemeden varsayım yapma.
- Var olan Python sanal ortamını, dependency manager'ı, Docker Compose tanımlarını ve proje scriptlerini tespit et; dosya yollarını kendin uydurma.

## 3. Değişiklik Disiplini
- Önce ilgili kodu ve çağrı zincirini incele; gereksiz refactor yapma.
- Değişiklikten önce `git status` ile çalışma ağacını kontrol et. Mevcut kullanıcı değişikliklerini silme veya ezme.
- Büyük veya birden fazla bileşeni etkileyen işlerde önce plan sun, ardından küçük ve doğrulanabilir adımlarla ilerle.
- Yalnızca istenen kapsamı değiştir. Yeni bağımlılık veya altyapı eklemeden önce gerekçesini açıkla.

## 4. Güvenlik ve Veri Koruma
- API anahtarlarını, tokenları, şifreleri veya hassas `.env` değerlerini çıktılara ya da Git commit'lerine dahil etme.
- Salt okunur inceleme ile veri değiştiren işlemleri birbirinden ayır.
- `docker compose down -v`, volume silme, `DROP`, `TRUNCATE`, yıkıcı migration, `git reset --hard`, `git clean` ve force push gibi yıkıcı işlemleri açık onay olmadan çalıştırma.
- İnceleme amacıyla veritabanı veya Redis verisini değiştirme; servisleri yeniden başlatma veya veri temizleme işlemini otomatik yapma.

## 5. Doğrulama
- Kod değişikliğinden sonra ilgili testleri ve kontrolleri gerçekten çalıştır.
- Go değişikliklerinde uygun olduğunda `go build ./...` ve `go test ./...` kullan.
- Python testlerini projenin mevcut sanal ortamı ve test düzeni üzerinden çalıştır.
- Testleri çalıştırmadan başarılı olduklarını iddia etme.
- İş bitiminde değişen dosyaları, çalıştırılan komutları, sonuçları ve kalan sorunları kısaca raporla.

## 6. Görev Kapsamı
- Görev yalnızca inceleme ise dosya veya veri değiştirme.
- Belirsizlikte mevcut koddan kanıt topla; varsayımları açıkça belirt.
- Her görevde önce en küçük, güvenilir çözümü tercih et; gereksiz mimari değişikliklerden kaçın.
