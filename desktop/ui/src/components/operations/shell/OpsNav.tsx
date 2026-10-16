import { PanelLeftClose, PanelLeftOpen } from "lucide-react";
import { SidebarNavLink } from "@/components/layout/SidebarNavLink";
import { NavStrip } from "@/components/operations/shell/NavStrip";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import { OPS_NAV, type OpsNavId } from "@/components/operations/shell/opsNavigation";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

interface OpsNavProps {
  activeId: OpsNavId;
  collapsed: boolean;
  onToggle: () => void;
}

/** The console's own navigation, beside the app sidebar: a grouped column
 * from lg (collapsible to icons), one scrolling row of links below it. */
export function OpsNav({ activeId, collapsed, onToggle }: OpsNavProps) {
  const { t } = useI18n();
  const href = useOpsHref();
  const label = t("operations.shell.navLabel");

  return (
    <>
      <aside
        className={cn(
          "hidden shrink-0 flex-col border-r border-sidebar-border bg-sidebar transition-[width] duration-200 lg:flex",
          collapsed ? "w-14" : "w-52",
        )}
      >
        <nav aria-label={label} className="min-h-0 flex-1 overflow-y-auto px-2 py-2 scrollbar-thin">
          {OPS_NAV.map((group, index) => (
            <div key={group.id} className={cn(index > 0 && (collapsed ? "mt-2 border-t border-sidebar-border pt-2" : "mt-3"))}>
              {!collapsed && (
                <p className="px-3 pb-1 text-micro font-medium tracking-wide text-muted-foreground uppercase">
                  {t(`operations.shell.groups.${group.id}`)}
                </p>
              )}
              <ul className="space-y-0.5">
                {group.items.map((item) => (
                  <li key={item.id}>
                    <SidebarNavLink
                      to={href(item.to)}
                      label={t(`operations.shell.items.${item.id}`)}
                      icon={item.icon}
                      collapsed={collapsed}
                      active={item.id === activeId}
                      className="py-1.5"
                    />
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </nav>
        <div className="shrink-0 border-t border-sidebar-border p-2">
          <Button
            variant="ghost"
            size={collapsed ? "icon" : "sm"}
            className={cn("w-full", !collapsed && "justify-start gap-3 px-3")}
            onClick={onToggle}
            aria-expanded={!collapsed}
            title={collapsed ? t("operations.shell.expand") : t("operations.shell.collapse")}
          >
            {collapsed ? <PanelLeftOpen aria-hidden /> : <PanelLeftClose aria-hidden />}
            {collapsed ? <span className="sr-only">{t("operations.shell.expand")}</span> : t("operations.shell.collapse")}
          </Button>
        </div>
      </aside>

      <NavStrip
        label={label}
        className="shrink-0 border-b border-border bg-sidebar px-3 lg:hidden"
        groups={OPS_NAV.map((group) =>
          group.items.map((item) => ({
            key: item.id,
            to: href(item.to),
            label: t(`operations.shell.items.${item.id}`),
            icon: item.icon,
            active: item.id === activeId,
          })),
        )}
      />
    </>
  );
}
