import { ArrowRight, PencilLine, Trash2 } from "lucide-react";
import { useMemo, useState, type ReactNode } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import { api, ApiError, type AlertRule } from "@/api";
import { conditionText, firingBands, formatThreshold, ruleThresholds, ruleTimeline } from "@/components/operations/alertRules/alertRules";
import { AlertStatePill } from "@/components/operations/alertRules/AlertStatePill";
import { signalChartSeries } from "@/components/operations/charts/signalChart";
import { TimeSeriesChart, type ChartSeries } from "@/components/operations/charts/TimeSeriesChart";
import { DetailFacts, DetailPanel, DetailSection, type DetailFact } from "@/components/operations/console/DetailPanel";
import { entityPath } from "@/components/operations/console/opsEntities";
import { signalFormatter, signalLabel } from "@/components/operations/console/signalFormat";
import { IssueSeverity } from "@/components/operations/issues/IssueSeverity";
import { useOpsContext, useOpsHref } from "@/components/operations/shell/OpsContext";
import type { OpsEntity } from "@/components/operations/shell/useOpsEntities";
import { toChartSeries } from "@/components/operations/sources/dataSourceSchema";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { useAlertRuleEvents, useRuleQuerySeries } from "@/hooks/useOpsAlerts";
import { useComponentSeries } from "@/hooks/useOpsSignals";
import { intlLocale } from "@/lib/languages";
import { formatDate, formatRelativeTime } from "@/lib/utils";

interface AlertRuleDetailPanelProps {
  ruleId: string | null;
  rule?: AlertRule;
  /** The list is still being read: an id it does not hold yet is not "gone". */
  loading: boolean;
  entityOf: (rule: AlertRule) => OpsEntity | undefined;
  unitOf: (rule: AlertRule) => string | undefined;
  onOpenChange: (open: boolean) => void;
  onEdit: (rule: AlertRule) => void;
  onDeleted: (id: string) => void;
}

/** One rule in full: its condition, the signal over the window with the
 * threshold and the spans it fired, and every state change it went through. */
export function AlertRuleDetailPanel({ ruleId, rule, loading, entityOf, unitOf, onOpenChange, onEdit, onDeleted }: AlertRuleDetailPanelProps) {
  const { t } = useI18n();
  const d = (key: string, params?: Record<string, string | number>) => t(`operations.alertRules.detail.${key}`, params);
  const entity = rule ? entityOf(rule) : undefined;

  let title: ReactNode = d("loading");
  if (rule) title = rule.name;
  else if (!loading) title = d("notFound");

  return (
    <DetailPanel
      open={ruleId !== null}
      onOpenChange={onOpenChange}
      title={title}
      description={
        rule ? (
          <>
            {entity?.name ?? rule.component_id}
            <span aria-hidden> · </span>
            {t(`cloud.environments.${rule.environment}`)}
          </>
        ) : undefined
      }
      badges={
        rule ? (
          <>
            <AlertStatePill state={rule.state} disabled={!rule.enabled} />
            <Badge variant="outline" className="px-2 py-0">
              <IssueSeverity severity={rule.severity} className="text-micro" />
            </Badge>
          </>
        ) : undefined
      }
      actions={rule ? <RuleActions key={rule.id} rule={rule} onEdit={onEdit} onDeleted={onDeleted} /> : undefined}
    >
      {!rule && loading && (
        <div className="space-y-3" aria-busy>
          <Skeleton className="h-24 w-full" />
          <Skeleton className="h-40 w-full" />
        </div>
      )}
      {!rule && !loading && <Notice variant="info" title={d("notFound")}>{d("notFoundBody")}</Notice>}
      {rule && <RuleBody rule={rule} entity={entity} unit={unitOf(rule)} />}
    </DetailPanel>
  );
}

function RuleActions({ rule, onEdit, onDeleted }: { rule: AlertRule; onEdit: (rule: AlertRule) => void; onDeleted: (id: string) => void }) {
  const { t } = useI18n();
  const a = (key: string, params?: Record<string, string | number>) => t(`operations.alertRules.detail.${key}`, params);
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);
  const [refusal, setRefusal] = useState<string | null>(null);

  const remove = async (): Promise<boolean> => {
    setBusy(true);
    try {
      await api.deleteAlertRule(rule.id);
      toast.success(a("deleted", { name: rule.name }));
      onDeleted(rule.id);
      return true;
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) {
        onDeleted(rule.id);
        return true;
      }
      setRefusal(err instanceof Error ? err.message : t("common.actionFailed"));
      return false;
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <Button size="sm" variant="outline" onClick={() => onEdit(rule)}>
        <PencilLine aria-hidden />
        {a("edit")}
      </Button>
      <Button
        size="sm"
        variant="outline"
        className="border-destructive/40 text-destructive hover:text-destructive"
        onClick={() => {
          setRefusal(null);
          setConfirming(true);
        }}
      >
        <Trash2 aria-hidden />
        {a("delete")}
      </Button>
      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title={a("deleteTitle", { name: rule.name })}
        description={a("deleteDescription")}
        confirmLabel={a("delete")}
        variant="destructive"
        loading={busy}
        error={refusal}
        errorTitle={a("deleteFailed")}
        onConfirm={remove}
      />
    </>
  );
}

function RuleBody({ rule, entity, unit }: { rule: AlertRule; entity?: OpsEntity; unit?: string }) {
  const { t, lang } = useI18n();
  const href = useOpsHref();
  const { range } = useOpsContext();
  const d = (key: string, params?: Record<string, string | number>) => t(`operations.alertRules.detail.${key}`, params);
  const ctx = useMemo(() => ({ locale: intlLocale(lang), t }), [lang, t]);
  const custom = !rule.signal;

  const series = useComponentSeries(rule.component_id, rule.environment, custom ? [] : [rule.signal], range, { enabled: !custom });
  const query = useRuleQuerySeries(rule.data_source_id ?? undefined, rule.query, range, custom);
  const events = useAlertRuleEvents(rule.id);
  const signalAnswer = series.data?.signals?.find((s) => s.signal === rule.signal);
  const shownUnit = signalAnswer?.unit || unit;
  const format = useMemo(() => signalFormatter(shownUnit, ctx), [shownUnit, ctx]);
  const signalName = custom ? t("operations.alertRules.customQuery") : signalLabel(t, rule.signal);

  const lines: ChartSeries[] = useMemo(() => {
    if (custom) return toChartSeries(query.data?.series ?? []);
    if (!signalAnswer) return [];
    return signalChartSeries(signalAnswer, false, { signal: signalName, previous: (label) => label });
  }, [custom, query.data, signalAnswer, signalName]);
  const timeline = useMemo(() => ruleTimeline(events.data ?? []), [events.data]);
  const bands = useMemo(() => firingBands(events.data ?? [], range.to), [events.data, range.to]);
  const thresholds = ruleThresholds(rule, d("threshold"));
  const chartError = custom ? query.error : (signalAnswer?.error?.message ?? series.error);
  const chartLoading = custom ? query.loading : series.loading;

  const when = (iso?: string | null) =>
    iso ? (
      <span title={formatDate(iso)}>
        <span className="font-mono tabular-nums">{formatDate(iso)}</span>
        <span className="text-muted-foreground"> · {formatRelativeTime(iso, lang)}</span>
      </span>
    ) : (
      <span className="text-muted-foreground">—</span>
    );

  const facts: DetailFact[] = [
    { label: d("condition"), value: <span className="font-medium">{conditionText(rule, shownUnit, ctx)}</span> },
    {
      label: d("entity"),
      value: entity ? (
        <Link to={href(entityPath(entity.id, "alerts"))} className="font-medium text-info hover:underline">
          {entity.name}
        </Link>
      ) : (
        <span className="font-mono">{rule.component_id}</span>
      ),
    },
    custom
      ? { label: d("query"), value: <code className="font-mono text-micro break-all">{rule.query}</code> }
      : { label: d("signal"), value: <span>{signalName} <span className="font-mono text-micro text-muted-foreground">{rule.signal}</span></span> },
    { label: d("stateSince"), value: when(rule.state_since) },
    {
      label: d("lastValue"),
      value: rule.last_value == null ? <span className="text-muted-foreground">—</span> : <span className="font-mono tabular-nums">{formatThreshold(rule.condition.op, rule.last_value, shownUnit, ctx)}</span>,
    },
    { label: d("lastEvaluated"), value: rule.last_evaluated_at ? when(rule.last_evaluated_at) : <span className="text-muted-foreground">{t("operations.alertRules.neverEvaluated")}</span> },
    {
      label: d("channels"),
      value:
        (rule.channels ?? []).length === 0 ? (
          <span className="text-muted-foreground">{d("noChannels")}</span>
        ) : (
          rule.channels.map((c) => t(`operations.alertRules.editor.channelKinds.${c}`)).join(", ")
        ),
    },
    { label: d("openIncident"), value: rule.open_incident ? d("yes") : d("no") },
  ];

  return (
    <>
      <DetailSection title={d("facts")}>
        <DetailFacts items={facts} />
      </DetailSection>

      <DetailSection title={d("chart")}>
        {chartError && !lines.length ? (
          <Notice variant="error" title={d("chartFailed")}>
            <p className="break-words text-caption">{chartError}</p>
          </Notice>
        ) : (
          <TimeSeriesChart
            series={lines}
            ariaLabel={d("chartLabel", { signal: signalName })}
            formatValue={format}
            thresholds={thresholds}
            bands={bands}
            height={200}
            loading={chartLoading}
            xRange={[range.from, range.to]}
            emptyLabel={d("noData")}
          />
        )}
        {bands.length > 0 && <p className="text-micro text-muted-foreground">{d("bandsHint")}</p>}
      </DetailSection>

      <DetailSection title={d("timeline")}>
        {events.loading && timeline.length === 0 ? (
          <Skeleton className="h-16 w-full" />
        ) : timeline.length === 0 ? (
          <p className="text-caption text-muted-foreground">{d("noEvents")}</p>
        ) : (
          <ol className="relative space-y-2 border-l border-border pl-4" aria-label={d("timeline")}>
            {timeline.map((ev, i) => (
              <li key={`${ev.at}:${i}`} className="relative text-caption">
                <span aria-hidden className="absolute top-1.5 -left-[21px] h-2 w-2 rounded-full bg-muted-foreground" />
                <span className="inline-flex flex-wrap items-center gap-1.5">
                  <AlertStatePill state={ev.from_state} />
                  <ArrowRight className="h-3 w-3 text-muted-foreground" aria-label={d("to")} />
                  <AlertStatePill state={ev.to_state} />
                  <span className="font-mono text-muted-foreground tabular-nums" title={formatDate(ev.at)}>
                    {formatDate(ev.at)}
                  </span>
                </span>
                {ev.value != null && (
                  <span className="ml-2 font-mono tabular-nums">{d("value", { value: formatThreshold(rule.condition.op, ev.value, shownUnit, ctx) })}</span>
                )}
                {ev.incident_id && (
                  <Link to={`/operations/issues?id=${encodeURIComponent(ev.incident_id)}`} className="ml-2 text-info hover:underline">
                    {d("openIssue")}
                  </Link>
                )}
              </li>
            ))}
          </ol>
        )}
      </DetailSection>
    </>
  );
}
