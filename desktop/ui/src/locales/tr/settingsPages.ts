import type { SettingsPagesDict } from "@/locales/en/settingsPages";

// Turkish dictionary for the settings sub-pages. Typed as SettingsPagesDict;
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
        "Play Developer API'de uygulamaları listeleyen bir uç yok. O liste yalnızca ayrı Reporting API'den gelir ve bu servis hesabının oraya erişimi olmayabilir. Geri kalan her şey çalışır; uygulama bunun yerine elle yazılır.",
    },

    picker: {
      title: "Mağaza uygulamasını seç",
      description: "Bu repoyu bağlı mağaza hesabındaki bir uygulamaya bağla.",
      appsLabel: "Bu hesaptaki uygulamalar",
      manualLabel: "Uygulama tanımlayıcısı",
      manualPlaceholder: "com.example.app",
      manualHint: "Bundle ID ya da paket adı, mağaza konsolunda göründüğü gibi.",
      link: "Uygulamayı bağla",
      linking: "Bağlanıyor…",
      linked: "Uygulama bağlandı",
      linkFailed: "Uygulama bağlanamadı",
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
    cliSwapHint: "Aynı anda yalnızca bir yerel CLI bağlı olabilir; bunu bağlarsanız diğerinin bağlantısı kalkar.",

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

  roles: {
    title: "Roller",
    subtitle:
      "Agent'ların atandığı isimli görevler (developer, analyst, architect, QA, product manager, …) ve şu anda hangi sistem görevinin hangi role bağlı olduğu.",
    loadFailed: "Yüklenemedi",
    saveFailed: "Kayıt başarısız",
    savedToast: "Rol kaydedildi",
    createdToast: "Rol oluşturuldu",
    deletedToast: "Rol silindi",
    deleteFailed: "Silme başarısız",
    assignmentsSavedToast: "Atamalar kaydedildi",
    purposeSavedToast: "Görev kaydedildi",

    newRole: "Yeni rol",
    empty: "Henüz rol yok.",
    agentCount: "{count} agent",
    selectRole: "Düzenlemek için bir rol seçin.",

    detailsTitle: "Detaylar",
    nameLabel: "Ad",
    descriptionLabel: "Açıklama",
    toolsLabel: "Gerekli tool'lar",
    toolsPlaceholder: "satır başına bir tool",
    toolsHelp:
      "Bu role atanan bir agent'ın policy'sinde bu tool'ların hepsi bulunmalı — biri eksikse önce eklenmesi istenir.",
    keyLabel: "Key",
    keyPlaceholder: "ör. developer",
    keyHelp: "Küçük harf, harf/rakam/alt çizgi. Oluşturulduktan sonra değiştirilemez.",
    create: "Oluştur",
    deleteRole: "Rolü sil",
    deleteRoleDescription: "“{name}” kalıcı olarak silinecek. Bu role sahip agent'lar rolü kaybeder.",
    removeAssignment: "Kaldır",

    assignmentsTitle: "Agent atamaları",
    assignmentsSubtitle:
      "Bu rol için hangi agent'ların, hangi alan(lar)da ve birden fazlası uygun olduğunda hangi öncelik sırasıyla seçilebileceği.",
    saveAssignments: "Atamaları kaydet",
    noAssignments: "Henüz atanmış agent yok.",
    addAgent: "Agent ekle…",
    priority: "Öncelik",
    areaAny: "Herhangi bir alan",
    area: {
      backend: "Backend",
      frontend: "Frontend",
      mobile: "Mobile",
    },
    areaCustomPlaceholder: "Alan ekle…",
    addArea: "Ekle",
    removeArea: "Alanı kaldır",

    dutiesTitle: "Sistem görevleri",
    dutiesSubtitle:
      "Rol değil, hook adı: sistem tarafından oluşturulan bir görevi veya repo profilleme çalışmasını şu anda hangi rol yanıtlıyor.",
    dutyNone: "— yok —",
    duty: {
      system_task_assignee: "Sistem tarafından oluşturulan görevler",
      repo_profiler: "Repo profilleme",
    },

    missingToolsTitle: "Eksik tool'lar",
    missingToolsBody:
      "Bir veya daha fazla agent'ta bu rolün gerektirdiği tool'lar eksik. Eklenip atama kaydedilsin mi?",
    grantTools: "Evet, ekle",
  },

  workflows: {
    title: "İş Akışları",
    subtitle: "Görev tipleri ve her birinin geçtiği sıralı, kolon bazlı iş akışı aşamaları.",
    loadFailed: "Yüklenemedi",
    saveFailed: "Kayıt başarısız",
    savedToast: "Görev tipi kaydedildi",
    createdToast: "Görev tipi oluşturuldu",
    deletedToast: "Görev tipi silindi",
    deleteFailed: "Silme başarısız",
    stagesSavedToast: "Aşamalar kaydedildi",
    stagesInvalid: "Kaydetmeden önce bazı aşamaların düzeltilmesi gerekiyor",

    newType: "Yeni görev tipi",
    empty: "Henüz görev tipi yok.",
    defaultTag: "varsayılan",
    selectType: "Düzenlemek için bir görev tipi seçin.",

    detailsTitle: "Detaylar",
    labelLabel: "Etiket",
    prefixLabel: "Key öneki",
    prefixLocked: "Bu tipte görev olduğu sürece önek kilitlidir.",
    prefixPlaceholder: "ör. T",
    isDefaultLabel: "Yeni görevler için varsayılan tip",
    isDefectLabel: "Defekt olarak sayılır (bugs_assigned KPI)",
    deleteType: "Görev tipini sil",
    deleteTypeDescription: "“{label}” kalıcı olarak silinecek. Bu yalnızca görevi yokken çalışır.",

    assigneeModeLabel: "Atama modu",
    assigneeMode: {
      none: "Yok",
      default: "Varsayılan",
      override: "Geçersiz kıl",
    },
    assigneeModeHelp: {
      none: "Bu tipteki yeni görevler istenen atananı (varsa) korur.",
      default: "Rolün agent'ı, yalnızca hiçbir atanan istenmediğinde atanan alanını doldurur.",
      override: "Rolün agent'ı, istenen ne olursa olsun her zaman atanan alanını doldurur.",
    },
    assigneeRoleLabel: "Atanan rol",

    typeBehavioursLabel: "Tip geneli davranışlar",

    keyLabel: "Key",
    cloneFromLabel: "Şuradan kopyala",
    cloneFromNone: "— boş —",
    create: "Oluştur",

    stagesTitle: "Aşamalar",
    stagesSubtitle: "Bu tipin sırayla geçtiği her board kolonu için bir satır. Satırları oklarla taşıyın.",
    saveStages: "Aşamaları kaydet",
    addStage: "Aşama ekle…",
    removeStage: "Aşamayı kaldır",
    moveUp: "Yukarı taşı",
    moveDown: "Aşağı taşı",
    orphanedStage: "Kolon artık yok",
    offPath: "Ana akış dışı",
    kindLabel: "Tür",
    onPathLabel: "Ana akışta",
    behavioursLabel: "Davranışlar",
    behaviourGroup: {
      entry: "Bu kolona girerken",
      exit: "Bir sonrakine geçmek için",
      other: "Diğer",
    },
    behaviour: {
      "dispatch_suspended": { label: "Otomatik başlatma durduruldu", description: "Görev bu kolonda beklerken sistem otomatik olarak bir ajan çalıştırmaz." },
      "route_to_subscribers": { label: "Aboneleri bilgilendir", description: "Bu kolona giren görev, sadece atanan ajanı değil, kolonu dinleyen tüm ajanları uyandırır." },
      "merge_pr_on_enter": { label: "Girişte PR'ı birleştir", description: "Bu kolona girildiğinde görevin pull request'i için birleştirme süreci tetiklenir." },
      "watch_deploy_on_resume": { label: "Devam ederken deploy'u izle", description: "Bu kolona geri dönen görev, canlıya çıkış süreci için izlenir." },
      "block_on_dependencies": { label: "Bağımlılıkta bekle", description: "Görevi engelleyen bitmemiş bir bağımlılık varsa, görev burada bekletilir." },
      "wait_for_ci": { label: "CI'ı bekle", description: "Bu kolona gönderim, pipeline (CI) kapısı açılana kadar bekler." },
      "ensure_pr_on_enter": { label: "Girişte PR aç", description: "Bu kolona girildiğinde, görevin pull request'i henüz yoksa otomatik açılır." },
      "detect_migration_on_enter": { label: "Girişte migration tespit et", description: "Bu kolona girildiğinde, branch'teki değişiklikler şema migration'ı içeriyor mu diye kontrol edilir." },
      "stage_deploy_on_enter": { label: "Girişte stage'e deploy et", description: "Bu kolona girildiğinde, deponun test stratejisine göre bir stage deploy'u tetiklenir." },
      "auto_enter": { label: "Otomatik giriş", description: "Atama veya uyandırma anında sistem görevi doğrudan belirtilen kolona taşır." },
      "advance_on_diff": { label: "Diff ile ilerlet", description: "Başarılı build ve gerçek bir kod değişikliğiyle biten çalışma, otomatik olarak belirtilen kolona taşınır." },
      "advance_on_document": { label: "Doküman ile ilerlet", description: "Bir doküman eklenerek biten çalışma, otomatik olarak belirtilen kolona taşınır." },
      "build_verify": { label: "Build doğrulama", description: "Bu kolondaki çalışma, bitmeden önce build hatalarını düzeltme turunu çalıştırır." },
      "commit_on_finish": { label: "Bitirken commit'le", description: "Bu kolondaki çalışma, çıkışta yaptığı değişiklikleri commit'ler." },
      "require_pr_for_review": { label: "İnceleme için PR gerektir", description: "İncelenecek açık bir pull request yoksa, buradaki çalışma reddedilir." },
      "require_criteria_complete": { label: "Kriterlerin tamamlanmasını gerektir", description: "Açık bir kabul kriteri varken bu kolona giriş reddedilir." },
      "criterion_verdict": { label: "Kriter kararı", description: "Bu kolonda kaydedilen karar, belirtilen inceleme kanalına atfedilir." },
      "forward_exit": { label: "İleri çıkış", description: "Bu kolondan yapılan çıkış, puanlama için ileri yönlü bir inceleme çıkışı sayılır." },
      "require_test_cases": { label: "Test senaryosu gerektir", description: "Bu kolon, görev için üretilen test senaryolarının puanlanmasını gerektirir." },
      "review_verdict_sweep": { label: "İnceleme kararını topla", description: "Bu kolonda tamamlanan bir inceleme turu, bir karara dönüştürülüp bir sonrakine aktarılır." },
      "require_execution_evidence": { label: "Çalıştırma kanıtı gerektir", description: "Ürünün gerçekten çalıştırıldığına dair kanıt yoksa, bu kolondaki QA turu reddedilir." },
      "require_product_check": { label: "Ürün kontrolü gerektir", description: "Ürünün kontrol edildiğine dair kanıt yoksa, bu kolondaki UAT turu reddedilir." },
      "review_chain_stage": { label: "İnceleme zinciri adımı", description: "Bu kolon, görev tipinin inceleme zincirinin zorunlu bir adımıdır; done/released'e geçmeden önce gereklidir." },
      "strip_writers": { label: "Yazma araçlarını kaldır", description: "Bu kolonda, izin verilenler hariç yazma/karar araçları ajanın politikasından çıkarılır." },
      "no_code_reading": { label: "Kod okumayı kapat", description: "Bu kolonda kod okuma araçları ajanın politikasından çıkarılır." },
      "no_workspace_writes": { label: "Çalışma alanına yazmayı kapat", description: "Bu görev tipindeki çalışmalar hiçbir zaman dosya yazma araçlarına sahip olmaz." },
      "require_repo_grounding": { label: "Depo inceleme kanıtı gerektir", description: "Deponun gerçekten okunduğuna dair kanıt yoksa, bu görev tipindeki çalışma reddedilir." },
    },
    noBehaviours: "Bu kapsamda kullanılabilir davranış yok.",
    instructionsLabel: "Talimatlar",
    participantsLabel: "Katılımcılar",
    participantMode: {
      worker: "Yürütücü",
      approver: "Onaylayıcı",
    },
    addParticipant: {
      worker: "Yürütücü ekle",
      approver: "Onaylayıcı ekle",
    },
    removeParticipant: "Kaldır",
    participantInstructionsPlaceholder: "Bu katılımcı için bu aşamada ek talimat (opsiyonel)",
    noSubscriberWarning: "“{role}” rolüne sahip hiçbir agent bu kolona abone değil — bu iş asla ona düşmeyecek.",
    kind: {
      intake: "Giriş",
      queue: "Kuyruk",
      work: "Çalışma",
      review: "İnceleme",
      approval: "Onay",
      rework: "Yeniden çalışma",
      parked: "Bekletilmiş",
      terminal: "Bitiş",
    },
  },
  catalog: {
    title: "Dış Ajan Kataloğu",
    loadFailed: "Katalog durumu yüklenemedi",
    syncFailed: "Katalog senkronizasyonu başarısız",
    syncToast: "Katalog senkronize edildi",
    syncNow: "Şimdi senkronize et",
    syncing: "Senkronize ediliyor…",
    notConfigured: "Harici bir ajan kataloğu yapılandırılmamış",
    notConfiguredHelp:
      "Sunucu ortamında AGENT_CATALOG_REPO değişkenini bir git deposuna (veya yerel bir dizin yoluna) işaret edin ve bu sayfayı kullanmak için yeniden başlatın.",
    lastSync: "Son senkronizasyon",
    repoRef: "Kaynak",
    created: "Oluşturulan",
    updated: "Güncellenen",
    merged: "Birleştirilen",
    skipped: "Atlanan",
    pendingCount: "Bekleyen",
    pendingTitle: "Seni bekleyen değişiklikler",
    pendingEmpty: "Bekleyen yok — katalogdaki her değişiklik uygulandı.",
    error: "Son hata",
    dismiss: "Kapat",
  },

};
