import { BarChart3, Bot, Boxes, GitBranch, Globe, Kanban, Plug, Server, Users, Wrench, type LucideIcon } from "lucide-react";
import { NavLink, Outlet } from "react-router-dom";
import { PageContent } from "@/components/layout/PageContent";
import { PageSuspense } from "@/components/layout/PageSuspense";
import { PageHeader } from "@/components/admin/PageHeader";
import { desktopToolchain } from "@/lib/desktop-bridge";
import { ADVANCED_TEAM_CONFIG } from "@/lib/features";
import { TOOLCHAIN_SETTINGS_PATH } from "@/lib/toolchainJob";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

interface SettingsNavItem {
  to: string;
  label: string;
  icon: LucideIcon;
}

interface SettingsNavGroup {
  id: string;
  label?: string;
  items: SettingsNavItem[];
}

export function SettingsLayout() {
  const { t } = useI18n();
  const groups: SettingsNavGroup[] = [
    {
      id: "general",
      items: [{ to: "/settings", label: t("frame.layout.settingsTabs.general"), icon: Globe }],
    },
    {
      id: "ai",
      label: t("frame.layout.settingsGroups.ai"),
      items: [
        { to: "/settings/llm", label: t("frame.layout.settingsTabs.llm"), icon: Bot },
        { to: "/settings/mcp", label: t("frame.layout.settingsTabs.mcp"), icon: Server },
        { to: "/settings/catalog", label: t("frame.layout.settingsTabs.catalog"), icon: Boxes },
        // Only the desktop shell can probe and install on this computer.
        ...(desktopToolchain()
          ? [{ to: TOOLCHAIN_SETTINGS_PATH, label: t("frame.layout.settingsTabs.environment"), icon: Wrench }]
          : []),
        ...(ADVANCED_TEAM_CONFIG
          ? [{ to: "/settings/roles", label: t("frame.layout.settingsTabs.roles"), icon: Users }]
          : []),
      ],
    },
    {
      id: "workspace",
      label: t("frame.layout.settingsGroups.workspace"),
      items: [
        { to: "/settings/integrations", label: t("frame.layout.settingsTabs.integrations"), icon: Plug },
        { to: "/settings/usage", label: t("frame.layout.settingsTabs.usage"), icon: BarChart3 },
        ...(ADVANCED_TEAM_CONFIG
          ? [
              { to: "/settings/board", label: t("frame.layout.settingsTabs.board"), icon: Kanban },
              { to: "/settings/workflows", label: t("frame.layout.settingsTabs.workflows"), icon: GitBranch },
            ]
          : []),
      ],
    },
  ];

  return (
    <PageContent>
      <PageHeader title={t("frame.layout.settings.title")} description={t("frame.layout.settings.description")} />
      <div className="flex flex-col gap-6 xl:flex-row xl:items-start xl:gap-10">
        <SettingsNav groups={groups} label={t("frame.layout.settings.title")} />
        <div className="min-w-0 flex-1">
          <PageSuspense>
            <Outlet />
          </PageSuspense>
        </div>
      </div>
    </PageContent>
  );
}

/**
 * Below xl the groups run as one scrolling row of pills, separated by a rule;
 * from xl they are a labelled column beside the page.
 */
function SettingsNav({ groups, label }: { groups: SettingsNavGroup[]; label: string }) {
  return (
    <nav
      aria-label={label}
      className="-mx-1 flex items-center gap-1 overflow-x-auto px-1 pb-1 xl:sticky xl:top-4 xl:mx-0 xl:w-56 xl:shrink-0 xl:flex-col xl:items-stretch xl:gap-5 xl:overflow-visible xl:p-0"
    >
      {groups
        .filter((group) => group.items.length > 0)
        .map((group, index) => (
          <div key={group.id} className="flex shrink-0 items-center gap-1 xl:flex-col xl:items-stretch xl:gap-0.5">
            {index > 0 && <span aria-hidden className="mx-1 h-5 w-px bg-border xl:hidden" />}
            {group.label && (
              <p className="hidden px-3 pb-1 text-micro font-semibold uppercase tracking-wide text-muted-foreground xl:block">
                {group.label}
              </p>
            )}
            {group.items.map(({ to, label: itemLabel, icon: Icon }) => (
              <NavLink
                key={to}
                to={to}
                end
                className={({ isActive }) =>
                  cn(
                    "flex items-center gap-2 whitespace-nowrap rounded-lg px-3 py-2 text-body font-medium transition-colors",
                    isActive
                      ? "bg-accent text-accent-foreground"
                      : "text-muted-foreground hover:bg-accent/60 hover:text-foreground",
                  )
                }
              >
                <Icon className="h-4 w-4 shrink-0" />
                {itemLabel}
              </NavLink>
            ))}
          </div>
        ))}
    </nav>
  );
}
