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
}

export function Header({ title, onMenuClick }: HeaderProps) {
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
        <SidebarBrand />
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
