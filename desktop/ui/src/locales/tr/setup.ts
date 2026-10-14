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
    desktopOnly: "Masaüstü uygulaması gerekir",
  },
  unknownHint: "Bu az önce kontrol edilemedi; yani burada hiçbir şey 'yapılmadı' demiyor.",
  desktopOnly: {
    title: "Bu adım TaskTrooper masaüstü uygulamasında yapılır",
    body: "Bu adım doğrudan kendi bilgisayarınızda çalışır, neyin kurulu olduğunu kontrol eder ve orada başsız bir Claude Code oturumu başlatır; bir tarayıcı sekmesi bunların hiçbirine erişemez. Uygulamayı indirin, aynı hesapla giriş yapın; bu akış orada devam eder.",
  },
  environment: {
    title: "Bu bilgisayarı kontrol et",
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
      "Ajanlar işlerini bu bilgisayardaki bir kodlama CLI'ı üzerinden yapar. Kullandıklarınızdan istediğinizi bağlayın, biri yeterli; sonradan başkalarını da ekleyebilirsiniz.",
    connect: "Bağla",
    connecting: "Bağlanıyor…",
    disconnect: "Bağlantıyı kes",
    connected: "Bağlı",
    connectedBody: "{binary}{version}: {agents} ajan ve {skills} beceri kuruldu.",
    notInstalled: "Kurulu değil",
    installWith: "Kurmak için: {command}",
    notInstalledBody: "Bu bilgisayarda bulunamadı.",
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
  firstRun: {
    analyticsNote: "TaskTrooper, aktif kurulumların anonim bir sayısını gönderir; Ayarlar'dan kapatabilirsiniz.",
    title: "TaskTrooper'ı nasıl kullanmak istiyorsunuz?",
    description: "Şimdi seçin; sonradan Ayarlar'dan değiştirebilirsiniz.",
    local: {
      title: "Bu bilgisayarda, hesapsız",
      body: "Sunucu, veritabanı ve ajanlarınız bu makinede çalışır. Giriş yok, hiçbir şey buradan çıkmaz.",
      action: "Hesapsız devam et",
    },
    account: {
      title: "Hesapla",
      body: "Panonuza ve ekibinize her cihazdan erişmek için giriş yapın.",
      action: "Giriş yap",
      signingIn: "Giriş yapılıyor…",
      failed: "Giriş yapılamadı",
    },
  },
  team: {
    title: "Nasıl bir ekip?",
    description:
      "Bir başlangıç noktası seçin. Açtığı ajanlar önceden seçilir; burada ve sonradan Ayarlar'dan değiştirebilirsiniz.",
    agentsLabel: "Ajanlar",
    editTeam: "Ekibi düzenle",
    editTitle: "Ekipteki ajanlar",
    editDescription:
      "Ajanları açıp kapatın. Açtığınız ajanlar ekibe katılır; kapalı ajanlara yeni iş verilmez.",
    save: "Kaydet",
    preparing: "Ajanlar hazırlanıyor…",
    coreLocked: "Ürün yöneticisi, sistem mimarı, QA, güvenlik ve sürüm mühendisi her zaman açıktır.",
    confirm: "Bu ekibi oluştur",
    loadFailed: "Ajanlarınız yüklenemedi",
    saveFailed: "Ekip kaydedilemedi",
    templates: {
      web: { name: "Web uygulaması", description: "Ön uç, arka uç ve bunları yayınlayanlar." },
      mobile: { name: "Mobil uygulama", description: "Arka uç ve tasarımın yanında bir mobil geliştirici." },
      game: { name: "Oyun", description: "Çekirdek ekibin yanında bir oyun geliştirici ve tasarım." },
      data: { name: "Veri ve analitik", description: "Çekirdek ekibin yanında arka uçla birlikte bir veri bilimci." },
      custom: { name: "Özel", description: "Yalnızca çekirdek ekiple başla." },
    },
  },
  toolchain: {
    title: "Bu bilgisayarı ekibine hazırla",
    description:
      "Ajanlarınız bu bilgisayarda birkaç geliştirici aracıyla en iyi şekilde çalışır. Neyin kurulacağını seçin; kurulum siz devam ederken arka planda sürer.",
    recommended: "Önerilen",
    optional: "İsteğe bağlı",
    installed: "Bu bilgisayarda zaten var ({count})",
    installsFirst: "Önce kurulacaklar: {items}",
    install: "Seçilenleri kur ve devam et",
    skip: "Şimdilik atla",
    continue: "Devam et",
    backgroundTitle: "Kurulum arka planda sürer",
    background:
      "Hemen devam edebilirsiniz. İlerlemeyi üst çubuktan ve Ayarlar → Geliştirme ortamı sayfasından takip edin.",
    declinedTitle: "Hiçbir şey kurulmadı",
    declinedBody:
      "Hazır olduğunuzda yeniden seçin ya da şimdilik atlayıp daha sonra Ayarlar → Geliştirme ortamı sayfasından kurun.",
    nothingMissingTitle: "Ekibinizin ihtiyaç duyduğu her şey zaten burada",
    nothingMissingBody: "Bu bilgisayarda ajanlarınızın kullandığı araçlar var.",
    checking: "Bu bilgisayar kontrol ediliyor…",
    preparing: "Ekibiniz hâlâ hazırlanıyor; ajanlar geldikçe bu liste dolacak.",
    teamLoadFailed: "Ekibiniz okunamadı; bu yüzden yalnızca tüm ajanların kullandığı araçlar listelendi.",
  },
  integrations: {
    title: "Ekibinizin kullandığı servisleri bağlayın",
    description: "Elinizde olanları şimdi kurun ya da atlayıp daha sonra Ayarlar'dan yapın.",
    chooseTitle: "Hangi entegrasyonları kurmak istersiniz?",
    chooseHint: "İstediklerinizi seçin ya da hiçbirini seçmeyin. Buradaki hiçbir şey zorunlu değil.",
    recommended: "Ekibiniz için önerilen",
    setUpSelected: "Seçilenleri kur",
    skip: "Şimdilik atla",
    setUpTitle: "Seçtiklerinizi kurun",
    setUpHint:
      "Elinizde ne varsa doldurun; hiçbiri zorunlu değil. Bu ekrandaki her şey daha sonra Ayarlar → Entegrasyonlar'da da var.",
    continue: "Devam et",
    back: "Geri",
    teamLoadFailed: "Ekibiniz okunamadı; öneriler eksik olabilir",
    items: {
      asc: { description: "iOS derlemelerini TestFlight'a ve App Store'a gönderin." },
      googlePlay: { description: "Android derlemelerini Google Play'in test kanallarına ve yayına gönderin." },
      cloud: {
        name: "Bulut hesapları",
        description: "Dağıtımlar ve canlıda çalışanı izlemek için Vercel, Google Cloud ya da AWS.",
      },
      jira: { description: "Issue'larının panoya gelebilmesi için bir Jira sitesi bağlayın." },
      issueSync: { description: "Etiketli GitHub ya da Jira issue'ları kendiliğinden göreve dönüşsün; iş bitince issue'ya yanıt verilsin." },
    },
  },
  nav: {
    finishSetup: "Kurulumu tamamla",
  },
};
