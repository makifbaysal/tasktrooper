// French dictionary for code-health findings. Must mirror
// en/findings.ts keys exactly.
export const findings = {
  intro: "Lu dans le code à chaque scan : anti-patterns, journalisation absente ou mal configurée, réglages de debug qui partent en production, secrets et plus. Les constats acceptés ou marqués comme faux positifs restent fermés lors des scans suivants.",
  allComponents: "Tous les composants",
  repositoryWide: "Tout le dépôt",
  openCount: "{count} ouverts",
  allCategories: "Toutes les catégories",
  occurrences: "{count} occurrences",
  lines: "{count} lignes",
  moreLocations: "et {count} de plus",
  showLocations: "Afficher les emplacements",
  hideLocations: "Masquer les emplacements",
  createTask: "Créer une tâche",
  createTaskSelected: "Créer une tâche pour {count}",
  selectAll: "Tout sélectionner",
  clearSelection: "Effacer",
  accept: "Accepter",
  falsePositive: "Faux positif",
  reopen: "Rouvrir",
  openTask: "Ouvrir {key}",
  reason: "Raison : {reason}",
  resolvedHint: "Le dernier scan ne le trouve plus.",
  updateFailed: "Impossible de mettre à jour le constat",
  componentSummary: {
    open: "{count} constats de santé du code ouverts",
    none: "Aucun constat de santé du code ouvert",
    view: "Voir les constats",
  },
  status: {
    open: "Ouvert",
    accepted: "Accepté",
    false_positive: "Faux positif",
    resolved: "Résolu",
  },
  severity: {
    high: "Élevée",
    medium: "Moyenne",
    low: "Faible",
  },
  category: {
    security: "Sécurité",
    configuration: "Configuration",
    logging: "Journalisation",
    error_handling: "Gestion des erreurs",
    observability: "Observabilité",
    reliability: "Fiabilité",
    quality: "Qualité du code",
  },
  empty: {
    noScanTitle: "Pas encore scanné",
    noScanDescription: "Les constats apparaissent après le premier scan du dépôt.",
    cleanTitle: "Aucun problème trouvé",
    cleanDescription: "Le dernier scan n'a rien trouvé à corriger ici.",
    filtered: "Aucun constat ne correspond à ce filtre.",
  },
  verdict: {
    acceptTitle: "Accepter le risque",
    acceptDescription: "Le constat est fermé et le reste lors des scans suivants, même si le code le contient toujours.",
    falsePositiveTitle: "Marquer comme faux positif",
    falsePositiveDescription: "Indiquez que le scan se trompe ici. Le constat reste fermé lors des scans suivants.",
    reasonLabel: "Raison (facultatif)",
    reasonPlaceholder: "Pourquoi c'est acceptable ici",
    save: "Enregistrer",
  },
  task: {
    title: "Créer une tâche",
    description: "Une tâche du tableau couvrant {count} constats. L'agent reçoit chaque emplacement et la façon de corriger chacun.",
    titleLabel: "Titre",
    notesLabel: "Notes pour l'agent (facultatif)",
    notesPlaceholder: "Ce que l'agent doit savoir d'abord",
    submit: "Créer la tâche",
    created: "Tâche {key} créée",
    failed: "Impossible de créer la tâche",
    defaultTitleMany: "Corriger {count} constats de santé du code dans {component}",
    severalComponents: "plusieurs composants",
  },
  rules: {
    hardcoded_secret: {
      title: "Secret dans le code",
      description: "Une clé ou un jeton au format connu se trouve dans le dépôt. Révoquez-le, puis lisez-le depuis la configuration.",
    },
    hardcoded_credential: {
      title: "Identifiant codé en dur",
      description: "Un mot de passe, une clé ou un jeton est écrit en littéral au lieu d'être lu depuis la configuration.",
    },
    committed_env_file: {
      title: "Fichier env avec secrets non ignoré",
      description: "Un fichier .env contenant de vrais secrets est versionné ou n'est pas ignoré par git.",
    },
    tls_verification_disabled: {
      title: "Vérification TLS désactivée",
      description: "La vérification du certificat ou du nom d'hôte est coupée, le trafic peut être intercepté.",
    },
    cleartext_traffic_allowed: {
      title: "HTTP en clair autorisé",
      description: "L'application autorise le HTTP non chiffré pour tous les hôtes.",
    },
    sql_injection_risk: {
      title: "Injection SQL possible",
      description: "Une valeur est insérée dans une chaîne SQL au lieu d'être passée en paramètre.",
    },
    dynamic_code_execution: {
      title: "Exécution de code dynamique",
      description: "eval, exec ou une désérialisation non sûre exécutent ce que dit l'entrée.",
    },
    unsafe_html_injection: {
      title: "HTML non échappé",
      description: "Du HTML brut est rendu sans assainissement, ce qui permet le cross-site scripting.",
    },
    permissive_cors: {
      title: "CORS ouvert à toute origine",
      description: "N'importe quel site peut appeler cette API depuis le navigateur d'un visiteur.",
    },
    insecure_token_storage: {
      title: "Jetons en stockage non protégé",
      description: "Des jetons ou mots de passe sont gardés dans localStorage, UserDefaults ou équivalent.",
    },
    http_body_logging: {
      title: "Corps ou en-têtes de requête journalisés",
      description: "Corps et en-têtes transportent jetons et données personnelles dans les logs.",
    },
    error_details_exposed: {
      title: "Détails d'erreur envoyés au client",
      description: "Des traces de pile ou des pages d'erreur de développement atteignent les utilisateurs.",
    },
    public_source_maps: {
      title: "Source maps publiques",
      description: "Le build de production publie le code source d'origine.",
    },
    debug_mode_in_production: {
      title: "Mode debug en production",
      description: "La configuration de production active le mode debug ou développement.",
    },
    debug_log_level_in_production: {
      title: "Niveau de log debug en production",
      description: "La configuration de production journalise au niveau debug ou trace.",
    },
    debuggable_release_build: {
      title: "Build release débogable",
      description: "N'importe qui peut attacher un débogueur au build release Android.",
    },
    release_minification_disabled: {
      title: "Build release non minifié",
      description: "R8/ProGuard est désactivé pour le build release.",
    },
    unconditional_debug_logging: {
      title: "Logger figé en debug",
      description: "Le niveau de log est fixé à debug dans le code, sans tenir compte de l'environnement.",
    },
    console_logs_in_production: {
      title: "Sorties console de debug en production",
      description: "Les appels console.log/debug/info partent dans le bundle et le build ne les retire pas.",
    },
    mobile_debug_prints: {
      title: "Prints de debug dans les builds release",
      description: "print, NSLog ou Log.d s'exécutent hors des gardes de debug.",
    },
    print_instead_of_logger: {
      title: "Prints au lieu de logs",
      description: "Le code du service écrit sur stdout au lieu d'utiliser le logger.",
    },
    print_stack_trace: {
      title: "Traces imprimées, non journalisées",
      description: "printStackTrace ou print_exc contournent la journalisation.",
    },
    no_structured_logging: {
      title: "Aucune bibliothèque de logging",
      description: "Le service n'a pas de logger, donc aucun log structuré par niveau.",
    },
    no_request_logging: {
      title: "Pas de journalisation des requêtes",
      description: "Le service HTTP ne journalise pas les requêtes (méthode, statut, durée).",
    },
    catch_without_logging: {
      title: "Erreurs avalées sans log",
      description: "Les erreurs capturées ne sont ni journalisées, ni relancées, ni signalées.",
    },
    empty_catch_block: {
      title: "Gestionnaire d'erreur vide",
      description: "Un bloc catch, except ou err vide masque tout échec sur ce chemin.",
    },
    bare_except: {
      title: "except nu",
      description: "except: capture aussi KeyboardInterrupt et SystemExit.",
    },
    no_error_boundary: {
      title: "Pas d'error boundary React",
      description: "Une seule erreur de rendu vide toute l'interface et rien n'est signalé.",
    },
    no_crash_reporting: {
      title: "Pas de remontée de crashs ou d'erreurs",
      description: "Les erreurs rencontrées par les utilisateurs en production restent invisibles.",
    },
    no_health_endpoint: {
      title: "Pas d'endpoint de santé",
      description: "Les sondes et contrôles de déploiement ne savent pas si le service fonctionne.",
    },
    http_client_without_timeout: {
      title: "Appels HTTP sans timeout",
      description: "Une dépendance lente peut bloquer les requêtes indéfiniment.",
    },
    force_unwrap: {
      title: "Nombreux force unwraps",
      description: "try!, as! et !! plantent sur un nil inattendu.",
    },
    mutable_default_argument: {
      title: "Argument par défaut mutable",
      description: "Une valeur par défaut list ou dict est partagée entre les appels.",
    },
    debug_leftovers: {
      title: "Instructions de debug oubliées",
      description: "Des debugger, points d'arrêt ou var_dump/dd restent dans le code.",
    },
    oversized_source_file: {
      title: "Fichiers source très volumineux",
      description: "Les fichiers de plus de 1 500 lignes sont difficiles à relire et à modifier.",
    },
    typescript_any_overuse: {
      title: "Nombreux any / @ts-ignore",
      description: "La vérification des types est désactivée à de nombreux endroits.",
    },
    tech_debt_markers: {
      title: "Nombreux TODO/FIXME",
      description: "Des problèmes connus qui ne sont suivis nulle part.",
    },
    no_automated_tests: {
      title: "Aucun test automatisé",
      description: "Le composant contient du code mais aucun fichier de test.",
    },
    no_linter_configured: {
      title: "Aucun linter configuré",
      description: "Les écarts de style et les erreurs courantes ne sont pas vérifiés.",
    },
    wildcard_import: {
      title: "Imports génériques",
      description: "from x import * masque l'origine des noms.",
    },
  },
};
