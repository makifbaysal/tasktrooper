import { Menu, Moon, Sun } from "lucide-react";
import { Button } from "@/components/ui/button";
import { HealthStatus } from "@/components/layout/HealthStatus";
import { SidebarBrand } from "@/components/layout/SidebarBrand";
import { useDesktopHost } from "@/components/runner/useDesktopRunner";
import { useI18n } from "@/hooks/useI18n";
import { useTheme } from "@/hooks/useTheme";
import { cn } from "@/lib/utils";

interface HeaderProps {
  title?: string;
  onMenuClick?: () => void;
  sidebarCollapsed?: boolean;
}

export function Header({ title, onMenuClick, sidebarCollapsed = false }: HeaderProps) {
  const { t } = useI18n();
  const { theme, toggleTheme } = useTheme();
  // Only the shell's window has macOS's traffic lights floating over its top
  // left corner (`titleBarStyle: "hiddenInset"`, `main/window.ts`) — a browser
  // tab has no such thing to clear, so the extra inset is shell-only.
  const inShell = useDesktopHost() !== null;

  return (
    <header className="relative z-50 flex h-14 shrink-0 items-center justify-between border-b border-sidebar-border bg-surface-raised/90 px-4 shadow-[var(--shadow-raised)] backdrop-blur-sm">
      <div className={cn("flex min-w-0 items-center gap-3", inShell && "pl-16")}>
        <Button variant="ghost" size="icon" className="lg:hidden" onClick={onMenuClick}>
          <Menu className="h-5 w-5" />
        </Button>
        {/*
          The header's own px-4 puts this at 16px; the sidebar nav icon below it
          sits at nav's px-2 + link's px-3 = 20px expanded, or nav's px-2 + the
          collapsed link's centered icon = 26px collapsed (WorkspaceSidebar /
          SidebarNavLink). These margins close that gap so the logo's left edge
          tracks the nav icon's left edge in both states.

          In shell, the sibling div above also carries pl-16 (64px) to clear
          the macOS traffic lights — the sidebar has no such offset since it
          renders below the header, out of the traffic lights' reach. Without
          canceling that 64px here too, the logo would sit 64px right of the
          nav icons in the packaged app. -ml-[68px]/-ml-[62px] are the same
          -4px/+2px base offsets above, minus the 64px pl-16 adds.

          That cancel-offset only makes sense once the sidebar (and its nav
          icons) is actually on screen, at lg and up — below that the mobile
          menu button sits in this same flex row, and pulling the logo 68px
          left drags it back over the button. Gate it with lg: so the narrow
          layout keeps the logo in its natural post-button position.
        */}
        <SidebarBrand
          collapsed={sidebarCollapsed}
          className={
            inShell
              ? sidebarCollapsed
                ? "lg:-ml-[62px]"
                : "lg:-ml-[68px]"
              : sidebarCollapsed
                ? "ml-0.5"
                : "-ml-1"
          }
        />
        {title && <h1 className="truncate text-title font-semibold">{title}</h1>}
      </div>

      <div className="min-w-0 flex-1 self-stretch" aria-hidden />

      <div className="flex items-center gap-2">
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
