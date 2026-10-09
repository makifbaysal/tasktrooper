import type { Dict } from "@/locales/en";
import { activityArea } from "@/locales/tr/activityArea";
import { addRepository } from "@/locales/tr/addRepository";
import { agentArea } from "@/locales/tr/agentArea";
import { analysisReview } from "@/locales/tr/analysisReview";
import { boardArea } from "@/locales/tr/boardArea";
import { chatArea } from "@/locales/tr/chatArea";
import { cloud } from "@/locales/tr/cloud";
import { content } from "@/locales/tr/content";
import { designSystem } from "@/locales/tr/designSystem";
import { frame } from "@/locales/tr/frame";
import { lib } from "@/locales/tr/lib";
import { operations } from "@/locales/tr/operations";
import { projectAdmin } from "@/locales/tr/projectAdmin";
import { projectModel } from "@/locales/tr/projectModel";
import { projectsHub } from "@/locales/tr/projectsHub";
import { release } from "@/locales/tr/release";
import { repositoryPage } from "@/locales/tr/repositoryPage";
import { settingsPages } from "@/locales/tr/settingsPages";
import { setup } from "@/locales/tr/setup";

// Turkish dictionary. Typed as Dict — must mirror en.ts keys exactly.
export const tr: Dict = {
  common: {
    save: "Kaydet",
    saving: "Kaydediliyor...",
    cancel: "İptal",
    refresh: "Yenile",
    resetDefault: "Varsayılana dön",
    actionFailed: "İşlem başarısız",
    saved: "Kaydedildi",
    saveFailed: "Kaydetme başarısız",
    comingSoon: "Yakında",
    errorBoundary: {
      title: "Bir şeyler ters gitti",
      body: "Beklenmeyen bir hata oluştu ve ekran çizilemedi. Sayfayı yeniden yükleyip tekrar deneyebilirsiniz.",
      retry: "Tekrar dene",
      reload: "Sayfayı yenile",
    },
    configError: {
      title: "Yapılandırma hatası",
      body: "Bu build'de yerel sunucu için API anahtarı yok, bu yüzden her istek reddedilir. VITE_API_KEY değerini (sunucudaki SERVER_API_KEY ile aynı olmalı) verip yeniden build alın.",
      missing: "Eksik:",
    },
  },
  settings: {
    language: {
      label: "Dil",
      help: "Asistan yanıtları ve sistem talimatları için kullanılır.",
    },
    loadFailed: "Ayarlar yüklenemedi",
    savedToast: "Ayarlar kaydedildi",
    github: {
      statusUnavailable: "Durum alınamadı.",
      connected: "✓ Bağlı: {login} — ajanlar private repo oluşturabilir, push edebilir ve taslak PR açabilir.",
      disconnect: "Bağlantıyı kes",
      connect: "Token'ı kaydet",
      tokenPlaceholder: "ghp_… veya github_pat_…",
      tokenHelp:
        "GitHub → Settings → Developer settings'ten alınan bir personal access token. Klasik token'da repo, workflow, admin:repo_hook ve read:org; fine-grained token'da Contents, Pull requests, Workflows (okuma ve yazma) ve Webhooks izinleri gerekir. workflow olmadan hiçbir ajan CI dosyası ekleyemez ya da değiştiremez. Saklanmadan önce GitHub'a karşı doğrulanır ve bu makinede şifreli tutulur.",
      connectedToast: "GitHub bağlandı",
      connectFailedToast: "GitHub bağlantısı başarısız",
      disconnectedToast: "GitHub bağlantısı kaldırıldı",
      missingScopesTitle: "Bu token'da eksik: {scopes}",
      missingWorkflowScope: "workflow yetkisi olmadan GitHub, .github/workflows altında dosya ekleyen ya da değiştiren her push'u reddeder; hiçbir ajan CI kuramaz ya da düzeltemez. Bu yetkiyle yeni bir token oluştur, bağlantıyı kesip yenisini kaydet.",
      fineGrainedHint: "Fine-grained token: Workflows: Read and write izni olduğundan emin ol, yoksa GitHub CI dosyalarına dokunan push'ları reddeder.",
      connectWithGitHub: "GitHub ile bağlan",
      connectWithGitHubHint: "github.com'da tek seferlik bir kodla giriş yap. Bağlantıyı TaskTrooper kendisi yeniler — token oluşturup yapıştırman gerekmez.",
      deviceCodeTitle: "Bu kodu GitHub'a gir",
      deviceCodeHint: "{url} adresini aç, giriş yap, kodu gir ve TaskTrooper'ı onayla. Bu pencere kendiliğinden devam eder.",
      copyCode: "Kodu kopyala",
      codeCopied: "Kod kopyalandı",
      openGitHub: "GitHub'ı aç",
      waitingForApproval: "Onayın bekleniyor…",
      flowExpired: "Kod onaylanmadan süresi doldu.",
      flowDenied: "Giriş GitHub'da reddedildi.",
      tryAgain: "Tekrar dene",
      cancel: "Vazgeç",
      useTokenInstead: "Bunun yerine personal access token kullan",
      useAppInstead: "Bunun yerine GitHub ile bağlan",
      modeApp: "GitHub girişiyle",
      modeToken: "erişim token'ı ile",
      needsInstallTitle: "Uygulamayı repolarına kur",
      needsInstallBody: "Bağlantı çalışıyor ama TaskTrooper uygulaması henüz hiçbir hesaba kurulu değil, bu yüzden hiçbir repoya erişemiyor. Hesabına ya da organizasyonuna kur ve All repositories seç; TaskTrooper'ın sonradan oluşturacağı repolar da kapsansın.",
      installApp: "GitHub'da kur",
      expiredTitle: "GitHub bağlantısının süresi doldu",
      expiredBody: "Altı ay kullanılmadı ya da GitHub'da iptal edildi. Yeniden bağlan.",
    },
    boilerplate: {
      title: "Boilerplate Kataloğu",
      descPrefix: "Agent'lar sıfırdan kod yazmadan önce bu repodaki",
      descMid: "dosyasını arar; eşleşen bir boilerplate varsa onu kopyalayarak başlar.",
      descSuffix: "veya tam URL kabul edilir.",
      loadFailed: "Ayarlar yüklenemedi",
    },
    notifications: {
      title: "Masaüstü bildirimleri",
      help: "Dikkatini gerektiren pano olayları için yerel bildirimler. Pencereyi kapatsan bile çalışmaya devam eder.",
      unavailable: "Masaüstü bildirimleri yalnızca TaskTrooper uygulamasında kullanılabilir, tarayıcıda değil.",
      enabled: "Etkin",
      analizReview: "İncelemen için hazır analiz",
      humanUat: "UAT'ını bekliyor",
      humanNeeded: "Bir agent insan kararına ihtiyaç duyuyor",
      agentComments: "Yeni agent yorumları",
      agentChatReplies: "Agent sohbet cevapları",
      loadFailed: "Ayarlar yüklenemedi",
      saveFailed: "Kaydetme başarısız",
    },
    account: {
      title: "Hesap",
      localHelp: "TaskTrooper bu bilgisayarda, hesapsız çalışıyor. Bir ekiple çalışmak için hesaba bağlan: board hesapta durur, iş yine burada, kendi anahtarlarınla çalışır.",
      signedIn: "{origin} hesabına bağlısın. Sana atanan görevler bu bilgisayarda çalışır.",
      connect: "Hesaba bağlan",
      connecting: "Bağlanıyor…",
      signOut: "Çıkış yap",
      signingOut: "Çıkış yapılıyor…",
      runsTitle: "Devam eden {count} yerel çalışma var",
      runsBody: "Bağlanmak yerel sunucuyu ve bu çalışmaları durdurur. Yerel veri olduğu gibi kalır; çıkış yapınca geri gelir.",
      confirmTitle: "Yerel sunucu durdurulsun mu?",
      confirmBody: "Bağlanmak bu bilgisayardaki yerel sunucuyu durdurur ve hesabın giriş sayfasını bu pencerede açar. Yerel veri olduğu gibi kalır; çıkış yapınca geri gelir.",
      confirm: "Devam et",
      cancel: "Vazgeç",
      failed: "Geçiş tamamlanamadı",
      unavailable: "Hesaplar yalnızca TaskTrooper masaüstü uygulamasında kullanılabilir.",
    },
    updates: {
      title: "Güncellemeler",
      help: "TaskTrooper birkaç saatte bir güncellemeleri kontrol eder ve arka planda indirir.",
      currentVersion: "TaskTrooper {version} sürümünü kullanıyorsun.",
      unavailable: "Güncellemeler yalnızca TaskTrooper masaüstü uygulamasında kullanılabilir, tarayıcıda değil.",
      unsupported: "Bu derleme kendini güncelleyemiyor",
      upToDate: "En güncel sürümdesin.",
      check: "Güncellemeleri kontrol et",
      checking: "Kontrol ediliyor…",
      checkFailed: "Güncellemeler kontrol edilemedi",
      lastChecked: "Son kontrol {when}",
      downloading: "Güncelleme indiriliyor… %{percent}",
      downloadingVersion: "TaskTrooper {version} indiriliyor… %{percent}",
      ready: "Bir güncelleme yüklenmeye hazır.",
      readyVersion: "TaskTrooper {version} yüklenmeye hazır.",
      restart: "Yeniden başlat ve yükle",
      restarting: "Yeniden başlatılıyor…",
      restartHelp: "Önce yerel süreçler durdurulur, uygulama yeni sürümle yeniden açılır. Uygulamadan çıkmak da güncellemeyi yükler, ama yeniden açmaz.",
      restartFailed: "Güncelleme için yeniden başlatılamadı",
    },
    concurrency: {
      title: "Eşzamanlılık sınırları",
      agentsLabel: "Eşzamanlı çalışan agent sayısı",
      tasksLabel: "Eşzamanlı çalışan task sayısı",
      agentsHelp: "Panodan aynı anda kaç agent run'ı çalışabilir.",
      tasksHelp: "Aynı anda kaç farklı task, çalışan bir agent tutabilir.",
      zeroHint: "0 sınırsız demektir",
      reset: "Sınırsız",
      resetting: "Sıfırlanıyor…",
      loadFailed: "Ayarlar yüklenemedi",
      saveFailed: "Kaydetme başarısız",
    },
  },
  addRepository,
  agentArea,
  analysisReview,
  boardArea,
  activityArea,
  chatArea,
  cloud,
  content,
  designSystem,
  frame,
  lib,
  operations,
  projectAdmin,
  projectModel,
  projectsHub,
  release,
  repositoryPage,
  settingsPages,
  setup,
};
