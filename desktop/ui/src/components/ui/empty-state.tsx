import { type LucideIcon } from "lucide-react";
import { cn } from "@/lib/utils";

export type EmptyStateVariant = "default" | "critical" | "search";

interface EmptyStateProps {
  icon: LucideIcon;
  title: string;
  description?: string;
  action?: React.ReactNode;
  className?: string;
  /** "default" keeps the existing muted circle; "critical" tints it toward
   * destructive for a failure state, "search" toward info for an empty
   * search/filter result. */
  variant?: EmptyStateVariant;
}

const iconWrapperVariants: Record<EmptyStateVariant, string> = {
  default: "bg-muted",
  critical: "bg-destructive/10",
  search: "bg-info/10",
};

const iconVariants: Record<EmptyStateVariant, string> = {
  default: "text-muted-foreground",
  critical: "text-destructive",
  search: "text-info",
};

export function EmptyState({ icon: Icon, title, description, action, className, variant = "default" }: EmptyStateProps) {
  return (
    <div className={cn("flex flex-col items-center justify-center gap-3 py-12 px-4 text-center", className)}>
      <div className={cn("flex h-12 w-12 items-center justify-center rounded-full", iconWrapperVariants[variant])}>
        <Icon className={cn("h-6 w-6", iconVariants[variant])} />
      </div>
      <div className="space-y-1">
        <h3 className="font-medium">{title}</h3>
        {description && <p className="text-caption text-muted-foreground max-w-sm">{description}</p>}
      </div>
      {action}
    </div>
  );
}
