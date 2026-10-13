---
name: safe-change
description: >-
  Projede güvenli, kontrollü ve doğrulanabilir kod veya konfigürasyon değişiklikleri yapmak için bu skill'i kullanın.
  Mevcut değişiklikleri kontrol etme, plan sunma, küçük adımlarla değişiklik uygulama, diff inceleme ve testleri çalıştırma adımlarını içerir.
---

# Safe Change Skill

Bu skill, Job Hunter projesinde kod veya konfigürasyon değişikliklerinin güvenli, küçük adımlarla, çalışma ağacını bozmadan ve testlerle doğrulanarak yapılmasını sağlar.

## Temel Kurallar

1. **Çalışma Ağacını ve Kullanıcı Değişikliklerini Koruma:**
   - Değişiklik yapmadan önce çalışma dizinini kontrol et. Kullanıcının mevcut değişikliklerini asla ezme veya silme.
   - `git status` komutunu çalıştır. Eğer komut başarısız olursa (örneğin `.git` dizini bulunamadı hatası verirse), Git deposu varmış gibi davranma (`git diff`, `git checkout` veya commit komutlarını çalıştırma). Bu durumu kullanıcıya bildir ve dosya zaman damgaları (LastWriteTime) ile yerel değişiklik takibi yap.
2. **Önceden Açıklama Zorunluluğu:**
   - Herhangi bir dosyaya dokunmadan önce, hangi dosyalara neden dokunulacağını kullanıcıya Türkçe olarak bildir.
3. **Kapsam ve Mimari Disiplini:**
   - Yalnızca hedeflenen değişikliğe odaklan; gereksiz refactor veya bağımlılık eklemelerinden kaçın.
   - Doğrulanmamış paket yolları veya dosya adları uydurma; projedeki mevcut yapıyı (`cmd/orchestrator/`, `internal/`, `scraper/`) esas al.
4. **Güvenlik ve Gizlilik:**
   - `.env` içerisindeki gizli değerleri, API anahtarlarını veya tokenları asla kaynak kod dosyalarına yazma.

---

## Değişiklik İş Akışı

### Adım 1: Durum Tespiti
- Çalışma ağacını kontrol et:
  - Git deposu aktif ise: `git status` ile izlenmeyen/değişen dosyaları listele.
  - Git deposu aktif değilse: Değiştirilecek dosyaların mevcut durumunu oku ve yedekliliğini doğrula.
- Değiştirilecek dosyaların mevcut bağımlılıklarını ve çağrı zincirini incele.

### Adım 2: Planlama ve Onay Alma
- Kapsamlı veya birden fazla bileşeni etkileyen işlerde (Go servisleri, Python scraper veya altyapı) doğrudan kod yazmaya başlama.
- Adım adım uygulanacak planı kullanıcıya sun ve onayını bekle.

### Adım 3: Küçük ve Odaklı Uygulama
- Değişiklikleri minimal tutarak ilgili dosyalara uygula.
- Mevcut kod açıklamalarını, docstring'leri ve biçimlendirmeyi koru.

### Adım 4: Değişiklikleri Doğrulama (Diff İncelemesi)
- Yapılan değişiklikleri kontrol et:
  - Git aktif ise: `git diff` ile satır satır değişiklikleri incele.
  - Git aktif değilse: Düzenlenen dosyayı tekrar görüntüleyerek sadece beklenen satırların değiştiğinden ve istenmeyen geçici dosyaların oluşmadığından emin ol.

### Adım 5: Derleme ve Test Doğrulaması
Windows PowerShell üzerinde ilgili servis testlerini fiilen çalıştır:
- **Go Bileşenleri İçin:**
  ```powershell
  go build ./...
  go test ./...
  ```
- **Python Bileşenleri İçin:**
  Projedeki mevcut `.venv` sanal ortamı üzerinden testleri veya sözdizimi kontrollerini yürüt:
  ```powershell
  .\.venv\Scripts\python.exe -m unittest discover -s scraper
  ```
- **Kesin Kural:** Testler çalıştırılmadan ve çıktısı doğrulanmadan asla testlerin geçtiğini veya kodun hatasız çalıştığını iddia etme.

### Adım 6: Raporlama
İşlem tamamlandığında kullanıcıya:
- Değiştirilen dosyaları,
- Çalıştırılan derleme ve test komutlarını,
- Test sonuçlarını ve varsa kalan açık noktaları bildir.
