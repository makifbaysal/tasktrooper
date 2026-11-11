import { Quote } from "lucide-react";
import type { ABReport, AudienceReport, LandingReport, MessageReport } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { Notice } from "@/components/ui/notice";
import { useI18n } from "@/hooks/useI18n";
import { abRows, positionBiased, signed, type ABRow } from "@/lib/abComparison";
import { formatPct } from "@/lib/audienceHub";
import { ScoreBars, ThemeBars } from "./Charts";
import { Section } from "./Section";

type Translate = (key: string, params?: Record<string, string | number>) => string;

function useKindT(group: "landing" | "messages" | "ab"): Translate {
  const { t } = useI18n();
  return (key, params) => t(`audienceReportKinds.${group}.${key}`, params);
}

const score = (v: number) => `${v.toFixed(1)} / 10`;

export function LandingSections({ report, landing }: { report: AudienceReport; landing: LandingReport }) {
  const { t } = useI18n();
  const k = useKindT("landing");
  const peopleOf = (count: number, total: number) => t("audienceHub.report.peopleOf", { count, total });
  return (
    <>
      <Section title={k("clarityTitle")} description={k("clarityHelp")}>
        <dl className="flex flex-wrap gap-x-8 gap-y-1 text-caption text-muted-foreground">
          <div className="flex gap-1.5">
            <dt className="font-medium text-foreground">{k("visitorIntent")}:</dt>
            <dd>{landing.intent}</dd>
          </div>
          <div className="flex gap-1.5">
            <dt className="font-medium text-foreground">{k("source")}:</dt>
            <dd className="break-all">{landing.source || k("pasted")}</dd>
          </div>
        </dl>
        {landing.clarity_by_segment && landing.clarity_by_segment.length > 0 && (
          <ScoreBars items={landing.clarity_by_segment} max={10} format={score} label={k("clarityTitle")} />
        )}
        <p className="text-caption text-muted-foreground">{k("clarityFoot")}</p>
      </Section>
      {report.strengths && report.strengths.length > 0 && (
        <Section title={k("strengthsTitle")} description={k("strengthsHelp")}>
          <ThemeBars themes={report.strengths} total={report.metrics.respondents} peopleLabel={peopleOf} tone="success" />
        </Section>
      )}
    </>
  );
}

export function MessagesSections({ report, messages }: { report: AudienceReport; messages: MessageReport }) {
  const { t } = useI18n();
  const k = useKindT("messages");
  const peopleOf = (count: number, total: number) => t("audienceHub.report.peopleOf", { count, total });
  const ranked = [...messages.variants].sort((a, b) => a.rank - b.rank);
  return (
    <>
      <Section title={k("rankingTitle")} description={k("rankingHelp")}>
        <ol className="space-y-4">
          {ranked.map((v) => {
            const winner = v.variant === messages.winner;
            return (
              <li key={v.variant}>
                <Card className="space-y-3 p-4">
                  <div className="flex flex-wrap items-start justify-between gap-2">
                    <div className="min-w-0">
                      <p className="flex flex-wrap items-center gap-2 text-caption text-muted-foreground">
                        <Badge variant={winner ? "success" : "secondary"}>{winner ? k("winner") : k("rank", { rank: v.rank })}</Badge>
                        {k("line", { n: v.variant })}
                      </p>
                      <p className="mt-1 text-body font-medium">{v.text}</p>
                    </div>
                    <div className="text-right">
                      <p className="text-caption text-muted-foreground">{k("overall")}</p>
                      <p className="text-title font-semibold tabular-nums">{score(v.overall)}</p>
                    </div>
                  </div>
                  <ScoreBars
                    label={k("line", { n: v.variant })}
                    max={10}
                    format={score}
                    items={[
                      { name: k("clarity"), value: v.clarity },
                      { name: k("relevance"), value: v.relevance },
                      { name: k("credibility"), value: v.credibility },
                      { name: k("appeal"), value: v.appeal },
                    ]}
                  />
                  <p className="text-caption text-muted-foreground">{k("bestPct", { pct: Math.round(v.best_pct) })}</p>
                  {v.by_segment && v.by_segment.length > 1 && (
                    <div>
                      <p className="mb-1 text-caption font-medium">{k("bySegment")}</p>
                      <ScoreBars items={v.by_segment} max={10} format={score} tone="warning" label={k("bySegment")} />
                    </div>
                  )}
                </Card>
              </li>
            );
          })}
        </ol>
      </Section>
      {report.strengths && report.strengths.length > 0 && (
        <Section title={k("strengthsTitle")} description={k("strengthsHelp")}>
          <ThemeBars themes={report.strengths} total={report.metrics.respondents} peopleLabel={peopleOf} tone="success" />
        </Section>
      )}
      {messages.rewrites && messages.rewrites.length > 0 && (
        <Section title={k("rewritesTitle")} description={k("rewritesHelp")}>
          <div className="grid gap-3 md:grid-cols-2">
            {messages.rewrites.map((q, i) => (
              <figure key={`${q.persona}-${i}`} className="rounded-md border border-border bg-surface-sunken/40 p-4">
                <Quote className="mb-1 h-4 w-4 text-muted-foreground" aria-hidden />
                <blockquote className="text-body leading-relaxed">{q.text}</blockquote>
                <figcaption className="mt-2 text-caption text-muted-foreground">
                  <span className="font-medium text-foreground">{q.persona}</span>
                  {[q.role, q.segment].filter(Boolean).length > 0 && ` · ${[q.role, q.segment].filter(Boolean).join(" · ")}`}
                </figcaption>
              </figure>
            ))}
          </div>
        </Section>
      )}
    </>
  );
}

const CONFIDENCE_VARIANT = { clear: "success", lean: "info", "toss-up": "warning" } as const;

function ComparisonTable({ ab }: { ab: ABReport }) {
  const k = useKindT("ab");
  const fmt = (row: ABRow, v: number | undefined) => (v === undefined ? "-" : row.unit === "pct" ? formatPct(v) : v.toFixed(1));
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-left text-body">
        <thead>
          <tr className="border-b border-border text-caption text-muted-foreground">
            <th rowSpan={2} className="py-2 pr-4 font-medium">
              {k("question")}
            </th>
            <th colSpan={2} className="py-1 pr-4 font-medium">
              {k("version", { label: "A" })}
            </th>
            <th colSpan={2} className="py-1 pr-4 font-medium">
              {k("version", { label: "B" })}
            </th>
            <th rowSpan={2} className="py-2 pr-4 font-medium">
              {k("diff")}
            </th>
            <th rowSpan={2} className="py-2 pr-4 font-medium">
              {k("paired")}
            </th>
            <th rowSpan={2} className="py-2 font-medium">
              {k("effect")}
            </th>
          </tr>
          <tr className="border-b border-border text-caption text-muted-foreground">
            <th className="py-1 pr-4 font-medium">{k("colMean")}</th>
            <th className="py-1 pr-4 font-medium">{k("colMedian")}</th>
            <th className="py-1 pr-4 font-medium">{k("colMean")}</th>
            <th className="py-1 pr-4 font-medium">{k("colMedian")}</th>
          </tr>
        </thead>
        <tbody>
          {abRows(ab).map((row) => (
            <tr key={row.key} className="border-b border-border/60 align-top last:border-0">
              <th scope="row" className="py-2 pr-4 font-medium">
                {k(`rows.${row.key}`)}
              </th>
              <td className="py-2 pr-4 tabular-nums">{fmt(row, row.aMean)}</td>
              <td className="py-2 pr-4 tabular-nums">{fmt(row, row.aMedian)}</td>
              <td className="py-2 pr-4 tabular-nums">{fmt(row, row.bMean)}</td>
              <td className="py-2 pr-4 tabular-nums">{fmt(row, row.bMedian)}</td>
              <td className="py-2 pr-4 tabular-nums">{signed(row.diff, row.unit === "pct" ? 0 : 1)}</td>
              <td className="py-2 pr-4 tabular-nums">{row.paired ? k("pairedValue", { a: row.paired.a, b: row.paired.b, same: row.paired.same }) : "-"}</td>
              <td className="py-2">
                {row.effect ? k("effectValue", { label: k(`effectLabel.${row.effect.label}`), size: row.effect.size.toFixed(2) }) : "-"}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export function ABSections({ report, ab }: { report: AudienceReport; ab: ABReport }) {
  const { t } = useI18n();
  const k = useKindT("ab");
  const peopleOf = (count: number, total: number) => t("audienceHub.report.peopleOf", { count, total });
  const total = report.metrics.respondents;
  return (
    <>
      <Section title={ab.winner === "tie" ? k("tie") : k("winner", { label: ab.winner })} description={k("compareHelp")}>
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-caption text-muted-foreground">{k("confidenceTitle")}</span>
          <Badge variant={CONFIDENCE_VARIANT[ab.confidence]}>{k(`confidence.${ab.confidence}`)}</Badge>
        </div>
        <p className="text-body text-muted-foreground">{k(`caveat.${ab.confidence}`)}</p>
        {positionBiased(ab) && (
          <Notice variant="warning" title={k("firstShown")} role="status">
            {k("positionBias", { pct: Math.round(ab.first_shown_win_pct) })}
          </Notice>
        )}
        <h3 className="text-body font-semibold">{k("compareTitle")}</h3>
        <ComparisonTable ab={ab} />
        <Notice variant="info" title={k("honestyTitle")} role="status">
          {k("honesty")}
        </Notice>
      </Section>
      <Section title={k("versionsTitle")} description={k("versionsHelp")}>
        <div className="grid gap-4 md:grid-cols-2">
          {ab.sides.map((side) => (
            <Card key={side.label} className="space-y-4 p-4">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <h3 className="text-body font-semibold">{k("version", { label: side.label })}</h3>
                {ab.winner === side.label && <Badge variant="success">{k("winner", { label: side.label })}</Badge>}
              </div>
              <p className="break-all text-caption text-muted-foreground">
                {k("source")}: {side.source || k("pasted")}
              </p>
              <ScoreBars
                label={k("version", { label: side.label })}
                max={100}
                format={formatPct}
                items={[
                  { name: k("rows.preferred"), value: side.win_pct },
                  { name: k("rows.actPct"), value: side.act_pct },
                ]}
              />
              <p className="text-caption text-muted-foreground">{k("preferredValue", { pct: formatPct(side.win_pct), count: side.wins, total })}</p>
              <p className="text-caption text-muted-foreground">
                {k("meanScore")}: <span className="font-medium tabular-nums text-foreground">{score(side.mean_score)}</span>
              </p>
              {side.win_pct_by_segment && side.win_pct_by_segment.length > 1 && (
                <div>
                  <p className="mb-1 text-caption font-medium">{k("preferenceTitle")}</p>
                  <ScoreBars items={side.win_pct_by_segment} max={100} format={formatPct} tone="warning" label={k("preferenceTitle")} />
                </div>
              )}
              {side.reasons && side.reasons.length > 0 && (
                <div>
                  <p className="mb-1 text-caption font-medium">{k("reasons")}</p>
                  <ThemeBars themes={side.reasons} total={total} peopleLabel={peopleOf} tone="success" />
                </div>
              )}
              {side.issues && side.issues.length > 0 && (
                <div>
                  <p className="mb-1 text-caption font-medium">{k("issues")}</p>
                  <ThemeBars themes={side.issues} total={total} peopleLabel={peopleOf} tone="destructive" />
                </div>
              )}
              {side.quotes && side.quotes.length > 0 && (
                <div className="space-y-2">
                  <p className="text-caption font-medium">{k("quotes")}</p>
                  {side.quotes.map((q, i) => (
                    <figure key={`${q.persona}-${i}`} className="rounded-md border border-border bg-surface-sunken/40 p-3">
                      <Quote className="mb-1 h-4 w-4 text-muted-foreground" aria-hidden />
                      <blockquote className="text-body leading-relaxed">{q.text}</blockquote>
                      <figcaption className="mt-1 text-caption text-muted-foreground">
                        <span className="font-medium text-foreground">{q.persona}</span>
                        {[q.role, q.segment].filter(Boolean).length > 0 && ` · ${[q.role, q.segment].filter(Boolean).join(" · ")}`}
                        {` · ${t("audienceReportKinds.html.syntheticPersona")}`}
                      </figcaption>
                    </figure>
                  ))}
                </div>
              )}
            </Card>
          ))}
        </div>
      </Section>
    </>
  );
}
