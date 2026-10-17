import { Navigate, useLocation, useParams } from "react-router-dom";
import { entityPath, type EntityTab } from "@/components/operations/console/roleViews";
import { DEFAULT_OPS_PRESET, OPS_PRESETS, isOpsEnvironment } from "@/components/operations/shell/OpsContext";

/** Moves an old console address to its new page, query string and all
 * (`/operations/incidents?id=…` keeps pointing at that incident). */
export function RedirectKeepingSearch({ to }: { to: string }) {
  const { search, hash } = useLocation();
  return <Navigate to={{ pathname: to, search, hash }} replace />;
}

export function EntityIndexRedirect() {
  const { componentId = "" } = useParams();
  const { search } = useLocation();
  return <Navigate to={{ pathname: entityPath(componentId), search }} replace />;
}

const LEGACY_TABS: Record<string, EntityTab> = {
  summary: "summary",
  errors: "errors",
  logs: "logs",
  deploys: "changes",
};

// The old component page read ?env=&range=&tab=; its 15-minute range has no
// preset here any more, so it opens on the nearest one.
export function legacyComponentTarget(componentId: string, search: string): string {
  const old = new URLSearchParams(search);
  const next = new URLSearchParams();
  const env = old.get("env");
  if (isOpsEnvironment(env) && env !== "production") next.set("env", env);
  const range = old.get("range") === "15m" ? "30m" : old.get("range");
  const preset = OPS_PRESETS.find((p) => p === range);
  if (preset && preset !== DEFAULT_OPS_PRESET) next.set("range", preset);
  const tab = LEGACY_TABS[old.get("tab") ?? ""] ?? "summary";
  const qs = next.toString();
  return `${entityPath(componentId, tab)}${qs ? `?${qs}` : ""}`;
}

export function LegacyComponentRedirect() {
  const { componentId = "" } = useParams();
  const { search } = useLocation();
  return <Navigate to={legacyComponentTarget(componentId, search)} replace />;
}
