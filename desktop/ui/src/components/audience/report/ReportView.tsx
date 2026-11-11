import { Quote } from "lucide-react";
import type { ReactNode } from "react";
import type { AudienceReport } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { Notice } from "@/components/ui/notice";
import { StatTile } from "@/components/ui/stat-tile";
import { useI18n } from "@/hooks/useI18n";
import { formatMoney, formatPct, verdictTone } from "@/lib/audienceHub";
import { cn } from "@/lib/utils";
import { IndexTrack, IntentChart, PriceHistogram, PriceTests, ThemeBars } from "./Charts";

function Section({ title, description, children, className }: { title: string; description?: string; children: ReactNode; className?: string }) {
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

const SENTIMENT_BORDER = {
  positive: "border-l-success",
  negative: "border-l-destructive",
  neutral: "border-l-border",
} as const;

const PRIORITY_VARIANT = { high: "destructive", medium: "warning", low: "secondary" } as const;

/**
 * The report of a finished study, laid out as a research report: the verdict
 * and what it rests on, the willingness to pay, how segments differ, what
 * people objected to and miss, what they said in their own words, and what to
 * do about it. Every number was computed from the answers; the narrative was
 * written over them. The people are synthetic and the page says so.
 */
export function ReportView({ report, title, audience }: { report: AudienceReport; title: string; audience?: string }) {
  const { t, lang } = useI18n();
  const r = (key: string, params?: Record<string, string | number>) => t(`audienceHub.report.${key}`, params);
  const m = report.metrics;
  const price = m.price;
  const tone = verdictTone(report.verdict.label);
  const people = (n: number) => r("people", { count: n });
  const peopleOf = (count: number, total: number) => r("peopleOf", { count, total });
  const money = (v: number) => formatMoney(v, price.currency, lang);
  const generated = new Date(report.generated_at);

  return (
    <div className="space-y-5">
      {report.synthesis_failed && (
        <Notice variant="warning" title={r("synthesisFailedTitle")}>
          {r("synthesisFailedBody")}
        </Notice>
      )}

      <Card className="space-y-5 p-6">
        <div className="flex flex-col gap-1">
          <p className="text-caption uppercase tracking-wide text-muted-foreground">{r("eyebrow")}</p>
          <h2 className="text-display font-semibold tracking-tight">{title}</h2>
          <p className="text-caption text-muted-foreground">
            {audience ? `${audience} · ` : ""}
            {generated.toLocaleDateString(lang, { year: "numeric", month: "long", day: "numeric" })}
          </p>
        </div>

        <div className="grid gap-6 md:grid-cols-[minmax(0,260px)_1fr]">
          <div>
            <p className="text-caption text-muted-foreground">{r("intentIndex")}</p>
            <div className="mt-1 flex items-baseline gap-2">
              <span className="text-[44px] font-semibold leading-none tabular-nums">{Math.round(report.verdict.score)}</span>
              <span className="text-body text-muted-foreground">/ 100</span>
            </div>
            <div className="mt-2">
              <Badge variant={tone}>{r(`verdict.${report.verdict.label}`)}</Badge>
            </div>
            <IndexTrack score={report.verdict.score} tone={tone === "secondary" ? "info" : tone} />
            <p className="mt-1 text-caption text-muted-foreground">{r("indexScale")}</p>
          </div>
          <div className="space-y-3">
            <h3 className="text-body font-semibold">{r("executiveSummary")}</h3>
            <p className="whitespace-pre-line text-body leading-relaxed">{report.summary}</p>
            {report.verdict.rationale && <p className="text-caption text-muted-foreground">{report.verdict.rationale}</p>}
          </div>
        </div>

        <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
          <StatTile label={r("respondents")} value={m.respondents} foot={m.failed > 0 ? r("couldNotAnswer", { count: m.failed }) : undefined} />
          <StatTile label={r("meanIntent")} value={`${m.intent_mean.toFixed(1)} / 10`} />
          <StatTile
            label={r("yesMaybeNo")}
            value={`${m.yes} · ${m.maybe} · ${m.no}`}
            foot={r("yesMaybeNoFoot", { yes: m.yes, maybe: m.maybe, no: m.no })}
          />
          <StatTile label={r("wouldPayNow")} value={formatPct(m.would_pay_now_pct)} />
        </div>
      </Card>

      <Section title={r("intentTitle")} description={r("intentHelp")}>
        <IntentChart distribution={m.intent_distribution} label={r("intentTitle")} peopleLabel={people} />
      </Section>

      <Section title={r("payTitle")} description={r("payHelp", { currency: price.currency })}>
        {price.answered === 0 ? (
          <p className="text-body text-muted-foreground">{price.not_willing > 0 ? r("payNobody") : r("payNoData")}</p>
        ) : (
          <>
            <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
              <StatTile label={r("median")} value={money(price.median)} />
              <StatTile label={r("mean")} value={money(price.mean)} />
              <StatTile label={r("range")} value={`${money(price.min)} - ${money(price.max)}`} foot={r("quartiles", { low: money(price.p25), high: money(price.p75) })} />
              <StatTile label={r("payNothing")} value={price.not_willing} foot={r("payNothingFoot")} />
            </div>
            <div>
              <h3 className="mb-2 text-body font-semibold">{r("histogram")}</h3>
              <PriceHistogram buckets={price.histogram} currency={price.currency} lang={lang} label={r("histogram")} peopleLabel={people} />
            </div>
          </>
        )}
        {price.tests && price.tests.length > 0 && (
          <div>
            <h3 className="mb-2 text-body font-semibold">{r("priceTests")}</h3>
            <PriceTests
              tests={price.tests}
              currency={price.currency}
              lang={lang}
              label={(test) => (test.asked === 0 ? r("notAsked") : r("buyAt", { pct: Math.round(test.buy_pct), asked: test.asked }))}
            />
          </div>
        )}
      </Section>

      {report.segments.length > 0 && (
        <Section title={r("segmentsTitle")}>
          <div className="overflow-x-auto">
            <table className="w-full text-left text-body">
              <thead>
                <tr className="border-b border-border text-caption text-muted-foreground">
                  <th className="py-2 pr-4 font-medium">{r("segment")}</th>
                  <th className="py-2 pr-4 font-medium">{r("respondents")}</th>
                  <th className="py-2 pr-4 font-medium">{r("meanIntent")}</th>
                  <th className="py-2 pr-4 font-medium">{r("yesPct")}</th>
                  <th className="py-2 font-medium">{r("medianPrice")}</th>
                </tr>
              </thead>
              <tbody>
                {report.segments.map((s) => (
                  <tr key={s.name} className="border-b border-border/60 align-top last:border-0">
                    <td className="py-2 pr-4">
                      <p className="font-medium">{s.name}</p>
                      {s.insight && <p className="mt-0.5 max-w-prose text-caption text-muted-foreground">{s.insight}</p>}
                    </td>
                    <td className="py-2 pr-4 tabular-nums">{s.respondents}</td>
                    <td className="py-2 pr-4 tabular-nums">{s.intent_mean.toFixed(1)}</td>
                    <td className="py-2 pr-4 tabular-nums">{formatPct(s.yes_pct)}</td>
                    <td className="py-2 tabular-nums">{s.median_price > 0 ? money(s.median_price) : "-"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Section>
      )}

      <div className="grid gap-5 lg:grid-cols-2">
        {report.objections.length > 0 && (
          <Section title={r("objectionsTitle")} description={r("objectionsHelp")}>
            <ThemeBars themes={report.objections} total={m.respondents} peopleLabel={peopleOf} tone="destructive" />
          </Section>
        )}
        {report.needs.length > 0 && (
          <Section title={r("needsTitle")} description={r("needsHelp")}>
            <ThemeBars themes={report.needs} total={m.respondents} peopleLabel={peopleOf} tone="warning" />
          </Section>
        )}
      </div>
      {report.alternatives.length > 0 && (
        <Section title={r("alternativesTitle")} description={r("alternativesHelp")}>
          <ThemeBars themes={report.alternatives} total={m.respondents} peopleLabel={peopleOf} />
        </Section>
      )}

      {report.quotes.length > 0 && (
        <Section title={r("quotesTitle")} description={r("quotesHelp")}>
          <div className="grid gap-3 md:grid-cols-2">
            {report.quotes.map((q, i) => (
              <figure key={`${q.persona}-${i}`} className={cn("rounded-md border border-border border-l-4 bg-surface-sunken/40 p-4", SENTIMENT_BORDER[q.sentiment ?? "neutral"])}>
                <Quote className="mb-1 h-4 w-4 text-muted-foreground" aria-hidden />
                <blockquote className="text-body leading-relaxed">{q.text}</blockquote>
                <figcaption className="mt-2 text-caption text-muted-foreground">
                  <span className="font-medium text-foreground">{q.persona}</span>
                  {[q.role, q.segment].filter(Boolean).length > 0 && ` · ${[q.role, q.segment].filter(Boolean).join(" · ")}`}
                  {q.topic && <Badge variant="outline" className="ml-2">{q.topic}</Badge>}
                </figcaption>
              </figure>
            ))}
          </div>
        </Section>
      )}

      {report.recommendations.length > 0 && (
        <Section title={r("recommendationsTitle")}>
          <ol className="space-y-3">
            {report.recommendations.map((rec, i) => (
              <li key={rec.title} className="flex gap-3">
                <span className="mt-0.5 flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-primary/10 text-caption font-semibold text-primary">{i + 1}</span>
                <div className="min-w-0">
                  <p className="flex flex-wrap items-center gap-2 font-medium">
                    {rec.title}
                    {rec.priority && <Badge variant={PRIORITY_VARIANT[rec.priority]}>{r(`priority.${rec.priority}`)}</Badge>}
                  </p>
                  {rec.detail && <p className="mt-0.5 text-body text-muted-foreground">{rec.detail}</p>}
                </div>
              </li>
            ))}
          </ol>
        </Section>
      )}

      {report.open_questions && report.open_questions.length > 0 && (
        <Section title={r("openQuestionsTitle")}>
          <dl className="space-y-3">
            {report.open_questions.map((q) => (
              <div key={q.question}>
                <dt className="text-body font-medium">{q.question}</dt>
                <dd className="mt-0.5 text-body text-muted-foreground">{q.digest}</dd>
              </div>
            ))}
          </dl>
        </Section>
      )}

      <Section title={r("methodologyTitle")}>
        <p className="text-body text-muted-foreground">{report.methodology}</p>
        <Notice variant="info" title={r("disclaimerTitle")} role="status">
          {report.disclaimer}
        </Notice>
      </Section>
    </div>
  );
}
