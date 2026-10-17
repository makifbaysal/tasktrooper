import { CheckCircle2, Cloud, Network, Server } from "lucide-react";
import { useId, useState } from "react";
import { Link } from "react-router-dom";
import {
  aiJobRefusal,
  api,
  dataSourceFieldErrors,
  type AIJobRefusal,
  type DeployEnvironment,
  type EnableObservabilityResult,
  type ObservabilityGoal,
  type ObservabilityTarget,
} from "@/api";
import { FormDialog } from "@/components/admin/FormDialog";
import { RadioCards } from "@/components/operations/console/RadioCards";
import {
  defaultTarget,
  enableRequest,
  offeredGoals,
  suggestedGoals,
  validateEnable,
  type EnableDraft,
  type EnableField,
} from "@/components/operations/observability/observabilityReadiness";
import { ReadinessSummary } from "@/components/operations/observability/ReadinessSummary";
import type { OpsEntity } from "@/components/operations/shell/useOpsEntities";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { useObservabilityReadiness } from "@/hooks/useObservabilityReadiness";

interface EnableObservabilityDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  entity: OpsEntity;
  env: DeployEnvironment;
}

/** Asks the release engineer to instrument one component in one
 * environment: what it reports now, which goals to set up (the gaps found
 * are ticked), and where the telemetry goes. Creating it opens a board task;
 * the dialog then links to it. Mount only while open. */
export function EnableObservabilityDialog({ open, onOpenChange, entity, env }: EnableObservabilityDialogProps) {
  const { t } = useI18n();
  const o = (key: string, params?: Record<string, string | number>) => t(`operations.observability.${key}`, params);
  const goalsLabelId = useId();
  const targetLabelId = useId();
  const readiness = useObservabilityReadiness(entity.id, env, open);
  const envProvider = entity.environments.find((x) => x.environment === env)?.provider;
  const [touched, setTouched] = useState({ goals: false, target: false });
  const [draft, setDraft] = useState<EnableDraft>(() => ({ goals: [], target: defaultTarget(envProvider), otlpEndpoint: "" }));
  const [errors, setErrors] = useState<Partial<Record<EnableField, string>>>({});
  const [refusal, setRefusal] = useState<AIJobRefusal | null>(null);
  const [saveError, setSaveError] = useState("");
  const [saving, setSaving] = useState(false);
  const [created, setCreated] = useState<EnableObservabilityResult | null>(null);

  // Until the person picks, the goals follow the gaps the read found and the
  // target the cloud the binding reads from.
  const goals = touched.goals ? draft.goals : suggestedGoals(readiness.data);
  const target = touched.target ? draft.target : defaultTarget(readiness.data?.provider || envProvider);
  const current: EnableDraft = { ...draft, goals, target };
  const offered = offeredGoals(entity.role, readiness.data);
  const envLabel = t(`cloud.environments.${env}`);

  const toggleGoal = (goal: ObservabilityGoal, on: boolean) => {
    setTouched((x) => ({ ...x, goals: true }));
    setDraft((d) => ({ ...d, goals: on ? [...new Set([...goals, goal])] : goals.filter((g) => g !== goal) }));
    setErrors({});
  };
  const setTarget = (next: ObservabilityTarget) => {
    setTouched((x) => ({ ...x, target: true }));
    setDraft((d) => ({ ...d, target: next }));
    setErrors({});
  };

  const errorText = (field: EnableField, code: string) => {
    const key = `operations.observability.errors.${field}.${code}`;
    const text = t(key);
    if (text !== key) return text;
    const generic = `operations.errorDefinitions.errors.${code}`;
    const fallback = t(generic);
    return fallback === generic ? code : fallback;
  };

  const submit = async () => {
    const found = validateEnable(current);
    setErrors(found);
    setSaveError("");
    setRefusal(null);
    if (Object.keys(found).length > 0) return;
    setSaving(true);
    try {
      setCreated(await api.enableComponentObservability(entity.id, enableRequest(current, env)));
    } catch (err) {
      const coded = aiJobRefusal(err);
      const fields = dataSourceFieldErrors(err);
      if (coded) setRefusal(coded);
      else if (fields) {
        const shown: Partial<Record<EnableField, string>> = {};
        const rest: string[] = [];
        for (const [k, v] of Object.entries(fields)) {
          if (k === "goals" || k === "target" || k === "otlp_endpoint") shown[k] = v;
          else rest.push(`${k}: ${errorText("environment", v)}`);
        }
        setErrors(shown);
        setSaveError(rest.join(" ") || o("fixFields"));
      } else setSaveError(err instanceof Error ? err.message : t("common.actionFailed"));
    } finally {
      setSaving(false);
    }
  };

  if (created) {
    return (
      <FormDialog
        open={open}
        onOpenChange={onOpenChange}
        title={o("dialogTitle", { name: entity.name })}
        footer={
          <>
            <Button variant="outline" onClick={() => onOpenChange(false)}>
              {o("close")}
            </Button>
            <Button asChild>
              <Link to={`/board?task=${encodeURIComponent(created.task_id)}`}>{o("created.open", { key: created.task_key })}</Link>
            </Button>
          </>
        }
      >
        <div className="flex items-start gap-3" role="status">
          <CheckCircle2 className="mt-0.5 h-5 w-5 shrink-0 text-success" aria-hidden />
          <div className="space-y-1">
            <p className="text-body font-medium">{o("created.title", { key: created.task_key })}</p>
            <p className="text-caption text-muted-foreground">{o("created.body")}</p>
          </div>
        </div>
      </FormDialog>
    );
  }

  return (
    <FormDialog
      open={open}
      onOpenChange={(next) => !saving && onOpenChange(next)}
      title={o("dialogTitle", { name: entity.name })}
      description={o("dialogDescription", { env: envLabel })}
      className="sm:max-w-2xl"
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
            {t("common.cancel")}
          </Button>
          <Button onClick={() => void submit()} disabled={saving}>
            {saving ? o("submitting") : o("submit")}
          </Button>
        </>
      }
    >
      {refusal && (
        <Notice variant="warning" title={t(`operations.aiJob.refusals.${refusal}.title`)}>
          {t(`operations.aiJob.refusals.${refusal}.body`)}
        </Notice>
      )}
      {saveError && (
        <Notice variant="error" title={o("submitFailed")}>
          <p className="break-words">{saveError}</p>
        </Notice>
      )}

      <section className="space-y-2 rounded-lg border border-border p-3" aria-labelledby={`${goalsLabelId}-readiness`}>
        <h3 id={`${goalsLabelId}-readiness`} className="text-micro font-medium tracking-wide text-muted-foreground uppercase">
          {o("readiness.title", { env: envLabel })}
        </h3>
        {readiness.data ? (
          <ReadinessSummary readiness={readiness.data} />
        ) : readiness.loading ? (
          <div className="space-y-2" aria-busy>
            <Skeleton className="h-4 w-2/3" />
            <Skeleton className="h-4 w-1/2" />
          </div>
        ) : (
          <p className="text-caption text-muted-foreground">{o("readiness.loadFailed", { message: readiness.error ?? "" })}</p>
        )}
      </section>

      <fieldset className="space-y-2" aria-describedby={errors.goals ? "enable-goals-error" : undefined}>
        <legend id={goalsLabelId} className="text-body font-medium">
          {o("goals.label")}
        </legend>
        <div className="grid gap-2 sm:grid-cols-2">
          {offered.map((goal) => {
            const id = `enable-goal-${goal}`;
            return (
              <div key={goal} className="flex items-start gap-2 rounded-md border border-border px-3 py-2">
                <Checkbox
                  id={id}
                  checked={goals.includes(goal)}
                  onCheckedChange={(v) => toggleGoal(goal, v === true)}
                  disabled={saving}
                  aria-invalid={errors.goals ? true : undefined}
                  aria-describedby={`${id}-help`}
                  className="mt-0.5"
                />
                <div className="min-w-0 space-y-0.5">
                  <Label htmlFor={id} className="cursor-pointer">
                    {o(`goals.${goal}.label`)}
                  </Label>
                  <p id={`${id}-help`} className="text-caption text-muted-foreground">
                    {o(`goals.${goal}.help`)}
                  </p>
                </div>
              </div>
            );
          })}
        </div>
        {errors.goals && (
          <p id="enable-goals-error" className="text-caption text-destructive">
            {errorText("goals", errors.goals)}
          </p>
        )}
      </fieldset>

      <div className="space-y-2">
        <p id={targetLabelId} className="text-body font-medium">
          {o("target.label")}
        </p>
        <RadioCards
          labelledBy={targetLabelId}
          value={target}
          onChange={setTarget}
          disabled={saving}
          className="sm:grid-cols-3"
          options={[
            { value: "own_cloud", icon: Cloud, label: o("target.own_cloud.label"), description: o("target.own_cloud.help") },
            { value: "self_hosted", icon: Server, label: o("target.self_hosted.label"), description: o("target.self_hosted.help") },
            { value: "otlp_endpoint", icon: Network, label: o("target.otlp_endpoint.label"), description: o("target.otlp_endpoint.help") },
          ]}
        />
        {errors.target && <p className="text-caption text-destructive">{errorText("target", errors.target)}</p>}
        {target === "otlp_endpoint" && (
          <div className="space-y-1.5">
            <Label htmlFor="enable-otlp">{o("otlpUrl")}</Label>
            <Input
              id="enable-otlp"
              type="url"
              inputMode="url"
              value={draft.otlpEndpoint}
              onChange={(ev) => {
                setDraft((d) => ({ ...d, otlpEndpoint: ev.target.value }));
                setErrors({});
              }}
              placeholder="https://otel.example.com:4318"
              disabled={saving}
              className="font-mono"
              aria-invalid={errors.otlp_endpoint ? true : undefined}
              aria-describedby={errors.otlp_endpoint ? "enable-otlp-error" : undefined}
            />
            {errors.otlp_endpoint && (
              <p id="enable-otlp-error" className="text-caption text-destructive">
                {errorText("otlp_endpoint", errors.otlp_endpoint)}
              </p>
            )}
          </div>
        )}
      </div>
      <p className="text-caption text-muted-foreground">{o("taskNote")}</p>
    </FormDialog>
  );
}
