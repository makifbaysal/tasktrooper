import type { TelemetryDict, TelemetryKindsDict } from "@/locales/en/opsTelemetry";

// French strings for the console's telemetry views; mirrors en/opsTelemetry.ts key for key.
export const telemetry: TelemetryDict = {
  table: {
    readAt: "Lue {time}",
    readAtTitle: "Lue le {time}. Le serveur garde une lecture 30 secondes : elle peut donc avoir jusqu'à 30 s de plus que la dernière actualisation de cette page.",
    window: "Fenêtre : {span}",
    windowUnknown: "Couvre la fenêtre récente propre à la source",
    windowDocs: "Fenêtres des tableaux",
    rows: "{count} lignes",
    flag: "Repère",
    sortBy: "Trier par {column}",
    truncated: "La source contient plus de lignes qu'elle n'en a envoyé : les {count} premières sont affichées.",
    showAll: "Afficher les {count} lignes",
    noRows: "La source n'a rien pour cela en ce moment.",
    noValue: "Aucune valeur",
    unnamedSource: "Source de données supprimée",
    recipe: "Recette {recipe}",
    customQuery: "Requête personnalisée",
    loadFailed: "Impossible de lire ce tableau",
    refreshFailed: "Impossible d'actualiser, la dernière réponse est affichée",
    errors: {
      failed: "Impossible de lire ce tableau",
      needsParams: "Il manque un paramètre à la liaison",
      needsParamsBody: "Le tableau a besoin d'une valeur que sa liaison n'a pas encore.",
      notFound: "La source de données n'existe plus",
      notFoundBody: "La source de données d'où ce tableau était lu n'existe plus. Liez-en une autre.",
      unsupported: "Cette source ne peut pas lister de lignes",
      unsupportedBody: "Son type peut lire le signal dans le temps, mais pas une ligne par élément. Une recette qui comporte ce tableau le permet.",
      badQuery: "La source a refusé la requête du tableau",
      timeout: "La source n'a pas répondu à temps",
      timeoutBody: "Elle est peut-être occupée. Réessayez dans un instant.",
      upstream: "La source n'a pas répondu",
      openSignals: "Ouvrir Signaux",
      retry: "Réessayer",
    },
  },
  endpoints: {
    title: "Endpoints",
    hint: "Chaque tableau couvre la fenêtre récente de sa source, lu toutes les minutes",
    scopeNote: "Chaque tableau d'endpoints couvre la fenêtre récente de sa source, indiquée sur le tableau ; la période ne s'applique donc pas.",
    flags: {
      errors: "Taux d'erreur le plus élevé",
      slowest: "p95 le plus lent",
    },
    empty: {
      title: "Pas encore de tableau des endpoints",
      body: "Les endpoints de {name} (débit, taux d'erreur et p95 par route) viennent d'une source dont la recette les liste : les métriques OpenTelemetry dans Prometheus, New Relic ou Datadog. Liez l'un de ses signaux dans Signaux.",
      signals: "Ouvrir Signaux",
      addSource: "Ajouter une source de données",
    },
  },
  pages: {
    scopeNote: "Chaque tableau web couvre la fenêtre récente de sa source, indiquée sur le tableau ; la période ne s'applique donc pas.",
    hint: "Chaque tableau couvre la fenêtre récente de sa source d'analyse web",
    tabsLabel: "Tableaux web",
    tabs: {
      pages: "Pages",
      referrers: "Référents",
      countries: "Pays",
      devices: "Appareils",
    },
    empty: {
      title: "Pas encore de tableau {table}",
      body: "Le tableau {table} de {name} vient d'une source d'analyse web (PostHog, Google Analytics 4 ou Plausible) liée dans Signaux.",
      signals: "Ouvrir Signaux",
      addSource: "Ajouter une source de données",
    },
  },
  crashes: {
    scopeNote: "Chaque tableau de plantages couvre la fenêtre récente de sa source, indiquée sur le tableau ; la période ne s'applique donc pas.",
    openIssue: "Ouvrir dans sa console",
    issues: {
      title: "Problèmes de plantage et d'ANR",
      hint: "Les problèmes d'erreur de Google Play et de Sentry, ceux qui touchent le plus d'utilisateurs en tête",
    },
    signatures: {
      title: "Signatures de diagnostic",
      hint: "Les signatures de blocage, de lancement et d'écriture disque du dernier build, selon App Store Connect",
    },
    types: {
      crash: "Plantage",
      anr: "ANR",
      non_fatal: "Non fatal",
      hangs: "Blocage",
      launches: "Lancement",
      disk_writes: "Écritures disque",
      fatal: "Fatal",
      error: "Erreur",
      warning: "Avertissement",
    },
    empty: {
      title: "Pas encore de données de plantage",
      body: "Les plantages de {name} viennent de Google Play vitals, d'App Store Connect ou de Sentry. Trouvez des suggestions dans Signaux : une app de store liée y propose sa source en une étape.",
      signals: "Ouvrir Signaux",
      addSource: "Ajouter une source de données",
    },
  },
  traces: {
    minDuration: "Durée minimale (ms)",
    minDurationHint: "Uniquement les traces d'au moins cette durée. Appuyez sur Entrée ou quittez le champ pour lancer la recherche.",
    manage: "Gérer la liaison des traces",
    loadFailed: "Impossible de rechercher les traces",
    refreshFailed: "Impossible d'actualiser les traces, la dernière réponse est affichée",
    capped: "Les {count} traces les plus récentes sont affichées. Réduisez la période ou augmentez la durée minimale pour voir les autres.",
    openFromLog: "Ouvrir la trace",
    clamped: {
      title: "Recherche sur les 7 derniers jours de la période",
      body: "Une recherche de traces couvre 7 jours au plus : elle part donc de {since}.",
    },
    unbound: {
      title: "Connectez une source de traces",
      body: "Les traces de {name} viennent d'un backend de tracing comme Grafana Tempo. Liez ses traces à une source Tempo, ou ajoutez-en une d'abord.",
      bind: "Lier les traces",
      addSource: "Ajouter une source de données",
    },
    empty: {
      title: "Aucune trace sur cette période",
      body: "La source de traces n'en contient aucune pour ce composant depuis {since}.",
      withMin: "Aucune trace de cette période n'a duré {min} ms ou plus.",
      clearMin: "Effacer la durée minimale",
    },
    list: {
      label: "Traces",
      count: "{count} traces",
      failedCount: "{count} en erreur",
      order: "Ordre",
      orders: {
        newest: "Plus récentes d'abord",
        slowest: "Plus lentes d'abord",
      },
      columns: {
        start: "Début",
        root: "Span racine",
        duration: "Durée",
        spans: "Spans",
        status: "Statut",
      },
      open: "Ouvrir la trace {name}",
      noRoot: "Span racine introuvable",
      noService: "Service inconnu",
      error: "Erreur",
      ok: "OK",
    },
    detail: {
      back: "Retour aux traces",
      untitled: "Trace",
      copy: "Copier l'ID de trace",
      copied: "ID de trace copié",
      duration: "Durée",
      spans: "Spans",
      errorCount: "Erreurs",
      services: "Services",
      serviceSpans: "{count} spans",
      serviceErrors: "{count} en échec",
      viewLabel: "Vue de la trace",
      views: {
        waterfall: "Cascade",
        table: "Tableau",
      },
      hint: "Choisissez un span pour voir ses attributs.",
      noSpans: "La source a envoyé cette trace sans spans.",
      refreshFailed: "Impossible de relire la trace, la dernière réponse est affichée",
      truncated: {
        title: "Tous les spans ne sont pas affichés",
        body: "La trace compte plus de spans qu'une réponse n'en contient ; les {count} premiers sont affichés.",
      },
      elsewhere: {
        title: "Trouvée via un autre composant",
        body: "Ce composant n'a pas de liaison de traces : la trace a donc été cherchée dans toutes les sources de traces de l'environnement et trouvée via {name}.",
        action: "L'ouvrir sur {name}",
      },
      errors: {
        notFound: "Trace introuvable",
        notFoundBody: "Aucune source de traces de cet environnement ne contient la trace {id}. Elle est peut-être plus ancienne que ce que la source conserve.",
        unbound: "Aucune source de traces où chercher",
        unboundBody: "Aucun composant de cet environnement n'a de liaison de traces : la trace {id} ne peut donc pas être recherchée.",
        timeout: "La source de traces n'a pas répondu à temps",
        timeoutBody: "Elle est peut-être occupée. Réessayez dans un instant.",
        failed: "Impossible de lire la trace",
      },
    },
    waterfall: {
      span: "Span",
      listLabel: "Spans",
      tableLabel: "Spans sous forme de tableau",
      rowLabel: "{service} {name}, commence à +{offset}, dure {duration}",
      failed: "en échec",
      noService: "Service inconnu",
      noName: "Span sans nom",
      expand: "Afficher les spans sous {name}",
      collapse: "Masquer les spans sous {name}",
      columns: {
        depth: "Profondeur",
        service: "Service",
        operation: "Opération",
        start: "Commence à",
        duration: "Durée",
        status: "Statut",
      },
      status: {
        unset: "Non défini",
        ok: "OK",
        error: "Erreur",
      },
    },
    span: {
      close: "Fermer le span",
      duration: "Durée",
      offset: "Commence à",
      started: "Heure de début",
      id: "ID du span",
      parent: "Span parent",
      copy: "Copier l'ID du span",
      copied: "ID du span copié",
      attributes: "Attributs",
      resource: "Ressource",
      noAttributes: "Aucun attribut.",
      statuses: {
        ok: "OK",
        error: "Erreur",
      },
      kinds: {
        server: "Serveur",
        client: "Client",
        internal: "Interne",
        producer: "Producteur",
        consumer: "Consommateur",
      },
    },
  },
  webVitals: {
    title: "Core Web Vitals",
    hint: "Le dernier p75 des chargements de page sur la période, comparé aux seuils de Google",
    traffic: "Trafic",
    bands: {
      good: "Bon",
      needsImprovement: "À améliorer",
      poor: "Mauvais",
    },
    quantileP75: "p75",
    thresholds: "Bon jusqu'à {good}, mauvais au-delà de {poor}",
    meterLabel: "{band} : bon jusqu'à {good}, mauvais au-delà de {poor}",
    bind: "Lier une source de données",
    noData: "Aucune donnée sur cette période",
    readFailed: "Lecture impossible",
    latest: "Dernière valeur de la période",
    live: "Valeur en direct, sans historique",
  },
  mobileVitals: {
    window: "7 derniers jours en {env}, quelle que soit la période.",
    asOf: "Au {date}",
    bind: "Lier dans Signaux",
    noData: "Aucune donnée sur les 7 derniers jours",
    notYet: "Pas encore publié",
    playLag: "Les données de Google Play ont 1 à 2 jours de retard : les jours les plus récents se remplissent plus tard.",
    readFailed: "Lecture impossible",
  },
  offers: {
    title: "Sources de données à connecter",
    found: "{count} sources de données à connecter",
    bound: "{count} signaux liés via {name}",
    titles: {
      playVitals: "Connecter Google Play vitals pour cette app",
      appStoreMetrics: "Connecter les métriques App Store pour cette app",
      generic: "Connecter {kind} pour cette app",
    },
    bodies: {
      playVitals: "Utilisateurs sans plantage, taux de plantage, taux d'ANR et principaux problèmes de plantage, lus avec les identifiants Google Play de l'espace de travail. Rien à installer.",
      appStoreMetrics: "Temps de lancement par version de l'app et signatures de blocage, de lancement et d'écriture disque de chaque build, lus avec la clé App Store Connect de l'espace de travail. Rien à installer.",
      generic: "Une source {kind} lirait ces signaux pour cette app.",
    },
    credentialMissing: {
      google_play: "Enregistrez d'abord les identifiants Google Play.",
      asc: "Enregistrez d'abord la clé App Store Connect.",
    },
    saveCredential: "Réglages → Intégrations",
    connect: "Connecter",
    connecting: "Connexion…",
    notNow: "Pas maintenant",
    recipe: "Recette {recipe}",
  },
  binding: {
    noPreviewTitle: "Pas d'aperçu pour ce signal",
    noPreview: "{signal} n'est pas une série à tracer : les logs, les traces et les groupes d'erreurs se lisent dans l'explorateur de logs, dans Traces et dans la boîte des erreurs. Enregistrez pour le lier.",
  },
  errors: {
    providers: {
      sentry: "Sentry",
    },
    fromProvider: "Lu depuis {provider}",
    noTask: "Ce groupe n'indique aucune source de données pour créer une tâche. Ouvrez-le plutôt dans {provider}.",
    openSource: "Source de données {provider}",
    fixSource: "Ouvrir la source de données",
  },
  sources: {
    storeCredentials: {
      google_play: "Compte de service Google Play",
      asc: "Clé API App Store Connect",
    },
    storeCredentialWorkspace: "de l'espace de travail",
    storeCredentialPlaceholder: "{credential} à utiliser",
    noStoreCredential: "{credential} : rien n'est encore enregistré.",
    saveStoreCredential: "À enregistrer dans Réglages → Intégrations",
    namePlaceholderStore: "Par défaut, le nom du type",
    errors: {
      store_credential: {
        not_found: "Ces identifiants ne sont pas encore enregistrés. Enregistrez-les d'abord dans Réglages → Intégrations.",
        wrong_provider: "Ces identifiants appartiennent à l'autre store.",
        not_permitted: "Les identifiants de store ne peuvent pas servir aux sources de données dans cette installation.",
      },
      org: {
        invalid: "Saisissez le slug de l'organisation, comme dans sentry.io/organizations/<slug>/.",
      },
      index: {
        invalid: "Saisissez un index, un data stream ou un motif, comme logs-* ; plusieurs, séparés par des virgules.",
      },
    },
  },
};

const AUTH_OPTIONS = {
  none: "Aucune",
  bearer: "Jeton bearer",
  basic: "Basic (nom d'utilisateur et mot de passe)",
};

export const telemetryKinds: TelemetryKindsDict = {
  google_play_vitals: {
    label: "Google Play vitals",
    description:
      "Les Android vitals de Google Play (taux de plantage et d'ANR, démarrages lents et principaux problèmes de plantage), lus avec les identifiants Google Play de l'espace de travail. Rien à installer ; Play les publie avec un ou deux jours de retard.",
    queryPlaceholder: "p. ex. {\"metric_set\": \"crashRateMetricSet\", \"metric\": \"userPerceivedCrashRate\", \"as\": \"percent\"}",
    queryHint:
      "JSON : {\"package\"?, \"metric_set\", \"metric\", \"dimension\"?, \"filter\"?, \"period\"?, \"as\"?}. metric_set vaut crashRateMetricSet, anrRateMetricSet, slowStartRateMetricSet ou errorCountMetricSet ; period vaut daily (par défaut) ou hourly ; as vaut value, percent ou free_percent. Les données quotidiennes ont 1 à 2 jours de retard.",
    fields: {
      store_credential: {
        label: "Identifiants Google Play",
        help: "Le compte de service enregistré dans Réglages → Intégrations → Google Play. Il lui faut l'API Play Developer Reporting activée dans son projet Google Cloud et, dans la Play Console, l'autorisation de voir les informations de l'app.",
      },
      package_name: {
        label: "Nom du package",
        help: "L'app lue quand une requête n'en nomme aucune. La liaison d'un composant le renseigne à partir de l'app Android liée à son dépôt.",
        placeholder: "com.example.app",
      },
    },
  },
  app_store_metrics: {
    label: "Métriques App Store Connect",
    description:
      "Le temps de lancement, les blocages, la mémoire, les écritures disque et la consommation de batterie d'une app iOS, tels que les montre Xcode Organizer, par version de l'app, ainsi que les diagnostics de blocage et de lancement de chaque build, lus avec la clé App Store Connect de l'espace de travail. Apple ne fournit pas de taux sans plantage.",
    queryPlaceholder: "p. ex. {\"metric\": \"launchTime\", \"percentile\": \"p90\"}",
    queryHint:
      "JSON : {\"app\"?, \"metric\", \"percentile\"?, \"device\"?, \"version\"?}. metric est une métrique Xcode Organizer (launchTime, hangRate, …) ou une catégorie entière (launch, hang, memory, disk, battery) ; percentile vaut p50 ou p90. Les valeurs arrivent par version de l'app et sont tracées en paliers.",
    fields: {
      store_credential: {
        label: "Identifiants App Store Connect",
        help: "La clé API enregistrée dans Réglages → Intégrations → App Store Connect.",
      },
      app_id: {
        label: "ID de l'app",
        help: "L'app lue quand une requête n'en nomme aucune : son ID App Store Connect. La liaison d'un composant le renseigne à partir de l'app iOS liée à son dépôt.",
        placeholder: "1234567890",
      },
    },
  },
  sentry: {
    label: "Sentry",
    description:
      "Utilisateurs et sessions sans plantage, nombres d'erreurs et problèmes depuis Sentry, pour les projets mobiles, web comme backend. Ses problèmes rejoignent la boîte des erreurs.",
    queryPlaceholder: "p. ex. {\"project\": \"ios-app\", \"metric\": \"crash_free_users\"}",
    queryHint:
      "JSON : {\"project\", \"metric\"?, \"environment\"?, \"release\"?, \"search\"?, \"days\"?}. metric vaut events (par défaut), event_rate, crash_free_users, crash_free_sessions, crash_rate, crash_rate_users, sessions ou users ; les métriques de plantage demandent le suivi des sessions dans le SDK.",
    fields: {
      url: {
        label: "URL Sentry",
        help: "https://sentry.io pour le cloud de Sentry (https://de.sentry.io pour une organisation de la région UE), ou l'adresse de votre Sentry auto-hébergé.",
        placeholder: "https://sentry.io",
      },
      org: {
        label: "Organisation",
        help: "Le slug de l'organisation, comme dans sentry.io/organizations/<slug>/.",
        placeholder: "acme",
      },
      token: {
        label: "Jeton d'authentification",
        help: "Un jeton avec les scopes org:read, project:read et event:read : un jeton d'organisation ou celui d'une intégration interne. Stocké chiffré.",
      },
    },
  },
  loki: {
    label: "Loki",
    description: "Grafana Loki ou Grafana Cloud Logs : les lignes de log d'un composant, et les taux d'erreur qui en sont tirés avec LogQL.",
    queryPlaceholder: "p. ex. {service_name=\"api\"} |= \"error\"",
    queryHint:
      "LogQL. Une requête de logs ({service_name=\"api\"} |= \"timeout\") alimente l'explorateur de logs ; une requête de métrique (sum(rate({service_name=\"api\"} | detected_level=\"error\" [5m]))) trace un graphique.",
    fields: {
      url: {
        help: "L'URL de base de Loki, sans /loki/api/v1. Pour Grafana Cloud, l'URL Logs (https://logs-prod-<n>.grafana.net).",
        placeholder: "http://localhost:3100",
      },
      auth: {
        options: AUTH_OPTIONS,
      },
      username: {
        help: "Pour Grafana Cloud, l'ID utilisateur Logs.",
      },
      password: {
        help: "Pour Grafana Cloud, un jeton d'access policy avec logs:read.",
      },
    },
  },
  elasticsearch: {
    label: "Elasticsearch / OpenSearch",
    description: "Des lignes de log conservées dans Elasticsearch, OpenSearch ou Elastic Cloud : un motif d'index, filtré par champ.",
    queryPlaceholder: "p. ex. {\"filters\": {\"service.name\": \"billing\"}}",
    queryHint: "JSON : {\"filters\": {\"<field>\": \"<value>\", …}}, chaque filtre devant correspondre, 32 au plus. Correspondance exacte sur un champ keyword ; sur un champ text, chaque mot doit être présent.",
    fields: {
      url: {
        help: "L'URL de base d'Elasticsearch ou d'OpenSearch. Pour Elastic Cloud, l'endpoint Elasticsearch du déploiement.",
        placeholder: "http://localhost:9200",
      },
      index: {
        label: "Index",
        help: "L'index, le data stream ou le motif qui contient les logs ; plusieurs, séparés par des virgules (logs-*,app-*).",
        placeholder: "logs-*",
      },
      time_field: {
        label: "Champ de date",
        help: "Le champ de date selon lequel chaque ligne est triée et filtrée. Laissé vide : @timestamp.",
        placeholder: "@timestamp",
      },
      message_field: {
        label: "Champ du message",
        help: "Le champ qui contient la ligne elle-même ; le filtre de texte y cherche. Laissé vide : message.",
        placeholder: "message",
      },
      level_field: {
        label: "Champ du niveau",
        help: "Le champ qui contient le niveau (log.level dans ECS). Laissé vide : log.level.",
        placeholder: "log.level",
      },
      auth: {
        options: {
          none: "Aucune",
          basic: "Basic (nom d'utilisateur et mot de passe)",
          api_key: "Clé API",
        },
      },
      api_key: {
        label: "Clé API",
        help: "La clé API encodée (base64 de id:key), telle que Kibana l'affiche à la création de la clé. Stockée chiffrée.",
      },
    },
  },
  tempo: {
    label: "Tempo",
    description: "Grafana Tempo ou Grafana Cloud Traces : les traces d'un composant, recherchées avec TraceQL.",
    queryPlaceholder: "p. ex. { resource.service.name = \"api\" }",
    queryHint: "TraceQL : les spans d'où partent les traces d'un composant, comme { resource.service.name = \"api\" }. Lues dans Traces, jamais sous forme de graphique.",
    fields: {
      url: {
        help: "La base de l'API HTTP de Tempo, sans /api. Pour Grafana Cloud, l'URL Traces qui se termine par /tempo.",
        placeholder: "http://localhost:3200",
      },
      auth: {
        options: AUTH_OPTIONS,
      },
      username: {
        help: "Pour Grafana Cloud, l'ID utilisateur Traces.",
      },
      password: {
        help: "Pour Grafana Cloud, un jeton d'access policy avec traces:read.",
      },
    },
  },
  posthog: {
    label: "PostHog",
    description:
      "Pages vues, sessions, visiteurs, exceptions JavaScript et web vitals depuis PostHog, avec les principales pages, référents, pays et appareils. Rien à installer en dehors de posthog-js.",
    queryPlaceholder: "p. ex. {\"metric\": \"lcp\", \"quantile\": 0.75}",
    queryHint:
      "JSON : {\"metric\"?, \"event\"?, \"quantile\"?, \"host\"?}. metric vaut page_views (par défaut), sessions, visitors, js_errors, events (avec l'événement compté) ou un web vital : lcp, inp, cls, fcp, au quantile 0.75 sauf indication contraire. host ne garde qu'un nom d'hôte.",
    fields: {
      url: {
        label: "URL PostHog",
        help: "https://us.posthog.com ou https://eu.posthog.com pour PostHog Cloud, ou l'adresse de votre PostHog auto-hébergé.",
        placeholder: "https://us.posthog.com",
      },
      project_id: {
        label: "ID du projet",
        help: "Le nombre dans l'URL du projet (/project/<id>) ou dans Project settings.",
        placeholder: "12345",
      },
      personal_api_key: {
        label: "Clé API personnelle",
        help: "Une clé API personnelle (phx_…) avec le scope Query Read sur ce projet. Stockée chiffrée.",
      },
    },
  },
  ga4: {
    label: "Google Analytics 4",
    description:
      "Pages vues, sessions et utilisateurs actifs d'une propriété Google Analytics 4, avec les principales pages, référents, pays et appareils. GA4 peut mettre jusqu'à deux jours à traiter les données : les points les plus récents peuvent encore augmenter.",
    queryPlaceholder: "p. ex. {\"metric\": \"page_views\"}",
    queryHint:
      "JSON : {\"metric\"?, \"event\"?, \"host\"?, \"property\"?}. metric vaut page_views (par défaut), sessions, visitors ou events (avec l'événement compté, l'eventName de GA4). property lit une autre propriété que celle de la source.",
    fields: {
      property_id: {
        label: "ID de la propriété",
        help: "Le nombre sous Admin → Property details (pas l'ID de mesure G-…).",
        placeholder: "123456789",
      },
      auth: {
        label: "Se connecter avec",
        help: "Un compte Google Cloud connecté, ou une clé de compte de service pour cette seule source.",
        options: {
          cloud_account: "Un compte Google Cloud connecté",
          service_account: "Une clé de compte de service",
        },
      },
      cloud_account_id: {
        label: "Compte Google Cloud",
        help: "Son compte de service doit avoir le rôle Viewer sur la propriété (Admin → Property access management), avec l'API Google Analytics Data activée dans son projet.",
      },
      service_account_json: {
        label: "Clé de compte de service (JSON)",
        help: "La clé d'un compte de service qui a le rôle Viewer sur la propriété, avec l'API Google Analytics Data activée dans son projet. Collez le fichier JSON en entier. Stockée chiffrée.",
      },
    },
  },
  plausible: {
    label: "Plausible",
    description: "Pages vues, visites et visiteurs depuis Plausible Analytics, avec les principales pages, sources, pays et appareils.",
    queryPlaceholder: "p. ex. {\"metric\": \"visitors\"}",
    queryHint: "JSON : {\"metric\"?, \"host\"?, \"site\"?}. metric vaut page_views (par défaut), sessions (les visites) ou visitors. site lit un autre site que celui de la source.",
    fields: {
      url: {
        label: "URL Plausible",
        help: "https://plausible.io, ou l'adresse de votre Plausible auto-hébergé.",
        placeholder: "https://plausible.io",
      },
      site_id: {
        label: "Site",
        help: "Le domaine du site tel qu'il est nommé dans Plausible.",
        placeholder: "example.com",
      },
      api_key: {
        label: "Clé Stats API",
        help: "Une clé Stats API (Account settings → API keys) de l'équipe propriétaire du site. Stockée chiffrée.",
      },
    },
  },
  datadog: {
    label: "Datadog",
    description: "Débits de requêtes, erreurs et latence APM, et métriques d'hôtes ou de conteneurs, depuis l'API de métriques de Datadog.",
    queryPlaceholder: "p. ex. sum:trace.http.request.hits{service:api,env:prod}.as_rate()",
    queryHint:
      "Une requête de métrique Datadog, telle qu'un dashboard l'écrit : avg:system.cpu.user{service:web} by {host}. Un tableau lit la dernière valeur de chaque groupe de tags.",
    fields: {
      site: {
        options: {
          datadoghq_com: "US1 · datadoghq.com",
          us3_datadoghq_com: "US3 · us3.datadoghq.com",
          us5_datadoghq_com: "US5 · us5.datadoghq.com",
          datadoghq_eu: "EU1 · datadoghq.eu",
          ap1_datadoghq_com: "AP1 · ap1.datadoghq.com",
          "ddog-gov_com": "US1-FED · ddog-gov.com",
        },
        label: "Site",
        help: "Le site de votre URL Datadog : app.datadoghq.com correspond à datadoghq.com, app.datadoghq.eu à datadoghq.eu, us5.datadoghq.com à us5.datadoghq.com.",
      },
      api_key: {
        label: "Clé API",
        help: "Organization settings → API keys. Stockée chiffrée.",
      },
      app_key: {
        label: "Clé d'application",
        help: "Organization settings → Application keys ; une clé à portée restreinte doit avoir timeseries_query et metrics_read. Stockée chiffrée.",
      },
    },
  },
  newrelic: {
    label: "New Relic",
    description: "Débit, erreurs et latence APM, pages vues et web vitals côté navigateur, et taux de plantage mobiles, depuis New Relic avec NRQL.",
    queryPlaceholder: "p. ex. SELECT percentile(duration * 1000, 95) FROM Transaction WHERE appName = 'api'",
    queryHint:
      "NRQL, sans SINCE, UNTIL ni TIMESERIES : le type les ajoute pour la période. Un tableau est une requête FACET, une ligne par facette.",
    fields: {
      region: {
        label: "Région",
        help: "Où se trouvent les données du compte : eu pour one.eu.newrelic.com, sinon us.",
        options: {
          us: "US",
          eu: "EU",
        },
      },
      account_id: {
        label: "ID du compte",
        help: "Le numéro du compte, affiché sous son nom dans le sélecteur de comptes.",
        placeholder: "1234567",
      },
      api_key: {
        label: "Clé utilisateur",
        help: "Une clé utilisateur (NRAK-…) issue de API keys, d'un utilisateur qui peut interroger le compte. Stockée chiffrée.",
      },
    },
  },
};
