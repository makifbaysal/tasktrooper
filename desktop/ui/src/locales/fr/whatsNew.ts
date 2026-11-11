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
