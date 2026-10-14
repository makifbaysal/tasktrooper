// Turkish dictionary for code-health findings. Must mirror
// en/findings.ts keys exactly.
export const findings = {
  intro: "Her taramada koddan okunur: anti-pattern'ler, eksik ya da hatalı yapılandırılmış loglama, production'a giden debug ayarları, gizli anahtarlar ve fazlası. Kabul ettiğiniz ya da false positive olarak işaretlediğiniz bulgular sonraki taramalarda kapalı kalır.",
  allComponents: "Tüm bileşenler",
  repositoryWide: "Depo geneli",
  openCount: "{count} açık",
  allCategories: "Tüm kategoriler",
  occurrences: "{count} yerde",
  lines: "{count} satır",
  moreLocations: "ve {count} yer daha",
  showLocations: "Konumları göster",
  hideLocations: "Konumları gizle",
  createTask: "Task aç",
  createTaskSelected: "{count} bulgu için task aç",
  selectAll: "Tümünü seç",
  clearSelection: "Temizle",
  accept: "Kabul et",
  falsePositive: "False positive",
  reopen: "Yeniden aç",
  openTask: "{key} task'ını aç",
  reason: "Gerekçe: {reason}",
  resolvedHint: "Son tarama bunu artık bulmuyor.",
  updateFailed: "Bulgu güncellenemedi",
  componentSummary: {
    open: "{count} açık kod sağlığı bulgusu",
    none: "Açık kod sağlığı bulgusu yok",
    view: "Bulguları gör",
  },
  status: {
    open: "Açık",
    accepted: "Kabul edildi",
    false_positive: "False positive",
    resolved: "Çözüldü",
  },
  severity: {
    high: "Yüksek",
    medium: "Orta",
    low: "Düşük",
  },
  category: {
    security: "Güvenlik",
    configuration: "Konfigürasyon",
    logging: "Loglama",
    error_handling: "Hata yönetimi",
    observability: "Gözlemlenebilirlik",
    reliability: "Güvenilirlik",
    quality: "Kod kalitesi",
  },
  empty: {
    noScanTitle: "Henüz taranmadı",
    noScanDescription: "Bulgular deponun ilk taramasından sonra görünür.",
    cleanTitle: "Sorun bulunmadı",
    cleanDescription: "Son tarama burada düzeltilecek bir şey bulmadı.",
    filtered: "Bu filtreye uyan bulgu yok.",
  },
  verdict: {
    acceptTitle: "Riski kabul et",
    acceptDescription: "Bulgu kapanır ve kod hâlâ içerse bile sonraki taramalarda kapalı kalır.",
    falsePositiveTitle: "False positive olarak işaretle",
    falsePositiveDescription: "Taramanın burada yanıldığını belirtin. Bulgu sonraki taramalarda kapalı kalır.",
    reasonLabel: "Gerekçe (isteğe bağlı)",
    reasonPlaceholder: "Burada neden sorun olmadığı",
    save: "Kaydet",
  },
  task: {
    title: "Task aç",
    description: "{count} bulguyu kapsayan tek bir board task'ı. Agent her konumu ve her birinin nasıl düzeltileceğini alır.",
    titleLabel: "Başlık",
    notesLabel: "Agent için notlar (isteğe bağlı)",
    notesPlaceholder: "Agent'ın önce bilmesi gerekenler",
    submit: "Task aç",
    created: "{key} task'ı açıldı",
    failed: "Task açılamadı",
    defaultTitleMany: "{component} içindeki {count} kod sağlığı bulgusunu düzelt",
    severalComponents: "birden çok bileşen",
  },
  rules: {
    hardcoded_secret: {
      title: "Kodda gizli anahtar var",
      description: "Bilinen biçimde bir anahtar ya da token depoda duruyor. Önce yenileyin, sonra konfigürasyondan okuyun.",
    },
    hardcoded_credential: {
      title: "Koda gömülü kimlik bilgisi",
      description: "Bir parola, anahtar ya da token konfigürasyondan okunmak yerine sabit değer olarak yazılmış.",
    },
    committed_env_file: {
      title: "Gizli bilgi içeren env dosyası git'te",
      description: "Gerçek gizli bilgiler içeren bir .env dosyası izleniyor ya da git tarafından yok sayılmıyor.",
    },
    tls_verification_disabled: {
      title: "TLS doğrulaması kapalı",
      description: "Sertifika veya host adı kontrolü kapalı; trafik araya girilerek okunabilir.",
    },
    cleartext_traffic_allowed: {
      title: "Şifresiz HTTP'ye izin var",
      description: "Uygulama tüm hostlar için şifresiz HTTP'ye izin veriyor.",
    },
    sql_injection_risk: {
      title: "Olası SQL enjeksiyonu",
      description: "Bir değer parametre olarak geçilmek yerine SQL metnine ekleniyor.",
    },
    dynamic_code_execution: {
      title: "Dinamik kod çalıştırma",
      description: "eval, exec ya da güvensiz deserialization girdi ne derse onu çalıştırır.",
    },
    unsafe_html_injection: {
      title: "Kaçışsız HTML",
      description: "Ham HTML temizlenmeden render ediliyor; bu XSS'e kapı açar.",
    },
    permissive_cors: {
      title: "CORS her kaynağa açık",
      description: "Herhangi bir web sitesi bu API'yi ziyaretçinin tarayıcısından çağırabilir.",
    },
    insecure_token_storage: {
      title: "Token'lar düz depolamada",
      description: "Token veya parolalar localStorage, UserDefaults ya da benzerinde tutuluyor.",
    },
    http_body_logging: {
      title: "İstek gövdesi veya header'ları loglanıyor",
      description: "Gövde ve header'lar token ve kişisel verileri loglara taşır.",
    },
    error_details_exposed: {
      title: "Hata ayrıntıları istemciye gidiyor",
      description: "Stack trace'ler veya geliştirici hata sayfaları kullanıcıya ulaşıyor.",
    },
    public_source_maps: {
      title: "Herkese açık source map'ler",
      description: "Production build orijinal kaynak kodu yayınlıyor.",
    },
    debug_mode_in_production: {
      title: "Production'da debug modu",
      description: "Production konfigürasyonu debug ya da development modunu açıyor.",
    },
    debug_log_level_in_production: {
      title: "Production'da debug log seviyesi",
      description: "Production konfigürasyonu debug veya trace seviyesinde logluyor.",
    },
    debuggable_release_build: {
      title: "Debug edilebilir release build",
      description: "Android release build'ine herkes debugger bağlayabilir.",
    },
    release_minification_disabled: {
      title: "Release build küçültülmüyor",
      description: "Release build için R8/ProGuard kapalı.",
    },
    unconditional_debug_logging: {
      title: "Logger koda debug olarak sabitlenmiş",
      description: "Log seviyesi ortamdan bağımsız olarak kodda debug'a ayarlanmış.",
    },
    console_logs_in_production: {
      title: "Production'da debug console çıktısı",
      description: "console.log/debug/info çağrıları bundle'a giriyor ve build bunları temizlemiyor.",
    },
    mobile_debug_prints: {
      title: "Release build'de debug print'leri",
      description: "print, NSLog ya da Log.d debug koruması dışında çalışıyor.",
    },
    print_instead_of_logger: {
      title: "Loglama yerine print",
      description: "Servis kodu logger yerine stdout'a yazıyor.",
    },
    print_stack_trace: {
      title: "Stack trace loglanmıyor, yazdırılıyor",
      description: "printStackTrace ya da print_exc loglama hattını atlıyor.",
    },
    no_structured_logging: {
      title: "Loglama kütüphanesi yok",
      description: "Serviste logger yok; seviyeli, yapılandırılmış log üretilmiyor.",
    },
    no_request_logging: {
      title: "İstek loglaması yok",
      description: "HTTP servisi istekleri (method, status, süre) loglamıyor.",
    },
    catch_without_logging: {
      title: "Hatalar loglanmadan yutuluyor",
      description: "Yakalanan hatalar ne loglanıyor, ne yeniden fırlatılıyor, ne raporlanıyor.",
    },
    empty_catch_block: {
      title: "Boş hata yakalayıcı",
      description: "Boş bir catch, except ya da err bloğu o yoldaki tüm hataları gizler.",
    },
    bare_except: {
      title: "Çıplak except",
      description: "except: KeyboardInterrupt ve SystemExit'i de yakalar.",
    },
    no_error_boundary: {
      title: "React error boundary yok",
      description: "Tek bir render hatası tüm arayüzü boşaltır ve hiçbir şey raporlanmaz.",
    },
    no_crash_reporting: {
      title: "Crash veya hata raporlaması yok",
      description: "Kullanıcıların production'da yaşadığı hatalar görünmez kalıyor.",
    },
    no_health_endpoint: {
      title: "Health endpoint'i yok",
      description: "Probe'lar ve deploy kontrolleri servisin ayakta olup olmadığını bilemez.",
    },
    http_client_without_timeout: {
      title: "Timeout'suz HTTP çağrıları",
      description: "Yavaş bir bağımlılık istekleri süresiz bekletebilir.",
    },
    force_unwrap: {
      title: "Çok sayıda force unwrap",
      description: "try!, as! ve !! beklenmeyen bir nil'de uygulamayı çökertir.",
    },
    mutable_default_argument: {
      title: "Değiştirilebilir varsayılan argüman",
      description: "list veya dict varsayılanı çağrılar arasında paylaşılır.",
    },
    debug_leftovers: {
      title: "Kodda kalmış debug ifadeleri",
      description: "debugger, breakpoint'ler veya var_dump/dd kodda kalmış.",
    },
    oversized_source_file: {
      title: "Çok büyük kaynak dosyalar",
      description: "1.500 satırı aşan dosyaları incelemek ve değiştirmek zor.",
    },
    typescript_any_overuse: {
      title: "Çok sayıda any / @ts-ignore",
      description: "Tip kontrolü birçok yerde devre dışı.",
    },
    tech_debt_markers: {
      title: "Çok sayıda TODO/FIXME",
      description: "Hiçbir yerde takip edilmeyen bilinen sorunlar.",
    },
    no_automated_tests: {
      title: "Otomatik test yok",
      description: "Bileşende kaynak kod var ama test dosyası yok.",
    },
    no_linter_configured: {
      title: "Linter yapılandırılmamış",
      description: "Stil kaymaları ve yaygın hata kalıpları denetlenmiyor.",
    },
    wildcard_import: {
      title: "Wildcard import",
      description: "from x import * isimlerin nereden geldiğini gizler.",
    },
  },
};
