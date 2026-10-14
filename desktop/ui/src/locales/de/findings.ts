// German dictionary for code-health findings. Must mirror
// en/findings.ts keys exactly.
export const findings = {
  intro: "Bei jedem Scan aus dem Code gelesen: Anti-Patterns, fehlendes oder falsch konfiguriertes Logging, Debug-Einstellungen, die in Produktion landen, Geheimnisse und mehr. Akzeptierte oder als Fehlalarm markierte Befunde bleiben bei späteren Scans geschlossen.",
  allComponents: "Alle Komponenten",
  repositoryWide: "Repository-weit",
  openCount: "{count} offen",
  allCategories: "Alle Kategorien",
  occurrences: "{count} Vorkommen",
  lines: "{count} Zeilen",
  moreLocations: "und {count} weitere",
  showLocations: "Fundstellen zeigen",
  hideLocations: "Fundstellen ausblenden",
  createTask: "Aufgabe erstellen",
  createTaskSelected: "Aufgabe für {count} erstellen",
  selectAll: "Alle auswählen",
  clearSelection: "Leeren",
  accept: "Akzeptieren",
  falsePositive: "Fehlalarm",
  reopen: "Wieder öffnen",
  openTask: "{key} öffnen",
  reason: "Begründung: {reason}",
  resolvedHint: "Der letzte Scan findet das nicht mehr.",
  updateFailed: "Befund konnte nicht aktualisiert werden",
  componentSummary: {
    open: "{count} offene Codequalitäts-Befunde",
    none: "Keine offenen Codequalitäts-Befunde",
    view: "Befunde ansehen",
  },
  status: {
    open: "Offen",
    accepted: "Akzeptiert",
    false_positive: "Fehlalarm",
    resolved: "Behoben",
  },
  severity: {
    high: "Hoch",
    medium: "Mittel",
    low: "Niedrig",
  },
  category: {
    security: "Sicherheit",
    configuration: "Konfiguration",
    logging: "Logging",
    error_handling: "Fehlerbehandlung",
    observability: "Beobachtbarkeit",
    reliability: "Zuverlässigkeit",
    quality: "Codequalität",
  },
  empty: {
    noScanTitle: "Noch nicht gescannt",
    noScanDescription: "Befunde erscheinen nach dem ersten Scan des Repositorys.",
    cleanTitle: "Keine Probleme gefunden",
    cleanDescription: "Der letzte Scan hat hier nichts zu beheben gefunden.",
    filtered: "Keine Befunde für diesen Filter.",
  },
  verdict: {
    acceptTitle: "Risiko akzeptieren",
    acceptDescription: "Der Befund wird geschlossen und bleibt bei späteren Scans geschlossen, auch wenn der Code ihn noch enthält.",
    falsePositiveTitle: "Als Fehlalarm markieren",
    falsePositiveDescription: "Sagen Sie dem Scan, dass er hier falsch liegt. Der Befund bleibt bei späteren Scans geschlossen.",
    reasonLabel: "Begründung (optional)",
    reasonPlaceholder: "Warum das hier in Ordnung ist",
    save: "Speichern",
  },
  task: {
    title: "Aufgabe erstellen",
    description: "Eine Board-Aufgabe für {count} Befunde. Der Agent erhält jede Fundstelle und wie sie zu beheben ist.",
    titleLabel: "Titel",
    notesLabel: "Hinweise für den Agenten (optional)",
    notesPlaceholder: "Was der Agent zuerst wissen sollte",
    submit: "Aufgabe erstellen",
    created: "Aufgabe {key} erstellt",
    failed: "Aufgabe konnte nicht erstellt werden",
    defaultTitleMany: "{count} Codequalitäts-Befunde in {component} beheben",
    severalComponents: "mehreren Komponenten",
  },
  rules: {
    hardcoded_secret: {
      title: "Geheimnis im Code",
      description: "Ein Schlüssel oder Token in bekanntem Format liegt im Repository. Erst rotieren, dann aus der Konfiguration laden.",
    },
    hardcoded_credential: {
      title: "Fest codierte Zugangsdaten",
      description: "Ein Passwort, Schlüssel oder Token steht als Literal im Code statt in der Konfiguration.",
    },
    committed_env_file: {
      title: "Env-Datei mit Geheimnissen nicht ignoriert",
      description: "Eine .env-Datei mit echten Geheimnissen wird versioniert oder von git nicht ignoriert.",
    },
    tls_verification_disabled: {
      title: "TLS-Prüfung deaktiviert",
      description: "Zertifikats- oder Hostnamenprüfung ist aus, der Verkehr kann abgefangen werden.",
    },
    cleartext_traffic_allowed: {
      title: "Unverschlüsseltes HTTP erlaubt",
      description: "Die App erlaubt unverschlüsseltes HTTP für jeden Host.",
    },
    sql_injection_risk: {
      title: "Mögliche SQL-Injection",
      description: "Ein Wert wird in einen SQL-String eingefügt statt als Parameter übergeben.",
    },
    dynamic_code_execution: {
      title: "Dynamische Codeausführung",
      description: "eval, exec oder unsichere Deserialisierung führen aus, was die Eingabe vorgibt.",
    },
    unsafe_html_injection: {
      title: "Unmaskiertes HTML",
      description: "Rohes HTML wird ohne Bereinigung gerendert, was Cross-Site-Scripting ermöglicht.",
    },
    permissive_cors: {
      title: "CORS erlaubt jeden Ursprung",
      description: "Jede Website kann diese API aus dem Browser eines Besuchers aufrufen.",
    },
    insecure_token_storage: {
      title: "Tokens im Klartextspeicher",
      description: "Tokens oder Passwörter liegen in localStorage, UserDefaults oder Ähnlichem.",
    },
    http_body_logging: {
      title: "Request-Bodys oder Header werden geloggt",
      description: "Bodys und Header tragen Tokens und personenbezogene Daten in die Logs.",
    },
    error_details_exposed: {
      title: "Fehlerdetails gehen an Clients",
      description: "Stacktraces oder Entwickler-Fehlerseiten erreichen Nutzer.",
    },
    public_source_maps: {
      title: "Öffentliche Source Maps",
      description: "Der Produktions-Build veröffentlicht den Originalquellcode.",
    },
    debug_mode_in_production: {
      title: "Debug-Modus in Produktion",
      description: "Die Produktionskonfiguration schaltet den Debug- oder Entwicklungsmodus ein.",
    },
    debug_log_level_in_production: {
      title: "Debug-Loglevel in Produktion",
      description: "Die Produktionskonfiguration loggt auf Debug- oder Trace-Level.",
    },
    debuggable_release_build: {
      title: "Debugbarer Release-Build",
      description: "Jeder kann einen Debugger an den Android-Release-Build hängen.",
    },
    release_minification_disabled: {
      title: "Release-Build nicht minimiert",
      description: "R8/ProGuard ist für den Release-Build deaktiviert.",
    },
    unconditional_debug_logging: {
      title: "Logger fest auf Debug",
      description: "Das Loglevel ist im Code auf Debug gesetzt und ignoriert die Umgebung.",
    },
    console_logs_in_production: {
      title: "Debug-Konsolenausgaben in Produktion",
      description: "console.log/debug/info landen im Bundle und der Build entfernt sie nicht.",
    },
    mobile_debug_prints: {
      title: "Debug-Ausgaben in Release-Builds",
      description: "print, NSLog oder Log.d laufen außerhalb von Debug-Guards.",
    },
    print_instead_of_logger: {
      title: "Print statt Logging",
      description: "Servicecode schreibt nach stdout statt den Logger zu nutzen.",
    },
    print_stack_trace: {
      title: "Stacktraces gedruckt statt geloggt",
      description: "printStackTrace oder print_exc umgehen das Logging.",
    },
    no_structured_logging: {
      title: "Keine Logging-Bibliothek",
      description: "Der Service hat keinen Logger, also keine strukturierten Logs mit Leveln.",
    },
    no_request_logging: {
      title: "Kein Request-Logging",
      description: "Der HTTP-Service loggt keine Requests (Methode, Status, Dauer).",
    },
    catch_without_logging: {
      title: "Fehler ohne Logging verschluckt",
      description: "Abgefangene Fehler werden weder geloggt, weitergeworfen noch gemeldet.",
    },
    empty_catch_block: {
      title: "Leerer Fehlerhandler",
      description: "Ein leerer catch-, except- oder err-Block verbirgt jeden Fehler auf diesem Pfad.",
    },
    bare_except: {
      title: "Nacktes except",
      description: "except: fängt auch KeyboardInterrupt und SystemExit ab.",
    },
    no_error_boundary: {
      title: "Keine React Error Boundary",
      description: "Ein einziger Renderfehler leert die ganze Oberfläche und nichts wird gemeldet.",
    },
    no_crash_reporting: {
      title: "Kein Crash- oder Fehlerreporting",
      description: "Fehler, die Nutzer in Produktion treffen, bleiben unsichtbar.",
    },
    no_health_endpoint: {
      title: "Kein Health-Endpoint",
      description: "Probes und Deploy-Checks können nicht erkennen, ob der Service läuft.",
    },
    http_client_without_timeout: {
      title: "HTTP-Aufrufe ohne Timeout",
      description: "Eine langsame Abhängigkeit kann Requests unbegrenzt blockieren.",
    },
    force_unwrap: {
      title: "Viele Force-Unwraps",
      description: "try!, as! und !! stürzen bei unerwartetem nil ab.",
    },
    mutable_default_argument: {
      title: "Veränderliches Standardargument",
      description: "Ein list- oder dict-Standardwert wird zwischen Aufrufen geteilt.",
    },
    debug_leftovers: {
      title: "Debug-Anweisungen im Code",
      description: "debugger, Breakpoints oder var_dump/dd sind im Code geblieben.",
    },
    oversized_source_file: {
      title: "Sehr große Quelldateien",
      description: "Dateien mit über 1.500 Zeilen sind schwer zu prüfen und zu ändern.",
    },
    typescript_any_overuse: {
      title: "Viele any / @ts-ignore",
      description: "Die Typprüfung ist an vielen Stellen abgeschaltet.",
    },
    tech_debt_markers: {
      title: "Viele TODO/FIXME",
      description: "Bekannte Probleme, die nirgends verfolgt werden.",
    },
    no_automated_tests: {
      title: "Keine automatisierten Tests",
      description: "Die Komponente hat Quellcode, aber keine Testdateien.",
    },
    no_linter_configured: {
      title: "Kein Linter konfiguriert",
      description: "Stilabweichungen und typische Fehlermuster bleiben ungeprüft.",
    },
    wildcard_import: {
      title: "Wildcard-Importe",
      description: "from x import * verschleiert, woher Namen stammen.",
    },
  },
};
