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
  v0_3_20: {
    title: "Operasyonlar'da TaskTrooper Telemetry, FigCraft'ta çizen tasarımcı, deploy işaretleri",
    items: {
      operationsSource: "Ayarlar'da TaskTrooper Telemetry'yi bağla, kendiliğinden bir Operasyonlar veri kaynağı olur: her bileşen için RED grafikleri, loglar, trace'ler, hata kayıtları ve endpoint'ler.",
      agentTools: "Ajanlar rollerine gereken telemetri araçlarını alıyor: endpoint kök neden analizi, SLO'lar, monitörler ve yük testleri. Yalnızca bir ajanın oluşturduğu monitör ve KPI'ları değiştirip silebilirler.",
      deployMarkers: "Canlıya çıkan her deploy, bağlı telemetride commit'iyle işaretlenir; bir gerileme onu getiren sürümle yan yana görünür.",
      figcraftDesigner: "ui-designer artık FigCraft'ta çiziyor: her tasarım görevi tasarım sisteminin üzerine kurulu, düzenlenebilir kendi dosyasını alır; sayfalarını görevden önizleyebilir, FigCraft'ta açabilir ya da Finder'da gösterebilirsin. FigCraft, VectorCraft ve PhotoCraft araçları Ayarlar → Geliştirme ortamı'ndan kurulur.",
      telemetryScreens: "Yeni telemetri ekranları: genel bakış, servis haritası, altyapı, uygulamalar, profiling, LLM kullanımı ve yük testleri.",
    },
  },
  v0_3_19: {
    title: "Test edip bitiren QA, repo bazlı otomasyon testleri, her göreve ayrı önizleme",
    items: {
      qaManualRound: "QA artık kodu derlemek yerine çalışan ürünü görevin kabul kriterlerine göre test ediyor; build ve unit testler code review'da kalıyor.",
      qaFinishes: "Bir QA turu çalıştırılmamış test case'i ya da kararsız kriter bırakarak bitemez; kayıtlı bir hata olmadan görevi geri gönderemez.",
      automationTests: "Bir repo için Otomasyon testleri'ni aç (Bileşenler → Review): QA her görevin case'lerini qa-automation/ altına Testcontainers + WireMock testi olarak ekler, görev QA'dan ancak bu suite yeşilken çıkar. Açınca reponun eksik testlerini yazan tek bir görev açılır.",
      taskPreviews: "Her görev kendi portunda kendi yerel önizlemesini alır; bir görevi taşımak yalnızca o görevin önizlemesini durdurur.",
    },
  },
  v0_3_18: {
    title: "Ajanlarla sesli konuş, her yerde ekle ve etiketle, daha sade görev planları",
    items: {
      voiceChat: "Ajanlarla Türkçe ya da İngilizce sesli konuş: cevap yazıldıkça okunur, çizimler ekranda görünür ve konuşarak araya girebilirsin. Ses modelini ve sesi Ayarlar → Ses'ten seç.",
      attachAndMention: "Ana sayfada, üstteki hızlı sorma kutusunda ve görev yorumlarında dosya ve resim yapıştırıp ekleyebilir; ajanları, projeleri ve repoları @ ile etiketleyebilirsin.",
      notifications: "Bildirim merkezi her bildirimi gönderenin adı ve avatarıyla, senin dilinde gösterir; kenar çubuğundaki sayaç gerçekten okunmamışları sayar.",
      onePerFeature: "PM bir özellik, repo ve rol için çok sayıda küçük görev yerine tek görev açar; her özellik bir kez geliştirilir, incelenir ve yayınlanır.",
      releaseBatch: "Release engineer onaylaman için toplu bir yayın önerebilir ve bir bileşenin nasıl deploy edildiğini adım adım sorar.",
    },
  },
  v0_3_17: {
    title: "Önce hosted preview, ajanların görebildiği ekran görüntüleri, QA ping-pong'u yok",
    items: {
      hostedPreview: "Bir görevin pull request'inde hazır bir hosted preview (Vercel) varsa preview paneli lokal çalıştırma başlatmak yerine önce onu açıyor.",
      chatImages: "Ajan sohbetine eklediğin ekran görüntüleri artık Claude Code, opencode, Cursor ve Antigravity üzerinde çalışan ajanlara ulaşıyor; içindekini okuyabiliyorlar.",
      qaBlocked: "QA bir şeyi ortamda eksik olduğu için (entegrasyon, credential ya da cihaz) doğrulayamazsa görev QA ile geliştirici arasında gidip gelmek yerine seni bekliyor; QA değişmeden geri gelen, zaten reddettiği kodu tekrar test etmiyor.",
    },
  },
  v0_3_16: {
    title: "İşten önce audience testi, karşılaştıran tekrar çalıştırmalar, eksiksiz görevler",
    items: {
      studyRerun: "Ürünü değiştirdikten sonra biten bir çalışmayı tekrar çalıştır ve neyin değiştiğini gör: karar puanı, kaybolan ya da yeni çıkan itirazlar ve her kişinin değişimi.",
      pmAudienceTest: "Bir isteğin başarısı insanların tepkisine bağlıysa (fiyat, ücretli özellik, metin, onboarding) PM işi açmadan önce audience testi öneriyor, planı değiştirip yeniden test etmene izin veriyor ve sen deyince görevleri açıyor.",
      serialReleases: "Sürümler her bileşen için sırayla çıkıyor; aynı servisin iki dağıtımı artık çakışmıyor.",
      completeTasks: "Ajanların açtığı görevler her zaman açıklama, kabul kriterleri ve bir atanan içeriyor; boş ya da atanmamış görev reddediliyor ve ajan düzeltiyor.",
      chatThinking: "Ajan çalışırken düşünmesi önce yanıt gibi görünmek yerine doğrudan düşünme adımlarına akıyor; sistem mimarı teknik soruları koda bakarak yanıtlıyor.",
      mascot: "Yeni maskot Trooper boş ekranlarda, kurulumda ve ajan avatarlarında.",
    },
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
