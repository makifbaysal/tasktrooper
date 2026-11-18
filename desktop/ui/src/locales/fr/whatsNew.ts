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
  v0_3_20: {
    title: "TaskTrooper Telemetry dans Opérations, un designer qui dessine dans FigCraft, des marqueurs de déploiement",
    items: {
      operationsSource: "Connectez TaskTrooper Telemetry dans Réglages et elle devient d'elle-même une source de données d'Opérations : graphiques RED, logs, traces, erreurs et endpoints pour chaque composant.",
      agentTools: "Les agents reçoivent les outils de télémétrie dont leur rôle a besoin : analyse de cause racine par endpoint, SLO, moniteurs et tests de charge. Ils ne peuvent modifier ou supprimer que les moniteurs et KPI créés par un agent.",
      deployMarkers: "Chaque déploiement mis en production est marqué dans la télémétrie connectée avec son commit, pour qu'une régression apparaisse à côté de la version qui l'a causée.",
      figcraftDesigner: "Le ui-designer dessine dans FigCraft : chaque tâche de design a son propre fichier modifiable fondé sur votre design system, dont vous pouvez prévisualiser les pages, l'ouvrir dans FigCraft ou l'afficher dans le Finder depuis la tâche. FigCraft, VectorCraft et PhotoCraft s'installent depuis Réglages → Environnement de développement.",
      telemetryScreens: "Nouveaux écrans de télémétrie : vue d'ensemble, carte des services, infrastructure, apps, profiling, usage des LLM et tests de charge.",
    },
  },
  v0_3_19: {
    title: "Une QA qui teste et conclut, des tests automatisés par dépôt, un aperçu par tâche",
    items: {
      qaManualRound: "La QA teste désormais le produit en marche selon les critères de la tâche au lieu de le compiler : build et tests unitaires restent à la revue de code.",
      qaFinishes: "Une passe de QA ne peut pas se terminer avec un cas non exécuté ou un critère sans verdict, ni renvoyer une tâche sans échec enregistré.",
      automationTests: "Activez Tests automatisés pour un dépôt (Composants → Revue) : la QA ajoute les cas de chaque tâche en tests Testcontainers + WireMock sous qa-automation/, et une tâche ne quitte la QA qu'avec cette suite au vert. L'activer ouvre une tâche qui écrit les tests manquants.",
      taskPreviews: "Chaque tâche a son propre aperçu local sur son propre port, et déplacer une tâche n'arrête que son aperçu.",
    },
  },
  v0_3_18: {
    title: "Parlez à vos agents, joignez et mentionnez partout, des plans de tâches plus légers",
    items: {
      voiceChat: "Parlez aux agents à voix haute en turc ou en anglais : la réponse est lue au fil de l'écriture, les dessins s'affichent à l'écran et vous pouvez interrompre en parlant. Choisissez le modèle et la voix dans Réglages → Voix.",
      attachAndMention: "Collez ou joignez des fichiers et des images, et mentionnez agents, projets et dépôts avec @, sur l'accueil, dans la question rapide en haut et dans les commentaires de tâche.",
      notifications: "Le centre de notifications affiche l'expéditeur de chaque notification avec son avatar, dans votre langue, et le badge de la barre latérale compte ce qui est vraiment non lu.",
      onePerFeature: "Le PM ouvre une tâche par fonctionnalité, dépôt et rôle au lieu de plusieurs petites, pour que chaque fonctionnalité soit développée, revue et publiée une seule fois.",
      releaseBatch: "Le release engineer peut proposer une publication groupée à votre approbation et demande pas à pas comment un composant se déploie.",
    },
  },
  v0_3_17: {
    title: "Aperçus hébergés d'abord, captures que les agents voient, fini le ping-pong QA",
    items: {
      hostedPreview: "Quand la pull request d'une tâche a un aperçu hébergé prêt (Vercel), le panneau d'aperçu l'ouvre d'abord au lieu de lancer une exécution locale.",
      chatImages: "Les captures d'écran jointes dans un chat d'agent atteignent désormais les agents sous Claude Code, opencode, Cursor et Antigravity, qui peuvent lire leur contenu.",
      qaBlocked: "Quand la QA ne peut pas vérifier quelque chose faute de ressource dans l'environnement (intégration, identifiant ou appareil), la tâche vous attend au lieu de faire des allers-retours entre la QA et le développeur ; la QA ne reteste plus un code déjà rejeté et inchangé.",
    },
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
