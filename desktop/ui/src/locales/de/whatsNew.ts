// Deutsches Wörterbuch für das Fenster "Neuigkeiten"; gleiche Struktur wie en/whatsNew.ts.
export const whatsNew = {
  dialogTitle: "Neu in TaskTrooper {version}",
  seeAll: "Alle Änderungen ansehen",
  gotIt: "Verstanden",
  card: {
    title: "Neuigkeiten",
    help: "Sieh nach, was sich in der neuesten Version von TaskTrooper geändert hat.",
    open: "Neuigkeiten anzeigen",
  },
  v0_3_15: {
    title: "Agenten bekommen den richtigen Leitfaden, Aufgaben einen Agenten, Docker Befehle",
    items: {
      agentsMd: "Die Agent-Leitfaden-Aufgabe aus dem Projekt-Setup schreibt jetzt eine einzige AGENTS.md für jede Agent-CLI und verlangt keine README mehr, die das Projekt schon hat.",
      assignTasks: "Aufgaben, die ein Scan oder ein Agent anlegt, bekommen beim Erstellen einen passenden Agenten, und der PM kann den Bearbeiter einer Aufgabe direkt setzen oder korrigieren, statt sie neu anzulegen.",
      dockerCommands: "Containerisierte Komponenten erhalten Docker-Befehle (Buildkit, Image-Build, Compose up, down und stop), Registry-, Plattform- und Tag-Einstellungen, eine Nur-in-Containern-Regel für Agenten, und jeder Befehl lässt sich entfernen.",
    },
  },
  v0_3_14: {
    title: "Zielgruppenforschung auf dem Handy",
    items: {
      phoneAudience: "Verwalte Zielgruppen in der TaskTrooper-App: mit dem PM oder per Formular anlegen, aufbauen und mit jeder Person einzeln sprechen.",
      phoneStudies: "Starte Studien vom Handy aus: Typ wählen, das Interviewskript prüfen und freigeben, die Interviews verfolgen und den Bericht erzeugen.",
      phoneReports: "Lies Studienberichte auf dem Handy und teile sie als eine einzelne HTML-Datei.",
    },
  },
  v0_3_13: {
    title: "Dein Handy, deine Zielgruppe und deine Telemetrie",
    items: {
      tunnel: "Melde dich unter Einstellungen → Fernzugriff bei Cloudflare oder ngrok an und erreiche diesen Computer mit der TaskTrooper-Mobil-App über einen Tunnel, den du selbst kontrollierst. Koppel ein Handy in einem Schritt: Handy koppeln öffnet ein Fenster mit QR-Code und Adresse.",
      audience: "Zielgruppenforschung: Lass den PM-Agenten synthetische Personas erstellen oder führe selbst eine Studie durch - Interviews, Landingpage-Test, Botschaftstest oder A/B-Test - lies den Bericht und exportiere ihn als eine HTML-Datei.",
      telemetry: "Die neuen Telemetrie-Ansichten unter Betrieb zeigen Services, Endpunkte, Traces, Logs, Fehler, Vorfälle, Monitore, Deployments und SLOs, und der Release-Engineer kümmert sich um die entstehenden Vorfälle.",
      repoSetup: "Beim Import eines Repositorys öffnet sich jetzt die Checkliste „Projekt startklar machen“ (Scan, Werkzeuge, Index, README und Agenten-Leitfaden, Tests, CI, Auslieferung) mit einer Schaltfläche pro Schritt und einer, die alle der Reihe nach ausführt. Das Abmelden von deinem Konto entfernt es jetzt vollständig, sodass der Hinweis \"Arbeit ohne Konto\" nicht mehr stehen bleibt.",
      runTrace: "Jeder Agentenlauf hinterlässt jetzt einen echten Trace in der Aufgabenleiste: was er gelesen, geändert und ausgeführt hat, mit Git abgeglichen. Nach einer Vorschau macht ein Klick die Arbeit des Laufs hinter einem Backup-Branch rückgängig.",
      reviewHistory: "Jede Aufgabe trägt jetzt ihren Review-Verlauf: jede Review- und QA-Runde mit Reviewer, Befunden und deren Verbleib, den Rückläufern zwischen Spalten und was jeder Reviewer abgedeckt hat und was nicht. Eine Karte \"Braucht deine Prüfung\" listet, was Reviewer und Server von einer Person angesehen haben wollen.",
    },
  },
};
