// German dictionary for the repository setup checklist. Must mirror
// en/readiness.ts keys exactly.
export const readiness = {
  title: "Projekt startklar machen",
  intro: "Was diesem Projekt fehlt, damit Agenten gut damit arbeiten können, in sinnvoller Reihenfolge. Überspringe, was du nicht brauchst.",
  progress: "{done} von {total} erledigt",
  skippedCount: "{count} übersprungen",
  complete: "Dieses Projekt ist startklar.",
  show: "Checkliste anzeigen",
  hide: "Ausblenden",
  expand: "Checkliste aufklappen",
  collapse: "Checkliste einklappen",
  skip: "Überspringen",
  unskip: "Überspringen rückgängig",
  doIt: "Ausführen",
  open: "Öffnen",
  install: "Installieren",
  viewTask: "{key} ansehen",
  taskOpen: "Aufgabe {key} läuft.",
  skippedNote: "Übersprungen.",
  waitingFor: "Wartet auf: {step}.",
  started: "Gestartet.",
  taskOpened: "Aufgabe {key} auf dem Board angelegt.",
  allStarted: "{runs} gestartet und {tasks} Aufgaben angelegt.",
  actionFailed: "Das hat nicht geklappt",
  runAll: "Alle der Reihe nach ausführen",
  runAllTitle: "Alle der Reihe nach ausführen?",
  runAllTasks: "Auf dem Board werden {count} Aufgaben angelegt, jede wartet auf die vorherige.",
  runAllRuns: "{count} starten sofort, ohne Aufgabe.",
  runAllCost: "Jede Aufgabe beansprucht die Zeit deiner Agenten.",
  runAllConfirm: "Alle ausführen",
  status: {
    done: "Erledigt",
    todo: "Offen",
    running: "Läuft",
    blocked: "Wartet auf einen früheren Schritt",
    skipped: "Übersprungen",
    unknown: "Nicht geprüft",
  },
  missing: {
    readme: "README",
    agent_guide: "Agenten-Leitfaden",
  },
  steps: {
    scan: {
      title: "Projekt scannen",
      why: "Der Scan findet die Komponenten, Befehle und CI, aus denen alle anderen Schritte lesen.",
    },
    toolchain: {
      title: "Werkzeuge für diesen Stack installieren",
      why: "Agenten bauen und testen auf diesem Computer, er braucht also die Compiler, die sie aufrufen.",
    },
    index: {
      title: "Code indexieren",
      why: "Mit dem Index finden Agenten die richtigen Dateien, statt alles zu lesen.",
    },
    guide: {
      title: "README und Agenten-Leitfaden hinzufügen",
      why: "Agenten beginnen jede Aufgabe mit diesen Dateien, sie sparen am meisten Zeit und Fehler.",
    },
    tests: {
      title: "Tests lauffähig machen",
      why: "Ein bestandener Testlauf zeigt dir und den Agenten, dass eine Änderung nichts kaputtgemacht hat.",
    },
    ci: {
      title: "Prüfungen in CI ausführen",
      why: "CI wiederholt Build und Tests bei jeder Änderung, bevor sie den Hauptzweig erreicht.",
    },
    delivery: {
      title: "Festlegen, wie Änderungen live gehen",
      why: "Ein Auslieferungsziel sagt TaskTrooper, wohin eine fertige Änderung geht und wie sie geprüft wird.",
    },
    observability: {
      title: "Im Betrieb beobachten",
      why: "Metriken, Logs und Traces zeigen dir, wenn ein Release etwas kaputtmacht.",
    },
    findings: {
      title: "Code-Health-Befunde prüfen",
      why: "Der Scan markiert Debug-Einstellungen, Geheimnisse und fehlendes Logging, die früh billig zu beheben sind.",
    },
    design: {
      title: "Designsystem einrichten",
      why: "Ein Designsystem hält neue Bildschirme konsistent mit den bestehenden.",
    },
    store: {
      title: "Store-App verknüpfen",
      why: "Mit der verknüpften App-Store- oder Google-Play-App kann TaskTrooper Builds ausliefern und Bewertungen verfolgen.",
    },
  },
  detail: {
    scan: {
      no_scan: "Noch nicht gescannt.",
      scan_failed: "Der letzte Scan ist fehlgeschlagen.",
      no_components: "Der Scan hat keine Komponenten gefunden.",
      running: "Scan läuft…",
      done: "{cmp} Komponenten gefunden.",
    },
    toolchain: {
      unknown: "Benötigt: {languages}.",
      no_bridge: "Nur die Desktop-App kann die Werkzeuge dieses Computers prüfen.",
      missing: "Auf diesem Computer fehlt: {tools}.",
      installed: "Alles, was dieser Stack braucht, ist installiert.",
    },
    index: {
      not_indexed: "Noch nicht indexiert.",
      failed: "Der letzte Indexlauf ist fehlgeschlagen.",
      unavailable: "Der Code-Index ist nicht verfügbar.",
      running: "{processed} von {total} Dateien.",
      done: "{files} Dateien indexiert.",
    },
    guide: {
      missing: "Fehlt: {missing}.",
      unreadable: "Die Arbeitskopie kann nicht gelesen werden.",
      done: "README und Agenten-Leitfaden gefunden.",
    },
    tests: {
      gaps: "Kein Testbefehl für: {components}.",
      done: "Jede Komponente hat einen Testbefehl.",
    },
    ci: {
      no_ci: "Kein CI-Workflow gefunden.",
      done: "{checks} CI-Prüfungen gefunden.",
    },
    delivery: {
      gaps: "Keine bestätigte Auslieferung für: {components}.",
      done: "Jede auslieferbare Komponente hat ein Auslieferungsziel.",
    },
    observability: {
      gaps: "Lücken in: {components}.",
      done: "Keine Lücken gefunden.",
    },
    findings: {
      open: "{count} offen ({high} hoher Schweregrad).",
      done: "Keine offenen Befunde.",
    },
    design: {
      none: "Noch kein Designsystem.",
      unavailable: "Das Designsystem konnte nicht gelesen werden.",
      done: "Ein Designsystem ist vorhanden.",
    },
    store: {
      no_app: "Keine Store-App verknüpft.",
      unavailable: "Die Store-Apps konnten nicht gelesen werden.",
      done: "Eine Store-App ist verknüpft.",
    },
  },
};
