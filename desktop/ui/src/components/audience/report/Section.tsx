import type { ReactNode } from "react";
import { Card } from "@/components/ui/card";
import { cn } from "@/lib/utils";

export function Section({
  title,
  description,
  children,
  className,
}: {
  title: string;
  description?: string;
  children: ReactNode;
  className?: string;
}) {
  return (
    <Card className={cn("space-y-4 p-5", className)}>
      <div>
        <h2 className="text-title font-semibold tracking-tight">{title}</h2>
        {description && <p className="mt-1 text-caption text-muted-foreground">{description}</p>}
      </div>
      {children}
    </Card>
  );
}
