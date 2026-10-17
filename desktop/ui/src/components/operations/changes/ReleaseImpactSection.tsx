import { Gauge } from "lucide-react";
import { useMemo } from "react";
import { Link } from "react-router-dom";
import type { ComponentRole, Release, ReleaseImpact, ReleaseImpactCheck, ReleaseImpactCheckSignal, ReleaseImpactSignal } from "@/api";
import { ImpactVerdictPill } from "@/components/operations/changes/ImpactVerdictPill";
import {
  impactDeltaText,
  impactSignalName,
  impactState,
  impactThresholdText,
  impactValue,
  impactWindow,
  sortImpactSignals,
  stillSettling,
  uncheckedThresholds,
} from "@/components/operations/changes/releaseImpact";
import { signalChartSeries } from "@/components/operations/charts/signalChart";
import { TimeSeriesChart } from "@/components/operations/charts/TimeSeriesChart";
import { DetailSection } from "@/components/operations/console/DetailPanel";
import { entityPath, releaseLabel } from "@/components/operations/console/opsEntities";
import { viewFor } from "@/components/operations/console/roleViews";
import { signalFormatter, type SignalFormatContext } from "@/components/operations/console/signalFormat";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { useReleaseImpact } from "@/hooks/useOpsAlerts";
import { useComponentSeries } from "@/hooks/useOpsSignals";
import { intlLocale } from "@/lib/languages";
import { cn } from "@/lib/utils";

const TH = "px-2 py-1.5 text-left text-micro font-medium text-muted-foreground whitespace-nowrap";
const TD = "px-2 py-1.5 align-middle";

const DELTA_TONE: Record<string, string> = {
  worse: "text-destructive",
  improved: "text-success",
  neutral: "text-muted-foreground",
  unknown: "text-muted-foreground",
};

const THRESHOLD_NAMES = ["max_error_rate_delta", "max_p95_delta_pct"];

function thresholdName(t: SignalFormatContext["t"], name: string): string {
  return THRESHOLD_NAMES.includes(name) ? t(`operations.changes.detail.impact.thresholdNames.${name}`) : name;
}

interface ReleaseImpactSectionProps {
  release: Release;
  /** The owning entity's role, for which golden signals to chart. */
  role?: ComponentRole;
  entityId?: string;
}

/** Before and after the deploy: each golden signal's window means, the same
 * window a day earlier, the move and its verdict, and a small chart per
 * signal around the deploy. */
export function ReleaseImpactSection({ release, role, entityId }: ReleaseImpactSectionProps) {
  const { t } = useI18n();
  const k = (key: string, params?: Record<string, string | number>) => t(`operations.changes.detail.impact.${key}`, params);
  const deployed = Boolean(release.deployed_at || release.finished_at);
  const settling = release.status === "deploying" || release.status === "verifying" || release.status === "awaiting_verdict";
  const impact = useReleaseImpact(release.id, deployed, settling || Boolean(release.checks?.impact?.pending));
  const data = impact.data;

  let body;
  if (!deployed) {
    body = <p className="text-caption text-muted-foreground">{k("notDeployed")}</p>;
  } else if (impact.loading && !data) {
    body = <Skeleton className="h-32 w-full" />;
  } else if (impact.error && !data) {
    body = (
      <Notice variant="error" title={k("loadFailed")}>
        <p className="break-words text-caption">{impact.error}</p>
        <Button variant="link" size="sm" className="h-auto p-0" onClick={() => void impact.reload()}>
          {t("operations.chart.retry")}
        </Button>
      </Notice>
    );
  } else if (data) {
    body = <ImpactBody release={release} impact={data} check={release.checks?.impact} role={role} entityId={entityId ?? data.component_id ?? undefined} />;
  }

  return (
    <DetailSection title={k("title")} aside={data ? <ImpactVerdictPill state={impactState(data)} /> : undefined}>
      {body}
    </DetailSection>
  );
}

interface ImpactBodyProps {
  release: Release;
  impact: ReleaseImpact;
  check?: ReleaseImpactCheck;
  role?: ComponentRole;
  entityId?: string;
}

function ImpactBody({ release, impact, check, role, entityId }: ImpactBodyProps) {
  const { t, lang } = useI18n();
  const href = useOpsHref();
  const k = (key: string, params?: Record<string, string | number>) => t(`operations.changes.detail.impact.${key}`, params);
  const ctx = useMemo(() => ({ locale: intlLocale(lang), t }), [lang, t]);
  const golden = useMemo(() => viewFor(role).golden.map((g) => g.id), [role]);
  const signals = useMemo(() => sortImpactSignals(impact.signals ?? [], golden), [impact.signals, golden]);
  const envLabel = t(`cloud.environments.${impact.environment}`);
  const unchecked = uncheckedThresholds(check);
  const names = (list: string[]) => list.map((name) => thresholdName(t, name)).join(", ");

  return (
    <div className="space-y-3">
      {impact.pending && <Notice variant="info" title={k("pendingTitle")}>{k("pendingBody", { minutes: impact.window_minutes })}</Notice>}
      {unchecked.settling.length > 0 && (
        <p className="text-caption text-muted-foreground">{k("uncheckedSettling", { thresholds: names(unchecked.settling) })}</p>
      )}
      {unchecked.unread.length > 0 && <p className="text-caption text-muted-foreground">{k("unchecked", { thresholds: names(unchecked.unread) })}</p>}
      {signals.length === 0 ? (
        <div className="space-y-2 text-caption text-muted-foreground">
          <p>{k("noSignals", { env: envLabel })}</p>
          {entityId && (
            <Button variant="outline" size="sm" asChild>
              <Link to={href(entityPath(entityId, "signals"))}>
                <Gauge aria-hidden />
                {k("bindSignals")}
              </Link>
            </Button>
          )}
        </div>
      ) : (
        <>
          <p className="text-caption text-muted-foreground">{k("description", { minutes: impact.window_minutes })}</p>
          <div className="overflow-auto rounded-md border border-border">
            <table className="w-full border-collapse text-caption" aria-label={k("tableLabel")}>
              <thead className="bg-muted">
                <tr>
                  <th scope="col" className={TH}>{k("signal")}</th>
                  <th scope="col" className={cn(TH, "text-right")}>{k("before")}</th>
                  <th scope="col" className={cn(TH, "text-right")}>{k("after")}</th>
                  <th scope="col" className={cn(TH, "text-right")}>{k("baseline")}</th>
                  <th scope="col" className={cn(TH, "text-right")}>{k("delta")}</th>
                  <th scope="col" className={TH}>{k("verdict")}</th>
                </tr>
              </thead>
              <tbody>
                {signals.map((s) => (
                  <ImpactRow key={s.signal} signal={s} held={check?.signals?.find((c) => c.signal === s.signal)} ctx={ctx} />
                ))}
              </tbody>
            </table>
          </div>
          {impact.component_id && impact.deployed_at && (
            <ImpactCharts
              componentId={impact.component_id}
              impact={impact}
              signals={signals.map((s) => s.signal).filter((id) => golden.length === 0 || golden.includes(id))}
              marker={releaseLabel(release)}
            />
          )}
        </>
      )}
    </div>
  );
}

function ImpactRow({ signal: s, held, ctx }: { signal: ReleaseImpactSignal; held?: ReleaseImpactCheckSignal; ctx: SignalFormatContext }) {
  const { t } = useI18n();
  const threshold = held ? impactThresholdText(held, ctx) : undefined;
  return (
    <tr className="border-t border-border/60">
      <th scope="row" className={cn(TD, "text-left font-medium")}>
        {impactSignalName(s.signal, t)}
        {threshold && (
          <span className="block text-micro font-normal text-muted-foreground">
            {t("operations.changes.detail.impact.heldTo", { threshold })}
            {held?.breached && (
              <Badge variant="outline" className="ml-1.5 border-transparent bg-destructive/15 px-1.5 py-0 text-micro text-destructive">
                {t("operations.changes.detail.impact.breached")}
              </Badge>
            )}
            {stillSettling(held) && (
              <Badge variant="outline" className="ml-1.5 border-transparent bg-muted px-1.5 py-0 text-micro text-muted-foreground">
                {t("operations.changes.detail.impact.settling")}
              </Badge>
            )}
          </span>
        )}
      </th>
      <td className={cn(TD, "text-right font-mono tabular-nums")}>{impactValue(s.before, s.unit, ctx)}</td>
      <td className={cn(TD, "text-right font-mono tabular-nums")}>{impactValue(s.after, s.unit, ctx)}</td>
      <td className={cn(TD, "text-right font-mono text-muted-foreground tabular-nums")}>{impactValue(s.baseline, s.unit, ctx)}</td>
      <td className={cn(TD, "text-right font-mono tabular-nums", DELTA_TONE[s.verdict])}>{impactDeltaText(s, ctx)}</td>
      <td className={TD}>
        <ImpactVerdictPill state={s.verdict} />
      </td>
    </tr>
  );
}

function ImpactCharts({ componentId, impact, signals, marker }: { componentId: string; impact: ReleaseImpact; signals: string[]; marker: string }) {
  const { t, lang } = useI18n();
  const k = (key: string, params?: Record<string, string | number>) => t(`operations.changes.detail.impact.${key}`, params);
  const ctx = useMemo(() => ({ locale: intlLocale(lang), t }), [lang, t]);
  const span = impactWindow(impact.deployed_at, impact.window_minutes);
  const from = span?.from ?? 0;
  const to = span?.to ?? 0;
  const range = useMemo(() => ({ preset: "custom" as const, from, to }), [from, to]);
  const series = useComponentSeries(componentId, impact.environment, signals, range, { enabled: Boolean(span) && signals.length > 0 });
  if (!span || signals.length === 0) return null;
  const markers = [{ at: span.at, label: marker }];

  return (
    <div className="grid gap-3 sm:grid-cols-2" aria-label={k("chartsLabel")}>
      {signals.map((id) => {
        const answer = series.data?.signals?.find((s) => s.signal === id);
        const name = impactSignalName(id, t);
        const lines = answer ? signalChartSeries(answer, false, { signal: name, previous: (label) => label }) : [];
        return (
          <div key={id} className="min-w-0 space-y-1 rounded-md border border-border p-2">
            <p className="truncate text-caption font-medium">{name}</p>
            <TimeSeriesChart
              series={lines}
              ariaLabel={k("chartLabel", { signal: name })}
              formatValue={signalFormatter(answer?.unit, ctx)}
              deployMarkers={markers}
              xRange={[span.from, span.to]}
              height={120}
              loading={series.loading}
              emptyLabel={answer?.error?.message ?? k("noData")}
              syncKey={`impact:${impact.release_id}`}
            />
          </div>
        );
      })}
    </div>
  );
}
