import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import type { AudienceStudy } from "@/api";
import { Card } from "@/components/ui/card";
import { useI18n } from "@/hooks/useI18n";
import { formatMoney } from "@/lib/audienceHub";

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div>
      <dt className="text-caption text-muted-foreground">{label}</dt>
      <dd className="mt-0.5 text-body">{children}</dd>
    </div>
  );
}

/** The first step of a study as it was set: the kind, the goal and what the kind was given. */
export function StudyContextSummary({ study }: { study: AudienceStudy }) {
  const { t, lang } = useI18n();
  const c = (key: string, params?: Record<string, string | number>) => t(`audienceStudy.summary.${key}`, params);
  const pages = study.inputs.pages ?? [];
  return (
    <Card className="p-5">
      <dl className="grid gap-4 sm:grid-cols-2">
        <Row label={c("kind")}>{t(`audienceStudy.kinds.${study.kind}.title`)}</Row>
        <Row label={c("goal")}>{study.goal || c("noGoal")}</Row>
        {study.inputs.intent && <Row label={c("intent")}>{study.inputs.intent}</Row>}
        {pages.map((p, i) => (
          <Row key={i} label={study.kind === "ab_test" ? c("pageAB", { label: i === 0 ? "A" : "B" }) : c("page")}>
            <span className="break-all">{p.url || c("pasted")}</span>
          </Row>
        ))}
        {study.inputs.variants && study.inputs.variants.length > 0 && (
          <Row label={c("variants")}>
            <ol className="list-decimal pl-5">
              {study.inputs.variants.map((v) => (
                <li key={v}>{v}</li>
              ))}
            </ol>
          </Row>
        )}
        {study.price_points.length > 0 && <Row label={c("prices")}>{study.price_points.map((p) => formatMoney(p, study.currency, lang)).join(", ")}</Row>}
      </dl>
    </Card>
  );
}

/** The second step: who is being asked. */
export function StudyPeopleSummary({ study }: { study: AudienceStudy }) {
  const { t } = useI18n();
  return (
    <Card className="space-y-1 p-5">
      <p className="font-medium">
        <Link to={`/audience/${study.audience_id}`} className="underline-offset-4 hover:underline">
          {study.audience_name}
        </Link>
      </p>
      <p className="text-body text-muted-foreground">{t("audienceStudy.summary.people", { count: study.persona_total })}</p>
    </Card>
  );
}
