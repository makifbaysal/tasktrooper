import {
  Activity,
  AppWindow,
  Boxes,
  Film,
  Flame,
  HardDrive,
  Layers,
  LayoutDashboard,
  Sparkles,
  Timer,
  Waypoints,
  Zap,
  BellRing,
  Bug,
  Gauge,
  Network,
  Rocket,
  Route as RouteIcon,
  ShieldAlert,
  Target,
  Database,
  GitCommitHorizontal,
  Globe,
  LayoutGrid,
  ScrollText,
  Server,
  Siren,
  Smartphone,
  Webhook,
  Workflow,
  type LucideIcon,
} from "lucide-react";
import type { ComponentRole } from "@/api";
import { entityTypeOf } from "@/components/operations/console/entityTypes";
import { isEntityTab, type EntityTab } from "@/components/operations/console/roleViews";

export type OpsNavId =
  | "all"
  | "services"
  | "web"
  | "mobile"
  | "workers"
  | "errors"
  | "logs"
  | "changes"
  | "issues"
  | "alerts"
  | "sources"
  | "alertConnections"
  | "telServices"
  | "telEndpoints"
  | "telTraces"
  | "telLogs"
  | "telErrors"
  | "telIncidents"
  | "telMonitors"
  | "telDeploys"
  | "telSlo"
  | "telOverview"
  | "telMap"
  | "telHosts"
  | "telKubernetes"
  | "telDatabases"
  | "telQueues"
  | "telServerless"
  | "telApps"
  | "telReplays"
  | "telProfiling"
  | "telLlm"
  | "telLoadTests";

export type OpsNavGroupId = "explore" | "monitor" | "telemetry" | "telInfra" | "telApps" | "telPerformance" | "settings";

export interface OpsNavItem {
  id: OpsNavId;
  to: string;
  icon: LucideIcon;
}

export interface OpsNavGroup {
  id: OpsNavGroupId;
  items: OpsNavItem[];
}

export const OPS_NAV: OpsNavGroup[] = [
  {
    id: "explore",
    items: [
      { id: "all", to: "/operations", icon: LayoutGrid },
      { id: "services", to: "/operations/services", icon: Server },
      { id: "web", to: "/operations/web", icon: Globe },
      { id: "mobile", to: "/operations/mobile", icon: Smartphone },
      { id: "workers", to: "/operations/workers", icon: Workflow },
    ],
  },
  {
    id: "monitor",
    items: [
      { id: "errors", to: "/operations/errors", icon: Bug },
      { id: "logs", to: "/operations/logs", icon: ScrollText },
      { id: "changes", to: "/operations/changes", icon: GitCommitHorizontal },
      { id: "issues", to: "/operations/issues", icon: Siren },
      { id: "alerts", to: "/operations/alerts", icon: BellRing },
    ],
  },
  {
    id: "telemetry",
    items: [
      { id: "telOverview", to: "/operations/telemetry/overview", icon: LayoutDashboard },
      { id: "telMap", to: "/operations/telemetry/map", icon: Waypoints },
      { id: "telServices", to: "/operations/telemetry/services", icon: Activity },
      { id: "telEndpoints", to: "/operations/telemetry/endpoints", icon: Network },
      { id: "telTraces", to: "/operations/telemetry/traces", icon: RouteIcon },
      { id: "telLogs", to: "/operations/telemetry/logs", icon: ScrollText },
      { id: "telErrors", to: "/operations/telemetry/errors", icon: ShieldAlert },
      { id: "telIncidents", to: "/operations/telemetry/incidents", icon: Siren },
      { id: "telMonitors", to: "/operations/telemetry/monitors", icon: Gauge },
      { id: "telDeploys", to: "/operations/telemetry/deploys", icon: Rocket },
      { id: "telSlo", to: "/operations/telemetry/slo", icon: Target },
    ],
  },
  {
    id: "telInfra",
    items: [
      { id: "telHosts", to: "/operations/telemetry/infra/hosts", icon: HardDrive },
      { id: "telKubernetes", to: "/operations/telemetry/infra/kubernetes", icon: Boxes },
      { id: "telDatabases", to: "/operations/telemetry/infra/databases", icon: Database },
      { id: "telQueues", to: "/operations/telemetry/infra/queues", icon: Layers },
      { id: "telServerless", to: "/operations/telemetry/infra/serverless", icon: Zap },
    ],
  },
  {
    id: "telApps",
    items: [
      { id: "telApps", to: "/operations/telemetry/apps", icon: AppWindow },
      { id: "telReplays", to: "/operations/telemetry/replays", icon: Film },
    ],
  },
  {
    id: "telPerformance",
    items: [
      { id: "telProfiling", to: "/operations/telemetry/profiling", icon: Flame },
      { id: "telLlm", to: "/operations/telemetry/llm", icon: Sparkles },
      { id: "telLoadTests", to: "/operations/telemetry/load-tests", icon: Timer },
    ],
  },
  {
    id: "settings",
    items: [
      { id: "sources", to: "/operations/sources", icon: Database },
      { id: "alertConnections", to: "/operations/alert-connections", icon: Webhook },
    ],
  },
];

export const OPS_NAV_ITEMS: OpsNavItem[] = OPS_NAV.flatMap((g) => g.items);

/** A page under a nav item that has a title of its own; the nav lights its parent. */
export interface OpsSubPage {
  path: string;
  parent: OpsNavId;
  titleKey: string;
}

export const OPS_SUB_PAGES: OpsSubPage[] = [
  { path: "/operations/errors/definitions", parent: "errors", titleKey: "operations.errorDefinitions.title" },
];

export function opsSubPage(pathname: string): OpsSubPage | undefined {
  const path = pathname.replace(/\/+$/, "");
  return OPS_SUB_PAGES.find((p) => path === p.path);
}

const TELEMETRY_NAV_PREFIX = "tel";

/** The telemetry group's labels live in its own dictionary; the console's
 * older groups keep theirs under `operations.shell`. */
export function navItemLabelKey(id: OpsNavId): string {
  return id.startsWith(TELEMETRY_NAV_PREFIX)
    ? `telemetryHub.nav.items.${id}`
    : `operations.shell.items.${id}`;
}

export function navGroupLabelKey(id: OpsNavGroupId): string {
  if (id === "telemetry") return "telemetryHub.nav.group";
  return id.startsWith(TELEMETRY_NAV_PREFIX) ? `telemetryHub.nav.groups.${id}` : `operations.shell.groups.${id}`;
}

export function navGroupOf(id: OpsNavId): OpsNavGroupId {
  return OPS_NAV.find((g) => g.items.some((i) => i.id === id))?.id ?? "explore";
}

export interface OpsEntityRoute {
  entityId: string;
  tab?: EntityTab;
}

// A hand-typed or truncated link ("/operations/entity/%E0%A4") must not take
// the shell down: an id that does not decode is kept as written, and the page
// says it is not found.
function decodeSegment(segment: string): string {
  try {
    return decodeURIComponent(segment);
  } catch {
    return segment;
  }
}

export function matchEntityRoute(pathname: string): OpsEntityRoute | undefined {
  const m = /^\/operations\/entity\/([^/]+)(?:\/([^/]+))?/.exec(pathname);
  if (!m) return undefined;
  const tab = m[2];
  return { entityId: decodeSegment(m[1]), tab: isEntityTab(tab) ? tab : undefined };
}

// An entity page lights its own type in the nav, the way the list it was
// opened from would; a type with no list of its own falls back to All entities.
export function activeNavId(pathname: string, entityRole?: ComponentRole): OpsNavId {
  const path = pathname.replace(/\/+$/, "") || "/";
  if (matchEntityRoute(path)) {
    if (!entityRole) return "all";
    const type = entityTypeOf(entityRole);
    return type === "other" ? "all" : type;
  }
  const item = OPS_NAV_ITEMS.find((i) => i.id !== "all" && (path === i.to || path.startsWith(`${i.to}/`)));
  return item?.id ?? "all";
}
