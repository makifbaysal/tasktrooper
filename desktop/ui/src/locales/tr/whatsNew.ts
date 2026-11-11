// "Yenilikler" penceresinin Türkçe sözlüğü; yapı en/whatsNew.ts ile aynı olmalı.
export const whatsNew = {
  dialogTitle: "TaskTrooper {version} sürümünde yenilikler",
  seeAll: "Tüm değişiklikleri gör",
  gotIt: "Anladım",
  card: {
    title: "Yenilikler",
    help: "TaskTrooper'ın son sürümünde nelerin değiştiğini gör.",
    open: "Yenilikleri göster",
  },
  v0_3_15: {
    title: "Ajanlar doğru rehberi, görevler bir ajanı, Docker komutları buluyor",
    items: {
      agentsMd: "Proje kurulumundaki ajan rehberi görevi artık tüm ajan CLI'ları için tek bir AGENTS.md yazıyor ve projede zaten olan README'yi istemiyor.",
      assignTasks: "Tarama ya da bir ajan tarafından açılan görevlere oluşturulurken uygun bir ajan atanıyor; PM da bir görevin atananını yeniden oluşturmadan yerinde ayarlayıp düzeltebiliyor.",
      dockerCommands: "Konteynerli bileşenler Docker komutları (buildkit, imaj oluşturma, compose başlat, kaldır ve durdur), registry, platform ve etiket ayarları, ajanlar için yalnızca konteynerde çalıştırma kuralı kazandı; her komut da kaldırılabiliyor.",
    },
  },
  v0_3_14: {
    title: "Audience araştırması artık telefonunda",
    items: {
      phoneAudience: "TaskTrooper mobil uygulamasından audience'ları yönet: PM ile ya da bir formla oluştur, kur ve içindeki her kişiyle birebir konuş.",
      phoneStudies: "Çalışmaları telefonundan çalıştır: türü seç, görüşme senaryosunu gözden geçirip onayla, görüşmeleri takip et ve raporu oluştur.",
      phoneReports: "Çalışma raporlarını telefonunda oku ve tek bir HTML dosyası olarak paylaş.",
    },
  },
  v0_3_13: {
    title: "Telefonun, hedef kitlen ve telemetrin",
    items: {
      tunnel: "Ayarlar → Uzaktan erişim'den Cloudflare veya ngrok'a giriş yap; TaskTrooper mobil uygulamasıyla kendi kontrol ettiğin bir tünel üzerinden bu bilgisayara bağlan. Telefonu tek adımda eşle: Telefonu eşle, QR kodu ve adresin olduğu bir pencere açar.",
      audience: "Hedef kitle araştırması: PM ajanından sentetik kişiler iste ya da çalışmayı kendin yürüt - görüşmeler, açılış sayfası testi, mesaj testi veya A/B testi - raporu oku ve tek bir HTML dosyası olarak dışa aktar.",
      telemetry: "Operasyonlar'daki yeni Telemetri ekranları servisleri, uç noktaları, izleri, logları, hataları, olayları, monitörleri, dağıtımları ve SLO'ları gösterir; açılan olayları release mühendisi ele alır.",
      repoSetup: "Bir depoyu içe aktarmak artık \"Bu projeyi hazırla\" kontrol listesini açar (tarama, araçlar, indeks, README ve ajan rehberi, testler, CI, teslimat); her adımın bir düğmesi ve hepsini sırayla çalıştıran bir düğme vardır. Hesabından çıkış yapmak artık hesabı tamamen temizler; \"hesapsız çalışıyor\" uyarısı ekranda takılı kalmaz.",
      runTrace: "Her ajan çalıştırması artık görev çekmecesinde gerçek bir iz bırakıyor: ne okudu, neyi değiştirdi, ne çalıştırdı; git ile doğrulanmış. Önizlemeden sonra tek tıkla, bir yedek dalın arkasında çalıştırmanın işi geri alınabiliyor.",
      reviewHistory: "Her görev artık inceleme geçmişini taşıyor: her inceleme ve QA turu, kimin incelediği, ne bulduğu ve bulguların akıbeti, sütunlar arası geri dönüşler ve her inceleyenin neyi kapsayıp neyi kapsamadığı. \"İncelemenizi bekliyor\" kartı, inceleyenlerin ve sunucunun bir kişinin bakmasını istediği yerleri listeler.",
    },
  },
};
