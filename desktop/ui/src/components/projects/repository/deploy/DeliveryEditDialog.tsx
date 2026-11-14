import { Ban, Check, GitMerge, Hand, type LucideIcon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import {
  api,
  MAX_ERROR_RATE_DELTA,
  MAX_P95_DELTA_PCT,
  MAX_SMOKE_LATENCY_MS,
  MAX_SOAK_MINUTES,
  type Component,
  type ComponentDelivery,
  type ComponentEnvironment,
  type SmokeCheck,
} from "@/api";
import { FormDialog } from "@/components/admin/FormDialog";
import { RadioCards } from "@/components/operations/console/RadioCards";
import {
  deliverySummary,
  HOW_FOR_WHEN,
  howOf,
  whenOf,
  withHow,
  withNever,
  type DeliveryHow,
  type DeliveryWhen,
} from "@/components/projects/repository/deploy/deliveryChoice";
import { MAX_SMOKE_NAME_LEN, SmokeChecksEditor } from "@/components/projects/repository/deploy/SmokeChecksEditor";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Switch } from "@/components/ui/switch";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

const VERIFY_DEFAULTS = { soak_minutes: 10, max_new_errors: 0, smoke: [] };

type Step = "when" | "how" | "verify" | "review";

const WHEN_ICON: Record<DeliveryWhen, LucideIcon> = { merge: GitMerge, manual: Hand, never: Ban };

interface DeliveryEditDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  componentId: string;
  /** The effective profile to seed the form with (override, or detected, or none yet). */
  current: ComponentDelivery | null;
  /** Only an override can be reset — there is nothing to fall back to otherwise. */
  hasOverride: boolean;
  /** The confirmed production environment, when this component is coupled to one. */
  production?: ComponentEnvironment | null;
  onSaved: (component: Component) => void;
}

/**
 * Sets one component's delivery override by asking, in order: when it
 * deploys, what carries the deploy out, how it is checked afterwards, then
 * shows the answer in one sentence. Validation mirrors
 * ComponentDelivery.Validate so a mistake is caught on its own step, and the
 * server's 400 message is shown verbatim for whatever this pass misses.
 */
export function DeliveryEditDialog({ open, onOpenChange, componentId, current, hasOverride, production = null, onSaved }: DeliveryEditDialogProps) {
  const { t } = useI18n();
  const [draft, setDraft] = useState<ComponentDelivery>(() => current ?? { mode: "none", verify: VERIFY_DEFAULTS, auto_rollback: true });
  const [when, setWhenState] = useState<DeliveryWhen | null>(() => (current ? whenOf(current) : null));
  const [how, setHowState] = useState<DeliveryHow | null>(() => (current ? howOf(current) : null));
  const [step, setStep] = useState<Step>("when");
  const [saving, setSaving] = useState(false);
  const [resetOpen, setResetOpen] = useState(false);
  const [resetting, setResetting] = useState(false);
  const [errors, setErrors] = useState<string[]>([]);

  const steps: Step[] = when === "never" ? ["when", "review"] : ["when", "how", "verify", "review"];
  const stepIndex = steps.indexOf(step);

  const setHow = (next: DeliveryHow) => {
    setHowState(next);
    setDraft((d) => withHow(d, next));
    setErrors([]);
  };

  const setWhen = (next: DeliveryWhen) => {
    setWhenState(next);
    setErrors([]);
    if (next === "never") {
      setHowState(null);
      setDraft(withNever);
      return;
    }
    if (how && HOW_FOR_WHEN[next].includes(how)) return;
    setHow(next === "merge" ? (production?.provider === "vercel" ? "vercel" : "actionsPush") : "tag");
  };

  const setSmoke = (next: SmokeCheck[]) => setDraft((d) => ({ ...d, verify: { ...d.verify, smoke: next } }));

  const validateHow = (d: ComponentDelivery): string[] => {
    const out: string[] = [];
    if (d.mode === "none") return out;
    if (d.mode === "dispatch" && !(d.workflow ?? "").trim()) {
      out.push(t("release.deliveryEdit.errors.workflowRequired"));
    }
    if (/[\\/]/.test(d.workflow ?? "")) {
      out.push(t("release.deliveryEdit.errors.workflowNoSlash"));
    }
    if (d.mode === "batch" && d.executor === "local" && !(d.local_command ?? "").trim()) {
      out.push(t("release.deliveryEdit.errors.localCommandRequired"));
    }
    return out;
  };

  const validateVerify = (d: ComponentDelivery): string[] => {
    const out: string[] = [];
    if (d.mode === "none") return out;
    const soak = d.verify.soak_minutes ?? 0;
    if (soak < 1 || soak > MAX_SOAK_MINUTES) {
      out.push(t("release.deliveryEdit.errors.soakRange", { max: MAX_SOAK_MINUTES }));
    }
    if ((d.verify.max_new_errors ?? 0) < 0) {
      out.push(t("release.deliveryEdit.errors.maxNewErrorsNegative"));
    }
    const errorRateDelta = d.verify.max_error_rate_delta ?? 0;
    if (!Number.isFinite(errorRateDelta) || errorRateDelta < 0 || errorRateDelta > MAX_ERROR_RATE_DELTA) {
      out.push(t("release.deliveryEdit.errors.maxErrorRateDeltaRange", { max: MAX_ERROR_RATE_DELTA }));
    }
    const p95DeltaPct = d.verify.max_p95_delta_pct ?? 0;
    if (!Number.isFinite(p95DeltaPct) || p95DeltaPct < 0 || p95DeltaPct > MAX_P95_DELTA_PCT) {
      out.push(t("release.deliveryEdit.errors.maxP95DeltaPctRange", { max: MAX_P95_DELTA_PCT }));
    }
    for (const c of d.verify.smoke ?? []) {
      if (c.method !== "GET" && c.method !== "HEAD") {
        out.push(t("release.deliveryEdit.errors.smokeMethod"));
      }
      const path = c.path.trim();
      const isAbsolute = path.includes("://");
      if (!path || (!isAbsolute && !path.startsWith("/"))) {
        out.push(t("release.deliveryEdit.errors.smokePath"));
      }
      if (c.expect_status && (c.expect_status < 100 || c.expect_status > 599)) {
        out.push(t("release.deliveryEdit.errors.smokeExpectStatus"));
      }
      if ((c.name?.length ?? 0) > MAX_SMOKE_NAME_LEN) {
        out.push(t("release.deliveryEdit.errors.smokeName", { max: MAX_SMOKE_NAME_LEN }));
      }
      if (c.max_latency_ms !== undefined && (c.max_latency_ms < 1 || c.max_latency_ms > MAX_SMOKE_LATENCY_MS)) {
        out.push(t("release.deliveryEdit.errors.smokeLatency", { max: MAX_SMOKE_LATENCY_MS }));
      }
    }
    return Array.from(new Set(out));
  };

  const problemsOn = (s: Step): string[] => (s === "how" ? validateHow(draft) : s === "verify" ? validateVerify(draft) : []);

  const next = () => {
    const problems = problemsOn(step);
    setErrors(problems);
    if (problems.length === 0) setStep(steps[stepIndex + 1]);
  };

  const back = () => {
    setErrors([]);
    setStep(steps[stepIndex - 1]);
  };

  const submit = async () => {
    const failing = steps.find((s) => problemsOn(s).length > 0);
    if (failing) {
      setErrors(problemsOn(failing));
      setStep(failing);
      return;
    }
    setSaving(true);
    try {
      const saved = await api.updateComponentDelivery(componentId, draft);
      toast.success(t("release.deliveryEdit.saved"));
      onSaved(saved);
    } catch (e) {
      const message = e instanceof Error ? e.message : t("release.delivery.saveFailed");
      setErrors([message]);
      toast.error(message);
    } finally {
      setSaving(false);
    }
  };

  const resetToDetected = async () => {
    setResetting(true);
    try {
      const saved = await api.updateComponentDelivery(componentId, null);
      toast.success(t("release.deliveryEdit.resetDone"));
      onSaved(saved);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("release.delivery.saveFailed"));
    } finally {
      setResetting(false);
      setResetOpen(false);
    }
  };

  const showWorkflow = how === "actionsPush" || how === "actionsDispatch";
  const isLast = stepIndex === steps.length - 1;
  const canAdvance = step === "when" ? when !== null : step === "how" ? how !== null : true;

  return (
    <>
      <FormDialog
        open={open}
        onOpenChange={onOpenChange}
        title={current ? t("release.deliveryEdit.editTitle") : t("release.deliveryEdit.newTitle")}
        description={t("release.deliveryEdit.description")}
        className="sm:max-w-2xl"
        footer={
          <>
            {hasOverride && (
              <Button variant="outline" className="sm:mr-auto" onClick={() => setResetOpen(true)} disabled={saving}>
                {t("release.deliveryEdit.resetToDetected")}
              </Button>
            )}
            {stepIndex === 0 ? (
              <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
                {t("common.cancel")}
              </Button>
            ) : (
              <Button variant="outline" onClick={back} disabled={saving}>
                {t("release.wizard.back")}
              </Button>
            )}
            {isLast ? (
              <Button onClick={() => void submit()} disabled={saving}>
                {saving ? t("release.deliveryEdit.saving") : t("release.deliveryEdit.save")}
              </Button>
            ) : (
              <Button onClick={next} disabled={!canAdvance}>
                {t("release.wizard.next")}
              </Button>
            )}
          </>
        }
      >
        <ol className="flex flex-wrap items-center gap-x-4 gap-y-1 text-caption" aria-label={t("release.wizard.stepsLabel")}>
          {steps.map((s, i) => (
            <li
              key={s}
              aria-current={s === step ? "step" : undefined}
              className={cn("flex items-center gap-1.5", s === step ? "font-semibold text-foreground" : "text-muted-foreground")}
            >
              <span
                aria-hidden
                className={cn(
                  "flex h-5 w-5 items-center justify-center rounded-full border text-micro",
                  i < stepIndex && "border-success bg-success/10 text-success",
                  s === step && "border-primary bg-primary text-primary-foreground",
                )}
              >
                {i < stepIndex ? <Check className="h-3 w-3" /> : i + 1}
              </span>
              {t(`release.wizard.steps.${s}`)}
            </li>
          ))}
        </ol>

        {errors.length > 0 && (
          <ul className="space-y-1 rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-caption text-destructive">
            {errors.map((err) => (
              <li key={err}>{err}</li>
            ))}
          </ul>
        )}

        {step === "when" && (
          <div className="space-y-2">
            <p id="delivery-when-question" className="text-body font-medium">
              {t("release.wizard.when.question")}
            </p>
            <RadioCards
              labelledBy="delivery-when-question"
              value={when}
              onChange={setWhen}
              options={(["merge", "manual", "never"] as const).map((w) => ({
                value: w,
                label: t(`release.wizard.when.${w}.label`),
                description: t(`release.wizard.when.${w}.description`),
                icon: WHEN_ICON[w],
              }))}
            />
          </div>
        )}

        {step === "how" && when && when !== "never" && (
          <div className="space-y-4">
            <div className="space-y-2">
              <p id="delivery-how-question" className="text-body font-medium">
                {t(`release.wizard.how.question.${when}`)}
              </p>
              {when === "manual" && <p className="text-caption text-muted-foreground">{t("release.wizard.how.manualHelp")}</p>}
              <RadioCards
                labelledBy="delivery-how-question"
                value={how}
                onChange={setHow}
                options={HOW_FOR_WHEN[when].map((h) => ({
                  value: h,
                  label: t(`release.wizard.how.${h}.label`),
                  description: t(`release.wizard.how.${h}.description`),
                }))}
              />
            </div>

            {how === "vercel" && (
              <p className="text-caption text-muted-foreground">
                {production?.provider === "vercel"
                  ? t("release.deliveryEdit.vercelProjectFromProd", { name: production.resource?.name ?? production.url ?? "" })
                  : t("release.deliveryEdit.vercelNoProdWarning")}
              </p>
            )}

            {how === "actionsPush" && (
              <Notice variant="warning" title={t("release.wizard.how.actionsPush.warningTitle")} role={null}>
                <p>{t("release.wizard.how.actionsPush.warningBody")}</p>
              </Notice>
            )}

            {showWorkflow && (
              <div className="space-y-1.5">
                <Label htmlFor="delivery-workflow">
                  {how === "actionsPush" ? t("release.wizard.how.workflowOptional") : t("release.deliveryEdit.workflow")}
                </Label>
                <Input
                  id="delivery-workflow"
                  value={draft.workflow ?? ""}
                  onChange={(e) => setDraft((d) => ({ ...d, workflow: e.target.value }))}
                  placeholder={t("release.deliveryEdit.workflowPlaceholder")}
                  className="font-mono"
                />
                <p className="text-micro text-muted-foreground">{t("release.deliveryEdit.workflowHelp")}</p>
              </div>
            )}

            {how === "tag" && (
              <div className="space-y-1.5">
                <Label htmlFor="delivery-tag-pattern">{t("release.deliveryEdit.tagPattern")}</Label>
                <Input
                  id="delivery-tag-pattern"
                  value={draft.tag_pattern ?? ""}
                  onChange={(e) => setDraft((d) => ({ ...d, tag_pattern: e.target.value }))}
                  placeholder={t("release.deliveryEdit.tagPatternPlaceholder")}
                  className="font-mono"
                />
                <p className="text-micro text-muted-foreground">{t("release.delivery.batchGithubActionsHint")}</p>
              </div>
            )}

            {how === "local" && (
              <div className="space-y-1.5">
                <Label htmlFor="delivery-local-command">{t("release.deliveryEdit.localCommand")}</Label>
                <Input
                  id="delivery-local-command"
                  value={draft.local_command ?? ""}
                  onChange={(e) => setDraft((d) => ({ ...d, local_command: e.target.value }))}
                  placeholder={t("release.deliveryEdit.localCommandPlaceholder")}
                  className="font-mono"
                />
              </div>
            )}
          </div>
        )}

        {step === "verify" && (
          <>
            <p className="text-body font-medium">{t("release.wizard.verify.question")}</p>
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label htmlFor="delivery-soak">{t("release.deliveryEdit.soakMinutes")}</Label>
                <Input
                  id="delivery-soak"
                  type="number"
                  min={1}
                  max={MAX_SOAK_MINUTES}
                  value={draft.verify.soak_minutes ?? ""}
                  onChange={(e) =>
                    setDraft((d) => ({ ...d, verify: { ...d.verify, soak_minutes: e.target.value === "" ? undefined : Number(e.target.value) } }))
                  }
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="delivery-max-errors">{t("release.deliveryEdit.maxNewErrors")}</Label>
                <Input
                  id="delivery-max-errors"
                  type="number"
                  min={0}
                  value={draft.verify.max_new_errors ?? 0}
                  onChange={(e) =>
                    setDraft((d) => ({ ...d, verify: { ...d.verify, max_new_errors: e.target.value === "" ? 0 : Number(e.target.value) } }))
                  }
                />
              </div>
            </div>

            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label htmlFor="delivery-max-error-rate-delta">{t("release.deliveryEdit.maxErrorRateDelta")}</Label>
                <Input
                  id="delivery-max-error-rate-delta"
                  type="number"
                  min={0}
                  max={MAX_ERROR_RATE_DELTA}
                  step="0.1"
                  value={draft.verify.max_error_rate_delta ?? ""}
                  placeholder={t("release.deliveryEdit.impactUnchecked")}
                  aria-describedby="delivery-impact-help"
                  onChange={(e) =>
                    setDraft((d) => ({
                      ...d,
                      verify: { ...d.verify, max_error_rate_delta: e.target.value === "" ? undefined : Number(e.target.value) },
                    }))
                  }
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="delivery-max-p95-delta">{t("release.deliveryEdit.maxP95DeltaPct")}</Label>
                <Input
                  id="delivery-max-p95-delta"
                  type="number"
                  min={0}
                  max={MAX_P95_DELTA_PCT}
                  value={draft.verify.max_p95_delta_pct ?? ""}
                  placeholder={t("release.deliveryEdit.impactUnchecked")}
                  aria-describedby="delivery-impact-help"
                  onChange={(e) =>
                    setDraft((d) => ({
                      ...d,
                      verify: { ...d.verify, max_p95_delta_pct: e.target.value === "" ? undefined : Number(e.target.value) },
                    }))
                  }
                />
              </div>
              <p id="delivery-impact-help" className="text-micro text-muted-foreground sm:col-span-2">
                {t("release.deliveryEdit.impactHelp")}
              </p>
            </div>

            <SmokeChecksEditor
              checks={draft.verify.smoke ?? []}
              onChange={setSmoke}
              componentId={componentId}
              baseUrl={production?.url}
            />

            <div className="flex items-center justify-between gap-3 rounded-md border border-border p-3">
              <div>
                <p className="text-body">{t("release.deliveryEdit.autoRollback")}</p>
                <p className="text-micro text-muted-foreground">{t("release.deliveryEdit.autoRollbackHelp")}</p>
              </div>
              <Switch checked={draft.auto_rollback} onCheckedChange={(checked) => setDraft((d) => ({ ...d, auto_rollback: checked }))} />
            </div>
          </>
        )}

        {step === "review" && (
          <div className="space-y-3">
            <p className="text-body font-medium">{t("release.wizard.review.question")}</p>
            <Notice variant="info" title={deliverySummary(draft, t)} role={null}>
              {draft.mode !== "none" && (
                <p>
                  {t("release.wizard.review.verify", {
                    minutes: draft.verify.soak_minutes ?? 10,
                    count: draft.verify.smoke?.length ?? 0,
                  })}{" "}
                  {draft.auto_rollback ? t("release.wizard.review.rollbackOn") : t("release.wizard.review.rollbackOff")}
                </p>
              )}
            </Notice>
            {when === "manual" && <p className="text-caption text-muted-foreground">{t("release.wizard.review.manualNext")}</p>}
          </div>
        )}
      </FormDialog>

      <ConfirmDialog
        open={resetOpen}
        onOpenChange={setResetOpen}
        title={t("release.deliveryEdit.resetConfirmTitle")}
        description={t("release.deliveryEdit.resetConfirmDesc")}
        confirmLabel={t("release.deliveryEdit.resetToDetected")}
        loading={resetting}
        onConfirm={resetToDetected}
      />
    </>
  );
}
