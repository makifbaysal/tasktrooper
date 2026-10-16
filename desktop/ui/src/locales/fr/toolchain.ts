// French dictionary for the developer-tool installer. Must mirror
// en/toolchain.ts keys exactly.
export const toolchain = {
  stepState: {
    queued: "En attente",
    running: "Installation",
    waiting: "Vous attend",
    done: "Installé",
    failed: "Échec",
    skipped: "Ignoré",
    cancelled: "Annulé",
  },
  itemStatus: {
    installed: "Installé",
    installedVersion: "Installé · {version}",
    missing: "Manquant",
    unusable: "Inutilisable",
  },
  badge: {
    running: "Configuration · {percent} %",
    done: "Configuration terminée",
    failed: "Configuration : {count} en échec",
    stopped: "Configuration arrêtée",
    incomplete: "Configuration incomplète",
  },
  waitingHint:
    "Une fenêtre d'installation ou de Terminal est ouverte sur cet ordinateur. Terminez-la là-bas ; la suite se fait toute seule.",
  method: "Via {method}",
  needsManual: "Nécessite {label}. Installez-le d'abord depuis sa page.",
  queuedAfterCurrent: "Ajouté après les étapes en cours",
  kind: {
    interactive: "Ouvre une fenêtre d'installation",
    manual: "S'installe depuis sa propre page",
  },
  reasons: {
    everyone: "Utilisé par tous les agents",
    agents: "Pour {agents}",
  },
  progress: {
    percent: "{percent} %",
    summary: "{done} sur {total} terminés",
    failed: "{count} en échec",
  },
  actions: {
    install: "Installer",
    openPage: "Ouvrir la page",
    openPageFor: "Ouvrir la page de {label}",
    stop: "Arrêter",
    dismiss: "Masquer",
    details: "Détails",
    restartAgents: "Redémarrer les agents",
    checkAgain: "Vérifier à nouveau",
    installRecommended: "Installer les recommandés",
  },
  restart: {
    hint: "Une partie de ce qui a été installé n'atteint les agents qu'après leur redémarrage.",
    title: "Redémarrer les agents ?",
    description:
      "TaskTrooper redémarre le service dans lequel tournent vos agents pour qu'ils voient les nouveaux outils. Ce qu'un agent est en train de faire est interrompu, et cette fenêtre se recharge.",
    confirm: "Redémarrer",
    failed: "Impossible de redémarrer les agents",
  },
  errors: {
    installFailed: "Impossible de lancer l'installation",
    declined: "Rien n'a été installé",
    stopFailed: "Impossible d'arrêter l'installation",
    dismissFailed: "Impossible de masquer la configuration terminée",
    openPageFailed: "Impossible d'ouvrir la page",
    reportFailed: "Impossible de vérifier cet ordinateur",
  },
  job: {
    title: {
      running: "Préparation de cet ordinateur",
      complete: "Configuration terminée",
      failed: "Configuration terminée avec des erreurs",
      stopped: "Configuration arrêtée",
      incomplete: "Configuration terminée, mais tout n'a pas été installé",
    },
  },
  running: {
    title: "Des outils de développement sont encore en cours d'installation",
    body: "git ou Claude Code sont peut-être encore en route. Une fois installés, cliquez sur Vérifier à nouveau.",
  },
  settings: {
    desktopOnlyTitle: "Cette page nécessite l'application de bureau TaskTrooper",
    desktopOnlyBody:
      "Elle vérifie et installe des outils de développement sur votre propre ordinateur, ce qu'un onglet de navigateur ne peut pas atteindre.",
    recommendedTitle: "Recommandé pour votre équipe",
    recommendedDescription: "Ce avec quoi vos agents activés travaillent le mieux et qui manque encore sur cet ordinateur.",
    recommendedNone: "Tout ce dont votre équipe a besoin est sur cet ordinateur.",
    recommendedManual:
      "Les outils qui s'installent depuis leur propre page ne font pas partie de l'installation en un clic ; utilisez Ouvrir la page ci-dessous.",
    allTitle: "Tous les outils de développement",
    allDescription: "Ce que TaskTrooper peut vérifier et installer sur cet ordinateur.",
    lastChecked: "Vérifié {when}.",
    checking: "Vérification de cet ordinateur…",
    groups: {
      basics: "Essentiels",
      web: "Web",
      backend: "Langages backend",
      mobile: "Mobile",
      data: "Données",
      games: "Jeux",
      infra: "Infra et sécurité",
    },
  },
};
