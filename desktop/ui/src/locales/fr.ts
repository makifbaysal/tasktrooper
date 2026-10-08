import type { Dict } from "@/locales/en";
import { activityArea } from "@/locales/fr/activityArea";
import { addRepository } from "@/locales/fr/addRepository";
import { agentArea } from "@/locales/fr/agentArea";
import { analysisReview } from "@/locales/fr/analysisReview";
import { boardArea } from "@/locales/fr/boardArea";
import { chatArea } from "@/locales/fr/chatArea";
import { cloud } from "@/locales/fr/cloud";
import { content } from "@/locales/fr/content";
import { designSystem } from "@/locales/fr/designSystem";
import { frame } from "@/locales/fr/frame";
import { lib } from "@/locales/fr/lib";
import { operations } from "@/locales/fr/operations";
import { projectAdmin } from "@/locales/fr/projectAdmin";
import { projectModel } from "@/locales/fr/projectModel";
import { projectsHub } from "@/locales/fr/projectsHub";
import { release } from "@/locales/fr/release";
import { repositoryPage } from "@/locales/fr/repositoryPage";
import { settingsPages } from "@/locales/fr/settingsPages";
import { setup } from "@/locales/fr/setup";

// French dictionary. Typed as Dict — must mirror en.ts keys exactly.
export const fr: Dict = {
  common: {
    save: "Enregistrer",
    saving: "Enregistrement...",
    cancel: "Annuler",
    refresh: "Actualiser",
    resetDefault: "Rétablir la valeur par défaut",
    actionFailed: "L'action a échoué",
    saved: "Enregistré",
    saveFailed: "Échec de l'enregistrement",
    comingSoon: "Bientôt disponible",
    errorBoundary: {
      title: "Une erreur s'est produite",
      body: "Une erreur inattendue a empêché l'affichage de l'écran. Essayez de recharger la page.",
      retry: "Réessayer",
      reload: "Recharger la page",
    },
    configError: {
      title: "Erreur de configuration",
      body: "Ce build n'a pas de clé API pour le serveur local : toutes les requêtes seraient donc refusées. Définissez VITE_API_KEY (elle doit correspondre à SERVER_API_KEY du serveur) puis recompilez.",
      missing: "Manquant :",
    },
  },
  settings: {
    language: {
      label: "Langue",
      help: "Utilisée pour les réponses de l'assistant et les instructions système.",
    },
    loadFailed: "Impossible de charger les paramètres",
    savedToast: "Paramètres enregistrés",
    github: {
      statusUnavailable: "Statut indisponible.",
      connected: "✓ Connecté : {login} — les agents peuvent créer des dépôts privés, pousser et ouvrir des PR brouillons.",
      disconnect: "Déconnecter",
      connect: "Enregistrer le jeton",
      tokenPlaceholder: "ghp_… ou github_pat_…",
      tokenHelp: "Un jeton d'accès personnel créé dans GitHub → Settings → Developer settings. Un jeton classique nécessite repo, workflow, admin:repo_hook et read:org ; un jeton fine-grained nécessite Contents, Pull requests, Workflows (Read and write) et Webhooks. Sans workflow, aucun agent ne peut ajouter ni modifier un fichier de CI. Il est vérifié auprès de GitHub avant d'être stocké, chiffré, sur cette machine.",
      connectedToast: "GitHub connecté",
      connectFailedToast: "Échec de la connexion à GitHub",
      disconnectedToast: "Connexion GitHub supprimée",
      missingScopesTitle: "Il manque à ce jeton : {scopes}",
      missingWorkflowScope: "Sans le scope workflow, GitHub refuse tout push qui ajoute ou modifie un fichier sous .github/workflows : aucun agent ne peut donc configurer ni corriger la CI. Créez un jeton avec ce scope, déconnectez-vous et enregistrez le nouveau.",
      fineGrainedHint: "Jeton fine-grained : vérifiez qu'il dispose de Workflows: Read and write, sinon GitHub refuse les pushs qui touchent aux fichiers de CI.",
      connectWithGitHub: "Se connecter avec GitHub",
      connectWithGitHubHint: "Connectez-vous sur github.com avec un code à usage unique. TaskTrooper renouvelle lui-même la connexion — aucun jeton à créer ni à coller.",
      deviceCodeTitle: "Saisissez ce code sur GitHub",
      deviceCodeHint: "Ouvrez {url}, connectez-vous, saisissez le code et autorisez TaskTrooper. Cette fenêtre continuera d'elle-même.",
      copyCode: "Copier le code",
      codeCopied: "Code copié",
      openGitHub: "Ouvrir GitHub",
      waitingForApproval: "En attente de votre approbation…",
      flowExpired: "Le code a expiré avant d'être approuvé.",
      flowDenied: "La connexion a été refusée sur GitHub.",
      tryAgain: "Réessayer",
      cancel: "Annuler",
      useTokenInstead: "Utiliser plutôt un jeton d'accès personnel",
      useAppInstead: "Se connecter plutôt avec GitHub",
      modeApp: "via la connexion GitHub",
      modeToken: "via un jeton d'accès",
      needsInstallTitle: "Installez l'app sur vos dépôts",
      needsInstallBody: "La connexion fonctionne, mais l'app TaskTrooper n'est encore installée sur aucun compte : elle n'atteint donc aucun dépôt. Installez-la sur votre compte ou votre organisation et choisissez All repositories, afin que les dépôts créés plus tard par TaskTrooper soient aussi couverts.",
      installApp: "Installer sur GitHub",
      expiredTitle: "La connexion GitHub a expiré",
      expiredBody: "Elle n'a pas été utilisée depuis six mois ou a été révoquée sur GitHub. Reconnectez-vous.",
    },
    boilerplate: {
      title: "Catalogue de boilerplates",
      descPrefix: "Avant d'écrire du code de zéro, les agents consultent",
      descMid: "dans ce dépôt ; si un boilerplate correspondant existe, ils commencent par le copier.",
      descSuffix: "ou une URL complète sont acceptés.",
      loadFailed: "Impossible de charger les paramètres",
    },
    notifications: {
      title: "Notifications de bureau",
      help: "Notifications natives pour les événements du tableau qui requièrent votre attention. Elles continuent de fonctionner après la fermeture de la fenêtre.",
      unavailable: "Les notifications de bureau ne sont disponibles que dans l'app TaskTrooper, pas dans un navigateur.",
      enabled: "Activées",
      analizReview: "Analyse prête pour votre revue",
      humanUat: "En attente de votre UAT",
      humanNeeded: "Un agent a besoin d'une décision humaine",
      agentComments: "Nouveaux commentaires d'agents",
      agentChatReplies: "Réponses des agents dans le chat",
      loadFailed: "Impossible de charger les paramètres",
      saveFailed: "Échec de l'enregistrement",
    },
    updates: {
      title: "Mises à jour",
      help: "TaskTrooper recherche des mises à jour toutes les quelques heures et les télécharge en arrière-plan.",
      currentVersion: "Vous utilisez TaskTrooper {version}.",
      unavailable: "Les mises à jour ne sont disponibles que dans l'application de bureau TaskTrooper, pas dans un navigateur.",
      unsupported: "Cette version ne peut pas se mettre à jour toute seule",
      upToDate: "Vous avez la dernière version.",
      check: "Rechercher des mises à jour",
      checking: "Vérification…",
      checkFailed: "Impossible de rechercher des mises à jour",
      lastChecked: "Dernière vérification {when}",
      downloading: "Téléchargement d'une mise à jour… {percent} %",
      downloadingVersion: "Téléchargement de TaskTrooper {version}… {percent} %",
      ready: "Une mise à jour est prête à être installée.",
      readyVersion: "TaskTrooper {version} est prête à être installée.",
      restart: "Redémarrer et installer",
      restarting: "Redémarrage…",
      restartHelp: "Les processus locaux sont d'abord arrêtés, puis l'application se rouvre sur la nouvelle version. Quitter l'application l'installe aussi, sans la rouvrir.",
      restartFailed: "Impossible de redémarrer pour la mise à jour",
    },
    concurrency: {
      title: "Limites de simultanéité",
      agentsLabel: "Agents simultanés max.",
      tasksLabel: "Tâches simultanées max.",
      agentsHelp: "Nombre d'exécutions d'agents pouvant tourner en même temps depuis le tableau.",
      tasksHelp: "Nombre de tâches différentes pouvant avoir un agent en cours d'exécution en même temps.",
      zeroHint: "0 signifie illimité",
      reset: "Illimité",
      resetting: "Réinitialisation…",
      loadFailed: "Impossible de charger les paramètres",
      saveFailed: "Échec de l'enregistrement",
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
