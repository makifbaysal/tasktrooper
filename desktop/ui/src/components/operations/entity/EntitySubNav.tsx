import {
  BellRing,
  Bug,
  FileText,
  Gauge,
  GitCommitHorizontal,
  Inbox,
  LayoutDashboard,
  ListTree,
  Network,
  Package,
  ScrollText,
  Siren,
  Smartphone,
  type LucideIcon,
} from "lucide-react";
import { SidebarNavLink } from "@/components/layout/SidebarNavLink";
import { entityPath } from "@/components/operations/console/opsEntities";
import { viewFor, type EntityTab } from "@/components/operations/console/roleViews";
import { NavStrip } from "@/components/operations/shell/NavStrip";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import type { OpsEntity } from "@/components/operations/shell/useOpsEntities";
import { useI18n } from "@/hooks/useI18n";

export const ENTITY_TAB_ICONS: Record<EntityTab, LucideIcon> = {
  summary: LayoutDashboard,
  endpoints: Network,
  pages: FileText,
  crashes: Smartphone,
  queues: Inbox,
  releases: Package,
  errors: Bug,
  logs: ScrollText,
  traces: ListTree,
  changes: GitCommitHorizontal,
  issues: Siren,
  alerts: BellRing,
  signals: Gauge,
};

interface EntitySubNavProps {
  entity: OpsEntity;
  active?: EntityTab;
}

/** An entity's own sections, picked by its role (`ROLE_VIEWS`): a column from
 * lg, one scrolling row below it. */
export function EntitySubNav({ entity, active }: EntitySubNavProps) {
  const { t } = useI18n();
  const href = useOpsHref();
  const tabs = viewFor(entity.role).tabs;
  const label = t("operations.entity.navLabel", { name: entity.name });
  const current = active ?? tabs[0];

  return (
    <>
      <nav aria-label={label} className="hidden w-44 shrink-0 lg:block">
        <ul className="space-y-0.5">
          {tabs.map((tab) => (
            <li key={tab}>
              <SidebarNavLink
                to={href(entityPath(entity.id, tab))}
                label={t(`operations.entity.tabs.${tab}`)}
                icon={ENTITY_TAB_ICONS[tab]}
                collapsed={false}
                active={tab === current}
                className="py-1.5"
              />
            </li>
          ))}
        </ul>
      </nav>
      <NavStrip
        label={label}
        className="-mt-2 border-b border-border lg:hidden"
        groups={[
          tabs.map((tab) => ({
            key: tab,
            to: href(entityPath(entity.id, tab)),
            label: t(`operations.entity.tabs.${tab}`),
            icon: ENTITY_TAB_ICONS[tab],
            active: tab === current,
          })),
        ]}
      />
    </>
  );
}
