import type { SettingsPagesDict } from "@/locales/en/settingsPages";

// Turkish dictionary for the settings sub-pages. Typed as SettingsPagesDict —
// must mirror en/settingsPages.ts keys exactly. Text moved verbatim from the
// original page sources.
export const settingsPages: SettingsPagesDict = {
  integrations: {
    title: "Entegrasyonlar",
    description: "TaskTrooper'ın senin adına giriş yaptığı dış hesaplar. Bir kez kaydedilir, tüm projeler kullanır.",
    loadFailed: "Mağaza kimlik bilgileri okunamadı",

    // Kimlik bilgisi kasasının eskiden durduğu repo deploy sayfasında görünür;
    // oraya giden #store-credentials bağlantısı hâlâ gerçek bir yere düşsün diye.
    movedTitle: "Mağaza kimlik bilgileri",
    movedBody: "Tüm çalışma alanı için bir kez kaydedilir ve artık Ayarlar → Entegrasyonlar altında duruyor.",
    movedLink: "Entegrasyonlar'ı aç",

    asc: {
      title: "App Store Connect",
      description: "App Store Connect → Users and Access → Integrations altından alınan API anahtarı. Her iOS sürümünü imzalar ve yükler.",
    },
    play: {
      title: "Google Play Console",
      description: "Play Console'a bağlı Google Cloud projesinden alınan servis hesabı anahtarı. Her Android sürümünü yükler ve terfi ettirir.",
    },

    stores: {
      connectFirst: "Erişilebilen uygulamaları görmek için önce bir kimlik bilgisi kaydet.",
      listApps: "Uygulamaları listele",
      appsTitle: "Bu hesaptaki uygulamalar",
      retry: "Listelemeyi tekrar dene",
      loading: "Mağaza konsolu okunuyor…",
      loadFailed: "Uygulamalar listelenemedi",
      notConnectedTitle: "Bu mağaza henüz bağlı değil",
      notConnectedBody:
        "Uygulamaları listeleyebilmek için önce Entegrasyonlar sayfasından mağaza hesabını bağla.",
      notConnectedAction: "Entegrasyonlar'a git",
      emptyTitle: "Henüz uygulama yok",
      emptyBody: "Kimlik bilgisi çalışıyor ama hesapta seçilecek bir uygulama yok.",
      unavailableTitle: "Bu hesap listelenemiyor",
      unavailableBody:
        "Play Developer API'de uygulamaları listeleyen bir uç yok — o liste yalnızca ayrı Reporting API'den gelir ve bu servis hesabının oraya erişimi olmayabilir. Geri kalan her şey çalışır; uygulama bunun yerine elle yazılır.",
    },

    picker: {
      title: "Mağaza uygulamasını seç",
      description: "Bu repoyu bağlı mağaza hesabındaki bir uygulamaya bağla.",
      appsLabel: "Bu hesaptaki uygulamalar",
      manualLabel: "Uygulama tanımlayıcısı",
      manualPlaceholder: "com.example.app",
      manualHint: "Bundle ID ya da paket adı — mağaza konsolunda göründüğü gibi.",
      link: "Uygulamayı bağla",
      linking: "Bağlanıyor…",
      linked: "Uygulama bağlandı",
      linkFailed: "Uygulama bağlanamadı",
    },

    gcloud: {
      title: "Google Cloud",
      description:
        "Servislerinin çalıştığı Google Cloud projesi için salt-okunur bir servis hesabı. TaskTrooper Cloud Run servislerini ve GKE kümelerini bununla okur; bununla hiçbir şey deploy edilmez.",
      statusUnavailable: "Bağlantı durumu okunamadı.",
      configured: "Bağlı",
      notConfigured: "Bağlı değil",
      updated: "{date} tarihinde kaydedildi",
      accountLabel: "Servis hesabı",
      projectLabel: "Proje",
      identityHint:
        "Açık metin olarak tutulan tek şey bu ikisi; bu kart hangi bağlantının kayıtlı olduğunu söyleyebilsin diye. Anahtarın kendisi şifreli saklanır ve bir daha gösterilmez.",
      jsonLabel: "Servis hesabı JSON'u",
      jsonPlaceholder: "{ \"type\": \"service_account\", … }",
      replaceHint:
        "Kayıtlı anahtar geri okunmaz: her kayıt onu tümüyle değiştirir ve Google Cloud'un kabul etmediği bir anahtar saklanmaz.",
      save: "Anahtarı kaydet",
      saved: "Servis hesabı kaydedildi",
      delete: "Kaldır",
      deleteTitle: "Bu servis hesabı kaldırılsın mı?",
      deleteDescription:
        "Yeni bir anahtar kaydedilene kadar Cloud Run servisleri ve GKE kümeleri okunamaz olur. Google Cloud tarafında hiçbir şey değişmez.",
      deleted: "Servis hesabı kaldırıldı",
      loadFailed: "Google Cloud bağlantısı okunamadı",
    },

    gcloudResources: {
      connectFirst: "Nelere erişilebildiğini görmek için önce bir servis hesabı kaydet.",
      list: "Kaynakları listele",
      title: "Bu projedeki kaynaklar",
      retry: "Listelemeyi tekrar dene",
      loading: "Google Cloud okunuyor…",
      loadFailed: "Kaynaklar listelenemedi",
      notConnectedTitle: "Google Cloud henüz bağlı değil",
      notConnectedBody:
        "Bir Cloud Run servisi ya da GKE kümesi seçebilmek için önce Ayarlar → Entegrasyonlar altından bir servis hesabı kaydet.",
      notConnectedAction: "Entegrasyonlar'a git",
      unavailableTitle: "Bu servis hesabı kaynakları listeleyemiyor",
      unavailableBody:
        "Anahtar çalışıyor ama bu projeyi listeleme yetkisi yok. Geri kalan her şey çalışır; kaynak adı bunun yerine elle yazılır.",
      emptyTitle: "Henüz seçilecek bir şey yok",
      emptyBody:
        "Servis hesabı bu projeyi okuyabiliyor ama projede ne Cloud Run servisi ne de GKE kümesi var.",
      cloudRunTitle: "Cloud Run servisleri",
      cloudRunBadge: "Cloud Run",
      gkeTitle: "GKE kümeleri",
      gkeBadge: "GKE",
      familyEmpty: "Bu projede yok.",
      familyUnavailableTitle: "Bu anahtarla listelenemiyor",
      cloudRunUnavailableBody:
        "Bu servis hesabının Cloud Run servislerini listeleme yetkisi yok. Projede roles/run.viewer vermek yeterli — GKE için gösterilenler bundan etkilenmez.",
      gkeUnavailableBody:
        "Bu servis hesabının GKE kümelerini listeleme yetkisi yok. Projede roles/container.viewer vermek yeterli — yukarıdaki Cloud Run servisleri bundan etkilenmez.",
      unreachableLocations: "Bazı bölgeler yanıt vermedi: {locations}",
      workloadsTitle: "Workload listesi bu bağlantı üzerinden alınamıyor",
      workloadsBody:
        "Küme adları, sürümleri ve node havuzları Google Cloud API'sinden gelir ve yukarıda görünür. Kümenin içinde ne çalıştığı gelmez: onu okumak kümenin kendi control plane'iyle konuşmayı gerektirir ve private bir control plane yalnızca kendi VPC'nizin içinden yanıt verir — paylaşımlı TaskTrooper sunucusu orada değil. Bu bağlantının bir sınırı, bir arızası değil.",
    },

    gcloudPicker: {
      title: "Google Cloud kaynağını seç",
      description: "Bu repoyu bağlı projedeki bir Cloud Run servisine ya da GKE kümesine bağla.",
      typeLabel: "Kaynak tipi",
      typeCloudRun: "Cloud Run servisi",
      typeGke: "GKE kümesi",
      manualLabel: "Kaynak adı",
      manualPlaceholder: "projects/my-project/locations/europe-west1/services/api",
      manualHint: "Google Cloud'da göründüğü haliyle tam kaynak adı — proje ve konum dahil.",
      bind: "Kaynağı bağla",
      binding: "Bağlanıyor…",
      bound: "Kaynak bağlandı",
      bindFailed: "Kaynak bağlanamadı",
    },

    vercelPicker: {
      title: "Vercel projesini seç",
      description: "Bu repoyu bağlı Vercel hesabındaki bir projeye bağla.",
      projectsLabel: "Bu hesaptaki projeler",
      loading: "Vercel okunuyor…",
      loadFailed: "Projeler listelenemedi",
      retry: "Listelemeyi tekrar dene",
      notConnectedTitle: "Vercel henüz bağlı değil",
      notConnectedBody: "Bir proje seçebilmek için önce Ayarlar → Entegrasyonlar altından Vercel hesabını bağla.",
      notConnectedAction: "Entegrasyonlar'a git",
      unavailableTitle: "Bu bağlantı projeleri listeleyemiyor",
      unavailableBody:
        "Token çalışıyor ama bu kapsamdaki projeleri listeleme yetkisi yok. Proje kimliği bunun yerine elle yapıştırılır.",
      emptyTitle: "Henüz proje yok",
      emptyBody: "Bağlantı çalışıyor ama bu Vercel kapsamında seçilecek bir proje yok.",
      manualLabel: "Proje kimliği",
      manualPlaceholder: "prj_…",
      manualHint: "Vercel panosu → proje → Settings → General → Project ID.",
      link: "Projeyi bağla",
      linking: "Bağlanıyor…",
      linked: "Proje bağlandı",
      linkFailed: "Proje bağlanamadı",
    },
  },
  llm: {
    loadFailed: "LLM sağlayıcıları yüklenemedi",
    updateFailed: "Güncellenemedi",

    endpointsTitle: "OpenAI-uyumlu Endpoint'ler",
    endpointsDesc:
      "Kendi IP'niz, Ollama, LM Studio, vLLM, OpenRouter, Groq… İstediğiniz kadar isimli endpoint ekleyin.",
    addEndpoint: "Endpoint ekle",
    endpointsEmpty: "Henüz endpoint yok. “Endpoint ekle” ile başlayın.",

    badgeDefault: "Varsayılan",
    badgeConnected: "Bağlı",
    badgeNotConnected: "Bağlı değil",
    urlLabel: "URL:",
    modelLabel: "Model:",
    apiKeyLabel: "API anahtarı:",
    apiKeyStored: "kayıtlı",
    timeoutLabel: "Zaman aşımı:",
    secondsValue: "{seconds} sn",
    edit: "Düzenle",
    makeDefault: "Varsayılan yap",
    delete: "Sil",

    nativeTitle: "Yerel Sağlayıcılar (Gemini · Anthropic)",
    reconnect: "Yeniden bağlan",
    connect: "Bağlan",
    disconnectShort: "Kes",

    cliTitle: "Yerel Ajan CLI'ları",
    cliDesc:
      "Bu sağlayıcılar görevi bir sunucuya değil, runner makinenizde açılan bir CLI oturumuna verir. API anahtarı ya da adres istemezler. “Bağla” dediğinizde komutun kurulu ve oturumunun açık olduğu doğrulanır, sonra açık olan bütün ajanların rolü, kuralları ve skill'leri o CLI'nin okuduğu düzende diske yazılır.",
    badgeLocalCli: "Yerel CLI",
    badgeComingSoon: "Yakında",
    cliComingSoonHint:
      "Bu CLI'yi çalıştıracak executor henüz yazılmadı; bu yüzden bir ajana atanamıyor.",
    cliConnecting: "Kuruluyor…",
    cliConnectedToast: "CLI bağlandı, ajan kataloğu kuruldu",
    cliDisconnectedToast: "CLI bağlantısı kaldırıldı",
    cliBinaryLabel: "Komut:",
    cliInstalledLabel: "Kurulan:",
    cliInstalledValue: "{agents} ajan · {skills} skill",
    cliCatalogLabel: "Katalog:",
    cliCatalogHint:
      "Bu klasör incelemeniz için tutulan bir kopyadır. Her görev, kendi ajanının kurallarını ve skill'lerini veritabanından kendi çalışma klasörüne yazar; bu yüzden buradaki kopya eskise bile koşuları etkilemez.",
    cliSwapHint: "Aynı anda yalnızca bir yerel CLI bağlı olabilir — bunu bağlarsanız diğerinin bağlantısı kalkar.",

    claudeCode: {
      stepInstalling: "claude doğrulanıyor ve ajan kataloğu kuruluyor…",
      stepDisconnectingCli: "CLI bağlantısı kaldırılıyor…",
      preflight: {
        title: "Ortam",
        refresh: "Yenile",
        loadFailed: "Ortam kontrol listesi yüklenemedi",
        empty: "Henüz kontrol edilecek bir şey yok",
        blocking: "\"{item}\" düzeltilene kadar Bağlan devre dışı",
        copyCommand: "Komutu kopyala",
        copied: "Kopyalandı",
      },
    },

    testSuccess: "Bağlantı başarılı",
    testFailed: "Bağlantı testi başarısız",
    connectedToast: "{provider} bağlandı",
    connectFailed: "Bağlantı kurulamadı",
    defaultUpdated: "Varsayılan sağlayıcı güncellendi",
    activateFailed: "Aktivasyon başarısız",
    disconnectedToast: "Bağlantı kaldırıldı",
    disconnectFailed: "Bağlantı kesilemedi",
    nameUrlRequired: "İsim ve Base URL zorunludur",
    endpointUpdated: "Endpoint güncellendi",
    endpointAdded: "Endpoint eklendi",
    saveFailed: "Kaydedilemedi",
    endpointDeleted: "Endpoint silindi",
    deleteFailed: "Silinemedi",

    deleteEndpointTitle: "Endpoint'i sil",
    deleteEndpointDesc:
      "“{name}” endpoint'i silinecek. Bu endpoint'i kullanan ajanlar oturum varsayılanına düşer. Devam edilsin mi?",

    connectDialogTitle: "{provider} bağlantısı",
    baseUrlLabel: "Base URL",
    apiKeyFieldLabel: "API anahtarı",
    apiKeyChangePlaceholder: "Değiştirmek için yeni anahtar girin",
    apiKeyBlankHint: "Boş bırakırsanız kayıtlı anahtar kullanılır.",
    timeoutFieldLabel: "İstek zaman aşımı (saniye)",
    timeoutHint:
      "Yerel modeller için 300–600 sn önerilir. Planner ve intake gibi büyük istekler daha uzun sürebilir.",
    test: "Test et",

    endpointDialogEditTitle: "Endpoint düzenle",
    endpointDialogAddTitle: "Endpoint ekle",
    presetLabel: "Hazır ayar",
    nameLabel: "İsim",
    namePlaceholder: "ör. Ev sunucusu / Ollama",
    defaultModelLabel: "Varsayılan model (opsiyonel)",
    defaultModelPlaceholder: "Ajan bazında da seçilebilir",
    apiKeyOptionalLabel: "API anahtarı (opsiyonel)",
    add: "Ekle",

    presetLmStudio: "LM Studio",
    presetOllama: "Ollama",
    presetOpenAI: "OpenAI",
    presetGroq: "Groq",
    presetOpenRouter: "OpenRouter",
    presetGeminiOpenAI: "Gemini (OpenAI uyumlu)",
    presetCustom: "Özel / kendi IP",
  },
  usage: {

    inputColumn: "Girdi",
    outputColumn: "Çıktı",
    modelColumn: "Model",
    callsColumn: "Çağrı",

    usageLoadFailed: "Kullanım verisi alınamadı",
    lastNDays: "Son {days} gün",
    llmCalls: "LLM Çağrısı",
    inputTokens: "Girdi Token",
    outputTokens: "Çıktı Token",
    byModel: "Modele Göre",
    noUsage: "Bu aralıkta kayıtlı kullanım yok.",
    daily: "Günlük",
  },
  board: {
    loadFailed: "Yüklenemedi",
    invalidName: "Geçersiz ad",
    duplicateName: "Bu isimde bir kolon zaten var",
    savedToast: "Board workflow kaydedildi",
    saveFailed: "Kayıt başarısız",

    title: "Board Workflow",
    subtitle:
      "İş akışı kolonları ve aralarındaki geçiş kuralları. Backlog sabittir.",
    addColumn: "Kolon Ekle",

    columnsTitle: "Kolonlar",
    columnsSubtitle:
      "İş akışı kolonları. Ad'dan teknik değer (slug) otomatik üretilir.",
    backlogLabel: "Backlog",
    fixedTag: "sabit",
    deleteColumn: "Kolonu sil",
    noColumns: "Henüz iş akışı kolonu yok.",

    transitionsTitle: "Geçiş kuralları",
    transitionsSubtitle:
      "Her kolon için, oradan taşınabilecek hedef kolonları seçin. Hiç seçim yoksa o kolondan her yere geçilebilir.",
    freeToAnywhere: "her yere serbest",

    newColumnTitle: "Yeni Kolon",
    columnNameLabel: "Kolon adı",
    columnNamePlaceholder: "Ör. Kod İncelemesi",
    slugPrefix: "slug:",
    add: "Ekle",
  },

  analizAssignment: {
    title: "Analiz Görev Atama",
    subtitle:
      "Backend, frontend veya mobile alanı için oluşturulan analiz görevinin hangi agent'a otomatik atanacağı.",
    loadFailed: "Yüklenemedi",
    savedToast: "Atama kaydedildi",
    saveFailed: "Kayıt başarısız",
    discardedToast: "Atama kaydedilmedi",

    areas: {
      backend: "Backend",
      frontend: "Frontend",
      mobile: "Mobile",
    },

    missingToolsTitle: "Eksik tool'lar",
    missingToolsBody:
      "Seçilen agent'ta analiz iş akışının ihtiyaç duyduğu bazı tool'lar eksik. Eklenip atama kaydedilsin mi?",
    grantTools: "Evet, ekle",
  },

};
