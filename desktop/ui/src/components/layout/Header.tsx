import { Menu, Moon, Sun } from "lucide-react";
import { Button } from "@/components/ui/button";
import { HealthStatus } from "@/components/layout/HealthStatus";
import { NotificationCenter } from "@/components/layout/NotificationCenter";
import { SidebarBrand } from "@/components/layout/SidebarBrand";
import { useI18n } from "@/hooks/useI18n";
import { useTheme } from "@/hooks/useTheme";

interface HeaderProps {
  title?: string;
  onMenuClick?: () => void;
  sidebarCollapsed?: boolean;
  /** Advances an agent's sidebar "last viewed" from a notification-center click. Optional so a Header shown outside WorkspaceShell (none today) still renders without a bell. */
  onAgentSeen?: (agentId: string, iso: string) => void;
}

export function Header({ title, onMenuClick, sidebarCollapsed = false, onAgentSeen }: HeaderProps) {
  const { t } = useI18n();
  const { theme, toggleTheme } = useTheme();

  return (
    <header className="relative z-50 flex h-14 shrink-0 items-center justify-between border-b border-sidebar-border bg-surface-raised/90 px-4 shadow-[var(--shadow-raised)] backdrop-blur-sm">
      <div className="flex min-w-0 items-center gap-3">
        <Button variant="ghost" size="icon" className="lg:hidden" onClick={onMenuClick}>
          <Menu className="h-5 w-5" />
        </Button>
        {/*
          The header's own px-4 puts this at 16px; the sidebar nav icon below it
          sits at nav's px-2 + link's px-3 = 20px expanded, or nav's px-2 + the
          collapsed link's centered icon = 26px collapsed (WorkspaceSidebar /
          SidebarNavLink). These margins close that gap so the logo's left edge
          tracks the nav icon's left edge in both states.

          No shell-only offset here: the shell (`main/window.ts`) positions the
          web app's whole view at `y: CHROME_HEIGHT`, below the native title
          bar strip the traffic lights sit in, so they never reach this row —
          same alignment math in the shell and in a browser tab.
        */}
        <SidebarBrand collapsed={sidebarCollapsed} className={sidebarCollapsed ? "ml-0.5" : "-ml-1"} />
        {title && <h1 className="truncate text-title font-semibold">{title}</h1>}
      </div>

      <div className="min-w-0 flex-1 self-stretch" aria-hidden />

      <div className="flex items-center gap-2">
        {onAgentSeen && <NotificationCenter onAgentSeen={onAgentSeen} />}
        <HealthStatus />

        <Button
          variant="ghost"
          size="icon"
          onClick={toggleTheme}
          title={theme === "dark" ? t("frame.layout.header.lightTheme") : t("frame.layout.header.darkTheme")}
        >
          {theme === "dark" ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
        </Button>
      </div>
    </header>
  );
}
