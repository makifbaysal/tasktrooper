// German dictionary for the developer-tool installer. Must mirror
// en/toolchain.ts keys exactly.
export const toolchain = {
  stepState: {
    queued: "In der Warteschlange",
    running: "Wird installiert",
    waiting: "Wartet auf Sie",
    done: "Installiert",
    failed: "Fehlgeschlagen",
    skipped: "Übersprungen",
    cancelled: "Abgebrochen",
  },
  itemStatus: {
    installed: "Installiert",
    installedVersion: "Installiert · {version}",
    missing: "Fehlt",
    unusable: "Nicht nutzbar",
  },
  badge: {
    running: "Einrichtung · {percent} %",
    done: "Einrichtung abgeschlossen",
    failed: "Einrichtung: {count} fehlgeschlagen",
    stopped: "Einrichtung gestoppt",
    incomplete: "Einrichtung unvollständig",
  },
  waitingHint:
    "Auf diesem Computer ist ein Installations- oder Terminal-Fenster geöffnet. Schließen Sie es dort ab, danach geht es von selbst weiter.",
  method: "Über {method}",
  needsManual: "Braucht {label}. Installieren Sie das zuerst über seine Seite.",
  queuedAfterCurrent: "Nach den laufenden Schritten eingereiht",
  kind: {
    interactive: "Öffnet ein Installationsfenster",
    manual: "Wird über die eigene Seite installiert",
  },
  reasons: {
    everyone: "Wird von allen Agenten genutzt",
    agents: "Für {agents}",
  },
  progress: {
    percent: "{percent} %",
    summary: "{done} von {total} abgeschlossen",
    failed: "{count} fehlgeschlagen",
  },
  actions: {
    install: "Installieren",
    openPage: "Seite öffnen",
    openPageFor: "Seite von {label} öffnen",
    stop: "Stoppen",
    dismiss: "Ausblenden",
    details: "Details",
    restartAgents: "Agenten neu starten",
    checkAgain: "Erneut prüfen",
    installRecommended: "Empfohlene installieren",
  },
  restart: {
    hint: "Einiges davon erreicht die Agenten erst nach einem Neustart.",
    title: "Agenten neu starten?",
    description:
      "TaskTrooper startet den Dienst neu, in dem Ihre Agenten laufen, damit sie die neuen Werkzeuge sehen. Was ein Agent gerade tut, wird unterbrochen, und dieses Fenster wird neu geladen.",
    confirm: "Neu starten",
    failed: "Die Agenten konnten nicht neu gestartet werden",
  },
  errors: {
    installFailed: "Die Installation konnte nicht gestartet werden",
    declined: "Es wurde nichts installiert",
    stopFailed: "Die Installation konnte nicht gestoppt werden",
    dismissFailed: "Die abgeschlossene Einrichtung konnte nicht ausgeblendet werden",
    openPageFailed: "Die Seite konnte nicht geöffnet werden",
    reportFailed: "Dieser Computer konnte nicht geprüft werden",
  },
  job: {
    title: {
      running: "Dieser Computer wird eingerichtet",
      complete: "Einrichtung beendet",
      failed: "Einrichtung mit Fehlern beendet",
      stopped: "Einrichtung gestoppt",
      incomplete: "Einrichtung beendet, aber nicht alles wurde installiert",
    },
  },
  running: {
    title: "Entwicklerwerkzeuge werden noch installiert",
    body: "git oder Claude Code sind womöglich noch unterwegs. Sobald sie da sind, klicken Sie auf Erneut prüfen.",
  },
  settings: {
    desktopOnlyTitle: "Diese Seite braucht die TaskTrooper-Desktop-App",
    desktopOnlyBody:
      "Sie prüft und installiert Entwicklerwerkzeuge auf Ihrem eigenen Computer, und darauf hat ein Browser-Tab keinen Zugriff.",
    recommendedTitle: "Empfohlen für Ihr Team",
    recommendedDescription: "Womit Ihre aktivierten Agenten am besten arbeiten und was diesem Computer noch fehlt.",
    recommendedNone: "Alles, was Ihr Team braucht, ist auf diesem Computer.",
    recommendedManual:
      "Werkzeuge, die über ihre eigene Seite installiert werden, gehören nicht zur Ein-Klick-Installation; nutzen Sie unten „Seite öffnen“.",
    allTitle: "Alle Entwicklerwerkzeuge",
    allDescription: "Was TaskTrooper auf diesem Computer prüfen und installieren kann.",
    lastChecked: "Geprüft {when}.",
    checking: "Dieser Computer wird geprüft…",
    groups: {
      basics: "Grundlagen",
      web: "Web",
      mobile: "Mobil",
      data: "Daten",
      games: "Spiele",
      infra: "Infrastruktur & Sicherheit",
    },
  },
};
