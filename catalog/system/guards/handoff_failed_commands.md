---
key: guard.handoff_failed_commands
version: 1
inputs: [Failed]
---
Otomatik code_review geçişi yapılmadı ve görev revizyona (need_revision) geri gönderildi: bu run komut çalıştırdı ama hiçbiri başarıyla bitmedi ({{.Failed}} run_terminal çağrısı hata verdi ya da izin verilmedi). Kırmızı biten bir build/test ya da reddedilen bir komut doğrulama sayılmaz. Değişiklik branch'te duruyor. Bir sonraki run projenin build ve test komutlarını yeniden çalıştırıp yeşil çıktıyı okumalı. pkill ve killall bu makinede engelli — başlattığın sunucuyu portundan durdur (macOS/Linux: `lsof -ti :<port> | xargs kill`; Windows: Host machine bölümündeki port komutu).
