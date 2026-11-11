import { Link } from "react-router-dom";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import { DetailFacts, DetailPanel, DetailSection } from "@/components/operations/console/DetailPanel";
import { ServicePicker } from "@/components/operations/telemetry/ServicePicker";
import { StateBadge } from "@/components/operations/telemetry/TelemetryBadges";
import { TelemetryQueryView } from "@/components/operations/telemetry/TelemetryQueryView";
import { TelemetryCell, TelemetryRow, TelemetryTable } from "@/components/operations/telemetry/TelemetryTable";
import { TelemetryToolbar } from "@/components/operations/telemetry/TelemetryToolbar";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { useTelemetryQuery } from "@/hooks/useTelemetryQuery";
import { useTelemetryScope } from "@/hooks/useTelemetryScope";
import { useUrlParam } from "@/hooks/useUrlParam";
import { intlLocale } from "@/lib/languages";
import { formatCount, ERROR_STATUSES, type TelemetryErrorDetail, type TelemetryErrorIssue } from "@/lib/telemetryHub";
import { cn } from "@/lib/utils";

const ALL = "all";

function ErrorDetailPanel({ fingerprint, onClose, range, env }: { fingerprint: string; onClose: () => void; range: string; env: string | undefined }) {
  const { t, lang } = useI18n();
  const h = (key: string, params?: Record<string, string | number>) => t(`telemetryHub.errors.${key}`, params);
  const href = useOpsHref();
  const time = new Intl.DateTimeFormat(intlLocale(lang), { dateStyle: "medium", timeStyle: "medium" });
  const read = useTelemetryQuery<TelemetryErrorDetail>(`errors/${encodeURIComponent(fingerprint)}`, { range, env }, { intervalMs: 0 });
  const e = read.data;
  return (
    <DetailPanel
      open
      onOpenChange={(open) => !open && onClose()}
      title={e ? `${e.type}: ${e.message}` : h("detail")}
      badges={e ? <StateBadge state={e.status} /> : undefined}
    >
      {!e ? (
        read.error ? (
          <p className="text-caption text-destructive">{read.error}</p>
        ) : (
          <Skeleton className="h-40 w-full" />
        )
      ) : (
        <>
          <DetailFacts
            items={[
              { label: h("columns.service"), value: e.service },
              { label: h("columns.route"), value: e.route || "-" },
              { label: h("columns.count"), value: e.count },
              { label: h("firstSeen"), value: time.format(e.firstSeen) },
              { label: h("lastSeen"), value: time.format(e.lastSeen) },
              { label: h("release"), value: e.release || "-" },
              { label: h("platform"), value: e.platform || "-" },
            ]}
          />
          {e.sampleTraceId && (
            <Link to={href(`/operations/telemetry/traces/${encodeURIComponent(e.sampleTraceId)}`)} className="text-caption font-medium text-info hover:underline">
              {h("openTrace")}
            </Link>
          )}
          {(e.frames?.length ?? 0) > 0 ? (
            <DetailSection title={h("stack")} aside={e.symbolicated ? h("symbolicated") : undefined}>
              <ol className="space-y-1 font-mono text-micro">
                {e.frames!.map((f, i) => (
                  <li key={i} className={cn("break-all", !f.inApp && "text-muted-foreground")}>
                    <span className="font-medium">{f.fn || h("anonymous")}</span>
                    <span className="block pl-3">
                      {f.file}:{f.line}
                      {f.col ? `:${f.col}` : ""}
                    </span>
                  </li>
                ))}
              </ol>
            </DetailSection>
          ) : e.stack ? (
            <DetailSection title={h("stack")}>
              <pre className="overflow-x-auto rounded-md bg-muted/40 p-3 font-mono text-micro whitespace-pre-wrap break-all">{e.stack}</pre>
            </DetailSection>
          ) : null}
        </>
      )}
    </DetailPanel>
  );
}

/** `/operations/telemetry/errors`: error issues grouped by fingerprint. */
export default function TelemetryErrorsPage() {
  const { t, lang } = useI18n();
  const h = (key: string, params?: Record<string, string | number>) => t(`telemetryHub.errors.${key}`, params);
  const locale = intlLocale(lang);
  const scope = useTelemetryScope();
  const [service, setService] = useUrlParam("service");
  const [status, setStatus] = useUrlParam("status");
  const [selected, setSelected] = useUrlParam("fp");
  const time = new Intl.DateTimeFormat(locale, { month: "short", day: "2-digit", hour: "2-digit", minute: "2-digit", hourCycle: "h23" });
  const read = useTelemetryQuery<TelemetryErrorIssue[]>("errors", { range: scope.range, env: scope.env, service: service || undefined, status: status || undefined, limit: 200 });

  return (
    <div className="space-y-4">
      <TelemetryToolbar>
        <ServicePicker value={service} onChange={setService} label={h("service")} />
        <Select value={status || ALL} onValueChange={(v) => setStatus(v === ALL ? null : v)}>
          <SelectTrigger className="h-8 w-44" aria-label={h("statusFilter")}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{h("allStatuses")}</SelectItem>
            {ERROR_STATUSES.map((x) => (
              <SelectItem key={x} value={x}>
                {t(`telemetryHub.states.${x}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </TelemetryToolbar>
      <TelemetryQueryView query={read} isEmpty={(d) => d.length === 0} emptyTitle={h("empty")} emptyDescription={h("emptyHint")}>
        {(rows) => (
          <TelemetryTable
            label={h("listLabel")}
            columns={[
              { key: "error", label: h("columns.error") },
              { key: "service", label: h("columns.service") },
              { key: "count", label: h("columns.count"), numeric: true },
              { key: "last", label: h("columns.lastSeen") },
              { key: "status", label: h("columns.status") },
            ]}
          >
            {[...rows]
              .sort((a, b) => b.lastSeen - a.lastSeen)
              .map((e) => (
                <TelemetryRow key={e.fingerprint} onOpen={() => setSelected(e.fingerprint)} selected={selected === e.fingerprint}>
                  <TelemetryCell className="max-w-md">
                    <button type="button" className="block max-w-full text-left hover:underline" onClick={() => setSelected(e.fingerprint)}>
                      <span className="block truncate font-medium">{e.type || h("unknownType")}</span>
                      <span className="block truncate text-micro text-muted-foreground">{e.message}</span>
                    </button>
                  </TelemetryCell>
                  <TelemetryCell>
                    {e.service}
                    {e.route && <span className="block font-mono text-micro text-muted-foreground">{e.route}</span>}
                  </TelemetryCell>
                  <TelemetryCell numeric>{formatCount(e.count, locale)}</TelemetryCell>
                  <TelemetryCell className="whitespace-nowrap text-muted-foreground">{time.format(e.lastSeen)}</TelemetryCell>
                  <TelemetryCell>
                    <StateBadge state={e.status} />
                  </TelemetryCell>
                </TelemetryRow>
              ))}
          </TelemetryTable>
        )}
      </TelemetryQueryView>
      {selected && <ErrorDetailPanel fingerprint={selected} onClose={() => setSelected(null)} range={scope.range} env={scope.env} />}
    </div>
  );
}
