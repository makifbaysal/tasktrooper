import { BarChart3, Bot, FileSearch, Globe, Kanban, Plug, Server } from "lucide-react";
import { NavLink, Outlet } from "react-router-dom";
import { PageContent } from "@/components/layout/PageContent";
import { PageHeader } from "@/components/admin/PageHeader";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

export function SettingsLayout() {
  const { t } = useI18n();
  const tabs = [
    { to: "/settings", label: t("frame.layout.settingsTabs.general"), icon: Globe, end: true },
    { to: "/settings/board", label: "Board", icon: Kanban, end: true },
    {
      to: "/settings/analiz-assignment",
      label: t("frame.layout.settingsTabs.analizAssignment"),
      icon: FileSearch,
      end: true,
    },
    { to: "/settings/llm", label: t("frame.layout.settingsTabs.llm"), icon: Bot, end: true },
    { to: "/settings/mcp", label: t("frame.layout.settingsTabs.mcp"), icon: Server, end: true },
    {
      to: "/settings/integrations",
      label: t("frame.layout.settingsTabs.integrations"),
      icon: Plug,
      end: true,
    },
    { to: "/settings/usage", label: t("frame.layout.settingsTabs.usage"), icon: BarChart3, end: true },
  ];

  return (
    <PageContent>
      <PageHeader title={t("frame.layout.settings.title")} description={t("frame.layout.settings.description")} />
      <nav className="mb-6 flex flex-wrap gap-1 border-b border-border">
        {tabs.map(({ to, label, icon: Icon, end }) => (
          <NavLink
            key={to}
            to={to}
            end={end}
            className={({ isActive }) =>
              cn(
                "flex items-center gap-2 border-b-2 px-4 py-2 text-sm font-medium transition-colors -mb-px",
                isActive
                  ? "border-primary text-primary"
                  : "border-transparent text-muted-foreground hover:border-border hover:text-foreground",
              )
            }
          >
            <Icon className="h-4 w-4 shrink-0" />
            {label}
          </NavLink>
        ))}
      </nav>
      <Outlet />
    </PageContent>
  );
}
