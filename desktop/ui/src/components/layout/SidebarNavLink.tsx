import type { ReactNode } from "react";
import { NavLink } from "react-router-dom";
import type { LucideIcon } from "lucide-react";
import { cn } from "@/lib/utils";

interface SidebarNavLinkProps {
  to: string;
  label: string;
  icon: LucideIcon;
  collapsed: boolean;
  end?: boolean;
  onClick?: () => void;
  /**
   * Rendered after the label — a notification badge, a count. Hidden while
   * collapsed along with the label itself: there is no room for either next
   * to the bare icon, and a badge with no label to sit beside would read as
   * unexplained.
   */
  trailing?: ReactNode;
}

export function SidebarNavLink({
  to,
  label,
  icon: Icon,
  collapsed,
  end,
  onClick,
  trailing,
}: SidebarNavLinkProps) {
  return (
    <NavLink
      to={to}
      end={end}
      onClick={onClick}
      className={({ isActive }) =>
        cn(
          "flex items-center gap-3 rounded-lg px-3 py-2 text-body font-medium transition-colors active:scale-[0.98]",
          collapsed && "justify-center px-2",
          isActive
            ? "bg-sidebar-accent text-sidebar-accent-foreground"
            : "text-sidebar-foreground hover:bg-sidebar-accent/60 hover:text-sidebar-accent-foreground active:bg-sidebar-accent/80",
        )
      }
      title={collapsed ? label : undefined}
    >
      <Icon className="h-4 w-4 shrink-0" />
      {!collapsed && <span className="min-w-0 flex-1 truncate">{label}</span>}
      {!collapsed && trailing}
    </NavLink>
  );
}
