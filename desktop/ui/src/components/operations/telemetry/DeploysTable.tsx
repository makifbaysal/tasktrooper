import { ExternalLink } from "lucide-react";
import { TelemetryCell, TelemetryRow, TelemetryTable } from "@/components/operations/telemetry/TelemetryTable";
import { Badge } from "@/components/ui/badge";
import { useI18n } from "@/hooks/useI18n";
import { intlLocale } from "@/lib/languages";
import type { TelemetryDeploy } from "@/lib/telemetryHub";

interface DeploysTableProps {
  deploys: TelemetryDeploy[];
  showService?: boolean;
}

function safeHttpUrl(raw: string | undefined): string | undefined {
  if (!raw) return undefined;
  try {
    const u = new URL(raw);
    return u.protocol === "https:" || u.protocol === "http:" ? u.toString() : undefined;
  } catch {
    return undefined;
  }
}

/** Deploy markers, newest first: what shipped, where, from which source. */
export function DeploysTable({ deploys, showService = true }: DeploysTableProps) {
  const { t, lang } = useI18n();
  const h = (key: string) => t(`telemetryHub.deploys.${key}`);
  const time = new Intl.DateTimeFormat(intlLocale(lang), { dateStyle: "medium", timeStyle: "short" });
  const rows = [...deploys].sort((a, b) => b.ts - a.ts);
  return (
    <TelemetryTable
      label={h("listLabel")}
      columns={[
        { key: "time", label: h("columns.time") },
        ...(showService ? [{ key: "service", label: h("columns.service") }] : []),
        { key: "version", label: h("columns.version") },
        { key: "env", label: h("columns.environment") },
        { key: "source", label: h("columns.source") },
        { key: "detail", label: h("columns.detail") },
      ]}
    >
      {rows.map((d) => {
        const url = safeHttpUrl(d.url);
        return (
          <TelemetryRow key={`${d.service}:${d.env}:${d.version}:${d.ts}`}>
            <TelemetryCell className="whitespace-nowrap text-muted-foreground">{time.format(d.ts)}</TelemetryCell>
            {showService && <TelemetryCell className="font-medium">{d.service}</TelemetryCell>}
            <TelemetryCell className="font-mono">{d.version}</TelemetryCell>
            <TelemetryCell>{d.env}</TelemetryCell>
            <TelemetryCell>
              <Badge variant="secondary">{d.source}</Badge>
            </TelemetryCell>
            <TelemetryCell className="max-w-72 truncate text-muted-foreground">
              {[d.commitMessage, d.author].filter(Boolean).join(" · ")}
              {url && (
                <a href={url} target="_blank" rel="noreferrer noopener" className="ml-2 inline-flex items-center gap-1 text-info hover:underline">
                  <ExternalLink className="h-3 w-3" aria-hidden />
                  <span className="sr-only">{h("open")}</span>
                </a>
              )}
            </TelemetryCell>
          </TelemetryRow>
        );
      })}
    </TelemetryTable>
  );
}
