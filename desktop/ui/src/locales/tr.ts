import type { Dict } from "@/locales/en";
import { addRepository } from "@/locales/tr/addRepository";
import { agentArea } from "@/locales/tr/agentArea";
import { boardArea } from "@/locales/tr/boardArea";
import { chatArea } from "@/locales/tr/chatArea";
import { cloud } from "@/locales/tr/cloud";
import { content } from "@/locales/tr/content";
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
        "GitHub → Settings → Developer settings'ten alınan bir personal access token. repo, admin:repo_hook ve read:org izinleri gerekir. Saklanmadan önce GitHub'a karşı doğrulanır ve bu makinede şifreli tutulur.",
      connectedToast: "GitHub bağlandı",
      connectFailedToast: "GitHub bağlantısı başarısız",
      disconnectedToast: "GitHub bağlantısı kaldırıldı",
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
      loadFailed: "Ayarlar yüklenemedi",
      saveFailed: "Kaydetme başarısız",
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
  boardArea,
  chatArea,
  cloud,
  content,
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
