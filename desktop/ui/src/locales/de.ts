import type { Dict } from "@/locales/en";
import { activityArea } from "@/locales/de/activityArea";
import { addRepository } from "@/locales/de/addRepository";
import { agentArea } from "@/locales/de/agentArea";
import { analysisReview } from "@/locales/de/analysisReview";
import { boardArea } from "@/locales/de/boardArea";
import { chatArea } from "@/locales/de/chatArea";
import { cloud } from "@/locales/de/cloud";
import { content } from "@/locales/de/content";
import { designSystem } from "@/locales/de/designSystem";
import { frame } from "@/locales/de/frame";
import { lib } from "@/locales/de/lib";
import { operations } from "@/locales/de/operations";
import { projectAdmin } from "@/locales/de/projectAdmin";
import { projectModel } from "@/locales/de/projectModel";
import { projectsHub } from "@/locales/de/projectsHub";
import { release } from "@/locales/de/release";
import { repositoryPage } from "@/locales/de/repositoryPage";
import { settingsPages } from "@/locales/de/settingsPages";
import { setup } from "@/locales/de/setup";

// German dictionary. Typed as Dict — must mirror en.ts keys exactly.
export const de: Dict = {
  common: {
    save: "Speichern",
    saving: "Wird gespeichert...",
    cancel: "Abbrechen",
    refresh: "Aktualisieren",
    resetDefault: "Auf Standard zurücksetzen",
    actionFailed: "Aktion fehlgeschlagen",
    saved: "Gespeichert",
    saveFailed: "Speichern fehlgeschlagen",
    comingSoon: "Demnächst verfügbar",
    errorBoundary: {
      title: "Etwas ist schiefgelaufen",
      body: "Ein unerwarteter Fehler ist aufgetreten, und die Ansicht konnte nicht dargestellt werden. Laden Sie die Seite neu.",
      retry: "Erneut versuchen",
      reload: "Seite neu laden",
    },
    configError: {
      title: "Konfigurationsfehler",
      body: "Dieser Build enthält keinen API-Schlüssel für den lokalen Server, daher würde jede Anfrage abgelehnt. Setzen Sie VITE_API_KEY (muss mit SERVER_API_KEY des Servers übereinstimmen) und bauen Sie neu.",
      missing: "Fehlt:",
    },
  },
  settings: {
    language: {
      label: "Sprache",
      help: "Wird für Antworten des Assistenten und Systemanweisungen verwendet.",
    },
    loadFailed: "Einstellungen konnten nicht geladen werden",
    savedToast: "Einstellungen gespeichert",
    github: {
      statusUnavailable: "Status nicht verfügbar.",
      connected: "✓ Verbunden: {login} — Agenten können private Repositorys erstellen, pushen und PR-Entwürfe öffnen.",
      disconnect: "Trennen",
      connect: "Token speichern",
      tokenPlaceholder: "ghp_… oder github_pat_…",
      tokenHelp: "Ein Personal Access Token aus GitHub → Settings → Developer settings. Ein klassisches Token benötigt repo, workflow, admin:repo_hook und read:org; ein Fine-grained Token benötigt Contents, Pull requests, Workflows (Read and write) und Webhooks. Ohne workflow kann kein Agent eine CI-Datei hinzufügen oder ändern. Das Token wird vor dem Speichern bei GitHub geprüft und verschlüsselt auf diesem Rechner abgelegt.",
      connectedToast: "GitHub verbunden",
      connectFailedToast: "GitHub-Verbindung fehlgeschlagen",
      disconnectedToast: "GitHub-Verbindung entfernt",
      missingScopesTitle: "Diesem Token fehlt: {scopes}",
      missingWorkflowScope: "Ohne die Berechtigung workflow lehnt GitHub jeden Push ab, der eine Datei unter .github/workflows hinzufügt oder ändert, sodass kein Agent CI einrichten oder reparieren kann. Erstellen Sie ein Token mit dieser Berechtigung, trennen Sie die Verbindung und speichern Sie das neue Token.",
      fineGrainedHint: "Fine-grained Token: Stellen Sie sicher, dass es Workflows: Read and write hat, sonst lehnt GitHub Pushes ab, die CI-Dateien betreffen.",
      connectWithGitHub: "Mit GitHub verbinden",
      connectWithGitHubHint: "Melden Sie sich auf github.com mit einem Einmalcode an. TaskTrooper erneuert die Verbindung selbst — kein Token zum Erstellen oder Einfügen.",
      deviceCodeTitle: "Geben Sie diesen Code auf GitHub ein",
      deviceCodeHint: "Öffnen Sie {url}, melden Sie sich an, geben Sie den Code ein und autorisieren Sie TaskTrooper. Dieses Fenster fährt automatisch fort.",
      copyCode: "Code kopieren",
      codeCopied: "Code kopiert",
      openGitHub: "GitHub öffnen",
      waitingForApproval: "Warte auf Ihre Freigabe…",
      flowExpired: "Der Code ist abgelaufen, bevor er freigegeben wurde.",
      flowDenied: "Die Anmeldung wurde auf GitHub abgelehnt.",
      tryAgain: "Erneut versuchen",
      cancel: "Abbrechen",
      useTokenInstead: "Stattdessen ein Personal Access Token verwenden",
      useAppInstead: "Stattdessen mit GitHub verbinden",
      modeApp: "über GitHub-Anmeldung",
      modeToken: "über Access Token",
      needsInstallTitle: "App in Ihren Repositorys installieren",
      needsInstallBody: "Die Verbindung funktioniert, aber die TaskTrooper-App ist noch auf keinem Konto installiert und erreicht daher kein Repository. Installieren Sie sie für Ihr Konto oder Ihre Organisation und wählen Sie All repositories, damit auch Repositorys abgedeckt sind, die TaskTrooper später erstellt.",
      installApp: "Auf GitHub installieren",
      expiredTitle: "Die GitHub-Verbindung ist abgelaufen",
      expiredBody: "Sie wurde sechs Monate lang nicht genutzt oder auf GitHub widerrufen. Verbinden Sie sich erneut.",
    },
    boilerplate: {
      title: "Boilerplate-Katalog",
      descPrefix: "Bevor Agenten Code von Grund auf schreiben, prüfen sie",
      descMid: "in diesem Repository; gibt es eine passende Boilerplate, kopieren sie diese als Ausgangspunkt.",
      descSuffix: "oder eine vollständige URL werden akzeptiert.",
      loadFailed: "Einstellungen konnten nicht geladen werden",
    },
    notifications: {
      title: "Desktop-Benachrichtigungen",
      help: "Native Benachrichtigungen für Board-Ereignisse, die Ihre Aufmerksamkeit erfordern. Sie funktionieren auch nach dem Schließen des Fensters weiter.",
      unavailable: "Desktop-Benachrichtigungen sind nur in der TaskTrooper-App verfügbar, nicht im Browser.",
      enabled: "Aktiviert",
      analizReview: "Analyse bereit für Ihr Review",
      humanUat: "Wartet auf Ihren UAT",
      humanNeeded: "Ein Agent benötigt eine menschliche Entscheidung",
      agentComments: "Neue Agent-Kommentare",
      agentChatReplies: "Antworten im Agent-Chat",
      loadFailed: "Einstellungen konnten nicht geladen werden",
      saveFailed: "Speichern fehlgeschlagen",
    },
    concurrency: {
      title: "Parallelitätslimits",
      agentsLabel: "Max. gleichzeitige Agenten",
      tasksLabel: "Max. gleichzeitige Aufgaben",
      agentsHelp: "Wie viele Agent-Läufe vom Board aus gleichzeitig ausgeführt werden können.",
      tasksHelp: "Wie viele verschiedene Aufgaben gleichzeitig einen laufenden Agenten haben können.",
      zeroHint: "0 bedeutet unbegrenzt",
      reset: "Unbegrenzt",
      resetting: "Wird zurückgesetzt…",
      loadFailed: "Einstellungen konnten nicht geladen werden",
      saveFailed: "Speichern fehlgeschlagen",
    },
  },
  addRepository,
  agentArea,
  analysisReview,
  boardArea,
  activityArea,
  chatArea,
  cloud,
  content,
  designSystem,
  frame,
  lib,
  operations,
  projectAdmin,
  projectModel,
  projectsHub,
  release,
  repositoryPage,
  settingsPages,
  setup,
};
