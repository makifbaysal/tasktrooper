import type { TelemetryDict, TelemetryKindsDict } from "@/locales/en/opsTelemetry";

// Turkish strings for the console's telemetry views; mirrors en/opsTelemetry.ts key for key.
export const telemetry: TelemetryDict = {
  table: {
    readAt: "{time} okundu",
    readAtTitle: "Okunma zamanı: {time}. Sunucu bir okumayı 30 saniye saklar; bu yüzden sayfanın son yenilenmesinden en fazla 30 sn daha eski olabilir.",
    window: "Son {span}",
    windowUnknown: "Kaynağın kendi yakın dönem penceresini kapsar",
    windowDocs: "Tablo pencereleri",
    rows: "{count} satır",
    flag: "İşaret",
    sortBy: "{column} sütununa göre sırala",
    truncated: "Kaynakta gönderdiğinden daha fazla satır var: ilk {count} satır gösteriliyor.",
    showAll: "{count} satırın hepsini göster",
    noRows: "Kaynakta şu an bunun için bir kayıt yok.",
    noValue: "Değer yok",
    unnamedSource: "Veri kaynağı silinmiş",
    recipe: "Tarif: {recipe}",
    customQuery: "Özel sorgu",
    loadFailed: "Bu tablo okunamadı",
    refreshFailed: "Yenilenemedi, son yanıt gösteriliyor",
    errors: {
      failed: "Bu tablo okunamadı",
      needsParams: "Bağlamada bir parametre eksik",
      needsParamsBody: "Tablonun, bağlamasında henüz olmayan bir değere ihtiyacı var.",
      notFound: "Veri kaynağı artık yok",
      notFoundBody: "Bu tablonun okunduğu veri kaynağı artık mevcut değil. Başka bir kaynak bağlayın.",
      unsupported: "Bu kaynak satırları listeleyemiyor",
      unsupportedBody: "Bu tür kaynak sinyali zaman içinde okuyabilir ama her öğe için ayrı bir satır veremez. Bu tabloyu içeren bir tarif bunu sağlar.",
      badQuery: "Kaynak tablonun sorgusunu reddetti",
      timeout: "Kaynak zamanında yanıt vermedi",
      timeoutBody: "Meşgul olabilir. Birazdan tekrar deneyin.",
      upstream: "Kaynak yanıt vermedi",
      openSignals: "Sinyaller'i aç",
      retry: "Tekrar dene",
    },
  },
  endpoints: {
    title: "Uç noktalar",
    hint: "Her tablo kaynağının kendi yakın dönem penceresini kapsar; dakikada bir okunur",
    scopeNote: "Her uç nokta tablosu, kaynağının tabloda gösterilen kendi yakın dönem penceresini kapsar; zaman aralığı burada geçerli değil.",
    flags: {
      errors: "En yüksek hata oranı",
      slowest: "En yavaş p95",
    },
    empty: {
      title: "Henüz uç nokta tablosu yok",
      body: "{name} bileşeninin uç noktaları (rota başına istek hacmi, hata oranı ve p95), tarifi bunları listeleyen bir kaynaktan gelir: Prometheus'taki OpenTelemetry metrikleri, New Relic ya da Datadog. Böyle bir kaynağın sinyallerinden birini Sinyaller'de bağlayın.",
      signals: "Sinyaller'i aç",
      addSource: "Veri kaynağı ekle",
    },
  },
  pages: {
    scopeNote: "Her web tablosu, kaynağının tabloda gösterilen kendi yakın dönem penceresini kapsar; zaman aralığı burada geçerli değil.",
    hint: "Her tablo, web analitiği kaynağının kendi yakın dönem penceresini kapsar",
    tabsLabel: "Web tabloları",
    tabs: {
      pages: "Sayfalar",
      referrers: "Yönlendirenler",
      countries: "Ülkeler",
      devices: "Cihazlar",
    },
    empty: {
      title: "Henüz {table} tablosu yok",
      body: "{name} için {table} tablosu, Sinyaller'de bağlanan bir web analitiği kaynağından gelir: PostHog, Google Analytics 4 ya da Plausible.",
      signals: "Sinyaller'i aç",
      addSource: "Veri kaynağı ekle",
    },
  },
  crashes: {
    scopeNote: "Her çökme tablosu, kaynağının tabloda gösterilen kendi yakın dönem penceresini kapsar; zaman aralığı burada geçerli değil.",
    openIssue: "Kendi konsolunda aç",
    issues: {
      title: "Çökme ve ANR grupları",
      hint: "Google Play ve Sentry'deki hata grupları, en çok kullanıcıyı etkileyenler önce",
    },
    signatures: {
      title: "Tanılama imzaları",
      hint: "App Store Connect'in en yeni derleme için bildirdiği takılma, açılış ve disk yazma imzaları",
    },
    types: {
      crash: "Çökme",
      anr: "ANR",
      non_fatal: "Ölümcül olmayan",
      hangs: "Takılma",
      launches: "Açılış",
      disk_writes: "Disk yazma",
      fatal: "Ölümcül",
      error: "Hata",
      warning: "Uyarı",
    },
    empty: {
      title: "Henüz çökme verisi yok",
      body: "{name} bileşeninin çökmeleri Google Play vitals, App Store Connect ya da Sentry'den gelir. Sinyaller'de öneri bulun: bağlı bir mağaza uygulaması kaynağını tek adımda sunar.",
      signals: "Sinyaller'i aç",
      addSource: "Veri kaynağı ekle",
    },
  },
  traces: {
    minDuration: "En az süre (ms)",
    minDurationHint: "Yalnızca en az bu kadar süren trace'ler. Aramak için Enter'a basın ya da kutudan çıkın.",
    manage: "Trace bağlamasını yönet",
    loadFailed: "Trace'ler aranamadı",
    refreshFailed: "Trace'ler yenilenemedi, son yanıt gösteriliyor",
    capped: "En yeni {count} trace gösteriliyor. Gerisini görmek için aralığı daraltın ya da en az süreyi artırın.",
    openFromLog: "Trace'i aç",
    clamped: {
      title: "Aralığın son 7 günü aranıyor",
      body: "Bir trace araması en fazla 7 günü kapsar, bu yüzden arama {since} itibarıyla başlıyor.",
    },
    unbound: {
      title: "Bir trace kaynağı bağlayın",
      body: "{name} bileşeninin trace'leri Grafana Tempo gibi bir tracing altyapısından gelir. Trace'lerini bir Tempo kaynağına bağlayın ya da önce bir tane ekleyin.",
      bind: "Trace'leri bağla",
      addSource: "Veri kaynağı ekle",
    },
    empty: {
      title: "Bu aralıkta trace yok",
      body: "Trace kaynağında bu bileşen için {since} tarihinden bu yana hiç trace yok.",
      withMin: "Bu aralıkta {min} ms ya da daha uzun süren trace yok.",
      clearMin: "En az süreyi temizle",
    },
    list: {
      label: "Trace'ler",
      count: "{count} trace",
      failedCount: "{count} tanesi hatalı",
      order: "Sıralama",
      orders: {
        newest: "Önce en yeni",
        slowest: "Önce en yavaş",
      },
      columns: {
        start: "Başlangıç",
        root: "Kök span",
        duration: "Süre",
        spans: "Span'lar",
        status: "Durum",
      },
      open: "{name} trace'ini aç",
      noRoot: "Kök span bulunamadı",
      noService: "Bilinmeyen servis",
      error: "Hata",
      ok: "OK",
    },
    detail: {
      back: "Trace'lere dön",
      untitled: "Trace",
      copy: "Trace ID'yi kopyala",
      copied: "Trace ID kopyalandı",
      duration: "Süre",
      spans: "Span'lar",
      errorCount: "Hatalar",
      services: "Servisler",
      serviceSpans: "{count} span",
      serviceErrors: "{count} başarısız",
      viewLabel: "Trace görünümü",
      views: {
        waterfall: "Şelale",
        table: "Tablo",
      },
      hint: "Özniteliklerini görmek için bir span seçin.",
      noSpans: "Kaynak bu trace'i span'ları olmadan gönderdi.",
      refreshFailed: "Trace yeniden okunamadı, son yanıt gösteriliyor",
      truncated: {
        title: "Span'ların hepsi gösterilmiyor",
        body: "Trace'te tek bir yanıtın taşıyabileceğinden fazla span var; ilk {count} tanesi gösteriliyor.",
      },
      elsewhere: {
        title: "Başka bir bileşen üzerinden bulundu",
        body: "Bu bileşenin trace bağlaması yok, bu yüzden trace ortamdaki bütün trace kaynaklarında arandı ve {name} üzerinden bulundu.",
        action: "{name} bileşeninde aç",
      },
      errors: {
        notFound: "Trace bulunamadı",
        notFoundBody: "Bu ortamın hiçbir trace kaynağında {id} trace'i yok. Kaynağın sakladığı süreden daha eski olabilir.",
        unbound: "Bakılacak trace kaynağı yok",
        unboundBody: "Bu ortamda hiçbir bileşenin trace bağlaması yok, bu yüzden {id} trace'i aranamıyor.",
        timeout: "Trace kaynağı zamanında yanıt vermedi",
        timeoutBody: "Meşgul olabilir. Birazdan tekrar deneyin.",
        failed: "Trace okunamadı",
      },
    },
    waterfall: {
      span: "Span",
      listLabel: "Span'lar",
      tableLabel: "Tablo olarak span'lar",
      rowLabel: "{service} {name}, +{offset} anında başlar, {duration} sürer",
      failed: "başarısız",
      noService: "Bilinmeyen servis",
      noName: "Adsız span",
      expand: "{name} altındaki span'ları göster",
      collapse: "{name} altındaki span'ları gizle",
      columns: {
        depth: "Derinlik",
        service: "Servis",
        operation: "İşlem",
        start: "Başlangıç anı",
        duration: "Süre",
        status: "Durum",
      },
      status: {
        unset: "Belirtilmemiş",
        ok: "OK",
        error: "Hata",
      },
    },
    span: {
      close: "Span'ı kapat",
      duration: "Süre",
      offset: "Başlangıç anı",
      started: "Başlangıç zamanı",
      id: "Span ID",
      parent: "Üst span",
      copy: "Span ID'yi kopyala",
      copied: "Span ID kopyalandı",
      attributes: "Öznitelikler",
      resource: "Kaynak",
      noAttributes: "Öznitelik yok.",
      statuses: {
        ok: "OK",
        error: "Hata",
      },
      kinds: {
        server: "Sunucu",
        client: "İstemci",
        internal: "Dahili",
        producer: "Üretici",
        consumer: "Tüketici",
      },
    },
  },
  webVitals: {
    title: "Core Web Vitals",
    hint: "Aralıktaki sayfa yüklemelerinin en son p75 değeri, Google'ın eşiklerine göre",
    traffic: "Trafik",
    bands: {
      good: "İyi",
      needsImprovement: "İyileştirilmeli",
      poor: "Kötü",
    },
    quantileP75: "p75",
    thresholds: "{good} değerine kadar iyi, {poor} üstü kötü",
    meterLabel: "{band}: {good} değerine kadar iyi, {poor} üstü kötü",
    bind: "Veri kaynağı bağla",
    noData: "Bu aralıkta veri yok",
    readFailed: "Okunamadı",
    latest: "Aralıktaki son değer",
    live: "Anlık değer, geçmişi yok",
  },
  mobileVitals: {
    window: "Seçilen aralıktan bağımsız, {env} ortamında son 7 gün.",
    asOf: "{date} itibarıyla",
    bind: "Sinyaller'de bağla",
    noData: "Son 7 günde veri yok",
    notYet: "Henüz yayınlanmadı",
    playLag: "Google Play'in verileri 1–2 gün geriden gelir; en yeni günler sonradan dolar.",
    readFailed: "Okunamadı",
  },
  offers: {
    title: "Bağlanabilecek veri kaynakları",
    found: "Bağlanabilecek {count} veri kaynağı",
    bound: "{name} üzerinden {count} sinyal bağlandı",
    titles: {
      playVitals: "Bu uygulama için Google Play vitals verilerini bağlayın",
      appStoreMetrics: "Bu uygulama için App Store metriklerini bağlayın",
      generic: "Bu uygulama için bir {kind} kaynağı bağlayın",
    },
    bodies: {
      playVitals: "Çökme yaşamayan kullanıcılar, çökme oranı, ANR oranı ve en sık çökme grupları; çalışma alanının Google Play kimlik bilgisiyle okunur. Kurulacak bir şey yok.",
      appStoreMetrics: "Uygulama sürümüne göre açılış süresi ve her derlemenin takılma, açılış ve disk yazma imzaları; çalışma alanının App Store Connect anahtarıyla okunur. Kurulacak bir şey yok.",
      generic: "Bu uygulamanın bu sinyallerini bir {kind} kaynağı okuyabilir.",
    },
    credentialMissing: {
      google_play: "Önce Google Play kimlik bilgisini kaydedin.",
      asc: "Önce App Store Connect anahtarını kaydedin.",
    },
    saveCredential: "Ayarlar → Entegrasyonlar",
    connect: "Bağla",
    connecting: "Bağlanıyor…",
    notNow: "Şimdi değil",
    recipe: "Tarif: {recipe}",
  },
  binding: {
    noPreviewTitle: "Bu sinyalin önizlemesi yok",
    noPreview: "{signal} grafiğe dökülecek bir seri değil: loglar Loglar sayfasında, trace'ler Trace'ler sekmesinde, hata grupları da Hata kutusunda okunur. Bağlamak için kaydedin.",
  },
  errors: {
    providers: {
      sentry: "Sentry",
    },
    fromProvider: "{provider} kaynağından okundu",
    noTask: "Bu grup, görev açılacak bir veri kaynağı belirtmiyor. Bunun yerine {provider} içinde açın.",
    openSource: "{provider} veri kaynağı",
    fixSource: "Veri kaynağını aç",
  },
  sources: {
    storeCredentials: {
      google_play: "Google Play hizmet hesabı",
      asc: "App Store Connect API anahtarı",
    },
    storeCredentialWorkspace: "çalışma alanı için kayıtlı",
    storeCredentialPlaceholder: "{credential} seçin",
    noStoreCredential: "Henüz kayıtlı bir {credential} yok.",
    saveStoreCredential: "Ayarlar → Entegrasyonlar'dan kaydedin",
    namePlaceholderStore: "Boş bırakılırsa türün adı kullanılır",
    errors: {
      store_credential: {
        not_found: "Bu kimlik bilgisi henüz kayıtlı değil. Önce Ayarlar → Entegrasyonlar'dan kaydedin.",
        wrong_provider: "Bu kimlik bilgisi diğer mağazaya ait.",
        not_permitted: "Bu kurulumda mağaza kimlik bilgileri veri kaynakları için kullanılamıyor.",
      },
      org: {
        invalid: "Organizasyonun kısa adını sentry.io/organizations/<slug>/ adresindeki gibi girin.",
      },
      index: {
        invalid: "Bir indeks, data stream ya da desen girin, örneğin logs-*; birden fazlaysa virgülle ayırın.",
      },
    },
  },
};

const AUTH_OPTIONS = {
  none: "Yok",
  bearer: "Bearer token",
  basic: "Basic (kullanıcı adı ve parola)",
};

export const telemetryKinds: TelemetryKindsDict = {
  google_play_vitals: {
    label: "Google Play vitals",
    description:
      "Google Play'den Android vitals verileri (çökme ve ANR oranları, yavaş açılışlar ve en sık çökme grupları), çalışma alanının Google Play kimlik bilgisiyle okunur. Kurulacak bir şey yok; Play bu verileri bir iki gün geç yayınlar.",
    queryPlaceholder: "ör. {\"metric_set\": \"crashRateMetricSet\", \"metric\": \"userPerceivedCrashRate\", \"as\": \"percent\"}",
    queryHint:
      "JSON: {\"package\"?, \"metric_set\", \"metric\", \"dimension\"?, \"filter\"?, \"period\"?, \"as\"?}. metric_set crashRateMetricSet, anrRateMetricSet, slowStartRateMetricSet ya da errorCountMetricSet olabilir; period daily (varsayılan) ya da hourly; as value, percent ya da free_percent. Günlük veriler 1–2 gün geriden gelir.",
    fields: {
      store_credential: {
        label: "Google Play kimlik bilgisi",
        help: "Ayarlar → Entegrasyonlar → Google Play altında kaydedilen hizmet hesabı. Google Cloud projesinde Play Developer Reporting API'nin etkin olması, Play Console'da da uygulama bilgilerini görüntüleme izninin olması gerekir.",
      },
      package_name: {
        label: "Paket adı",
        help: "Sorgu bir uygulama belirtmediğinde okunan uygulama. Bir bileşenin bağlaması bunu reposunun bağlı Android uygulamasından doldurur.",
        placeholder: "com.example.app",
      },
    },
  },
  app_store_metrics: {
    label: "App Store Connect metrikleri",
    description:
      "Bir iOS uygulamasının açılış süresi, takılmaları, bellek kullanımı, disk yazmaları ve pil tüketimi, Xcode Organizer'ın gösterdiği gibi uygulama sürümü başına; ayrıca her derlemenin takılma ve açılış tanılamaları. Çalışma alanının App Store Connect anahtarıyla okunur. Apple çökme yaşamayan kullanıcı oranı bildirmez.",
    queryPlaceholder: "ör. {\"metric\": \"launchTime\", \"percentile\": \"p90\"}",
    queryHint:
      "JSON: {\"app\"?, \"metric\", \"percentile\"?, \"device\"?, \"version\"?}. metric bir Xcode Organizer metriği (launchTime, hangRate, …) ya da bütün bir kategori (launch, hang, memory, disk, battery) olabilir; percentile p50 ya da p90. Değerler uygulama sürümü başına gelir ve basamak olarak çizilir.",
    fields: {
      store_credential: {
        label: "App Store Connect kimlik bilgisi",
        help: "Ayarlar → Entegrasyonlar → App Store Connect altında kaydedilen API anahtarı.",
      },
      app_id: {
        label: "Uygulama kimliği",
        help: "Sorgu bir uygulama belirtmediğinde okunan uygulama: App Store Connect'teki kimliği. Bir bileşenin bağlaması bunu reposunun bağlı iOS uygulamasından doldurur.",
        placeholder: "1234567890",
      },
    },
  },
  sentry: {
    label: "Sentry",
    description:
      "Sentry'den çökme yaşamayan kullanıcılar ve oturumlar, hata sayıları ve hata grupları; mobil, web ve backend projeleri için aynı şekilde. Hata grupları Hata kutusuna da düşer.",
    queryPlaceholder: "ör. {\"project\": \"ios-app\", \"metric\": \"crash_free_users\"}",
    queryHint:
      "JSON: {\"project\", \"metric\"?, \"environment\"?, \"release\"?, \"search\"?, \"days\"?}. metric events (varsayılan), event_rate, crash_free_users, crash_free_sessions, crash_rate, crash_rate_users, sessions ya da users olabilir; çökme metrikleri SDK'da oturum takibinin açık olmasını gerektirir.",
    fields: {
      url: {
        label: "Sentry URL'si",
        help: "Sentry'nin bulutu için https://sentry.io (AB bölgesindeki bir organizasyon için https://de.sentry.io) ya da kendi barındırdığınız Sentry'nin adresi.",
        placeholder: "https://sentry.io",
      },
      org: {
        label: "Organizasyon",
        help: "sentry.io/organizations/<slug>/ adresindeki gibi organizasyonun kısa adı.",
        placeholder: "acme",
      },
      token: {
        label: "Auth token",
        help: "org:read, project:read ve event:read kapsamlarına sahip bir token: bir organizasyon token'ı ya da bir dahili entegrasyonun token'ı. Şifrelenerek saklanır.",
      },
    },
  },
  loki: {
    label: "Loki",
    description: "Grafana Loki ya da Grafana Cloud Logs: bir bileşenin log satırları ve LogQL ile bunlardan sayılan hata oranları.",
    queryPlaceholder: "ör. {service_name=\"api\"} |= \"error\"",
    queryHint:
      "LogQL. Bir log sorgusu ({service_name=\"api\"} |= \"timeout\") Loglar sayfasını besler; bir metrik sorgusu (sum(rate({service_name=\"api\"} | detected_level=\"error\" [5m]))) grafik çizer.",
    fields: {
      url: {
        help: "/loki/api/v1 olmadan Loki'nin temel URL'si. Grafana Cloud için Logs URL'si (https://logs-prod-<n>.grafana.net).",
        placeholder: "http://localhost:3100",
      },
      auth: {
        options: AUTH_OPTIONS,
      },
      username: {
        help: "Grafana Cloud için Logs kullanıcı ID'si.",
      },
      password: {
        help: "Grafana Cloud için logs:read yetkisi olan bir access policy token'ı.",
      },
    },
  },
  elasticsearch: {
    label: "Elasticsearch / OpenSearch",
    description: "Elasticsearch, OpenSearch ya da Elastic Cloud'da tutulan log satırları: alanlara göre filtrelenen tek bir indeks deseni.",
    queryPlaceholder: "ör. {\"filters\": {\"service.name\": \"billing\"}}",
    queryHint: "JSON: {\"filters\": {\"<field>\": \"<value>\", …}}; filtrelerin hepsi eşleşmeli, en fazla 32 filtre. keyword alanında birebir, text alanında her kelime eşleşir.",
    fields: {
      url: {
        help: "Elasticsearch ya da OpenSearch temel URL'si. Elastic Cloud için deployment'ın Elasticsearch uç noktası.",
        placeholder: "http://localhost:9200",
      },
      index: {
        label: "İndeks",
        help: "Logları tutan indeks, data stream ya da desen; birden fazlaysa virgülle ayırın (logs-*,app-*).",
        placeholder: "logs-*",
      },
      time_field: {
        label: "Zaman alanı",
        help: "Satırların sıralandığı ve filtrelendiği tarih alanı. Boş bırakılırsa @timestamp.",
        placeholder: "@timestamp",
      },
      message_field: {
        label: "Mesaj alanı",
        help: "Satırın kendisini tutan alan; metin filtresi bu alanda arar. Boş bırakılırsa message.",
        placeholder: "message",
      },
      level_field: {
        label: "Düzey alanı",
        help: "Log düzeyini tutan alan (ECS'de log.level). Boş bırakılırsa log.level.",
        placeholder: "log.level",
      },
      auth: {
        options: {
          none: "Yok",
          basic: "Basic (kullanıcı adı ve parola)",
          api_key: "API anahtarı",
        },
      },
      api_key: {
        label: "API anahtarı",
        help: "Kodlanmış API anahtarı (id:key'in base64 hâli); anahtar oluşturulurken Kibana'nın gösterdiği biçimde. Şifrelenerek saklanır.",
      },
    },
  },
  tempo: {
    label: "Tempo",
    description: "Grafana Tempo ya da Grafana Cloud Traces: bir bileşenin TraceQL ile aranan trace'leri.",
    queryPlaceholder: "ör. { resource.service.name = \"api\" }",
    queryHint: "TraceQL: bir bileşenin trace'lerinin başladığı span'lar, örneğin { resource.service.name = \"api\" }. Grafik olarak değil, her zaman Trace'ler sekmesinde okunur.",
    fields: {
      url: {
        help: "/api olmadan Tempo HTTP API'sinin temel adresi. Grafana Cloud için /tempo ile biten Traces URL'si.",
        placeholder: "http://localhost:3200",
      },
      auth: {
        options: AUTH_OPTIONS,
      },
      username: {
        help: "Grafana Cloud için Traces kullanıcı ID'si.",
      },
      password: {
        help: "Grafana Cloud için traces:read yetkisi olan bir access policy token'ı.",
      },
    },
  },
  posthog: {
    label: "PostHog",
    description:
      "PostHog'dan sayfa görüntülemeleri, oturumlar, ziyaretçiler, JavaScript hataları ve web vitals değerleri; en çok görüntülenen sayfalar, yönlendirenler, ülkeler ve cihazlarla birlikte. posthog-js dışında kurulacak bir şey yok.",
    queryPlaceholder: "ör. {\"metric\": \"lcp\", \"quantile\": 0.75}",
    queryHint:
      "JSON: {\"metric\"?, \"event\"?, \"quantile\"?, \"host\"?}. metric page_views (varsayılan), sessions, visitors, js_errors, events (saydığı event ile birlikte) ya da bir web vital olabilir: lcp, inp, cls, fcp; quantile verilmezse 0.75. host verilirse yalnızca o host adı okunur.",
    fields: {
      url: {
        label: "PostHog URL'si",
        help: "PostHog Cloud için https://us.posthog.com ya da https://eu.posthog.com, veya kendi barındırdığınız PostHog'un adresi.",
        placeholder: "https://us.posthog.com",
      },
      project_id: {
        label: "Proje kimliği",
        help: "Projenin URL'sindeki (/project/<id>) ya da Project settings altında görünen numara.",
        placeholder: "12345",
      },
      personal_api_key: {
        label: "Kişisel API anahtarı",
        help: "Bu projede Query Read kapsamı olan bir kişisel API anahtarı (phx_…). Şifrelenerek saklanır.",
      },
    },
  },
  ga4: {
    label: "Google Analytics 4",
    description:
      "Bir Google Analytics 4 mülkünden sayfa görüntülemeleri, oturumlar ve aktif kullanıcılar; en çok görüntülenen sayfalar, yönlendirenler, ülkeler ve cihazlarla birlikte. GA4 verileri iki güne kadar işlediği için en yeni noktalar hâlâ artabilir.",
    queryPlaceholder: "ör. {\"metric\": \"page_views\"}",
    queryHint:
      "JSON: {\"metric\"?, \"event\"?, \"host\"?, \"property\"?}. metric page_views (varsayılan), sessions, visitors ya da events (saydığı event ile, GA4'ün eventName değeri) olabilir. property, kaynağınkinden farklı bir mülkü okur.",
    fields: {
      property_id: {
        label: "Mülk kimliği",
        help: "Admin → Property details altındaki numara (G-… ölçüm kimliği değil).",
        placeholder: "123456789",
      },
      auth: {
        label: "Oturum açma yöntemi",
        help: "Bağlı bir Google Cloud hesabı ya da yalnızca bu kaynak için bir hizmet hesabı anahtarı.",
        options: {
          cloud_account: "Bağlı bir Google Cloud hesabı",
          service_account: "Bir hizmet hesabı anahtarı",
        },
      },
      cloud_account_id: {
        label: "Google Cloud hesabı",
        help: "Hesabın hizmet hesabı mülkte Viewer olmalı (Admin → Property access management) ve projesinde Google Analytics Data API etkin olmalı.",
      },
      service_account_json: {
        label: "Hizmet hesabı anahtarı (JSON)",
        help: "Mülkte Viewer olan ve projesinde Google Analytics Data API etkin olan bir hizmet hesabının anahtarı. JSON dosyasının tamamını yapıştırın. Şifrelenerek saklanır.",
      },
    },
  },
  plausible: {
    label: "Plausible",
    description: "Plausible Analytics'ten sayfa görüntülemeleri, ziyaretler ve ziyaretçiler; en çok görüntülenen sayfalar, trafik kaynakları, ülkeler ve cihazlarla birlikte.",
    queryPlaceholder: "ör. {\"metric\": \"visitors\"}",
    queryHint: "JSON: {\"metric\"?, \"host\"?, \"site\"?}. metric page_views (varsayılan), sessions (ziyaretler) ya da visitors olabilir. site, kaynağınkinden farklı bir siteyi okur.",
    fields: {
      url: {
        label: "Plausible URL'si",
        help: "https://plausible.io ya da kendi barındırdığınız Plausible'ın adresi.",
        placeholder: "https://plausible.io",
      },
      site_id: {
        label: "Site",
        help: "Sitenin Plausible'da kayıtlı olduğu alan adı.",
        placeholder: "example.com",
      },
      api_key: {
        label: "Stats API anahtarı",
        help: "Siteye sahip ekibin bir Stats API anahtarı (Account settings → API keys). Şifrelenerek saklanır.",
      },
    },
  },
  vercel: {
    label: "Vercel",
    description: "Vercel projelerinizin sayfa görüntülemeleri, ziyaretçileri ve Core Web Vitals değerleri; bağlı bir Vercel hesabıyla Web Analytics ve Speed Insights'tan okunur. Kurulacak bir şey yok.",
    queryPlaceholder: "örn. {\"metric\": \"lcp\", \"project\": \"prj_…\"}",
    queryHint:
      "JSON: {\"metric\"?, \"project\"}. project, Vercel projesinin kimliğidir (prj_…). metric page_views (varsayılan), visitors, lcp, inp, cls veya fcp olur; bir vital, quantile (0.5, 0.75, 0.9, 0.95 veya 0.99) başka bir şey demedikçe p75'idir. Projede Web Analytics ve Speed Insights açık olmalıdır.",
  },
  datadog: {
    label: "Datadog",
    description: "Datadog'un metrik API'sinden APM istek hızları, hatalar ve gecikme ile host ya da container metrikleri.",
    queryPlaceholder: "ör. sum:trace.http.request.hits{service:api,env:prod}.as_rate()",
    queryHint:
      "Dashboard'da yazıldığı biçimde bir Datadog metrik sorgusu: avg:system.cpu.user{service:web} by {host}. Tablo, her etiket grubunun son değerini okur.",
    fields: {
      site: {
        options: {
          datadoghq_com: "US1 · datadoghq.com",
          us3_datadoghq_com: "US3 · us3.datadoghq.com",
          us5_datadoghq_com: "US5 · us5.datadoghq.com",
          datadoghq_eu: "EU1 · datadoghq.eu",
          ap1_datadoghq_com: "AP1 · ap1.datadoghq.com",
          "ddog-gov_com": "US1-FED · ddog-gov.com",
        },
        label: "Site",
        help: "Datadog URL'nizdeki site: app.datadoghq.com için datadoghq.com, app.datadoghq.eu için datadoghq.eu, us5.datadoghq.com için us5.datadoghq.com.",
      },
      api_key: {
        label: "API anahtarı",
        help: "Organization settings → API keys. Şifrelenerek saklanır.",
      },
      app_key: {
        label: "Uygulama anahtarı",
        help: "Organization settings → Application keys; kapsamı sınırlı bir anahtarın timeseries_query ve metrics_read yetkileri olmalı. Şifrelenerek saklanır.",
      },
    },
  },
  newrelic: {
    label: "New Relic",
    description: "New Relic'ten NRQL ile APM istek hacmi, hatalar ve gecikme, tarayıcı sayfa görüntülemeleri ve web vitals değerleri, mobil çökme oranları.",
    queryPlaceholder: "ör. SELECT percentile(duration * 1000, 95) FROM Transaction WHERE appName = 'api'",
    queryHint:
      "SINCE, UNTIL ya da TIMESERIES olmadan NRQL: bunları tür, aralığa göre kendisi ekler. Tablo bir FACET sorgusudur, her facet için bir satır.",
    fields: {
      region: {
        label: "Bölge",
        help: "Hesabın verilerinin bulunduğu yer: one.eu.newrelic.com için AB, aksi hâlde ABD.",
        options: {
          us: "ABD",
          eu: "AB",
        },
      },
      account_id: {
        label: "Hesap kimliği",
        help: "Hesap seçicide hesap adının altında görünen hesap numarası.",
        placeholder: "1234567",
      },
      api_key: {
        label: "Kullanıcı anahtarı",
        help: "API keys sayfasından, hesapta sorgu çalıştırabilen bir kullanıcının kullanıcı anahtarı (NRAK-…). Şifrelenerek saklanır.",
      },
    },
  },
};
