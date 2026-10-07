import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import App from "@/App";
import { ErrorBoundary } from "@/components/ErrorBoundary";
import { ConfigErrorPage } from "@/pages/ConfigErrorPage";
import { getStoredLocale } from "@/api";
import { loadLocale } from "@/hooks/useI18n";
import { getApiToken } from "@/lib/auth";
import { startIdleTracking } from "@/lib/idle";
import { normalizeLang } from "@/lib/languages";
// Bundled rather than fetched from Google: the packaged app serves this SPA from
// app://tasktrooper and has to render with no network at all.
import "@fontsource-variable/inter";
import "@fontsource/jetbrains-mono/400.css";
import "@fontsource/jetbrains-mono/500.css";
import "@/styles/globals.css";

async function main() {
  const rootEl = document.getElementById("root");
  if (!rootEl) return;
  startIdleTracking();

  // Only English ships in this bundle. Waiting for the stored language here
  // means the first frame is already in it, instead of English for a blink.
  // A failed load leaves English, which every lookup falls back to anyway.
  await loadLocale(normalizeLang(getStoredLocale())).catch(() => undefined);

  // There is no sign-in screen: the bearer token is stated by the desktop shell
  // or compiled in for browser development. With neither, every call would 401,
  // so say what is missing instead of mounting an app that cannot talk to
  // anything.
  if (!getApiToken()) {
    createRoot(rootEl).render(
      <StrictMode>
        <ConfigErrorPage missing={["VITE_API_KEY"]} />
      </StrictMode>,
    );
    return;
  }

  createRoot(rootEl).render(
    <StrictMode>
      <ErrorBoundary>
        <App />
      </ErrorBoundary>
    </StrictMode>,
  );
}

void main();
