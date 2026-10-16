import { Gauge, Sparkles, X } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { Link } from "react-router-dom";
import {
  ALERT_CHANNEL_KINDS,
  ALERT_CHANNELS_AVAILABLE,
  ALERT_RULE_OPS,
  api,
  dataSourceFieldErrors,
  type AlertChannelKind,
  type AlertRule,
  type AlertRuleOp,
  type AlertRuleSuggestion,
  type DeployEnvironment,
  type IncidentSeverity,
} from "@/api";
import { FormDialog } from "@/components/admin/FormDialog";
import {
  ALERT_SEVERITIES,
  conditionText,
  draftErrorCount,
  draftFromRule,
  draftFromSuggestion,
  emptyRuleDraft,
  mapServerFields,
  opIsChange,
  opNeedsThreshold,
  ruleInput,
  validateRuleDraft,
  type AlertRuleDraft,
  type DraftErrors,
  type DraftField,
  type RuleSourceMode,
} from "@/components/operations/alertRules/alertRules";
import { BacktestPanel } from "@/components/operations/alertRules/BacktestPanel";
import { DurationInput } from "@/components/operations/alertRules/DurationInput";
import { entityPath } from "@/components/operations/console/opsEntities";
import { signalLabel, signalValueParts } from "@/components/operations/console/signalFormat";
import { IssueSeverity } from "@/components/operations/issues/IssueSeverity";
import { OPS_ENVIRONMENTS, useOpsHref } from "@/components/operations/shell/OpsContext";
import type { OpsEntity } from "@/components/operations/shell/useOpsEntities";
import { metricsSources } from "@/components/operations/signals/signalBinding";
import { queryPlaceholder } from "@/components/operations/sources/dataSourceSchema";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { useDataSources } from "@/hooks/useDataSources";
import { useI18n } from "@/hooks/useI18n";
import { useComponentSignals } from "@/hooks/useOpsSignals";
import { intlLocale } from "@/lib/languages";

export interface AlertRuleEditorProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** The entities a new rule can watch; ignored with `lockedEntity`. */
  entities: OpsEntity[];
  /** An entity's own Alerts page: the rule is that entity's. */
  lockedEntity?: OpsEntity;
  defaultEnv: DeployEnvironment;
  /** The rule to edit; absent, a new one. */
  rule?: AlertRule | null;
  /** Opens with the server's suggestions for the entity already asked for. */
  suggest?: boolean;
  /** Catalog units by signal id, for a signal the component list has not answered yet. */
  catalogUnits: ReadonlyMap<string, string>;
  onSaved: (rule: AlertRule) => void;
}

/** Writes one alert rule: what it watches (a bound signal, or a data source's
 * own query), the condition, severity, where it notifies, whether it opens an
 * issue, and a backtest over past days before it is saved. Suggestions fill
 * it one at a time; each is reviewed and saved on its own. Mount it only
 * while open — its state starts from the props. */
export function AlertRuleEditor({
  open,
  onOpenChange,
  entities,
  lockedEntity,
  defaultEnv,
  rule,
  suggest = false,
  catalogUnits,
  onSaved,
}: AlertRuleEditorProps) {
  const { t, lang } = useI18n();
  const e = (key: string, params?: Record<string, string | number>) => t(`operations.alertRules.editor.${key}`, params);
  const href = useOpsHref();
  const ctx = useMemo(() => ({ locale: intlLocale(lang), t }), [lang, t]);
  const editing = Boolean(rule);
  const [draft, setDraft] = useState<AlertRuleDraft>(() =>
    rule ? draftFromRule(rule) : emptyRuleDraft(lockedEntity?.id ?? (entities.length === 1 ? entities[0].id : ""), defaultEnv),
  );
  const [revision, setRevision] = useState(0);
  const [errors, setErrors] = useState<DraftErrors>({});
  const [saveError, setSaveError] = useState("");
  const [saving, setSaving] = useState(false);
  const [suggestions, setSuggestions] = useState<AlertRuleSuggestion[] | null>(null);
  const [suggesting, setSuggesting] = useState(false);
  const [suggestError, setSuggestError] = useState("");
  const [activeIndex, setActiveIndex] = useState(-1);

  const entity = lockedEntity ?? entities.find((x) => x.id === draft.componentId);
  const componentSignals = useComponentSignals(draft.componentId, draft.environment, open && draft.componentId !== "");
  const bound = useMemo(() => (componentSignals.data?.signals ?? []).filter((s) => s.binding), [componentSignals.data]);
  const { sources } = useDataSources();
  const metrics = useMemo(() => metricsSources(sources), [sources]);
  const source = metrics.find((s) => s.id === draft.dataSourceId);

  const unit =
    draft.mode === "signal" ? (bound.find((s) => s.signal === draft.signal)?.unit ?? catalogUnits.get(draft.signal)) : undefined;
  const thresholdUnit = opIsChange(draft.op) ? "%" : unit ? signalValueParts(0, unit, ctx).unit : "";
  const condition = conditionText(
    {
      signal: draft.mode === "signal" ? draft.signal : "",
      query: draft.mode === "custom" ? draft.query : "",
      condition: {
        op: draft.op,
        threshold: opNeedsThreshold(draft.op) && draft.threshold.trim() !== "" ? Number(draft.threshold.replace(",", ".")) : null,
        for_seconds: Number.isFinite(draft.forSeconds) ? draft.forSeconds : 0,
        window_seconds: Number.isFinite(draft.windowSeconds) ? draft.windowSeconds : 0,
      },
    },
    unit,
    ctx,
  );
  const local = validateRuleDraft(draft);
  const ready = draftErrorCount(local) === 0;

  const change = (patch: Partial<AlertRuleDraft>) => {
    setDraft((d) => ({ ...d, ...patch }));
    setErrors({});
    setSaveError("");
  };

  const load = (next: AlertRuleDraft) => {
    setDraft(next);
    setRevision((r) => r + 1);
    setErrors({});
    setSaveError("");
  };

  const askSuggestions = async (componentId: string, environment: DeployEnvironment) => {
    if (!componentId) return;
    setSuggesting(true);
    setSuggestError("");
    try {
      const answer = (await api.suggestAlertRules(componentId, environment)).rules ?? [];
      setSuggestions(answer);
      if (answer.length > 0) {
        load(draftFromSuggestion(answer[0]));
        setActiveIndex(0);
      }
    } catch (err) {
      setSuggestError(err instanceof Error ? err.message : t("common.actionFailed"));
    } finally {
      setSuggesting(false);
    }
  };

  const asked = useRef(false);
  const askOnOpen = useRef(askSuggestions);
  askOnOpen.current = askSuggestions;
  useEffect(() => {
    if (!suggest || asked.current || editing || !draft.componentId) return;
    asked.current = true;
    void askOnOpen.current(draft.componentId, draft.environment);
  }, [suggest, editing, draft.componentId, draft.environment]);

  const pickSuggestion = (index: number) => {
    const s = suggestions?.[index];
    if (!s) return;
    load(draftFromSuggestion(s));
    setActiveIndex(index);
  };

  // What is left once one is saved or skipped: the next one, else an empty rule.
  const advanceFrom = (index: number) => {
    const rest = (suggestions ?? []).filter((_, i) => i !== index);
    setSuggestions(rest);
    if (rest.length === 0) {
      setActiveIndex(-1);
      return false;
    }
    const next = Math.min(index, rest.length - 1);
    load(draftFromSuggestion(rest[next]));
    setActiveIndex(next);
    return true;
  };

  const dropSuggestion = (index: number) => {
    if (advanceFrom(index)) return;
    if (index === activeIndex || activeIndex < 0) load(emptyRuleDraft(draft.componentId, draft.environment));
  };

  const reviewing = suggestions !== null && activeIndex >= 0 && activeIndex < suggestions.length;

  const save = async () => {
    const found = validateRuleDraft(draft);
    setErrors(found);
    setSaveError("");
    if (draftErrorCount(found) > 0) return;
    setSaving(true);
    try {
      const body = ruleInput(draft, condition);
      const saved = rule ? await api.updateAlertRule(rule.id, body) : await api.createAlertRule(body);
      onSaved(saved);
      if (reviewing && advanceFrom(activeIndex)) return;
      onOpenChange(false);
    } catch (err) {
      const fields = dataSourceFieldErrors(err);
      if (fields) {
        const { errors: inline, rest } = mapServerFields(fields);
        setErrors(inline);
        setSaveError(rest.map(([k, v]) => `${k}: ${errorText(v)}`).join(" "));
      } else {
        setSaveError(err instanceof Error ? err.message : t("common.actionFailed"));
      }
    } finally {
      setSaving(false);
    }
  };

  const errorText = (code: string) => {
    const key = `operations.alertRules.errors.${code}`;
    const text = t(key);
    return text === key ? code : text;
  };

  const shown = (field: DraftField) => errors[field];
  const fieldError = (field: DraftField) =>
    shown(field) ? (
      <p id={`alert-${field}-error`} className="text-caption text-destructive">
        {errorText(shown(field)!)}
      </p>
    ) : null;

  const setEntity = (componentId: string) => {
    change({ componentId, signal: "" });
    setSuggestions(null);
    setActiveIndex(-1);
  };

  const setEnvironment = (environment: DeployEnvironment) => {
    change({ environment, signal: "" });
    setSuggestions(null);
    setActiveIndex(-1);
  };

  const toggleChannel = (kind: AlertChannelKind, on: boolean) =>
    change({ channels: on ? [...new Set([...draft.channels, kind])] : draft.channels.filter((c) => c !== kind) });

  const envLabel = t(`cloud.environments.${draft.environment}`);
  const title = editing ? e("editTitle") : e("newTitle");

  return (
    <FormDialog
      open={open}
      onOpenChange={(next) => !saving && onOpenChange(next)}
      title={title}
      description={e("description")}
      className="sm:max-w-3xl"
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
            {t("common.cancel")}
          </Button>
          <Button onClick={() => void save()} disabled={saving}>
            {saving ? e("saving") : reviewing ? e("saveSuggestion") : e("save")}
          </Button>
        </>
      }
    >
      {saveError && (
        <Notice variant="error" title={e("saveFailed")}>
          <p className="break-words">{saveError}</p>
        </Notice>
      )}

      <div className="grid gap-4 sm:grid-cols-[1fr_12rem_auto] sm:items-end">
        <div className="space-y-1.5">
          <Label htmlFor="alert-entity">{e("entity")}</Label>
          {lockedEntity || editing ? (
            <p id="alert-entity" className="flex h-9 items-center truncate text-body font-medium">
              {entity?.name ?? draft.componentId}
            </p>
          ) : (
            <Select value={draft.componentId} onValueChange={setEntity} disabled={saving}>
              <SelectTrigger id="alert-entity" aria-invalid={shown("componentId") ? true : undefined}>
                <SelectValue placeholder={e("entityPlaceholder")} />
              </SelectTrigger>
              <SelectContent>
                {entities.map((x) => (
                  <SelectItem key={x.id} value={x.id}>
                    {x.name}
                    <span className="ml-2 text-muted-foreground">{x.repositoryName}</span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
          {fieldError("componentId")}
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="alert-env">{e("environment")}</Label>
          <Select value={draft.environment} onValueChange={(v) => setEnvironment(v as DeployEnvironment)} disabled={saving || editing}>
            <SelectTrigger id="alert-env">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {OPS_ENVIRONMENTS.map((env) => (
                <SelectItem key={env} value={env}>
                  {t(`cloud.environments.${env}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        {!editing && (
          <Button
            variant="outline"
            onClick={() => void askSuggestions(draft.componentId, draft.environment)}
            disabled={!draft.componentId || suggesting || saving}
          >
            <Sparkles aria-hidden />
            {suggesting ? e("suggesting") : e("suggest")}
          </Button>
        )}
      </div>

      {suggestError && (
        <Notice variant="error" title={e("suggestFailed")}>
          <p className="break-words">{suggestError}</p>
        </Notice>
      )}
      {suggestions && suggestions.length === 0 && !suggesting && <Notice variant="info" title={e("suggestNone")} />}
      {suggestions && suggestions.length > 0 && (
        <section className="space-y-2 rounded-md border border-info/30 bg-info/5 p-3" aria-label={e("suggestionsLabel")}>
          <p className="text-caption font-medium">{e("suggestionsTitle", { count: suggestions.length })}</p>
          <p className="text-caption text-muted-foreground">{e("suggestionsHint")}</p>
          <ul className="flex flex-wrap gap-1.5">
            {suggestions.map((s, i) => {
              const active = i === activeIndex;
              return (
                <li key={`${s.signal}:${s.condition?.op}:${s.name}`} className="flex items-center">
                  <Button
                    size="sm"
                    variant={active ? "secondary" : "outline"}
                    aria-pressed={active}
                    className="h-7 rounded-r-none"
                    onClick={() => pickSuggestion(i)}
                  >
                    {s.name || signalLabel(t, s.signal)}
                  </Button>
                  <Button
                    size="sm"
                    variant="outline"
                    className="h-7 rounded-l-none border-l-0 px-1.5"
                    aria-label={e("skipSuggestion", { name: s.name || signalLabel(t, s.signal) })}
                    onClick={() => dropSuggestion(i)}
                  >
                    <X aria-hidden />
                  </Button>
                </li>
              );
            })}
          </ul>
        </section>
      )}

      <fieldset className="space-y-3">
        <legend className="mb-1 text-body font-semibold">{e("watch")}</legend>
        <Tabs value={draft.mode} onValueChange={(mode) => change({ mode: mode as RuleSourceMode })} variant="pill" className="w-fit">
          <TabsList aria-label={e("sourceMode")}>
            <TabsTrigger value="signal">{e("modes.signal")}</TabsTrigger>
            <TabsTrigger value="custom">{e("modes.custom")}</TabsTrigger>
          </TabsList>
        </Tabs>

        {draft.mode === "signal" ? (
          <div className="space-y-1.5">
            <Label htmlFor="alert-signal">{e("signal")}</Label>
            {draft.componentId && !componentSignals.loading && bound.length === 0 ? (
              <Notice variant="info" title={e("noBoundSignals", { name: entity?.name ?? "", env: envLabel })}>
                {entity && (
                  <Button variant="link" size="sm" className="h-auto p-0" asChild>
                    <Link to={href(entityPath(entity.id, "signals"))}>
                      <Gauge aria-hidden />
                      {e("bindSignals")}
                    </Link>
                  </Button>
                )}
              </Notice>
            ) : (
              <Select value={draft.signal} onValueChange={(signal) => change({ signal })} disabled={saving || !draft.componentId}>
                <SelectTrigger id="alert-signal" aria-invalid={shown("signal") ? true : undefined}>
                  <SelectValue placeholder={draft.componentId ? e("signalPlaceholder") : e("pickEntityFirst")} />
                </SelectTrigger>
                <SelectContent>
                  {bound.map((s) => (
                    <SelectItem key={s.signal} value={s.signal}>
                      {signalLabel(t, s.signal)}
                      <span className="ml-2 font-mono text-micro text-muted-foreground">{s.signal}</span>
                    </SelectItem>
                  ))}
                  {draft.signal && !bound.some((s) => s.signal === draft.signal) && (
                    <SelectItem value={draft.signal}>
                      {signalLabel(t, draft.signal)}
                      <span className="ml-2 text-micro text-muted-foreground">{e("notBound")}</span>
                    </SelectItem>
                  )}
                </SelectContent>
              </Select>
            )}
            {fieldError("signal")}
          </div>
        ) : (
          <div className="space-y-3">
            <div className="space-y-1.5">
              <Label htmlFor="alert-source">{e("dataSource")}</Label>
              <Select value={draft.dataSourceId} onValueChange={(dataSourceId) => change({ dataSourceId })} disabled={saving}>
                <SelectTrigger id="alert-source" aria-invalid={shown("dataSourceId") ? true : undefined}>
                  <SelectValue placeholder={metrics.length === 0 ? e("noSources") : e("dataSourcePlaceholder")} />
                </SelectTrigger>
                <SelectContent>
                  {metrics.map((s) => (
                    <SelectItem key={s.id} value={s.id}>
                      {s.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {fieldError("dataSourceId")}
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="alert-query">{e("query")}</Label>
              <Textarea
                id="alert-query"
                value={draft.query}
                onChange={(ev) => change({ query: ev.target.value })}
                placeholder={source ? queryPlaceholder(t, source.kind) : undefined}
                rows={3}
                spellCheck={false}
                className="font-mono text-caption"
                aria-invalid={shown("query") ? true : undefined}
                disabled={saving}
              />
              {fieldError("query")}
            </div>
          </div>
        )}
      </fieldset>

      <fieldset className="space-y-3">
        <legend className="mb-1 text-body font-semibold">{e("conditionTitle")}</legend>
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-1.5">
            <Label htmlFor="alert-op">{e("op")}</Label>
            <Select value={draft.op} onValueChange={(op) => change({ op: op as AlertRuleOp })} disabled={saving}>
              <SelectTrigger id="alert-op">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {ALERT_RULE_OPS.map((op) => (
                  <SelectItem key={op} value={op}>
                    {e(`ops.${op}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          {opNeedsThreshold(draft.op) && (
            <div className="space-y-1.5">
              <Label htmlFor="alert-threshold">{e("threshold")}</Label>
              <div className="flex items-center gap-2">
                <Input
                  id="alert-threshold"
                  inputMode="decimal"
                  value={draft.threshold}
                  onChange={(ev) => change({ threshold: ev.target.value })}
                  aria-invalid={shown("threshold") ? true : undefined}
                  aria-describedby={opIsChange(draft.op) ? "alert-threshold-hint" : undefined}
                  className="w-32 font-mono tabular-nums"
                  disabled={saving}
                />
                {thresholdUnit && <span className="text-caption text-muted-foreground">{thresholdUnit}</span>}
              </div>
              {opIsChange(draft.op) && (
                <p id="alert-threshold-hint" className="text-micro text-muted-foreground">
                  {e("changeHint")}
                </p>
              )}
              {fieldError("threshold")}
            </div>
          )}
          <div className="space-y-1.5">
            <Label htmlFor="alert-for">{draft.op === "no_data" ? e("noDataFor") : e("for")}</Label>
            <DurationInput
              key={`for:${revision}`}
              id="alert-for"
              label={e("for")}
              seconds={draft.forSeconds}
              onChange={(forSeconds) => change({ forSeconds })}
              invalid={Boolean(shown("forSeconds"))}
              disabled={saving}
              describedBy="alert-for-hint"
            />
            <p id="alert-for-hint" className="text-micro text-muted-foreground">
              {e("forHint")}
            </p>
            {fieldError("forSeconds")}
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="alert-window">{e("window")}</Label>
            <DurationInput
              key={`window:${revision}`}
              id="alert-window"
              label={e("window")}
              seconds={draft.windowSeconds}
              onChange={(windowSeconds) => change({ windowSeconds })}
              invalid={Boolean(shown("windowSeconds"))}
              disabled={saving}
              describedBy="alert-window-hint"
            />
            <p id="alert-window-hint" className="text-micro text-muted-foreground">
              {e("windowHint")}
            </p>
            {fieldError("windowSeconds")}
          </div>
        </div>
        <p className="rounded-md bg-muted/50 px-3 py-2 text-caption" aria-live="polite">
          <span className="text-muted-foreground">{e("readsAs")} </span>
          <span className="font-medium">{condition}</span>
        </p>
      </fieldset>

      <BacktestPanel draft={draft} unit={unit} fallbackName={condition} ready={ready} />

      <fieldset className="space-y-3">
        <legend className="mb-1 text-body font-semibold">{e("notifyTitle")}</legend>
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-1.5">
            <Label htmlFor="alert-name">{e("name")}</Label>
            <Input
              id="alert-name"
              value={draft.name}
              onChange={(ev) => change({ name: ev.target.value })}
              placeholder={condition}
              aria-invalid={shown("name") ? true : undefined}
              disabled={saving}
            />
            {fieldError("name")}
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="alert-severity">{e("severity")}</Label>
            <Select value={draft.severity} onValueChange={(v) => change({ severity: v as IncidentSeverity })} disabled={saving}>
              <SelectTrigger id="alert-severity">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {ALERT_SEVERITIES.map((s) => (
                  <SelectItem key={s} value={s}>
                    <IssueSeverity severity={s} />
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </div>

        <div className="space-y-2" role="group" aria-labelledby="alert-channels-label">
          <p id="alert-channels-label" className="text-body font-medium">
            {e("channels")}
          </p>
          <ul className="grid gap-2 sm:grid-cols-3">
            {ALERT_CHANNEL_KINDS.map((kind) => {
              const available = ALERT_CHANNELS_AVAILABLE.includes(kind);
              const id = `alert-channel-${kind}`;
              return (
                <li key={kind} className="flex items-center gap-2">
                  <Checkbox
                    id={id}
                    checked={available && draft.channels.includes(kind)}
                    disabled={!available || saving || draft.openIncident}
                    onCheckedChange={(checked) => toggleChannel(kind, checked === true)}
                  />
                  <Label htmlFor={id} className={available ? "font-normal" : "font-normal text-muted-foreground"}>
                    {e(`channelKinds.${kind}`)}
                  </Label>
                  {!available && (
                    <Badge variant="outline" className="border-dashed px-1.5 py-0 text-micro text-muted-foreground">
                      {e("comingLater")}
                    </Badge>
                  )}
                </li>
              );
            })}
          </ul>
          {draft.openIncident && <p className="text-micro text-muted-foreground">{e("channelsViaIssue")}</p>}
          {fieldError("channels")}
        </div>

        <div className="flex items-center justify-between gap-3 rounded-md border border-border p-3">
          <div>
            <Label htmlFor="alert-open-incident" className="text-body">
              {e("openIncident")}
            </Label>
            <p className="text-micro text-muted-foreground">{e("openIncidentHelp")}</p>
          </div>
          <Switch id="alert-open-incident" checked={draft.openIncident} onCheckedChange={(openIncident) => change({ openIncident })} disabled={saving} />
        </div>
        <div className="flex items-center justify-between gap-3 rounded-md border border-border p-3">
          <div>
            <Label htmlFor="alert-enabled" className="text-body">
              {e("enabled")}
            </Label>
            <p className="text-micro text-muted-foreground">{e("enabledHelp")}</p>
          </div>
          <Switch id="alert-enabled" checked={draft.enabled} onCheckedChange={(enabled) => change({ enabled })} disabled={saving} />
        </div>
      </fieldset>
    </FormDialog>
  );
}
