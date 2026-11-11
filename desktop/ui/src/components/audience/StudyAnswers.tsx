import { ChevronDown, ChevronRight } from "lucide-react";
import { useState } from "react";
import type { StudyAnswer } from "@/api";
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

/** What each person said, one row each, opened to the full answers. */
export function StudyAnswers({
  answers,
  currency,
  onTalk,
}: {
  answers: StudyAnswer[];
  currency: string;
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
                    {typeof x.max_price === "number" && (
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
                      {x.reasoning && (
                        <div>
                          <dt className="font-medium">{a("reasoning")}</dt>
                          <dd className="text-muted-foreground">{x.reasoning}</dd>
                        </div>
                      )}
                      {x.price_reactions && x.price_reactions.length > 0 && (
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
                      <Items label={a("alternatives")} items={x.alternatives} />
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
