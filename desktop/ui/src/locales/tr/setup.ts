// Turkish dictionary for the guided first-run sequence. Must mirror
// en/setup.ts keys exactly.
export const setup = {
  title: "TaskTrooper'ı hazır hale getir",
  description: "Sırayla dört adım. Her biri bir sonrakini açar.",
  stepLabel: "Adım {index} / {total}",
  later: "Bunu sonra yapacağım",
  state: {
    done: "Tamam",
    todo: "Yapılacak",
    unknown: "Kontrol edilemedi",
    locked: "Kilitli",
    desktopOnly: "Mac uygulaması gerekir",
  },
  unknownHint: "Bu az önce kontrol edilemedi; yani burada hiçbir şey 'yapılmadı' demiyor.",
  desktopOnly: {
    title: "Bu adım TaskTrooper Mac uygulamasında yapılır",
    body: "Bu adım doğrudan kendi Mac'inizde çalışır, neyin kurulu olduğunu kontrol eder ve orada başsız bir Claude Code oturumu başlatır; bir tarayıcı sekmesi bunların hiçbirine erişemez. Uygulamayı indirin, aynı hesapla giriş yapın; bu akış orada devam eder.",
  },
  environment: {
    title: "Bu Mac'i kontrol et",
    description:
      "TaskTrooper'ın bu makinede ihtiyaç duydukları: git, Claude Code CLI ve içinde giriş yapılmış bir hesap. Aşağıda kırmızı olan her satır neyin eksik olduğunu ve ne çalıştırmanız gerektiğini söyler.",
    readyTitle: "Gerekli her şey yerinde",
    readyBody: "Artık bir ajan çalışma ortamı bağlayabilirsiniz.",
    notReadyTitle: "Gerekli bir şey eksik",
    notReadyBody:
      "Aşağıda işaretli maddeleri düzeltip Tekrar kontrol et'e basın. Hepsi yeşil olana kadar Bağlan kapalı kalır.",
    continue: "Devam et",
  },
  agent: {
    title: "Bir ajan çalışma ortamı bağlayın",
    description:
      "Ajanlar işlerini bu Mac'teki bir kodlama CLI'ı üzerinden yapar. Kullandıklarınızdan istediğinizi bağlayın, biri yeterli; sonradan başkalarını da ekleyebilirsiniz.",
    connect: "Bağla",
    connecting: "Bağlanıyor…",
    disconnect: "Bağlantıyı kes",
    connected: "Bağlı",
    connectedBody: "{binary}{version}: {agents} ajan ve {skills} beceri kuruldu.",
    notInstalled: "Kurulu değil",
    installWith: "Kurmak için: {command}",
    notInstalledBody: "Bu Mac'te bulunamadı.",
    noneTitle: "Henüz bir şey bağlı değil",
    noneBody: "Ajanların çalışabilmesi için en az bir CLI bağlayın ya da aşağıdan bir API sağlayıcısı ekleyin.",
    apiKeyTitle: "API anahtarı mı kullanmak istiyorsunuz?",
    apiKeyBody:
      "Ajanlar CLI yerine bir model API'si üzerinden de çalışabilir: OpenAI, Anthropic, Google Gemini, Groq ya da kendi anahtarınız ve base URL'inizle herhangi bir OpenAI uyumlu endpoint.",
    apiKeyAction: "API sağlayıcısı ekle",
    failed: "Bağlanma başarısız",
  },
  github: {
    title: "GitHub'ı bağla",
    description:
      "Ajanlar bu hesapla klonlar, dal açar, push eder ve pull request oluşturur. GitHub'ın kendi izin ekranına gideceksiniz; burada elle hiçbir şey yazılmaz.",
    doneTitle: "GitHub bağlı",
    doneBody: "Artık depo içe aktarabilirsiniz.",
    todoTitle: "GitHub henüz bağlı değil",
    todoBody: "Bağlanmadan içe aktarılacak bir şey ve bir ajanın push edebileceği bir yer olmaz.",
    continue: "Devam et",
  },
  project: {
    title: "İlk projen",
    description:
      "Bir proje, birlikte yayınlanan depoları gruplar. Bir tane oluşturun, sonra ilk deponuzu içine aktarın; türünün ne olduğu, nasıl deploy edildiği, nasıl derlenip test edildiği sorulacak.",
    createProject: "Proje oluştur",
    needsRepositoryTitle: "Şimdi bir depo içe aktarın",
    needsRepositoryBody: "Eklemek için aşağıdaki düğmeyi kullanın. Diğerlerini sonra ekleyebilirsiniz.",
    doneTitle: "İlk deponuz eklendi",
    doneBody: "Projeye bağlandı ve arka planda indeksleniyor. İstediğiniz zaman yenilerini ekleyin.",
    loadFailed: "Projeleriniz yüklenemedi",
  },
  nav: {
    finishSetup: "Kurulumu tamamla",
  },
};
