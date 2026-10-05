import { AlertTriangle, Check, Circle, Loader2, X } from "lucide-react";
import type { FeedStatus } from "@/lib/activityFeed";
import { cn } from "@/lib/utils";

interface FeedStatusIconProps {
  status: FeedStatus;
  className?: string;
}

export function FeedStatusIcon({ status, className }: FeedStatusIconProps) {
  const base = cn("h-3.5 w-3.5 shrink-0", className);
  switch (status) {
    case "running":
      return <Loader2 aria-hidden className={cn(base, "animate-spin text-warning")} />;
    case "completed":
      return <Check aria-hidden className={cn(base, "text-success")} />;
    case "failed":
      return <X aria-hidden className={cn(base, "text-destructive")} />;
    case "incomplete":
      return <AlertTriangle aria-hidden className={cn(base, "text-warning")} />;
    default:
      return <Circle aria-hidden className={cn(base, "text-muted-foreground")} />;
  }
}
