import type { TelemetryDict, TelemetryKindsDict } from "@/locales/en/opsTelemetry";

// German strings for the console's telemetry views; mirrors en/opsTelemetry.ts key for key.
export const telemetry: TelemetryDict = {
  table: {
    readAt: "Gelesen {time}",
    readAtTitle: "Gelesen am {time}. Der Server hält einen Lesevorgang 30 Sekunden vor, daher kann er bis zu 30 s älter sein als die letzte Aktualisierung dieser Seite.",
    window: "Zeitfenster: {span}",
    windowUnknown: "Umfasst das eigene jüngste Zeitfenster der Quelle",
    windowDocs: "Tabellen-Zeitfenster",
    rows: "{count} Zeilen",
    flag: "Markierung",
    sortBy: "Nach {column} sortieren",
    truncated: "Die Quelle hat mehr Zeilen, als sie gesendet hat: Die ersten {count} werden angezeigt.",
    showAll: "Alle {count} Zeilen anzeigen",
    noRows: "Die Quelle hat dafür gerade nichts.",
    noValue: "Kein Wert",
    unnamedSource: "Datenquelle entfernt",
    recipe: "Rezept {recipe}",
    customQuery: "Eigene Abfrage",
    loadFailed: "Diese Tabelle konnte nicht gelesen werden",
    refreshFailed: "Die Tabelle konnte nicht aktualisiert werden, daher wird die letzte Antwort angezeigt",
    errors: {
      failed: "Diese Tabelle konnte nicht gelesen werden",
      needsParams: "Der Verknüpfung fehlt ein Parameter",
      needsParamsBody: "Die Tabelle braucht einen Wert, den ihre Verknüpfung noch nicht hat.",
      notFound: "Die Datenquelle gibt es nicht mehr",
      notFoundBody: "Die Datenquelle, aus der diese Tabelle gelesen wurde, existiert nicht mehr. Verknüpfen Sie eine andere.",
      unsupported: "Diese Quelle kann keine Zeilen auflisten",
      unsupportedBody: "Ihre Art kann das Signal im Zeitverlauf lesen, aber nicht eine Zeile pro Eintrag. Ein Rezept mit dieser Tabelle liefert sie.",
      badQuery: "Die Quelle hat die Abfrage der Tabelle abgelehnt",
      timeout: "Die Quelle hat nicht rechtzeitig geantwortet",
      timeoutBody: "Sie ist vielleicht ausgelastet. Versuchen Sie es gleich noch einmal.",
      upstream: "Die Quelle hat nicht geantwortet",
      openSignals: "Signale öffnen",
      retry: "Erneut versuchen",
    },
  },
  endpoints: {
    title: "Endpunkte",
    hint: "Jede Tabelle umfasst das jüngste Zeitfenster ihrer Quelle, jede Minute gelesen",
    scopeNote: "Jede Endpunkt-Tabelle umfasst das jüngste Zeitfenster ihrer Quelle, das an der Tabelle steht; der Zeitraum gilt hier daher nicht.",
    flags: {
      errors: "Höchste Fehlerrate",
      slowest: "Höchste p95-Latenz",
    },
    empty: {
      title: "Noch keine Endpunkttabelle",
      body: "Die Endpunkte von {name} – Durchsatz, Fehlerrate und p95 pro Route – kommen aus einer Quelle, deren Rezept sie auflistet: OpenTelemetry-Metriken in Prometheus, New Relic oder Datadog. Verknüpfen Sie eines ihrer Signale unter Signale.",
      signals: "Signale öffnen",
      addSource: "Datenquelle hinzufügen",
    },
  },
  pages: {
    scopeNote: "Jede Web-Tabelle umfasst das jüngste Zeitfenster ihrer Quelle, das an der Tabelle steht; der Zeitraum gilt hier daher nicht.",
    hint: "Jede Tabelle umfasst das jüngste Zeitfenster ihrer Webanalyse-Quelle",
    tabsLabel: "Web-Tabellen",
    tabs: {
      pages: "Seiten",
      referrers: "Verweise",
      countries: "Länder",
      devices: "Geräte",
    },
    empty: {
      title: "Noch keine Tabelle „{table}“",
      body: "Die Daten für „{table}“ von {name} kommen aus einer Webanalyse-Quelle – PostHog, Google Analytics 4 oder Plausible –, die unter Signale verknüpft ist.",
      signals: "Signale öffnen",
      addSource: "Datenquelle hinzufügen",
    },
  },
  crashes: {
    scopeNote: "Jede Absturz-Tabelle umfasst das jüngste Zeitfenster ihrer Quelle, das an der Tabelle steht; der Zeitraum gilt hier daher nicht.",
    openIssue: "Beim Anbieter öffnen",
    issues: {
      title: "Absturz- und ANR-Gruppen",
      hint: "Fehlergruppen aus Google Play und Sentry, die mit den meisten betroffenen Nutzern zuerst",
    },
    signatures: {
      title: "Diagnosesignaturen",
      hint: "Signaturen für Hänger, Starts und Schreibvorgänge des neuesten Builds aus App Store Connect",
    },
    types: {
      crash: "Absturz",
      anr: "ANR",
      non_fatal: "Nicht schwerwiegend",
      hangs: "Hänger",
      launches: "Start",
      disk_writes: "Schreibvorgänge",
      fatal: "Schwerwiegend",
      error: "Fehler",
      warning: "Warnung",
    },
    empty: {
      title: "Noch keine Absturzdaten",
      body: "Die Abstürze von {name} kommen aus Google Play vitals, App Store Connect oder Sentry. Vorschläge finden Sie unter Signale: Eine verknüpfte Store-App bietet ihre Quelle in einem Schritt an.",
      signals: "Signale öffnen",
      addSource: "Datenquelle hinzufügen",
    },
  },
  traces: {
    minDuration: "Mindestdauer (ms)",
    minDurationHint: "Nur Traces, die mindestens so lange dauern. Drücken Sie Enter oder verlassen Sie das Feld, um zu suchen.",
    manage: "Traces-Verknüpfung verwalten",
    loadFailed: "Traces konnten nicht gesucht werden",
    refreshFailed: "Die Traces konnten nicht aktualisiert werden, daher wird die letzte Antwort angezeigt",
    capped: "Die neuesten {count} Traces werden angezeigt. Grenzen Sie den Zeitraum ein oder erhöhen Sie die Mindestdauer, um den Rest zu sehen.",
    openFromLog: "Trace öffnen",
    clamped: {
      title: "Durchsucht werden die letzten 7 Tage des Zeitraums",
      body: "Eine Trace-Suche umfasst höchstens 7 Tage, daher sucht sie ab {since}.",
    },
    unbound: {
      title: "Traces-Quelle verbinden",
      body: "Die Traces von {name} kommen aus einem Tracing-Backend wie Grafana Tempo. Verknüpfen Sie die Traces mit einer Tempo-Quelle oder fügen Sie zuerst eine hinzu.",
      bind: "Traces verknüpfen",
      addSource: "Datenquelle hinzufügen",
    },
    empty: {
      title: "Keine Traces in diesem Zeitraum",
      body: "Die Traces-Quelle hat seit {since} keine für diese Komponente.",
      withMin: "Kein Trace in diesem Zeitraum dauerte {min} ms oder länger.",
      clearMin: "Mindestdauer zurücksetzen",
    },
    list: {
      label: "Traces",
      count: "{count} Traces",
      failedCount: "{count} mit Fehlern",
      order: "Reihenfolge",
      orders: {
        newest: "Neueste zuerst",
        slowest: "Langsamste zuerst",
      },
      columns: {
        start: "Gestartet",
        root: "Root-Span",
        duration: "Dauer",
        spans: "Spans",
        status: "Status",
      },
      open: "Trace {name} öffnen",
      noRoot: "Root-Span nicht gefunden",
      noService: "Unbekannter Service",
      error: "Fehler",
      ok: "OK",
    },
    detail: {
      back: "Zurück zu den Traces",
      untitled: "Trace",
      copy: "Trace-ID kopieren",
      copied: "Trace-ID kopiert",
      duration: "Dauer",
      spans: "Spans",
      errorCount: "Fehler",
      services: "Services",
      serviceSpans: "{count} Spans",
      serviceErrors: "{count} fehlgeschlagen",
      viewLabel: "Trace-Ansicht",
      views: {
        waterfall: "Wasserfall",
        table: "Tabelle",
      },
      hint: "Wählen Sie einen Span, um seine Attribute zu sehen.",
      noSpans: "Die Quelle hat diesen Trace ohne Spans gesendet.",
      refreshFailed: "Der Trace konnte nicht erneut gelesen werden, daher wird die letzte Antwort angezeigt",
      truncated: {
        title: "Nicht alle Spans werden angezeigt",
        body: "Der Trace hat mehr Spans, als eine Antwort enthält; die ersten {count} werden angezeigt.",
      },
      elsewhere: {
        title: "Über eine andere Komponente gefunden",
        body: "Diese Komponente hat keine Traces-Verknüpfung, daher wurde der Trace in jeder Traces-Quelle der Umgebung gesucht und über {name} gefunden.",
        action: "Bei {name} öffnen",
      },
      errors: {
        notFound: "Trace nicht gefunden",
        notFoundBody: "Keine Traces-Quelle dieser Umgebung enthält den Trace {id}. Vielleicht ist er älter, als die Quelle Daten aufbewahrt.",
        unbound: "Keine Traces-Quelle zum Suchen",
        unboundBody: "Keine Komponente in dieser Umgebung hat eine Traces-Verknüpfung, daher lässt sich der Trace {id} nicht suchen.",
        timeout: "Die Traces-Quelle hat nicht rechtzeitig geantwortet",
        timeoutBody: "Sie ist vielleicht ausgelastet. Versuchen Sie es gleich noch einmal.",
        failed: "Der Trace konnte nicht gelesen werden",
      },
    },
    waterfall: {
      span: "Span",
      listLabel: "Spans",
      tableLabel: "Spans als Tabelle",
      rowLabel: "{service} {name}, beginnt bei +{offset}, dauert {duration}",
      failed: "fehlgeschlagen",
      noService: "Unbekannter Service",
      noName: "Unbenannter Span",
      expand: "Spans unter {name} anzeigen",
      collapse: "Spans unter {name} ausblenden",
      columns: {
        depth: "Tiefe",
        service: "Service",
        operation: "Operation",
        start: "Beginnt bei",
        duration: "Dauer",
        status: "Status",
      },
      status: {
        unset: "Nicht gesetzt",
        ok: "OK",
        error: "Fehler",
      },
    },
    span: {
      close: "Span schließen",
      duration: "Dauer",
      offset: "Beginnt bei",
      started: "Gestartet",
      id: "Span-ID",
      parent: "Übergeordneter Span",
      copy: "Span-ID kopieren",
      copied: "Span-ID kopiert",
      attributes: "Attribute",
      resource: "Ressource",
      noAttributes: "Keine Attribute.",
      statuses: {
        ok: "OK",
        error: "Fehler",
      },
      kinds: {
        server: "Server",
        client: "Client",
        internal: "Intern",
        producer: "Producer",
        consumer: "Consumer",
      },
    },
  },
  webVitals: {
    title: "Core Web Vitals",
    hint: "Der neueste p75-Wert der Seitenladevorgänge im Zeitraum, gemessen an den Schwellenwerten von Google",
    traffic: "Traffic",
    bands: {
      good: "Gut",
      needsImprovement: "Optimierung erforderlich",
      poor: "Schlecht",
    },
    quantileP75: "p75",
    thresholds: "Gut bis {good}, schlecht über {poor}",
    meterLabel: "{band}: gut bis {good}, schlecht über {poor}",
    bind: "Datenquelle verknüpfen",
    noData: "Keine Daten in diesem Zeitraum",
    readFailed: "Nicht lesbar",
    latest: "Neuester Wert im Zeitraum",
    live: "Live-Wert, kein Verlauf",
  },
  mobileVitals: {
    window: "Letzte 7 Tage in {env}, unabhängig vom Zeitraum.",
    asOf: "Stand: {date}",
    bind: "Unter Signale verknüpfen",
    noData: "Keine Daten in den letzten 7 Tagen",
    notYet: "Noch nicht veröffentlicht",
    playLag: "Die Daten von Google Play hinken 1–2 Tage hinterher, die neuesten Tage kommen also später hinzu.",
    readFailed: "Nicht lesbar",
  },
  offers: {
    title: "Datenquellen zum Verbinden",
    found: "{count} Datenquellen zum Verbinden",
    bound: "{count} Signale über {name} verknüpft",
    titles: {
      playVitals: "Google Play vitals für diese App verbinden",
      appStoreMetrics: "App-Store-Metriken für diese App verbinden",
      generic: "{kind} für diese App verbinden",
    },
    bodies: {
      playVitals: "Absturzfreie Nutzer, Absturzrate, ANR-Rate und die häufigsten Absturzgruppen, gelesen mit den Google-Play-Zugangsdaten des Arbeitsbereichs. Nichts zu installieren.",
      appStoreMetrics: "Startzeit je App-Version und die Signaturen für Hänger, Starts und Schreibvorgänge jedes Builds, gelesen mit dem App Store Connect-Schlüssel des Arbeitsbereichs. Nichts zu installieren.",
      generic: "Eine {kind}-Quelle würde diese Signale für diese App lesen.",
    },
    credentialMissing: {
      google_play: "Speichern Sie zuerst die Google-Play-Zugangsdaten.",
      asc: "Speichern Sie zuerst den App Store Connect-Schlüssel.",
    },
    saveCredential: "Einstellungen → Integrationen",
    connect: "Verbinden",
    connecting: "Wird verbunden…",
    notNow: "Nicht jetzt",
    recipe: "Rezept {recipe}",
  },
  binding: {
    noPreviewTitle: "Keine Vorschau für dieses Signal",
    noPreview: "{signal} ist keine Reihe für ein Diagramm: Logs, Traces und Fehlergruppen werden im Log-Explorer, unter Traces und in der Fehler-Inbox gelesen. Speichern Sie, um es zu verknüpfen.",
  },
  errors: {
    providers: {
      sentry: "Sentry",
    },
    fromProvider: "Aus {provider} gelesen",
    noTask: "Diese Gruppe nennt keine Datenquelle, aus der eine Aufgabe entstehen kann. Öffnen Sie sie stattdessen in {provider}.",
    openSource: "{provider}-Datenquelle",
    fixSource: "Datenquelle öffnen",
  },
  sources: {
    storeCredentials: {
      google_play: "Google-Play-Dienstkonto",
      asc: "App Store Connect-API-Schlüssel",
    },
    storeCredentialWorkspace: "für den Arbeitsbereich gespeichert",
    storeCredentialPlaceholder: "{credential} wählen",
    noStoreCredential: "Es ist noch kein {credential} gespeichert.",
    saveStoreCredential: "Unter Einstellungen → Integrationen speichern",
    namePlaceholderStore: "Standardmäßig der Name der Art",
    errors: {
      store_credential: {
        not_found: "Diese Zugangsdaten sind noch nicht gespeichert. Speichern Sie sie zuerst unter Einstellungen → Integrationen.",
        wrong_provider: "Diese Zugangsdaten gehören zum anderen Store.",
        not_permitted: "Store-Zugangsdaten lassen sich in dieser Installation nicht für Datenquellen nutzen.",
      },
      org: {
        invalid: "Geben Sie den Slug der Organisation ein, wie in sentry.io/organizations/<slug>/.",
      },
      index: {
        invalid: "Geben Sie einen Index, Data Stream oder ein Muster ein, etwa logs-*; mehrere durch Kommas getrennt.",
      },
    },
  },
};

const AUTH_OPTIONS = {
  none: "Keine",
  bearer: "Bearer-Token",
  basic: "Basic (Benutzername und Passwort)",
};

export const telemetryKinds: TelemetryKindsDict = {
  google_play_vitals: {
    label: "Google Play vitals",
    description:
      "Android Vitals aus Google Play – Absturz- und ANR-Raten, langsame Starts und die häufigsten Absturzgruppen –, gelesen mit den Google-Play-Zugangsdaten des Arbeitsbereichs. Nichts zu installieren; Play veröffentlicht sie mit ein bis zwei Tagen Verzögerung.",
    queryPlaceholder: 'z. B. {"metric_set": "crashRateMetricSet", "metric": "userPerceivedCrashRate", "as": "percent"}',
    queryHint:
      'JSON: {"package"?, "metric_set", "metric", "dimension"?, "filter"?, "period"?, "as"?}. metric_set ist crashRateMetricSet, anrRateMetricSet, slowStartRateMetricSet oder errorCountMetricSet; period daily (Standard) oder hourly; as value, percent oder free_percent. Tägliche Daten hinken 1–2 Tage hinterher.',
    fields: {
      store_credential: {
        label: "Google-Play-Zugangsdaten",
        help: "Das Dienstkonto, das unter Einstellungen → Integrationen → Google Play gespeichert ist. In seinem Google-Cloud-Projekt muss die Play Developer Reporting API aktiviert sein, und in der Play Console braucht es die Berechtigung, App-Informationen anzusehen.",
      },
      package_name: {
        label: "Paketname",
        help: "Die App, die gelesen wird, wenn eine Abfrage keine nennt. Die Verknüpfung einer Komponente übernimmt ihn aus der Android-App, die mit ihrem Repository verknüpft ist.",
        placeholder: "com.example.app",
      },
    },
  },
  app_store_metrics: {
    label: "App Store Connect-Metriken",
    description:
      "Startzeit, Hänger, Arbeitsspeicher, Schreibvorgänge und Akkuverbrauch einer iOS-App, wie Xcode Organizer sie pro App-Version zeigt, dazu die Hänger- und Startdiagnosen jedes Builds – gelesen mit dem App Store Connect-Schlüssel des Arbeitsbereichs. Eine absturzfreie Quote meldet Apple nicht.",
    queryPlaceholder: 'z. B. {"metric": "launchTime", "percentile": "p90"}',
    queryHint:
      'JSON: {"app"?, "metric", "percentile"?, "device"?, "version"?}. metric ist eine Metrik aus Xcode Organizer (launchTime, hangRate, …) oder eine ganze Kategorie (launch, hang, memory, disk, battery); percentile p50 oder p90. Die Werte kommen pro App-Version und werden als Stufen gezeichnet.',
    fields: {
      store_credential: {
        label: "App Store Connect-Zugangsdaten",
        help: "Der API-Schlüssel, der unter Einstellungen → Integrationen → App Store Connect gespeichert ist.",
      },
      app_id: {
        label: "App-ID",
        help: "Die App, die gelesen wird, wenn eine Abfrage keine nennt: ihre App Store Connect-ID. Die Verknüpfung einer Komponente übernimmt sie aus der iOS-App, die mit ihrem Repository verknüpft ist.",
        placeholder: "1234567890",
      },
    },
  },
  sentry: {
    label: "Sentry",
    description:
      "Absturzfreie Nutzer und Sitzungen, Fehlerzahlen und Fehlergruppen aus Sentry, für Mobile-, Web- und Backend-Projekte gleichermaßen. Seine Fehlergruppen erscheinen in der Fehler-Inbox.",
    queryPlaceholder: 'z. B. {"project": "ios-app", "metric": "crash_free_users"}',
    queryHint:
      'JSON: {"project", "metric"?, "environment"?, "release"?, "search"?, "days"?}. metric ist events (Standard), event_rate, crash_free_users, crash_free_sessions, crash_rate, crash_rate_users, sessions oder users; die Absturzmetriken brauchen Session-Tracking im SDK.',
    fields: {
      url: {
        label: "Sentry-URL",
        help: "https://sentry.io für die Cloud von Sentry (https://de.sentry.io für eine Organisation in der EU-Region) oder die Adresse Ihres selbst gehosteten Sentry.",
        placeholder: "https://sentry.io",
      },
      org: {
        label: "Organisation",
        help: "Der Slug der Organisation, wie in sentry.io/organizations/<slug>/.",
        placeholder: "acme",
      },
      token: {
        label: "Auth-Token",
        help: "Ein Token mit den Scopes org:read, project:read und event:read: ein Organisations-Token oder das einer internen Integration. Wird verschlüsselt gespeichert.",
      },
    },
  },
  loki: {
    label: "Loki",
    description: "Grafana Loki oder Grafana Cloud Logs: die Log-Zeilen einer Komponente und daraus mit LogQL gezählte Fehlerraten.",
    queryPlaceholder: 'z. B. {service_name="api"} |= "error"',
    queryHint:
      'LogQL. Eine Log-Abfrage ({service_name="api"} |= "timeout") speist den Log-Explorer; eine Metrikabfrage (sum(rate({service_name="api"} | detected_level="error" [5m]))) zeichnet ein Diagramm.',
    fields: {
      url: {
        help: "Die Basis-URL von Loki, ohne /loki/api/v1. Bei Grafana Cloud die Logs-URL (https://logs-prod-<n>.grafana.net).",
        placeholder: "http://localhost:3100",
      },
      auth: {
        options: AUTH_OPTIONS,
      },
      username: {
        help: "Bei Grafana Cloud die Benutzer-ID für Logs.",
      },
      password: {
        help: "Bei Grafana Cloud ein Access-Policy-Token mit logs:read.",
      },
    },
  },
  elasticsearch: {
    label: "Elasticsearch / OpenSearch",
    description: "Log-Zeilen aus Elasticsearch, OpenSearch oder Elastic Cloud: ein Indexmuster, nach Feldern gefiltert.",
    queryPlaceholder: 'z. B. {"filters": {"service.name": "billing"}}',
    queryHint: 'JSON: {"filters": {"<field>": "<value>", …}}, jeder Filter muss zutreffen, höchstens 32. Exakt bei einem keyword-Feld, jedes Wort bei einem text-Feld.',
    fields: {
      url: {
        help: "Die Basis-URL von Elasticsearch oder OpenSearch. Bei Elastic Cloud der Elasticsearch-Endpunkt des Deployments.",
        placeholder: "http://localhost:9200",
      },
      index: {
        label: "Index",
        help: "Der Index, Data Stream oder das Muster mit den Logs; mehrere durch Kommas getrennt (logs-*,app-*).",
        placeholder: "logs-*",
      },
      time_field: {
        label: "Zeitfeld",
        help: "Das Datumsfeld, nach dem jede Zeile sortiert und gefiltert wird. Leer gelassen: @timestamp.",
        placeholder: "@timestamp",
      },
      message_field: {
        label: "Meldungsfeld",
        help: "Das Feld mit der Zeile selbst; der Textfilter durchsucht es. Leer gelassen: message.",
        placeholder: "message",
      },
      level_field: {
        label: "Level-Feld",
        help: "Das Feld mit dem Level (ECS log.level). Leer gelassen: log.level.",
        placeholder: "log.level",
      },
      auth: {
        options: {
          none: "Keine",
          basic: "Basic (Benutzername und Passwort)",
          api_key: "API-Schlüssel",
        },
      },
      api_key: {
        label: "API-Schlüssel",
        help: "Der kodierte API-Schlüssel (base64 von id:key), wie Kibana ihn beim Erstellen des Schlüssels anzeigt. Wird verschlüsselt gespeichert.",
      },
    },
  },
  tempo: {
    label: "Tempo",
    description: "Grafana Tempo oder Grafana Cloud Traces: die Traces einer Komponente, mit TraceQL durchsucht.",
    queryPlaceholder: 'z. B. { resource.service.name = "api" }',
    queryHint: 'TraceQL: die Spans, von denen die Traces einer Komponente ausgehen, etwa { resource.service.name = "api" }. Wird unter Traces gelesen, nie als Diagramm.',
    fields: {
      url: {
        help: "Die Basis der HTTP-API von Tempo, ohne /api. Bei Grafana Cloud die Traces-URL, die auf /tempo endet.",
        placeholder: "http://localhost:3200",
      },
      auth: {
        options: AUTH_OPTIONS,
      },
      username: {
        help: "Bei Grafana Cloud die Benutzer-ID für Traces.",
      },
      password: {
        help: "Bei Grafana Cloud ein Access-Policy-Token mit traces:read.",
      },
    },
  },
  posthog: {
    label: "PostHog",
    description:
      "Seitenaufrufe, Sitzungen, Besucher, JavaScript-Ausnahmen und Web Vitals aus PostHog, mit den wichtigsten Seiten, Verweisen, Ländern und Geräten. Außer posthog-js ist nichts zu installieren.",
    queryPlaceholder: 'z. B. {"metric": "lcp", "quantile": 0.75}',
    queryHint:
      'JSON: {"metric"?, "event"?, "quantile"?, "host"?}. metric ist page_views (Standard), sessions, visitors, js_errors, events (mit dem gezählten event) oder ein Web Vital: lcp, inp, cls, fcp, beim Quantil 0.75, sofern nicht anders angegeben. host beschränkt auf einen Hostnamen.',
    fields: {
      url: {
        label: "PostHog-URL",
        help: "https://us.posthog.com oder https://eu.posthog.com für PostHog Cloud oder die Adresse Ihres selbst gehosteten PostHog.",
        placeholder: "https://us.posthog.com",
      },
      project_id: {
        label: "Projekt-ID",
        help: "Die Zahl in der URL des Projekts (/project/<id>) oder unter Project settings.",
        placeholder: "12345",
      },
      personal_api_key: {
        label: "Persönlicher API-Schlüssel",
        help: "Ein persönlicher API-Schlüssel (phx_…) mit dem Scope Query Read für dieses Projekt. Wird verschlüsselt gespeichert.",
      },
    },
  },
  ga4: {
    label: "Google Analytics 4",
    description:
      "Seitenaufrufe, Sitzungen und aktive Nutzer aus einer Property von Google Analytics 4, mit den wichtigsten Seiten, Verweisen, Ländern und Geräten. GA4 verarbeitet Daten bis zu zwei Tage lang, die neuesten Punkte können also noch steigen.",
    queryPlaceholder: 'z. B. {"metric": "page_views"}',
    queryHint:
      'JSON: {"metric"?, "event"?, "host"?, "property"?}. metric ist page_views (Standard), sessions, visitors oder events (mit dem gezählten event, dem eventName von GA4). property liest eine andere Property als die der Quelle.',
    fields: {
      property_id: {
        label: "Property-ID",
        help: "Die Zahl unter Verwaltung → Property-Details (nicht die G-…-Mess-ID).",
        placeholder: "123456789",
      },
      auth: {
        label: "Anmelden mit",
        help: "Ein verbundenes Google-Cloud-Konto oder ein Dienstkontoschlüssel nur für diese Quelle.",
        options: {
          cloud_account: "Ein verbundenes Google-Cloud-Konto",
          service_account: "Ein Dienstkontoschlüssel",
        },
      },
      cloud_account_id: {
        label: "Google-Cloud-Konto",
        help: "Sein Dienstkonto muss Betrachter der Property sein (Verwaltung → Property-Zugriffsverwaltung), und in seinem Projekt muss die Google Analytics Data API aktiviert sein.",
      },
      service_account_json: {
        label: "Dienstkontoschlüssel (JSON)",
        help: "Ein Schlüssel eines Dienstkontos, das Betrachter der Property ist und in dessen Projekt die Google Analytics Data API aktiviert ist. Fügen Sie die ganze JSON-Datei ein. Wird verschlüsselt gespeichert.",
      },
    },
  },
  plausible: {
    label: "Plausible",
    description: "Seitenaufrufe, Besuche und Besucher aus Plausible Analytics, mit den wichtigsten Seiten, Quellen, Ländern und Geräten.",
    queryPlaceholder: 'z. B. {"metric": "visitors"}',
    queryHint: 'JSON: {"metric"?, "host"?, "site"?}. metric ist page_views (Standard), sessions (Besuche) oder visitors. site liest eine andere Website als die der Quelle.',
    fields: {
      url: {
        label: "Plausible-URL",
        help: "https://plausible.io oder die Adresse Ihres selbst gehosteten Plausible.",
        placeholder: "https://plausible.io",
      },
      site_id: {
        label: "Website",
        help: "Die Domain der Website, wie sie in Plausible heißt.",
        placeholder: "example.com",
      },
      api_key: {
        label: "Stats-API-Schlüssel",
        help: "Ein Stats-API-Schlüssel (Account settings → API keys) des Teams, dem die Website gehört. Wird verschlüsselt gespeichert.",
      },
    },
  },
  vercel: {
    label: "Vercel",
    description: "Seitenaufrufe, Besucher und Core Web Vitals Ihrer Vercel-Projekte aus Web Analytics und Speed Insights, gelesen mit einem verbundenen Vercel-Konto. Nichts zu installieren.",
    queryPlaceholder: "z. B. {\"metric\": \"lcp\", \"project\": \"prj_…\"}",
    queryHint:
      "JSON: {\"metric\"?, \"project\"}. project ist die ID des Vercel-Projekts (prj_…). metric ist page_views (Standard), visitors, lcp, inp, cls oder fcp; ein Vital ist sein p75, sofern quantile (0.5, 0.75, 0.9, 0.95 oder 0.99) nichts anderes sagt. Web Analytics und Speed Insights müssen für das Projekt aktiviert sein.",
  },
  datadog: {
    label: "Datadog",
    description: "APM-Anfrageraten, Fehler und Latenz sowie Host- oder Container-Metriken aus der Metrik-API von Datadog.",
    queryPlaceholder: "z. B. sum:trace.http.request.hits{service:api,env:prod}.as_rate()",
    queryHint:
      "Eine Datadog-Metrikabfrage, wie ein Dashboard sie schreibt: avg:system.cpu.user{service:web} by {host}. Eine Tabelle liest den neuesten Wert jeder Tag-Gruppe.",
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
        help: "Die Site in Ihrer Datadog-URL: app.datadoghq.com ist datadoghq.com, app.datadoghq.eu ist datadoghq.eu, us5.datadoghq.com ist us5.datadoghq.com.",
      },
      api_key: {
        label: "API-Schlüssel",
        help: "Organization settings → API keys. Wird verschlüsselt gespeichert.",
      },
      app_key: {
        label: "Anwendungsschlüssel",
        help: "Organization settings → Application keys; ein eingeschränkter Schlüssel braucht timeseries_query und metrics_read. Wird verschlüsselt gespeichert.",
      },
    },
  },
  newrelic: {
    label: "New Relic",
    description: "APM-Durchsatz, Fehler und Latenz, Seitenaufrufe und Web Vitals im Browser sowie mobile Absturzraten aus New Relic, mit NRQL.",
    queryPlaceholder: "z. B. SELECT percentile(duration * 1000, 95) FROM Transaction WHERE appName = 'api'",
    queryHint:
      "NRQL, ohne SINCE, UNTIL oder TIMESERIES: Die Art ergänzt sie für den Zeitraum. Eine Tabelle ist eine FACET-Abfrage, eine Zeile pro Facet.",
    fields: {
      region: {
        label: "Region",
        help: "Wo die Daten des Kontos liegen: eu für one.eu.newrelic.com, sonst us.",
        options: {
          us: "US",
          eu: "EU",
        },
      },
      account_id: {
        label: "Konto-ID",
        help: "Die Nummer des Kontos, angezeigt unter dem Kontonamen in der Kontoauswahl.",
        placeholder: "1234567",
      },
      api_key: {
        label: "Benutzerschlüssel",
        help: "Ein Benutzerschlüssel (NRAK-…) unter API keys, von einem Benutzer, der das Konto abfragen darf. Wird verschlüsselt gespeichert.",
      },
    },
  },
};
