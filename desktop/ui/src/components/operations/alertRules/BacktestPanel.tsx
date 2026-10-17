import { FlaskConical } from "lucide-react";
import { useMemo, useRef, useState } from "react";
import { api, ApiError, dataSourceFieldErrors, type AlertBacktest, type AlertRuleOp } from "@/api";
import {
  BACKTEST_CHANGE_SERIES,
  BACKTEST_DEFAULT_DAYS,
  BACKTEST_MAX_DAYS,
  BACKTEST_MIN_DAYS,
  BACKTEST_QUERY_SERIES,
  backtestKey,
  backtestRequest,
  fireBands,
  formatThreshold,
  judgedThresholds,
  opIsChange,
  validBacktestDays,
  type AlertRuleDraft,
} from "@/components/operations/alertRules/alertRules";
import { TimeSeriesChart, type ChartSeries } from "@/components/operations/charts/TimeSeriesChart";
import { signalFormatter, signalLabel } from "@/components/operations/console/signalFormat";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { useI18n } from "@/hooks/useI18n";
import { intlLocale } from "@/lib/languages";
import { formatDate, formatDuration } from "@/lib/utils";

interface BacktestPanelProps {
  draft: AlertRuleDraft;
  unit?: string;
  fallbackName: string;
  /** False while the draft has errors: a backtest of an invalid rule says nothing. */
  ready: boolean;
}

interface Run {
  answer: AlertBacktest;
  body: string;
  from: number;
  to: number;
  op: AlertRuleOp;
  threshold: number | null;
}

const DAY_MS = 86_400_000;

/** What the rule would have done over the last days, before it is saved:
 * the series with its threshold, the fires shaded and listed. */
export function BacktestPanel({ draft, unit, fallbackName, ready }: BacktestPanelProps) {
  const { t, lang } = useI18n();
  const b = (key: string, params?: Record<string, string | number>) => t(`operations.alertRules.backtest.${key}`, params);
  const ctx = useMemo(() => ({ locale: intlLocale(lang), t }), [lang, t]);
  const [daysText, setDaysText] = useState(String(BACKTEST_DEFAULT_DAYS));
  const [running, setRunning] = useState(false);
  const [run, setRun] = useState<Run | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const seq = useRef(0);

  const days = Number(daysText);
  const daysValid = validBacktestDays(days);
  const body = backtestRequest(draft, daysValid ? days : BACKTEST_DEFAULT_DAYS, fallbackName, ctx.locale);
  const bodyKey = backtestKey(draft, ctx.locale);
  const stale = run !== null && run.body !== bodyKey;

  const start = async () => {
    if (!ready || !daysValid) return;
    const mine = ++seq.current;
    const to = Date.now();
    setRunning(true);
    setFailure(null);
    try {
      const answer = await api.backtestAlertRule(body);
      if (mine !== seq.current) return;
      setRun({ answer, body: bodyKey, from: to - body.days * DAY_MS, to, op: body.condition.op, threshold: body.condition.threshold });
    } catch (err) {
      if (mine !== seq.current) return;
      const fields = dataSourceFieldErrors(err);
      if (fields) setFailure(Object.entries(fields).map(([k, v]) => `${k}: ${errorText(v)}`).join(" "));
      else if (err instanceof ApiError && err.type === "unbound") setFailure(b("unbound"));
      else if (err instanceof ApiError && err.type === "unsupported") setFailure(b("unsupported"));
      else setFailure(err instanceof Error ? err.message : t("common.actionFailed"));
    } finally {
      if (mine === seq.current) setRunning(false);
    }
  };

  const errorText = (code: string) => {
    const key = `operations.alertRules.errors.${code}`;
    const text = t(key);
    return text === key ? code : text;
  };

  // A change rule is judged on its change series, in percent; any other on
  // the values themselves.
  const changeRun = run ? opIsChange(run.op) : false;
  const format = useMemo(() => signalFormatter(changeRun ? "%" : unit, ctx), [changeRun, unit, ctx]);
  const series: ChartSeries[] = useMemo(() => {
    const labelOf = (name: string) =>
      name === BACKTEST_CHANGE_SERIES
        ? t("operations.alertRules.backtest.changeSeries")
        : name === BACKTEST_QUERY_SERIES || !name
          ? t("operations.alertRules.customQuery")
          : signalLabel(t, name);
    return (run?.answer.series ?? [])
      .filter((s) => (changeRun ? s.name === BACKTEST_CHANGE_SERIES : s.name !== BACKTEST_CHANGE_SERIES))
      .map((s, i) => ({ id: `${i}:${s.name}`, label: labelOf(s.name), points: s.points ?? [], colorIndex: (i % 8) + 1 }));
  }, [run, changeRun, t]);
  const bands = useMemo(() => (run ? fireBands(run.answer.fires ?? [], run.to) : []), [run]);
  const spanDays = run ? Math.round((run.to - run.from) / DAY_MS) : 0;
  const thresholds = run ? judgedThresholds({ condition: { op: run.op, threshold: run.threshold, for_seconds: 0, window_seconds: 0 } }, b("threshold")) : [];

  return (
    <section className="space-y-3 rounded-md border border-border p-3" aria-labelledby="alert-backtest-title">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="min-w-0">
          <h3 id="alert-backtest-title" className="text-body font-semibold">
            {b("title")}
          </h3>
          <p className="text-caption text-muted-foreground">{b("description")}</p>
        </div>
        <div className="flex items-end gap-2">
          <div className="space-y-1">
            <Label htmlFor="alert-backtest-days" className="text-caption">
              {b("days")}
            </Label>
            <Input
              id="alert-backtest-days"
              type="number"
              min={BACKTEST_MIN_DAYS}
              max={BACKTEST_MAX_DAYS}
              value={daysText}
              onChange={(e) => setDaysText(e.target.value)}
              aria-invalid={daysValid ? undefined : true}
              className="h-8 w-20 font-mono tabular-nums"
            />
          </div>
          <Button variant="outline" size="sm" onClick={() => void start()} disabled={running || !ready || !daysValid}>
            <FlaskConical aria-hidden />
            {running ? b("running") : b("run")}
          </Button>
        </div>
      </div>
      {!daysValid && <p className="text-caption text-destructive">{b("daysRange", { min: BACKTEST_MIN_DAYS, max: BACKTEST_MAX_DAYS })}</p>}
      {!ready && <p className="text-caption text-muted-foreground">{b("fixFirst")}</p>}
      {failure && (
        <Notice variant="error" title={b("failed")}>
          <p className="break-words font-mono text-caption">{failure}</p>
        </Notice>
      )}
      {stale && <Notice variant="info" title={b("stale")} />}
      {(run || running) && (
        <>
          <TimeSeriesChart
            series={series}
            ariaLabel={b("chartLabel")}
            formatValue={format}
            thresholds={thresholds}
            bands={bands}
            height={180}
            loading={running && !run}
            xRange={run ? [run.from, run.to] : undefined}
            emptyLabel={b("noSeries")}
          />
          {run && (
            <div className="space-y-1">
              <p className="text-caption font-medium" role="status">
                {bands.length === 0 ? b("noFires", { days: spanDays }) : b("fires", { count: bands.length, days: spanDays })}
              </p>
              {bands.length > 0 && (
                <ul className="max-h-40 divide-y divide-border overflow-auto rounded-md border border-border text-caption" aria-label={b("firesLabel")}>
                  {bands.map((band) => (
                    <li key={band.from} className="flex flex-wrap items-center gap-x-3 gap-y-0.5 px-3 py-1.5">
                      <span className="font-mono tabular-nums">{formatDate(new Date(band.from).toISOString())}</span>
                      <span className="text-muted-foreground">
                        {band.open ? b("stillFiring") : formatDuration(band.to - band.from, lang)}
                      </span>
                      {band.peak != null && (
                        <span className="ml-auto font-mono tabular-nums">{b("peak", { value: formatThreshold(run.op, band.peak, unit, ctx) })}</span>
                      )}
                    </li>
                  ))}
                </ul>
              )}
            </div>
          )}
        </>
      )}
    </section>
  );
}
