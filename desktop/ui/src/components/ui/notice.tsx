import { AlertTriangle, Info, XCircle, type LucideIcon } from "lucide-react";
import { cn } from "@/lib/utils";

export type NoticeVariant = "warning" | "error" | "info";

interface NoticeProps {
  variant?: NoticeVariant;
  title: string;
  /** The body. Plain text, or nodes when the caller renders markdown itself. */
  children?: React.ReactNode;
  className?: string;
}

const variantStyles: Record<NoticeVariant, { box: string; icon: LucideIcon; iconClass: string }> = {
  warning: {
    box: "border-amber-500/40 bg-amber-500/10 text-amber-900 dark:text-amber-100",
    icon: AlertTriangle,
    iconClass: "text-amber-600 dark:text-amber-400",
  },
  error: {
    box: "border-destructive/40 bg-destructive/10 text-destructive",
    icon: XCircle,
    iconClass: "text-destructive",
  },
  info: {
    box: "border-border bg-muted text-foreground",
    icon: Info,
    iconClass: "text-muted-foreground",
  },
};

/**
 * A calm, inline callout: a condition the user should read and can act on.
 *
 * It exists because "something went wrong" and "your provider quota is spent"
 * had the same red stack-trace treatment, which taught people to read neither.
 * Warning is the default because that is the case that needed one.
 */
export function Notice({ variant = "warning", title, children, className }: NoticeProps) {
  const style = variantStyles[variant];
  const Icon = style.icon;
  return (
    <div className={cn("flex gap-3 rounded-2xl border px-4 py-3 text-body shadow-sm", style.box, className)}>
      <Icon className={cn("mt-0.5 h-4 w-4 shrink-0", style.iconClass)} aria-hidden />
      <div className="min-w-0 space-y-1">
        <p className="font-medium">{title}</p>
        {children && <div className="opacity-90">{children}</div>}
      </div>
    </div>
  );
}
