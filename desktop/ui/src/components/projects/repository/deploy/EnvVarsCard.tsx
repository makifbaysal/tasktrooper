import { KeyRound, Plus, RefreshCw, Rocket, X } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import {
  api,
  type ComponentEnvStatus,
  type EnvInput,
  type EnvVarKind,
  type EnvVarView,
} from "@/api";
import { ProviderIcon } from "@/components/projects/model/ProviderIcon";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Notice } from "@/components/ui/notice";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

const KINDS: EnvVarKind[] = ["value", "generated", "human_secret", "human_bcrypt", "optional"];

interface Draft {
  kind?: EnvVarKind;
  value: string;
  /** A set variable opened for a new value or kind. */
  editing?: boolean;
  regenerate?: boolean;
}

interface AddedVar {
  name: string;
  kind?: EnvVarKind;
  value: string;
}

/** A draft's request: a new value, a regenerated secret, or a reclassification. */
function inputFor(name: string, d: Draft): EnvInput | null {
  if (d.regenerate) return { name, kind: d.kind, regenerate: true };
  if (d.value) return { name, kind: d.kind, value: d.value };
  if (d.kind) return { name, kind: d.kind };
  return null;
}

interface EnvVarsCardProps {
  repositoryId: string;
  /** One component's target; omitted, every target of the repository is shown. */
  componentId?: string;
  /** Only the variables still waiting on a person — the task drawer's view. */
  pendingOnly?: boolean;
  /** Called once a target has nothing left for a person to do. */
  onSettled?: () => void;
  /** Drawn inside a dialog: no card chrome, the actions pinned to the bottom. */
  embedded?: boolean;
  className?: string;
}

function pending(v: EnvVarView): boolean {
  return v.action === "human" || v.action === "classify";
}

/**
 * The production target's environment variables against what the component
 * requires: TaskTrooper fills value and generated ones itself, a person enters
 * the rest here. A typed secret leaves this component on submit and is
 * cleared from state right after — it is never read back from the server.
 */
export function EnvVarsCard({ repositoryId, componentId, pendingOnly = false, onSettled, embedded = false, className }: EnvVarsCardProps) {
  const { t } = useI18n();
  const [targets, setTargets] = useState<ComponentEnvStatus[] | null>(null);

  const load = useCallback(async () => {
    try {
      const res = await api.listEnvRequirements(repositoryId);
      setTargets(res.targets);
    } catch (e) {
      setTargets([]);
      toast.error(e instanceof Error ? e.message : t("cloud.envVars.loadFailed"));
    }
  }, [repositoryId, t]);

  useEffect(() => {
    setTargets(null);
    void load();
  }, [load]);

  const shown = useMemo(
    () =>
      (targets ?? []).filter(
        (target) =>
          (!componentId || target.component_id === componentId) &&
          (!pendingOnly || target.error || target.vars.some(pending)),
      ),
    [targets, componentId, pendingOnly],
  );

  if (targets === null) {
    if (pendingOnly) return null;
    if (embedded) return <Skeleton className={cn("m-6 h-40", className)} />;
    return (
      <Card className={className}>
        <CardContent className="p-4">
          <Skeleton className="h-24 w-full" />
        </CardContent>
      </Card>
    );
  }
  if (shown.length === 0) return null;

  return (
    <div className={cn(embedded ? "flex min-h-0 flex-1 flex-col" : "space-y-4", className)}>
      {shown.map((target) => (
        <TargetCard
          key={target.environment_id}
          repositoryId={repositoryId}
          target={target}
          pendingOnly={pendingOnly}
          embedded={embedded}
          onChanged={(next) => {
            setTargets((prev) => prev?.map((p) => (p.environment_id === next.environment_id ? next : p)) ?? prev);
            if (!next.error && !next.vars.some(pending)) onSettled?.();
          }}
        />
      ))}
    </div>
  );
}

interface TargetCardProps {
  repositoryId: string;
  target: ComponentEnvStatus;
  pendingOnly: boolean;
  embedded: boolean;
  onChanged: (next: ComponentEnvStatus) => void;
}

function TargetCard({ repositoryId, target, pendingOnly, embedded, onChanged }: TargetCardProps) {
  const { t } = useI18n();
  const [drafts, setDrafts] = useState<Record<string, Draft>>({});
  const [added, setAdded] = useState<AddedVar[]>([]);
  const [saving, setSaving] = useState(false);
  const [redeploying, setRedeploying] = useState(false);
  const [wroteSinceDeploy, setWroteSinceDeploy] = useState(false);
  const provider = t(`cloud.providers.${target.provider}`);
  const previews = target.capabilities.targets.includes("preview");
  // Waiting on a person first: the rows the dialog was opened for.
  const rows = (pendingOnly ? target.vars.filter(pending) : target.vars)
    .slice()
    .sort((a, b) => Number(pending(b)) - Number(pending(a)));
  const counts = {
    missing: target.vars.filter(pending).length,
    auto: target.vars.filter((v) => v.action === "auto").length,
    set: target.vars.filter((v) => !pending(v) && v.action !== "auto").length,
  };

  const setDraft = (name: string, patch: Partial<Draft>) =>
    setDrafts((prev) => ({ ...prev, [name]: { ...(prev[name] ?? { value: "" }), ...patch } }));

  const clearDraft = (name: string) =>
    setDrafts((prev) => {
      const next = { ...prev };
      delete next[name];
      return next;
    });

  const inputs = (): EnvInput[] => [
    ...target.vars.flatMap((v) => {
      const d = drafts[v.name];
      const input = d ? inputFor(v.name, d) : null;
      return input && (input.kind ?? v.kind) ? [input] : [];
    }),
    ...added.flatMap((a) => {
      const name = a.name.trim();
      if (!name || !a.kind) return [];
      return [{ name, kind: a.kind, value: a.value || undefined }];
    }),
  ];

  const submit = async (vars: EnvInput[]) => {
    setSaving(true);
    try {
      const next = await api.applyEnvRequirements(repositoryId, target.component_id, vars);
      setDrafts({});
      setAdded([]);
      onChanged(next);
      const left = next.vars.filter(pending).map((v) => v.name);
      if (next.error) toast.error(next.error);
      else if (left.length > 0) toast.warning(t("cloud.envVars.stillMissing", { names: left.join(", ") }));
      else toast.success(t("cloud.envVars.applied", { provider }));
      if (vars.length > 0 && !target.capabilities.writes_roll_out) setWroteSinceDeploy(true);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("cloud.envVars.applyFailed"));
    } finally {
      setSaving(false);
    }
  };

  const redeploy = async () => {
    setRedeploying(true);
    try {
      await api.redeployForEnvRequirements(repositoryId, target.component_id);
      setWroteSinceDeploy(false);
      toast.success(t("cloud.envVars.redeployed"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("cloud.envVars.redeployFailed"));
    } finally {
      setRedeploying(false);
    }
  };

  const pendingInputs = inputs();

  const heading = (
    <div className="space-y-1.5">
      <div className="flex flex-wrap items-center gap-2">
        {!embedded && <KeyRound className="h-4 w-4 text-muted-foreground" />}
        {!embedded && <span className="text-body font-semibold">{t("cloud.envVars.title")}</span>}
        <Badge variant="outline" className="gap-1.5">
          <ProviderIcon provider={target.provider} className="h-3 w-3" />
          {target.resource_name || target.component_name}
        </Badge>
        {counts.missing > 0 && <Badge variant="warning">{t("cloud.envVars.summaryMissing", { count: counts.missing })}</Badge>}
        {counts.auto > 0 && <Badge variant="info">{t("cloud.envVars.summaryAuto", { count: counts.auto })}</Badge>}
        {counts.set > 0 && <Badge variant="success">{t("cloud.envVars.summarySet", { count: counts.set })}</Badge>}
      </div>
      <p className="text-caption text-muted-foreground">{t("cloud.envVars.subtitle", { provider })}</p>
    </div>
  );

  const body = (
    <>
      {target.error && (
        <Notice variant="error" title={t("cloud.envVars.providerError", { provider })}>
          {target.error}
        </Notice>
      )}
      {target.capabilities.overwritten_on_deploy && <Notice variant="warning" title={t("cloud.envVars.overwrittenOnDeploy")} />}
      {rows.length === 0 ? (
        <p className="text-caption text-muted-foreground">{t("cloud.envVars.none")}</p>
      ) : (
        <div className="divide-y divide-border overflow-hidden rounded-lg border border-border">
          {rows.map((v) => (
            <EnvVarRow
              key={v.name}
              v={v}
              previews={previews}
              draft={drafts[v.name]}
              onDraft={(patch) => setDraft(v.name, patch)}
              onCancel={() => clearDraft(v.name)}
            />
          ))}
        </div>
      )}
      {!pendingOnly && (
        <div className="space-y-2">
          {added.map((a, i) => (
            <AddedVarRow
              key={i}
              value={a}
              onChange={(next) => setAdded((prev) => prev.map((p, j) => (j === i ? next : p)))}
              onRemove={() => setAdded((prev) => prev.filter((_, j) => j !== i))}
            />
          ))}
          <Button size="sm" variant="ghost" onClick={() => setAdded((prev) => [...prev, { name: "", value: "" }])}>
            <Plus className="mr-1 h-3.5 w-3.5" />
            {t("cloud.envVars.addVar")}
          </Button>
        </div>
      )}
    </>
  );

  const actions = (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center gap-2">
        <Button size="sm" variant="outline" disabled={saving} onClick={() => void submit([])}>
          <RefreshCw className={cn("mr-1.5 h-3.5 w-3.5", saving && "animate-spin")} />
          {t("cloud.envVars.recheck")}
        </Button>
        {!target.capabilities.writes_roll_out && (
          <Button size="sm" variant={wroteSinceDeploy ? "secondary" : "ghost"} disabled={redeploying} onClick={() => void redeploy()}>
            <Rocket className="mr-1.5 h-3.5 w-3.5" />
            {t("cloud.envVars.redeploy")}
          </Button>
        )}
        <Button
          size="sm"
          className="ml-auto"
          disabled={saving || pendingInputs.length === 0}
          onClick={() => void submit(pendingInputs)}
        >
          {saving ? t("cloud.envVars.applying") : t("cloud.envVars.apply")}
        </Button>
      </div>
      {wroteSinceDeploy && <p className="text-caption text-muted-foreground">{t("cloud.envVars.redeployHint")}</p>}
    </div>
  );

  if (embedded) {
    return (
      <>
        <div className="min-h-0 flex-1 space-y-4 overflow-y-auto px-6 py-4">
          {heading}
          {body}
        </div>
        <div className="border-t border-border px-6 py-4">{actions}</div>
      </>
    );
  }

  return (
    <Card>
      <CardHeader className="pb-3">{heading}</CardHeader>
      <CardContent className="space-y-4">
        {body}
        {actions}
      </CardContent>
    </Card>
  );
}

function Presence({ label, present }: { label: string; present: boolean }) {
  const { t } = useI18n();
  return (
    <span
      className="inline-flex items-center gap-1.5 text-caption text-muted-foreground"
      title={`${label}: ${present ? t("cloud.envVars.set") : t("cloud.envVars.missing")}`}
    >
      <span className={cn("h-2 w-2 rounded-full", present ? "bg-success" : "bg-warning")} aria-hidden />
      <span className={cn(!present && "text-foreground")}>{label}</span>
      <span className="sr-only">{present ? t("cloud.envVars.set") : t("cloud.envVars.missing")}</span>
    </span>
  );
}

interface EnvVarRowProps {
  v: EnvVarView;
  previews: boolean;
  draft?: Draft;
  onDraft: (patch: Partial<Draft>) => void;
  onCancel: () => void;
}

function KindSelect({ value, onChange }: { value?: EnvVarKind; onChange: (k: EnvVarKind) => void }) {
  const { t } = useI18n();
  return (
    <Select value={value ?? ""} onValueChange={(k) => onChange(k as EnvVarKind)}>
      <SelectTrigger className="h-9 w-full" aria-label={t("cloud.envVars.kindLabel")}>
        <SelectValue placeholder={t("cloud.envVars.kindPlaceholder")} />
      </SelectTrigger>
      <SelectContent>
        {KINDS.map((k) => (
          <SelectItem key={k} value={k}>
            {t(`cloud.envVars.kinds.${k}`)}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

function ValueInput({ name, kind, value, onChange, changing }: {
  name: string;
  kind?: EnvVarKind;
  value: string;
  onChange: (v: string) => void;
  changing?: boolean;
}) {
  const { t } = useI18n();
  const placeholder =
    kind === "human_bcrypt"
      ? t("cloud.envVars.passwordPlaceholder")
      : kind === "human_secret"
        ? changing
          ? t("cloud.envVars.newValuePlaceholder")
          : t("cloud.envVars.secretPlaceholder")
        : t("cloud.envVars.valuePlaceholder");
  return (
    <Input
      className="h-9 w-full"
      type={kind === "value" ? "text" : "password"}
      autoComplete="off"
      spellCheck={false}
      placeholder={placeholder}
      aria-label={`${name} ${placeholder}`}
      value={value}
      onChange={(e) => onChange(e.target.value)}
    />
  );
}

function EnvVarRow({ v, previews, draft, onDraft, onCancel }: EnvVarRowProps) {
  const { t } = useI18n();
  const kind = draft?.kind ?? v.kind;
  const classify = v.action === "classify";
  const pendingRow = classify || v.action === "human";
  const editing = pendingRow || Boolean(draft?.editing);
  const takesValue = kind === "value" ? classify || Boolean(draft?.editing) : kind === "human_secret" || kind === "human_bcrypt";

  return (
    <div className="grid gap-3 px-4 py-3 sm:grid-cols-[minmax(0,1fr)_auto_16rem] sm:items-start">
      <div className="min-w-0 space-y-0.5 sm:pt-1.5">
        <div className="truncate font-mono text-caption font-medium">{v.name}</div>
        {v.description ? (
          <p className="text-caption text-muted-foreground">{v.description}</p>
        ) : (
          v.example_path && (
            <p className="text-caption text-muted-foreground">{t("cloud.envVars.exampleFrom", { path: v.example_path })}</p>
          )
        )}
      </div>
      <div className="flex flex-wrap items-center gap-3 sm:pt-2">
        {kind === "optional" ? (
          <Badge variant="secondary" className="text-micro">
            {t("cloud.envVars.notNeeded")}
          </Badge>
        ) : (
          <>
            <Presence label={t("cloud.envVars.production")} present={v.production} />
            {previews && <Presence label={t("cloud.envVars.preview")} present={v.preview} />}
          </>
        )}
      </div>
      <div className="flex flex-col gap-2">
        {editing ? (
          <>
            {(classify || draft?.editing) && (
              <KindSelect
                value={draft?.kind ?? (classify ? undefined : v.kind)}
                onChange={(k) =>
                  onDraft({
                    kind: k,
                    regenerate: false,
                    value: k === "value" ? draft?.value || v.value || v.example_value || "" : "",
                  })
                }
              />
            )}
            {takesValue && (
              <ValueInput
                name={v.name}
                kind={kind}
                value={draft?.value ?? ""}
                changing={Boolean(draft?.editing)}
                onChange={(value) => onDraft({ value })}
              />
            )}
            {draft?.editing && (
              <div className="flex flex-wrap gap-2">
                {kind === "generated" && (
                  <Button
                    size="sm"
                    variant={draft.regenerate ? "default" : "outline"}
                    onClick={() => onDraft({ regenerate: !draft.regenerate })}
                    title={t("cloud.envVars.regenerateOn")}
                  >
                    <RefreshCw className="mr-1 h-3.5 w-3.5" />
                    {t("cloud.envVars.regenerate")}
                  </Button>
                )}
                <Button size="sm" variant="ghost" onClick={onCancel}>
                  {t("cloud.envVars.cancelEdit")}
                </Button>
              </div>
            )}
          </>
        ) : (
          <div className="flex items-center justify-end gap-2 sm:pt-1">
            {v.action === "auto" ? (
              <Badge variant="info" className="text-micro">
                {t("cloud.envVars.autoFill")}
              </Badge>
            ) : (
              kind && <span className="text-caption text-muted-foreground">{t(`cloud.envVars.kinds.${kind}`)}</span>
            )}
            <Button
              size="sm"
              variant="ghost"
              className="h-7"
              onClick={() => onDraft({ editing: true, value: v.kind === "value" ? v.value ?? "" : "" })}
            >
              {t("cloud.envVars.edit")}
            </Button>
          </div>
        )}
      </div>
    </div>
  );
}

function AddedVarRow({ value, onChange, onRemove }: {
  value: AddedVar;
  onChange: (next: AddedVar) => void;
  onRemove: () => void;
}) {
  const { t } = useI18n();
  const takesValue = value.kind === "value" || value.kind === "human_secret" || value.kind === "human_bcrypt";
  return (
    <div className="grid gap-2 rounded-lg border border-dashed border-border px-4 py-3 sm:grid-cols-[minmax(0,1fr)_14rem_minmax(0,1fr)_auto] sm:items-center">
      <Input
        className="h-9 font-mono"
        placeholder={t("cloud.envVars.addVarName")}
        aria-label={t("cloud.envVars.addVarName")}
        autoComplete="off"
        spellCheck={false}
        value={value.name}
        onChange={(e) => onChange({ ...value, name: e.target.value.toUpperCase().replace(/[^A-Z0-9_]/g, "_") })}
      />
      <KindSelect value={value.kind} onChange={(kind) => onChange({ ...value, kind, value: "" })} />
      {takesValue ? (
        <ValueInput
          name={value.name || t("cloud.envVars.addVarName")}
          kind={value.kind}
          value={value.value}
          onChange={(v) => onChange({ ...value, value: v })}
        />
      ) : (
        <span className="hidden sm:block" />
      )}
      <Button size="sm" variant="ghost" onClick={onRemove} aria-label={t("cloud.envVars.cancelEdit")}>
        <X className="h-3.5 w-3.5" />
      </Button>
    </div>
  );
}
