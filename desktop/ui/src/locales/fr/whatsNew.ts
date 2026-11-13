// Dictionnaire français de la fenêtre "Nouveautés"; même forme que en/whatsNew.ts.
export const whatsNew = {
  dialogTitle: "Nouveautés de TaskTrooper {version}",
  seeAll: "Voir tous les changements",
  gotIt: "Compris",
  card: {
    title: "Nouveautés",
    help: "Découvrez ce qui a changé dans la dernière version de TaskTrooper.",
    open: "Afficher les nouveautés",
  },
  v0_3_16: {
    title: "Tests d'audience avant le travail, relances comparées, tâches complètes",
    items: {
      studyRerun: "Relancez une étude terminée après avoir modifié le produit et voyez ce qui a bougé : le score du verdict, les objections disparues ou apparues et l'évolution de chaque personne.",
      pmAudienceTest: "Quand une demande dépend de la réaction des gens (un prix, une fonction payante, des textes, l'onboarding), le PM propose un test d'audience avant d'ouvrir le travail, vous laisse réviser et retester, puis ouvre les tâches quand vous le dites.",
      serialReleases: "Les versions sont livrées une à la fois par composant, donc deux déploiements du même service ne se chevauchent plus.",
      completeTasks: "Les tâches ouvertes par les agents ont toujours une description, des critères d'acceptation et un responsable ; une tâche vide ou non assignée est refusée et corrigée par l'agent.",
      chatThinking: "Pendant qu'un agent travaille, sa réflexion s'affiche directement dans les étapes de réflexion au lieu d'apparaître d'abord comme une réponse ; l'architecte système répond aux questions techniques à partir du code.",
      mascot: "Trooper, la nouvelle mascotte, apparaît dans les écrans vides, la configuration et les avatars des agents.",
    },
  },
  v0_3_15: {
    title: "Les agents ont le bon guide, les tâches un agent et Docker ses commandes",
    items: {
      agentsMd: "La tâche de guide d'agent de la configuration du projet écrit désormais un seul AGENTS.md pour toutes les CLI d'agent et ne demande plus un README que le projet a déjà.",
      assignTasks: "Les tâches ouvertes par une analyse ou un agent reçoivent un agent adapté à leur création, et le PM peut définir ou corriger le responsable d'une tâche sur place au lieu de la recréer.",
      dockerCommands: "Les composants conteneurisés gagnent des commandes Docker (buildkit, construction d'image, compose up, down et stop), des réglages de registre, plateformes et étiquette, une règle d'exécution uniquement dans les conteneurs pour les agents, et chaque commande peut être retirée.",
    },
  },
  v0_3_14: {
    title: "La recherche d'audience sur votre téléphone",
    items: {
      phoneAudience: "Gérez vos audiences depuis l'app mobile TaskTrooper : créez-les avec le PM ou un formulaire, construisez-les et parlez en tête à tête avec chaque personne.",
      phoneStudies: "Lancez des études depuis votre téléphone : choisissez le type, relisez et approuvez le script d'entretien, suivez les entretiens et générez le rapport.",
      phoneReports: "Lisez les rapports d'étude sur votre téléphone et partagez-les sous la forme d'un seul fichier HTML.",
    },
  },
  v0_3_13: {
    title: "Votre téléphone, votre audience et votre télémétrie",
    items: {
      tunnel: "Connectez-vous à Cloudflare ou ngrok depuis Réglages → Accès à distance et joignez cet ordinateur depuis l'app mobile TaskTrooper par un tunnel que vous contrôlez. Associez un téléphone en une étape : Associer un téléphone ouvre une fenêtre avec le code QR et l'adresse.",
      audience: "Étude d'audience : demandez à l'agent PM des personas synthétiques ou lancez vous-même une étude - entretiens, test de page d'atterrissage, test de messages ou test A/B - lisez le rapport et exportez-le en un seul fichier HTML.",
      telemetry: "Les nouveaux écrans Télémétrie dans Opérations montrent services, endpoints, traces, journaux, erreurs, incidents, moniteurs, déploiements et SLO, et l'ingénieur de release prend en charge les incidents qui en découlent.",
      repoSetup: "Importer un dépôt ouvre désormais la liste « Préparer ce projet » (analyse, outils, index, README et guide pour agents, tests, CI, livraison) avec un bouton par étape et un pour tout lancer dans l'ordre. Se déconnecter de votre compte l'efface désormais complètement, si bien que la bannière \"travail sans compte\" ne reste plus affichée.",
      runTrace: "Chaque exécution d'un agent laisse désormais une vraie trace dans le panneau de la tâche : ce qu'il a lu, modifié et exécuté, vérifié par git. Après un aperçu, un clic annule le travail de l'exécution derrière une branche de sauvegarde.",
      reviewHistory: "Chaque tâche porte désormais son historique de revue : chaque tour de revue et de QA avec qui a relu, ce qui a été trouvé et ce qu'il en est advenu, les renvois entre colonnes et ce que chaque relecteur a couvert ou non. Une carte \"À examiner par vous\" liste ce que les relecteurs et le serveur veulent qu'une personne regarde.",
    },
  },
};
