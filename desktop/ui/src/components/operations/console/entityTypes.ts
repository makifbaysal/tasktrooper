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

const GOLDEN: Record<OpsEntityType, SignalSpec[]> = {
  services: ROLE_VIEWS.backend.golden,
  web: ROLE_VIEWS.frontend.golden,
  mobile: ROLE_VIEWS.mobile.golden,
  workers: ROLE_VIEWS.worker.golden,
  other: [],
};

export function goldenColumnsFor(type: OpsEntityType): SignalSpec[] {
  return GOLDEN[type];
}
