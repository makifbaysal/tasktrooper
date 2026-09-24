import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import App from "@/App";
import { ErrorBoundary } from "@/components/ErrorBoundary";
import { ConfigErrorPage } from "@/pages/ConfigErrorPage";
import { getApiToken } from "@/lib/auth";
// Bundled rather than fetched from Google: the packaged app serves this SPA from
// app://tasktrooper and has to render with no network at all.
import "@fontsource-variable/inter";
import "@fontsource/jetbrains-mono/400.css";
import "@fontsource/jetbrains-mono/500.css";
// One import for both map organisms (ProjectArchitectureMap, WorkspaceMapView)
// so neither has to remember to bring its own copy; globals.css themes it.
import "@xyflow/react/dist/style.css";
import "@/styles/globals.css";

function main() {
  const rootEl = document.getElementById("root");
  if (!rootEl) return;

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

main();
