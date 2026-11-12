import { useEffect, useState } from "react";
import { api, type StudyComparison, type StudyMetricChange, type StudyThemeChange } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { metricImprovesWhenUp, signedDelta, themeChangeTone } from "@/lib/audienceHub";
import { cn } from "@/lib/utils";

function deltaClass(key: string, delta: number): string {
  if (delta === 0) return "text-muted-foreground";
  return delta > 0 === metricImprovesWhenUp(key) ? "text-emerald-600 dark:text-emerald-400" : "text-amber-600 dark:text-amber-400";
}

function MetricRow({ metric, label }: { metric: StudyMetricChange; label: string }) {
  return (
    <div className="flex items-baseline justify-between gap-4 border-b border-border py-2 last:border-b-0">
      <dt className="text-sm text-muted-foreground">{label}</dt>
      <dd className="tabular-nums text-sm">
        {metric.before} → <span className="font-medium">{metric.after}</span>{" "}
        <span className={cn("ml-1 text-caption", deltaClass(metric.key, metric.delta))}>{signedDelta(metric.delta)}</span>
      </dd>
    </div>
  );
}

function ThemeList({ title, themes, worry }: { title: string; themes: StudyThemeChange[]; worry: boolean }) {
  const { t } = useI18n();
  if (themes.length === 0) return null;
  return (
    <section className="space-y-2" aria-label={title}>
      <h3 className="text-sm font-semibold">{title}</h3>
      <ul className="space-y-1.5">
        {themes.map((th) => (
          <li key={th.label} className="flex items-center justify-between gap-3 text-sm">
            <span className="min-w-0 flex-1">{th.label}</span>
            <span className="tabular-nums text-caption text-muted-foreground">
              {th.before_pct}% → {th.after_pct}%
            </span>
            <Badge variant={themeChangeTone(th.status, worry)} className="w-20 justify-center">
              {t(`audienceHub.compare.theme.${th.status}`)}
            </Badge>
          </li>
        ))}
      </ul>
    </section>
  );
}

/**
 * What changed between a study and the one it re-ran: the verdict, the answer
 * mix, the segments, the themes that went away or arrived and each person's
 * own change of mind. All of it is computed from the two reports.
 */
export function StudyComparisonView({ studyId }: { studyId: string }) {
  const { t } = useI18n();
  const c = (key: string, params?: Record<string, string | number>) => t(`audienceHub.compare.${key}`, params);
  const [cmp, setCmp] = useState<StudyComparison | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    api
      .compareAudienceStudy(studyId)
      .then((r) => !cancelled && setCmp(r))
      .catch(() => !cancelled && setCmp(null))
      .finally(() => !cancelled && setLoading(false));
    return () => {
      cancelled = true;
    };
  }, [studyId]);

  if (loading) return <Skeleton className="h-40 w-full" />;
  if (!cmp) return null;

  return (
    <section className="space-y-4" aria-labelledby="compare-heading">
      <div>
        <h2 id="compare-heading" className="text-title font-semibold tracking-tight">
          {c("heading")}
        </h2>
        <p className="text-caption text-muted-foreground">{c("subhead", { before: cmp.before.title })}</p>
      </div>
      <Card className="space-y-5 p-5">
        <div>
          <p className="text-caption text-muted-foreground">{c("verdict")}</p>
          <p className="tabular-nums text-3xl font-semibold tracking-tight">
            {cmp.verdict.before} → {cmp.verdict.after}{" "}
            <span className={cn("text-lg", deltaClass("verdict_score", cmp.verdict.delta))}>{signedDelta(cmp.verdict.delta)}</span>
          </p>
          <p className="text-caption text-muted-foreground">
            {cmp.verdict_labels[0]} → {cmp.verdict_labels[1]}
          </p>
        </div>
        {cmp.changes && (
          <blockquote className="border-l-2 border-border pl-3 text-sm">
            <p className="text-caption text-muted-foreground">{c("whatChanged")}</p>
            <p className="whitespace-pre-wrap">{cmp.changes}</p>
          </blockquote>
        )}
        {cmp.metrics.length > 0 && (
          <dl>
            {cmp.metrics.map((m) => (
              <MetricRow key={m.key} metric={m} label={c(`metric.${m.key}`)} />
            ))}
          </dl>
        )}
        {cmp.paired > 0 && (
          <div className="space-y-2">
            <h3 className="text-sm font-semibold">{c("people")}</h3>
            <p className="text-sm text-muted-foreground">
              {c("peopleSummary", { paired: cmp.paired, improved: cmp.improved, worse: cmp.worse, same: cmp.same })}
            </p>
            <ul className="space-y-2">
              {cmp.people.slice(0, 6).map((p) => (
                <li key={p.persona_id} className="rounded-lg border border-border p-3 text-sm">
                  <div className="flex items-baseline justify-between gap-3">
                    <span className="font-medium">
                      {p.name}
                      {p.segment && <span className="ml-2 text-caption font-normal text-muted-foreground">{p.segment}</span>}
                    </span>
                    <span className="tabular-nums">
                      {p.before_intent} → {p.after_intent}{" "}
                      <span className={cn("text-caption", deltaClass("intent", p.after_intent - p.before_intent))}>
                        {signedDelta(p.after_intent - p.before_intent)}
                      </span>
                    </span>
                  </div>
                  {p.after_quote && <p className="mt-1 text-muted-foreground">“{p.after_quote}”</p>}
                </li>
              ))}
            </ul>
          </div>
        )}
        {cmp.segments.length > 0 && (
          <div className="space-y-2">
            <h3 className="text-sm font-semibold">{c("segments")}</h3>
            <ul className="space-y-1.5">
              {cmp.segments.map((s) => (
                <li key={s.name} className="flex items-baseline justify-between gap-3 text-sm">
                  <span>{s.name}</span>
                  <span className="tabular-nums text-muted-foreground">
                    {c("intent")} {s.before_intent} → {s.after_intent} · {c("yes")} {s.before_yes_pct}% → {s.after_yes_pct}%
                  </span>
                </li>
              ))}
            </ul>
          </div>
        )}
        <ThemeList title={c("objections")} themes={cmp.objections} worry />
        <ThemeList title={c("needs")} themes={cmp.needs} worry />
        <ThemeList title={c("alternatives")} themes={cmp.alternatives} worry />
        <ThemeList title={c("strengths")} themes={cmp.strengths} worry={false} />
        <p className="text-caption text-muted-foreground">{c("disclaimer")}</p>
      </Card>
    </section>
  );
}
