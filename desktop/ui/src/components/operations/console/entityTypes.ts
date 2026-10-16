import type { ComponentRole } from "@/api";
import { ROLE_VIEWS, type SignalSpec } from "@/components/operations/console/roleViews";

export type OpsEntityType = "services" | "web" | "mobile" | "workers" | "other";

export type OpsExplorerType = Exclude<OpsEntityType, "other">;

export const OPS_ENTITY_TYPES: OpsEntityType[] = ["services", "web", "mobile", "workers", "other"];

export const EXPLORER_TYPE_PATHS: Record<OpsExplorerType, string> = {
  services: "/operations/services",
  web: "/operations/web",
  mobile: "/operations/mobile",
  workers: "/operations/workers",
};

export function entityTypeOf(role: ComponentRole): OpsEntityType {
  switch (role) {
    case "backend":
      return "services";
    case "frontend":
      return "web";
    case "mobile":
    case "desktop":
      return "mobile";
    case "worker":
      return "workers";
    default:
      return "other";
  }
}

export function isEntityType(value: string | null | undefined): value is OpsEntityType {
  return OPS_ENTITY_TYPES.some((t) => t === value);
}

// The roles each type lists, the one whose golden signals lead first. Mobile
// lists desktop apps too, whose golden list differs (crash rate, no ANR).
const TYPE_ROLES: Record<OpsEntityType, ComponentRole[]> = {
  services: ["backend"],
  web: ["frontend"],
  mobile: ["mobile", "desktop"],
  workers: ["worker"],
  other: [],
};

/** A type list's golden columns: every golden signal of the roles it lists,
 * the lead role's first. With `roles` (the rows' own), only the columns some
 * row has; with none of the type's roles among them, all of them. */
export function goldenColumnsFor(type: OpsEntityType, roles?: Iterable<ComponentRole | undefined>): SignalSpec[] {
  const present = roles ? new Set(roles) : undefined;
  const listed = TYPE_ROLES[type].filter((role) => present?.has(role) ?? true);
  const out = new Map<string, SignalSpec>();
  for (const role of listed.length > 0 ? listed : TYPE_ROLES[type]) {
    for (const spec of ROLE_VIEWS[role].golden) if (!out.has(spec.id)) out.set(spec.id, spec);
  }
  return [...out.values()];
}

/** Whether a golden column is one of this row's role's own golden signals. */
export function roleHasGolden(role: ComponentRole | undefined, signal: string): boolean {
  return role ? ROLE_VIEWS[role].golden.some((spec) => spec.id === signal) : false;
}
