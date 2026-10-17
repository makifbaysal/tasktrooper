import { useEffect } from "react";
import { matchPath, useLocation } from "react-router-dom";
import { desktopAnalytics } from "@/lib/desktop-bridge";

/**
 * Product analytics: screen views and a closed catalog of product events.
 *
 * The same event names and params go out from the desktop, the web app and iOS
 * (`mobile/Shared/Analytics.swift` in the company edition), so one GA report
 * lines the three up. Params are ids, enums and counts only — never a title, a
 * prompt, a repository name or an email. The desktop shell enforces that a
 * second time (`validateAnalyticsTrack` in the main process).
 *
 * Where events go is a sink: in the desktop shell the main process (which
 * sends nothing while Settings → General's switch is off); in a browser
 * whatever the build registers with `setAnalyticsSink` — nothing at all in
 * this edition.
 */

export type AnalyticsParams = Record<string, string | number>;

export interface AnalyticsSink {
  track(name: string, params: AnalyticsParams): void;
  identify(userId: string | null): void;
}

type NoParams = Record<string, never>;

/**
 * Every product event and its params. Add here first; iOS
 * (`AppAnalytics.swift`) uses the same names for the same actions.
 */
export interface AnalyticsEvents {
  login: { method: string };
  logout: NoParams;
  first_run_choice: { choice: "local" | "account" };
  workspace_create: NoParams;
  task_create: { source: "manual"; task_type: string };
  task_move: { from_column: string; to_column: string };
  task_review: { decision: "approve" | "send_back"; gate: "analysis" | "uat" };
  task_claim: { previous_owner: "none" | "someone" };
  issue_import: { provider: string };
  chat_start: { agent_role: string; source: "chat" | "home_quick_ask" };
  chat_message_send: { agent_role: string; outcome: "ok" };
  project_create: NoParams;
  repo_add: { provider: "github" | "folder" | "new" };
  integration_connect: { provider: string; method?: "token" | "device" };
  llm_connect: { provider: string };
  mcp_server_add: { transport: string };
  language_change: { language: string };
  release_action: { action: string };
  code_search: { results: number };
  member_invite: { role: string };
  invite_accept: NoParams;
  computer_pair: { outcome: "ok" | "failed" };
  refine_with_ai: NoParams;
}

/** The role key every platform reports for an agent: its catalog slug, or "custom". */
export function agentRole(agent: { catalog_slug?: string | null } | null | undefined): string {
  return agent?.catalog_slug || "custom";
}

export type AnalyticsEventName = keyof AnalyticsEvents;

let registered: AnalyticsSink | null = null;

/** The browser's sink, set once at startup by an edition that has one. */
export function setAnalyticsSink(sink: AnalyticsSink | null): void {
  registered = sink;
}

function desktopSink(): AnalyticsSink | null {
  const host = desktopAnalytics();
  const track = host?.track;
  const identify = host?.identify;
  if (!track || !identify) return null;
  return {
    track: (name, params) => void track({ name, params }).catch(() => undefined),
    identify: (userId) => void identify(userId).catch(() => undefined),
  };
}

// Read at call time, like every other bridge lookup: the shell's bridge wins
// over a registered browser sink, so a page loaded inside the desktop app is
// counted once, as the desktop.
function sink(): AnalyticsSink | null {
  return desktopSink() ?? registered;
}

export function logEvent<K extends AnalyticsEventName>(name: K, params: AnalyticsEvents[K]): void {
  const clean: AnalyticsParams = {};
  for (const [key, value] of Object.entries(params as Record<string, string | number | undefined>)) {
    if (value !== undefined) clean[key] = value;
  }
  try {
    sink()?.track(name, clean);
  } catch {
    // Counting must never break the action it counts.
  }
}

/**
 * The signed-in member, or null on sign-out. Only a one-way hash of the id
 * leaves the app — the same hash on every platform, so GA counts one person
 * once however many of the apps they use.
 */
export async function identifyUser(uid: string | null): Promise<void> {
  const target = sink();
  if (!target) return;
  try {
    target.identify(uid === null || uid === "" ? null : await hashUserId(uid));
  } catch {
    // No SubtleCrypto (an insecure origin): stay anonymous rather than send the raw id.
  }
}

/** sha256("tasktrooper:" + uid) as hex. iOS computes the same in `Analytics.hashedUserId`. */
export async function hashUserId(uid: string): Promise<string> {
  const bytes = new TextEncoder().encode(`tasktrooper:${uid}`);
  const digest = await crypto.subtle.digest("SHA-256", bytes);
  return Array.from(new Uint8Array(digest), (b) => b.toString(16).padStart(2, "0")).join("");
}

/**
 * Templated route → screen name. First match wins, so a longer pattern goes
 * before a shorter one that would also match. A path that matches nothing is
 * not counted: a redirect stub is never a screen anybody saw.
 */
export const SCREENS: ReadonlyArray<readonly [pattern: string, name: string]> = [
  ["/setup", "setup"],
  ["/home", "home"],
  ["/board", "board"],
  ["/backlog", "backlog"],
  ["/released", "released"],
  ["/projects", "projects"],
  ["/projects/new", "repo_add"],
  ["/projects/:projectId", "project_detail"],
  ["/agents/:agentId/chat", "agent_chat"],
  ["/agents/:agentId/chat/:sessionId", "agent_chat"],
  ["/agents/new/settings", "agent_new"],
  ["/agents/:agentId/settings", "agent_settings"],
  ["/agents/:agentId/skills", "agent_skills"],
  ["/agents/:agentId/rules", "agent_rules"],
  ["/agents/:agentId/columns", "agent_columns"],
  ["/agents/:agentId/memory", "agent_memory"],
  ["/agents/:agentId/performance", "agent_performance"],
  ["/repositories/:repositoryId", "repository_detail"],
  ["/repositories/:repositoryId/tasks/:taskId/analysis", "analysis_review"],
  ["/operations", "ops_explorer"],
  ["/operations/services", "ops_services"],
  ["/operations/web", "ops_web"],
  ["/operations/mobile", "ops_mobile"],
  ["/operations/mobile/app/:repositoryId", "ops_store_apps"],
  ["/operations/workers", "ops_workers"],
  ["/operations/errors", "ops_errors"],
  ["/operations/errors/definitions", "ops_error_definitions"],
  ["/operations/logs", "ops_logs"],
  ["/operations/changes", "ops_changes"],
  ["/operations/issues", "ops_issues"],
  ["/operations/alerts", "ops_alerts"],
  ["/operations/sources", "ops_data_sources"],
  ["/operations/alert-connections", "ops_alert_connections"],
  ["/operations/entity/:componentId/summary", "ops_entity_summary"],
  ["/operations/entity/:componentId/endpoints", "ops_entity_endpoints"],
  ["/operations/entity/:componentId/pages", "ops_entity_pages"],
  ["/operations/entity/:componentId/crashes", "ops_entity_crashes"],
  ["/operations/entity/:componentId/traces", "ops_entity_traces"],
  ["/operations/entity/:componentId/errors", "ops_entity_errors"],
  ["/operations/entity/:componentId/logs", "ops_entity_logs"],
  ["/operations/entity/:componentId/changes", "ops_entity_changes"],
  ["/operations/entity/:componentId/issues", "ops_entity_issues"],
  ["/operations/entity/:componentId/queues", "ops_entity_queues"],
  ["/operations/entity/:componentId/releases", "ops_entity_releases"],
  ["/operations/entity/:componentId/alerts", "ops_entity_alerts"],
  ["/operations/entity/:componentId/signals", "ops_entity_signals"],
  ["/files", "files"],
  ["/memory", "shared_memory"],
  ["/settings", "settings_general"],
  ["/settings/board", "settings_board"],
  ["/settings/roles", "settings_roles"],
  ["/settings/workflows", "settings_workflows"],
  ["/settings/llm", "settings_llm"],
  ["/settings/mcp", "settings_mcp"],
  ["/settings/integrations", "settings_integrations"],
  ["/settings/usage", "settings_usage"],
  ["/settings/catalog", "settings_catalog"],
  ["/settings/environment", "settings_environment"],
];

/** The screen a path shows and its route template, or null for a path that is not a screen. */
export function screenFor(pathname: string): { name: string; template: string } | null {
  for (const [pattern, name] of SCREENS) {
    if (matchPath({ path: pattern, end: true }, pathname)) return { name, template: pattern };
  }
  return null;
}

/**
 * One `page_view` per screen change, named by the route template so no id in
 * the URL is ever sent. Mounted once, inside the router.
 */
export function useScreenTracking(): void {
  const { pathname } = useLocation();
  useEffect(() => {
    const screen = screenFor(pathname);
    if (!screen) return;
    try {
      sink()?.track("page_view", {
        screen_name: screen.name,
        page_title: screen.name,
        page_location: `${window.location.origin}${screen.template}`,
      });
    } catch {
      // see logEvent()
    }
  }, [pathname]);
}
