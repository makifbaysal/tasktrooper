import { ArrowRight, Loader2, Plus, Sparkles } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { toast } from "sonner";
import { api, type Audience, type StudyKind } from "@/api";
import { AudienceFormDialog } from "@/components/audience/AudienceFormDialog";
import { StudyActionBar } from "@/components/audience/StudyActionBar";
import { StudyContextForm } from "@/components/audience/StudyContextForm";
import { StudyKindCards } from "@/components/audience/StudyKindCards";
import { StudyWizardShell } from "@/components/audience/StudyWizardShell";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { useAskPm } from "@/hooks/useAskPm";
import { useI18n } from "@/hooks/useI18n";
import { logEvent } from "@/lib/analytics";
import { cn } from "@/lib/utils";
import { contextProblem, contextToInput, emptyContext, type ContextDraft } from "@/lib/studyContext";
import type { WizardStep } from "@/lib/studyWizard";
import { Users } from "lucide-react";

/**
 * The first two steps of a study, before it exists: what to find out (the
 * kind and its inputs) and who to ask (one of the ready audiences). Moving on
 * plans the study, which has the person's agent write the interview script,
 * and opens it at its script step.
 */
export function NewStudyPage() {
  const { t } = useI18n();
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const pm = useAskPm();
  const [step, setStep] = useState<Extract<WizardStep, "context" | "personas">>("context");
  const [kind, setKind] = useState<StudyKind | null>(null);
  const [draft, setDraft] = useState<ContextDraft>(emptyContext);
  const [audiences, setAudiences] = useState<Audience[] | null>(null);
  const [audienceId, setAudienceId] = useState(params.get("audience") ?? "");
  const [planning, setPlanning] = useState(false);
  const [planError, setPlanError] = useState<string | null>(null);
  const [formOpen, setFormOpen] = useState(false);
  const c = (key: string, params?: Record<string, string | number>) => t(`audienceStudy.context.${key}`, params);

  useEffect(() => {
    let live = true;
    api
      .audiences()
      .then((r) => live && setAudiences(r.audiences))
      .catch((e) => {
        if (live) {
          setAudiences([]);
          toast.error(e instanceof Error ? e.message : t("audienceHub.list.loadFailed"));
        }
      });
    return () => {
      live = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const ready = useMemo(() => (audiences ?? []).filter((a) => a.status === "ready" && a.persona_count > 0), [audiences]);
  const chosen = ready.find((a) => a.id === audienceId) ?? null;
  const problem = kind ? contextProblem(kind, draft) : "pickKind";
  const calls = chosen ? chosen.persona_count + 2 : 0;

  const askPm = () => {
    const text = draft.goal.trim();
    if (text) void pm.ask(text);
  };

  const createAudience = () => {
    if (!pm.canAsk) return setFormOpen(true);
    logEvent("audience_create", { source: "pm" });
    void pm.ask(t("audienceStudy.start.createMessage"));
  };

  const plan = async () => {
    if (!kind || !chosen) return;
    setPlanning(true);
    setPlanError(null);
    try {
      const study = await api.planAudienceStudy(chosen.id, contextToInput(kind, draft));
      navigate(`/audience/studies/${study.id}`, { replace: true });
    } catch (e) {
      setPlanError(e instanceof Error ? e.message : t("common.saveFailed"));
      setPlanning(false);
    }
  };

  return (
    <StudyWizardShell title={c("title")} description={step === "context" ? c("description") : t("audienceStudy.personas.description")} current={step} reached={step}>
      {step === "context" ? (
        <>
          <Card className="space-y-3 p-5">
            <Textarea
              aria-label={c("title")}
              rows={3}
              value={draft.goal}
              placeholder={c("goalPlaceholder")}
              onChange={(e) => setDraft({ ...draft, goal: e.target.value })}
            />
            {pm.canAsk && (
              <div className="flex items-center justify-between gap-3">
                <p className="text-caption text-muted-foreground">{c("askPmHelp")}</p>
                <Button type="button" variant="outline" onClick={askPm} disabled={!draft.goal.trim() || pm.asking}>
                  {pm.asking ? <Loader2 className="animate-spin" /> : <Sparkles />}
                  {c("askPm")}
                </Button>
              </div>
            )}
          </Card>
          <section className="space-y-3" aria-labelledby="kind-heading">
            <h2 id="kind-heading" className="text-title font-semibold tracking-tight">
              {c("pickKind")}
            </h2>
            <StudyKindCards value={kind} onChange={setKind} />
          </section>
          {kind && (
            <Card className="p-5">
              <StudyContextForm kind={kind} value={draft} onChange={setDraft} />
            </Card>
          )}
          <StudyActionBar
            status={problem && problem !== "pickKind" ? c(`errors.${problem}`) : kind ? c("ready") : c("errors.pickKind")}
            actions={
              <Button onClick={() => setStep("personas")} disabled={Boolean(problem)}>
                {c("next")}
                <ArrowRight />
              </Button>
            }
          />
        </>
      ) : (
        <>
          {audiences === null ? (
            <div className="grid gap-3 md:grid-cols-2">
              <Skeleton className="h-32 w-full" />
              <Skeleton className="h-32 w-full" />
            </div>
          ) : ready.length === 0 ? (
            <Card>
              <EmptyState
                icon={Users}
                title={t("audienceStudy.personas.noneTitle")}
                description={t("audienceStudy.personas.noneBody")}
                action={
                  <Button onClick={createAudience}>
                    <Plus />
                    {t("audienceStudy.personas.create")}
                  </Button>
                }
              />
            </Card>
          ) : (
            <section className="space-y-3" aria-labelledby="audience-heading">
              <div className="flex items-center justify-between gap-3">
                <h2 id="audience-heading" className="text-title font-semibold tracking-tight">
                  {t("audienceStudy.personas.pick")}
                </h2>
                <Button variant="ghost" size="sm" onClick={createAudience}>
                  <Plus />
                  {t("audienceStudy.personas.create")}
                </Button>
              </div>
              <div role="radiogroup" aria-labelledby="audience-heading" className="grid gap-3 md:grid-cols-2">
                {ready.map((a) => (
                  <button
                    key={a.id}
                    type="button"
                    role="radio"
                    aria-checked={a.id === audienceId}
                    onClick={() => setAudienceId(a.id)}
                    className="rounded-xl text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  >
                    <Card className={cn("flex h-full flex-col gap-2 p-4 transition-colors hover:bg-accent/30", a.id === audienceId && "border-primary bg-primary/5 ring-1 ring-primary")}>
                      <span className="font-semibold">{a.name}</span>
                      <span className="line-clamp-2 text-caption text-muted-foreground">{a.brief.product}</span>
                      <span className="flex flex-wrap items-center gap-1.5">
                        <Badge variant="outline">{t("audienceHub.list.people", { count: a.persona_count })}</Badge>
                        {a.brief.segments?.slice(0, 3).map((s) => (
                          <Badge key={s.name} variant="secondary">
                            {s.name}
                          </Badge>
                        ))}
                      </span>
                    </Card>
                  </button>
                ))}
              </div>
            </section>
          )}
          {planError && (
            <Notice variant="error" title={t("audienceStudy.personas.planFailed")}>
              {planError}
            </Notice>
          )}
          <StudyActionBar
            status={chosen ? t("audienceStudy.personas.cost", { people: chosen.persona_count, calls }) : t("audienceStudy.personas.chooseOne")}
            actions={
              <>
                <Button variant="outline" onClick={() => setStep("context")} disabled={planning}>
                  {t("audienceStudy.back")}
                </Button>
                <Button onClick={() => void plan()} disabled={!chosen || planning}>
                  {planning && <Loader2 className="animate-spin" />}
                  {t("audienceStudy.personas.next")}
                  <ArrowRight />
                </Button>
              </>
            }
          />
        </>
      )}
      <AudienceFormDialog open={formOpen} onOpenChange={setFormOpen} onSaved={(a) => navigate(`/audience/${a.id}`)} />
    </StudyWizardShell>
  );
}

export default NewStudyPage;
