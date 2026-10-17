import { useState } from "react";
import { Outlet, useLocation } from "react-router-dom";
import { PageSuspense } from "@/components/layout/PageSuspense";
import { EXPLORER_TYPE_PATHS, entityTypeOf } from "@/components/operations/console/entityTypes";
import { OpsNav } from "@/components/operations/shell/OpsNav";
import { OpsProvider, useOpsHref } from "@/components/operations/shell/OpsContext";
import { OPS_NAV_ITEMS, activeNavId, matchEntityRoute, navGroupOf, opsSubPage } from "@/components/operations/shell/opsNavigation";
import { OpsTopBar, type OpsCrumb } from "@/components/operations/shell/OpsTopBar";
import { useOpsEntity } from "@/components/operations/shell/useOpsEntities";
import { useI18n } from "@/hooks/useI18n";

const NAV_COLLAPSED_KEY = "tt.ops.nav.collapsed";

function readCollapsed(): boolean | null {
  try {
    const raw = localStorage.getItem(NAV_COLLAPSED_KEY);
    return raw === null ? null : raw === "1";
  } catch {
    return null;
  }
}

function writeCollapsed(value: boolean) {
  try {
    localStorage.setItem(NAV_COLLAPSED_KEY, value ? "1" : "0");
  } catch {
    // Storage blocked: the choice lasts this visit only.
  }
}

/** The Operations console's template: its own grouped navigation, the global
 * top bar (environment, time window, compare, refresh) and the page. All
 * console state lives in the URL, through OpsProvider. */
export function OpsShell() {
  return (
    <OpsProvider>
      <OpsShellFrame />
    </OpsProvider>
  );
}

function OpsShellFrame() {
  const { t } = useI18n();
  const { pathname } = useLocation();
  const href = useOpsHref();
  const entityRoute = matchEntityRoute(pathname);
  const { entity } = useOpsEntity(entityRoute?.entityId);
  const activeId = activeNavId(pathname, entity?.role);
  // Nobody chose yet: an entity page has its own sub-navigation, so the
  // console's column starts folded there to leave the charts their room.
  const [collapsedPref, setCollapsedPref] = useState<boolean | null>(readCollapsed);
  const collapsed = collapsedPref ?? entityRoute !== undefined;

  const toggle = () => {
    const next = !collapsed;
    setCollapsedPref(next);
    writeCollapsed(next);
  };

  let crumbs: OpsCrumb[];
  let title: string | undefined;
  if (entityRoute) {
    const type = entity ? entityTypeOf(entity.role) : "other";
    crumbs = [{ label: t("operations.shell.items.all"), to: href("/operations") }];
    if (type !== "other") crumbs.push({ label: t(`operations.shell.items.${type}`), to: href(EXPLORER_TYPE_PATHS[type]) });
    crumbs.push({ label: entity?.name ?? t("operations.entity.loadingName") });
  } else {
    crumbs = [{ label: t(`operations.shell.groups.${navGroupOf(activeId)}`) }];
    title = t(`operations.shell.items.${activeId}`);
    const sub = opsSubPage(pathname);
    const parent = sub && OPS_NAV_ITEMS.find((i) => i.id === sub.parent);
    if (sub && parent) {
      crumbs.push({ label: title, to: href(parent.to) });
      title = t(sub.titleKey);
    }
  }

  return (
    <div className="flex h-full min-h-0 flex-1 flex-col overflow-hidden lg:flex-row">
      <OpsNav activeId={activeId} collapsed={collapsed} onToggle={toggle} />
      <div className="flex min-h-0 min-w-0 flex-1 flex-col">
        <OpsTopBar crumbs={crumbs} title={title} />
        <div className="relative min-h-0 flex-1 overflow-auto scrollbar-thin">
          <div className="min-w-0 p-4 lg:p-6">
            <PageSuspense>
              <Outlet />
            </PageSuspense>
          </div>
        </div>
      </div>
    </div>
  );
}
