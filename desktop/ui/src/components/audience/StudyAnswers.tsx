import { ChevronDown, ChevronRight } from "lucide-react";
import { useState } from "react";
import type { StudyAnswer, StudyKind } from "@/api";
import { PersonaAvatar } from "@/components/audience/PersonaAvatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { useI18n } from "@/hooks/useI18n";
import { formatMoney, intentTone } from "@/lib/audienceHub";

const VERDICT_VARIANT = { yes: "success", maybe: "warning", no: "destructive" } as const;

function Items({ label, items }: { label: string; items?: string[] }) {
  if (!items || items.length === 0) return null;
  return (
    <div>
      <dt className="font-medium">{label}</dt>
      <dd className="text-muted-foreground">{items.join("; ")}</dd>
    </div>
  );
}

function Text({ label, value }: { label: string; value?: string }) {
  if (!value) return null;
  return (
    <div>
      <dt className="font-medium">{label}</dt>
      <dd className="text-muted-foreground">{value}</dd>
    </div>
  );
}

function KindDetails({ answer, kind, variants }: { answer: StudyAnswer; kind: StudyKind; variants: string[] }) {
  const { t } = useI18n();
  const k = (key: string, params?: Record<string, string | number>) => t(`audienceReportKinds.answers.${key}`, params);
  const x = answer.answers;
  if (kind === "landing_page_test" && x.landing) {
    return (
      <>
        <Text label={k("firstImpression")} value={x.landing.first_impression} />
        <Text label={k("clarity")} value={k("badge", { score: x.landing.clarity_score })} />
        <Items label={k("trustSignals")} items={x.landing.trust_signals} />
        <Text label={k("change")} value={x.landing.change} />
      </>
    );
  }
  if (kind === "message_testing") {
    return (
      <>
        {x.ratings && x.ratings.length > 0 && (
          <div>
            <dt className="font-medium">{k("ratings")}</dt>
            <dd>
              <ul className="space-y-0.5 text-muted-foreground">
                {x.ratings.map((r) => (
                  <li key={r.variant}>
                    <span className="font-medium text-foreground">{variants[r.variant - 1] ?? k("line", { n: r.variant })}</span>
                    {" - "}
                    {k("ratingLine", { variant: k("line", { n: r.variant }), clarity: r.clarity, relevance: r.relevance, credibility: r.credibility, appeal: r.appeal })}
                    {r.comment ? ` (${r.comment})` : ""}
                  </li>
                ))}
              </ul>
            </dd>
          </div>
        )}
        {x.best_variant ? <Text label={k("best")} value={variants[x.best_variant - 1] ?? k("line", { n: x.best_variant })} /> : null}
        <Text label={k("stronger")} value={x.stronger} />
      </>
    );
  }
  if (kind === "ab_test" && (x.page_a || x.page_b)) {
    const first = x.shown_first ?? "a";
    const order: ("a" | "b")[] = first === "b" ? ["b", "a"] : ["a", "b"];
    return (
      <>
        {order.map((key, index) => {
          const card = key === "a" ? x.page_a : x.page_b;
          if (!card) return null;
          const label = key.toUpperCase();
          return (
            <div key={key} className="rounded-md border border-border/60 p-2">
              <dt className="font-medium">
                {k("version", { label })} <span className="font-normal text-muted-foreground">({index === 0 ? k("seenFirst") : k("seenSecond")})</span>
              </dt>
              <dd>
                <dl className="mt-1 space-y-1">
                  <Text label={k("firstImpression")} value={card.first_impression} />
                  <Text label={k("clarity")} value={k("badge", { score: card.clarity_score })} />
                  <Text label={k("actScore")} value={k("actValue", { score: card.act_score, verdict: t(`audienceHub.answers.verdict.${card.verdict}`) })} />
                  <Items label={k("objections")} items={card.objections} />
                  <Items label={k("missing")} items={card.missing} />
                  <Items label={k("trustSignals")} items={card.trust_signals} />
                  <Text label={k("change")} value={card.change} />
                </dl>
              </dd>
            </div>
          );
        })}
        {x.preferred && <Text label={k("preferred")} value={k("version", { label: x.preferred.toUpperCase() })} />}
        <Text label={k("why")} value={x.reasoning} />
      </>
    );
  }
  return null;
}

/** What each person said, one row each, opened to the full answers. */
export function StudyAnswers({
  answers,
  currency,
  kind = "user_interviews",
  variants = [],
  onTalk,
}: {
  answers: StudyAnswer[];
  currency: string;
  kind?: StudyKind;
  /** The candidate lines of a message test, in the study's own order. */
  variants?: string[];
  /** Opens a follow-up conversation with this person; absent when they no longer exist. */
  onTalk: (answer: StudyAnswer) => void;
}) {
  const { t, lang } = useI18n();
  const a = (key: string, params?: Record<string, string | number>) => t(`audienceHub.answers.${key}`, params);
  const [open, setOpen] = useState<string | null>(null);
  return (
    <Card className="p-2">
      <ul className="divide-y divide-border/60">
        {answers.map((ans) => {
          const expanded = open === ans.id;
          const x = ans.answers;
          return (
            <li key={ans.id} className="px-3 py-2">
              <div className="flex items-center gap-3">
                <button
                  type="button"
                  className="flex min-w-0 flex-1 items-center gap-3 rounded-md py-1 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  onClick={() => setOpen(expanded ? null : ans.id)}
                  aria-expanded={expanded}
                >
                  {expanded ? <ChevronDown className="h-4 w-4 shrink-0" aria-hidden /> : <ChevronRight className="h-4 w-4 shrink-0" aria-hidden />}
                  <PersonaAvatar name={ans.persona.name} className="h-8 w-8" />
                  <span className="min-w-0">
                    <span className="block truncate font-medium">{ans.persona.name}</span>
                    <span className="block truncate text-caption text-muted-foreground">
                      {[ans.persona.role, ans.persona.segment].filter(Boolean).join(" · ")}
                    </span>
                  </span>
                </button>
                {ans.status === "ok" ? (
                  <div className="flex shrink-0 items-center gap-2">
                    <Badge variant={intentTone(x.intent_score)}>{x.intent_score}/10</Badge>
                    <Badge variant={VERDICT_VARIANT[x.verdict] ?? "secondary"}>{a(`verdict.${x.verdict}`)}</Badge>
                    {kind === "user_interviews" && typeof x.max_price === "number" && (
                      <span className="hidden text-caption tabular-nums text-muted-foreground sm:inline">{formatMoney(x.max_price, currency, lang)}</span>
                    )}
                  </div>
                ) : (
                  <Badge variant="destructive">{a("failed")}</Badge>
                )}
                {ans.persona_id && (
                  <Button variant="outline" size="sm" onClick={() => onTalk(ans)}>
                    {a("talk")}
                  </Button>
                )}
              </div>
              {expanded && (
                <div className="mt-2 space-y-2 pl-14 text-caption">
                  {ans.status === "failed" ? (
                    <p className="text-destructive">{ans.error || a("failedBody")}</p>
                  ) : (
                    <dl className="space-y-1.5">
                      {kind !== "ab_test" && x.reasoning && (
                        <div>
                          <dt className="font-medium">{a("reasoning")}</dt>
                          <dd className="text-muted-foreground">{x.reasoning}</dd>
                        </div>
                      )}
                      <KindDetails answer={ans} kind={kind} variants={variants} />
                      {kind === "user_interviews" && x.price_reactions && x.price_reactions.length > 0 && (
                        <div>
                          <dt className="font-medium">{a("priceReactions")}</dt>
                          <dd className="text-muted-foreground">
                            {x.price_reactions
                              .map((r) => `${formatMoney(r.price, currency, lang)}: ${r.would_buy ? a("wouldBuy") : a("wouldNotBuy")}`)
                              .join(" · ")}
                          </dd>
                        </div>
                      )}
                      <Items label={a("missing")} items={x.missing} />
                      <Items label={a("objections")} items={x.objections} />
                      {kind === "user_interviews" && <Items label={a("alternatives")} items={x.alternatives} />}
                      {x.answers?.map((qa) => (
                        <div key={qa.question}>
                          <dt className="font-medium">{qa.question}</dt>
                          <dd className="text-muted-foreground">{qa.answer}</dd>
                        </div>
                      ))}
                    </dl>
                  )}
                </div>
              )}
            </li>
          );
        })}
      </ul>
    </Card>
  );
}
