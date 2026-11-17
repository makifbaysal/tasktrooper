import { ArrowRight } from "lucide-react";
import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { cn } from "@/lib/utils";

interface TelemetrySectionProps {
  title: string;
  /** A "see all" link to the list the section summarises. */
  more?: { to: string; label: string };
  children: ReactNode;
  className?: string;
}

/** A titled card on an overview: a short list with a way to the full one. */
export function TelemetrySection({ title, more, children, className }: TelemetrySectionProps) {
  const href = useOpsHref();
  return (
    <Card className={cn("min-w-0 overflow-hidden", className)}>
      <div className="flex items-center gap-2 border-b border-border px-4 py-2.5">
        <h3 className="text-body font-medium">{title}</h3>
        {more && (
          <Button asChild variant="ghost" size="sm" className="ml-auto">
            <Link to={href(more.to)}>
              {more.label}
              <ArrowRight aria-hidden />
            </Link>
          </Button>
        )}
      </div>
      {children}
    </Card>
  );
}

/** One row of a section's list. */
export function TelemetrySectionRow({ children }: { children: ReactNode }) {
  return <div className="flex min-w-0 items-center gap-3 border-b border-border/50 px-4 py-2.5 text-caption last:border-0">{children}</div>;
}

export function TelemetrySectionEmpty({ children }: { children: ReactNode }) {
  return <p className="px-4 py-6 text-center text-caption text-muted-foreground">{children}</p>;
}
