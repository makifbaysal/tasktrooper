// French dictionary for the repository setup checklist. Must mirror
// en/readiness.ts keys exactly.
export const readiness = {
  title: "Préparer ce projet",
  intro: "Ce qui manque à ce projet pour que les agents y travaillent bien, dans un ordre logique. Ignorez ce dont vous n'avez pas besoin.",
  progress: "{done} sur {total} terminées",
  skippedCount: "{count} ignorées",
  complete: "Ce projet est prêt.",
  show: "Afficher la liste",
  hide: "Masquer",
  expand: "Déplier la liste",
  collapse: "Replier la liste",
  skip: "Ignorer",
  unskip: "Annuler l'abandon",
  doIt: "Lancer",
  open: "Ouvrir",
  install: "Installer",
  viewTask: "Voir {key}",
  taskOpen: "La tâche {key} est en cours.",
  skippedNote: "Ignorée.",
  waitingFor: "En attente de : {step}.",
  started: "Lancé.",
  taskOpened: "Tâche {key} ouverte sur le tableau.",
  allStarted: "{runs} lancés et {tasks} tâches ouvertes.",
  actionFailed: "Impossible de le faire",
  runAll: "Tout lancer dans l'ordre",
  runAllTitle: "Tout lancer dans l'ordre ?",
  runAllTasks: "{count} tâches seront ouvertes sur le tableau, chacune attend la précédente.",
  runAllRuns: "{count} démarreront tout de suite, sans tâche.",
  runAllCost: "Chaque tâche utilise le temps de vos agents.",
  runAllConfirm: "Tout lancer",
  status: {
    done: "Terminée",
    todo: "À faire",
    running: "En cours",
    blocked: "Attend une étape précédente",
    skipped: "Ignorée",
    unknown: "Non vérifiée",
  },
  missing: {
    readme: "README",
    agent_guide: "guide pour agents",
  },
  steps: {
    scan: {
      title: "Analyser le projet",
      why: "L'analyse trouve les composants, les commandes et la CI dont toutes les autres étapes dépendent.",
    },
    toolchain: {
      title: "Installer les outils de cette stack",
      why: "Les agents compilent et testent sur cet ordinateur, il lui faut donc les compilateurs qu'ils appellent.",
    },
    index: {
      title: "Indexer le code",
      why: "L'index permet aux agents de trouver les bons fichiers au lieu de tout lire.",
    },
    guide: {
      title: "Ajouter un README et un guide pour agents",
      why: "Les agents commencent chaque tâche par ces fichiers, ce sont eux qui font gagner le plus de temps et d'erreurs.",
    },
    tests: {
      title: "Rendre les tests exécutables",
      why: "Une exécution de tests réussie permet à vous comme aux agents de savoir qu'un changement n'a rien cassé.",
    },
    ci: {
      title: "Exécuter les vérifications en CI",
      why: "La CI rejoue le build et les tests à chaque changement avant qu'il n'atteigne la branche principale.",
    },
    delivery: {
      title: "Choisir comment les changements sont livrés",
      why: "Une cible de livraison dit à TaskTrooper où va un changement terminé et comment le vérifier.",
    },
    observability: {
      title: "Le surveiller en production",
      why: "Les métriques, les logs et les traces vous signalent quand une version casse quelque chose.",
    },
    findings: {
      title: "Passer en revue les constats de santé du code",
      why: "L'analyse signale des réglages de debug, des secrets et un logging manquant, peu coûteux à corriger tôt.",
    },
    design: {
      title: "Mettre en place le système de design",
      why: "Un système de design garde les nouveaux écrans cohérents avec ceux que vous avez déjà.",
    },
    store: {
      title: "Lier l'application du store",
      why: "Lier l'app App Store ou Google Play permet à TaskTrooper de livrer des builds et de suivre les avis.",
    },
  },
  detail: {
    scan: {
      no_scan: "Pas encore analysé.",
      scan_failed: "La dernière analyse a échoué.",
      no_components: "L'analyse n'a trouvé aucun composant.",
      running: "Analyse en cours…",
      done: "{cmp} composants trouvés.",
    },
    toolchain: {
      unknown: "Requis : {languages}.",
      no_bridge: "Seule l'application de bureau peut vérifier les outils de cet ordinateur.",
      missing: "Manquants sur cet ordinateur : {tools}.",
      installed: "Tout ce dont cette stack a besoin est installé.",
    },
    index: {
      not_indexed: "Pas encore indexé.",
      failed: "La dernière indexation a échoué.",
      unavailable: "L'index du code n'est pas disponible.",
      running: "{processed} fichiers sur {total}.",
      done: "{files} fichiers indexés.",
    },
    guide: {
      missing: "Manquant : {missing}.",
      unreadable: "La copie de travail est illisible.",
      done: "README et guide pour agents trouvés.",
    },
    tests: {
      gaps: "Pas de commande de test pour : {components}.",
      done: "Chaque composant a une commande de test.",
    },
    ci: {
      no_ci: "Aucun workflow de CI trouvé.",
      done: "{checks} vérifications de CI trouvées.",
    },
    delivery: {
      gaps: "Aucune livraison confirmée pour : {components}.",
      done: "Chaque composant déployable a une cible de livraison.",
    },
    observability: {
      gaps: "Lacunes dans : {components}.",
      done: "Aucune lacune trouvée.",
    },
    findings: {
      open: "{count} ouverts ({high} de gravité haute).",
      done: "Aucun constat ouvert.",
    },
    design: {
      none: "Pas encore de système de design.",
      unavailable: "Le système de design n'a pas pu être lu.",
      done: "Un système de design est en place.",
    },
    store: {
      no_app: "Aucune application de store liée.",
      unavailable: "Les applications de store n'ont pas pu être lues.",
      done: "Une application de store est liée.",
    },
  },
};
