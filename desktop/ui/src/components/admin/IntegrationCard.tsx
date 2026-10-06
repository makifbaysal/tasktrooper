import { type ReactNode } from "react";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { cn } from "@/lib/utils";

export type IntegrationStatusTone = "connected" | "attention" | "idle";

const TONE_VARIANT = {
  connected: "success",
  attention: "warning",
  idle: "outline",
} as const;

const TONE_DOT: Record<IntegrationStatusTone, string> = {
  connected: "bg-success",
  attention: "bg-warning",
  idle: "bg-muted-foreground/50",
};

export function IntegrationStatus({ tone, label }: { tone: IntegrationStatusTone; label: string }) {
  return (
    <Badge variant={TONE_VARIANT[tone]} className="gap-1.5 font-medium">
      <span className={cn("h-1.5 w-1.5 rounded-full", TONE_DOT[tone])} aria-hidden />
      {label}
    </Badge>
  );
}

interface IntegrationCardProps {
  /** The service's mark, drawn inside the card's icon tile. */
  icon: ReactNode;
  name: string;
  description?: string;
  status?: { tone: IntegrationStatusTone; label: string };
  /** Right-aligned header actions: connect, disconnect, refresh. */
  actions?: ReactNode;
  /** The connection's details, under a divider: identity, accounts, a form. */
  children?: ReactNode;
  className?: string;
}

/**
 * One external service on the Integrations page — the same header (mark,
 * name, status, one-line purpose, actions) for every integration, so a page of
 * them reads as one list rather than a set of differently built forms.
 */
export function IntegrationCard({ icon, name, description, status, actions, children, className }: IntegrationCardProps) {
  return (
    <Card className={cn("overflow-hidden", className)}>
      <div className="flex flex-wrap items-center gap-3 px-5 py-4">
        <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg border border-border bg-muted/40 text-foreground">
          {icon}
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="text-body font-semibold leading-tight">{name}</h3>
            {status && <IntegrationStatus tone={status.tone} label={status.label} />}
          </div>
          {description && <p className="mt-1 text-caption text-muted-foreground">{description}</p>}
        </div>
        {actions && <div className="flex shrink-0 flex-wrap items-center gap-2">{actions}</div>}
      </div>
      {children && <div className="space-y-3 border-t border-border bg-muted/10 px-5 py-4">{children}</div>}
    </Card>
  );
}

/** A titled group of integrations — code hosting, cloud, stores. */
export function IntegrationSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="space-y-3">
      <h2 className="text-caption font-semibold uppercase tracking-wide text-muted-foreground">{title}</h2>
      <div className="space-y-4">{children}</div>
    </section>
  );
}
