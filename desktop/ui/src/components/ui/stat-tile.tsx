import type { ReactNode } from "react";
import { Card } from "@/components/ui/card";
import { cn } from "@/lib/utils";

export interface StatTileProps {
  label: string;
  value: ReactNode;
  /** Native tooltip on the value, e.g. the uncompacted number. */
  title?: string;
  foot?: ReactNode;
  tone?: "default" | "warning";
  className?: string;
  children?: ReactNode;
}

export function StatTile({ label, value, title, foot, tone = "default", className, children }: StatTileProps) {
  return (
    <Card className={cn("flex flex-col p-4", className)}>
      <p className="text-caption text-muted-foreground">{label}</p>
      <p className={cn("mt-1 text-display font-semibold", tone === "warning" && "text-warning")} title={title}>
        {value}
      </p>
      {children}
      {foot && <p className="mt-auto pt-2 text-caption text-muted-foreground">{foot}</p>}
    </Card>
  );
}
