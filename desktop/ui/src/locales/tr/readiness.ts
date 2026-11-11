// Turkish dictionary for the repository setup checklist. Must mirror
// en/readiness.ts keys exactly.
export const readiness = {
  title: "Bu projeyi hazırla",
  intro: "Ajanların bu projede iyi çalışabilmesi için eksik olanlar, mantıklı sırayla. İhtiyacın olmayanı atlayabilirsin.",
  progress: "{total} adımın {done}'i tamam",
  skippedCount: "{count} atlandı",
  complete: "Bu proje hazır.",
  show: "Kontrol listesini göster",
  hide: "Gizle",
  expand: "Kontrol listesini aç",
  collapse: "Kontrol listesini kapat",
  skip: "Atla",
  unskip: "Atlamayı geri al",
  doIt: "Yap",
  open: "Aç",
  install: "Kur",
  viewTask: "{key} görevine git",
  taskOpen: "{key} görevi sürüyor.",
  skippedNote: "Atlandı.",
  waitingFor: "Bekleniyor: {step}.",
  started: "Başlatıldı.",
  taskOpened: "{key} görevi panoda açıldı.",
  allStarted: "{runs} işlem başlatıldı, {tasks} görev açıldı.",
  actionFailed: "Bu yapılamadı",
  runAll: "Hepsini sırayla çalıştır",
  runAllTitle: "Hepsi sırayla çalıştırılsın mı?",
  runAllTasks: "Panoda {count} görev açılacak; her biri öncekini bekler.",
  runAllRuns: "{count} işlem görevsiz, hemen başlayacak.",
  runAllCost: "Her görev ajanlarının zamanını kullanır.",
  runAllConfirm: "Hepsini çalıştır",
  status: {
    done: "Tamam",
    todo: "Yapılacak",
    running: "Sürüyor",
    blocked: "Önceki adım bekleniyor",
    skipped: "Atlandı",
    unknown: "Kontrol edilmedi",
  },
  missing: {
    readme: "README",
    agent_guide: "ajan rehberi",
  },
  steps: {
    scan: {
      title: "Projeyi tara",
      why: "Tarama, diğer her adımın okuduğu bileşenleri, komutları ve CI'ı bulur.",
    },
    toolchain: {
      title: "Bu yığın için araçları kur",
      why: "Ajanlar bu bilgisayarda derleyip test eder; çağırdıkları derleyicilerin burada olması gerekir.",
    },
    index: {
      title: "Kodu indeksle",
      why: "İndeks, ajanların her şeyi okumak yerine doğru dosyaları bulmasını sağlar.",
    },
    guide: {
      title: "README ve ajan rehberi ekle",
      why: "Ajanlar her göreve bu dosyalarla başlar; en çok zamanı ve hatayı bunlar kurtarır.",
    },
    tests: {
      title: "Testleri çalıştırılabilir yap",
      why: "Geçen bir test koşusu, senin ve ajanların bir değişikliğin bir şeyi bozmadığını anlama yoludur.",
    },
    ci: {
      title: "Kontrolleri CI'da çalıştır",
      why: "CI, ana dala girmeden önce her değişiklikte derlemeyi ve testleri yineler.",
    },
    delivery: {
      title: "Değişikliklerin nasıl yayına gideceğini seç",
      why: "Teslimat hedefi, biten bir değişikliğin nereye gittiğini ve nasıl doğrulanacağını TaskTrooper'a söyler.",
    },
    observability: {
      title: "Canlıda izle",
      why: "Metrikler, loglar ve izler bir sürümün bir şeyi bozduğunu sana söyler.",
    },
    findings: {
      title: "Kod sağlığı bulgularını gözden geçir",
      why: "Tarama, erken düzeltmesi ucuz olan debug ayarlarını, gizli anahtarları ve eksik loglamayı işaretler.",
    },
    design: {
      title: "Tasarım sistemini kur",
      why: "Tasarım sistemi, yeni ekranları mevcut ekranlarla tutarlı tutar.",
    },
    store: {
      title: "Mağaza uygulamasını bağla",
      why: "App Store veya Google Play uygulamasını bağlamak, TaskTrooper'ın build göndermesini ve yorumları izlemesini sağlar.",
    },
  },
  detail: {
    scan: {
      no_scan: "Henüz taranmadı.",
      scan_failed: "Son tarama başarısız oldu.",
      no_components: "Tarama hiç bileşen bulamadı.",
      running: "Taranıyor…",
      done: "{cmp} bileşen bulundu.",
    },
    toolchain: {
      unknown: "Gerekenler: {languages}.",
      no_bridge: "Bu bilgisayarın araçlarını yalnızca masaüstü uygulaması kontrol edebilir.",
      missing: "Bu bilgisayarda eksik: {tools}.",
      installed: "Bu yığının ihtiyacı olan her şey kurulu.",
    },
    index: {
      not_indexed: "Henüz indekslenmedi.",
      failed: "Son indeksleme başarısız oldu.",
      unavailable: "Kod indeksi kullanılamıyor.",
      running: "{total} dosyanın {processed}'i.",
      done: "{files} dosya indekslendi.",
    },
    guide: {
      missing: "Eksik: {missing}.",
      unreadable: "Çalışma kopyası okunamıyor.",
      done: "README ve ajan rehberi bulundu.",
    },
    tests: {
      gaps: "Test komutu olmayanlar: {components}.",
      done: "Her bileşenin bir test komutu var.",
    },
    ci: {
      no_ci: "CI iş akışı bulunamadı.",
      done: "{checks} CI kontrolü bulundu.",
    },
    delivery: {
      gaps: "Onaylı teslimatı olmayanlar: {components}.",
      done: "Yayına çıkan her bileşenin bir teslimat hedefi var.",
    },
    observability: {
      gaps: "Eksik olanlar: {components}.",
      done: "Eksik bulunmadı.",
    },
    findings: {
      open: "{count} açık ({high} yüksek önem).",
      done: "Açık bulgu yok.",
    },
    design: {
      none: "Henüz tasarım sistemi yok.",
      unavailable: "Tasarım sistemi okunamadı.",
      done: "Bir tasarım sistemi var.",
    },
    store: {
      no_app: "Bağlı mağaza uygulaması yok.",
      unavailable: "Mağaza uygulamaları okunamadı.",
      done: "Bir mağaza uygulaması bağlı.",
    },
  },
};
