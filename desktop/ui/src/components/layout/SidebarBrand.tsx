import { Logo } from "@/assets/logo";
import { cn } from "@/lib/utils";

interface SidebarBrandProps {
  collapsed?: boolean;
  className?: string;
}

export function SidebarBrand({ collapsed = false, className }: SidebarBrandProps) {
  if (collapsed) {
    return (
      <div className={cn("flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-primary text-primary-foreground", className)}>
        <Logo className="h-4 w-4" />
      </div>
    );
  }

  return (
    <div className={cn("flex min-w-0 items-center gap-2", className)}>
      <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-primary text-primary-foreground">
        <Logo className="h-4 w-4" />
      </div>
      <p className="truncate text-body font-semibold text-sidebar-accent-foreground">TaskTrooper</p>
    </div>
  );
}
