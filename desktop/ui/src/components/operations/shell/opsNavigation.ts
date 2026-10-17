import {
  BellRing,
  Bug,
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
  | "alertConnections";

export type OpsNavGroupId = "explore" | "monitor" | "settings";

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
