import { useMemo, useState } from "react";
import {
  ERROR_ACTIONS,
  ERROR_CATEGORIES,
  api,
  dataSourceFieldErrors,
  isTooManyDefinitionsError,
  type ErrorAction,
  type ErrorCategory,
  type ErrorDefinition,
  type IncidentSeverity,
} from "@/api";
import { KeyValueEditor } from "@/components/admin/KeyValueEditor";
import { FormDialog } from "@/components/admin/FormDialog";
import { ALERT_SEVERITIES } from "@/components/operations/alertRules/alertRules";
import { DurationInput } from "@/components/operations/alertRules/DurationInput";
import { ALL } from "@/components/operations/console/opsEntities";
import {
  draftErrorCount,
  draftFromDefinition,
  emptyDefinitionDraft,
  isThresholded,
  mapDefinitionFields,
  switchAction,
  validateDefinitionDraft,
  definitionInput,
  type DefinitionDraft,
  type DraftErrors,
  type DraftField,
} from "@/components/operations/errorDefinitions/errorDefinitions";
import { RegexTestBox } from "@/components/operations/errorDefinitions/RegexTestBox";
import { IssueSeverity } from "@/components/operations/issues/IssueSeverity";
import type { OpsEntity, OpsRepository } from "@/components/operations/shell/useOpsEntities";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";

// Every field the form shows an error under; a refusal of any other goes in
// the form's own error.
const RENDERED: ReadonlySet<DraftField> = new Set<DraftField>([
  "repositoryId",
  "componentId",
  "name",
  "match",
  "fingerprint",
  "messageRegex",
  "exceptionType",
  "endpoint",
  "status",
  "attributes",
  "category",
  "severity",
  "action",
  "threshold",
  "thresholdCount",
  "thresholdWindow",
  "rationale",
]);

export interface ErrorDefinitionEditorProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  repositories: OpsRepository[];
  entities: OpsEntity[];
  /** The definition to edit; absent, a new one. */
  definition?: ErrorDefinition | null;
  /** A new definition's starting point (the page's filters, an error group). */
  initial?: DefinitionDraft;
  /** What the pattern test box starts with (an error group's message). */
  sample?: string;
  onSaved: (definition: ErrorDefinition) => void;
}

/** Writes one error definition: what it matches (every field set must hold),
 * what it means, and what a match sets off. A suggestion can be saved and
 * accepted in one go. Mount it only while open — its state starts from the
 * props. */
export function ErrorDefinitionEditor({
  open,
  onOpenChange,
  repositories,
  entities,
  definition,
  initial,
  sample: initialSample = "",
  onSaved,
}: ErrorDefinitionEditorProps) {
  const { t } = useI18n();
  const e = (key: string, params?: Record<string, string | number>) => t(`operations.errorDefinitions.editor.${key}`, params);
  const editing = Boolean(definition);
  const suggested = definition?.status === "suggested";
  const [draft, setDraft] = useState<DefinitionDraft>(() =>
    definition ? draftFromDefinition(definition) : (initial ?? emptyDefinitionDraft(repositories.length === 1 ? repositories[0].id : "")),
  );
  const [sample, setSample] = useState(initialSample);
  const [errors, setErrors] = useState<DraftErrors>({});
  const [saveError, setSaveError] = useState("");
  const [saving, setSaving] = useState(false);

  const components = useMemo(
    () => entities.filter((x) => x.repositoryId === draft.repositoryId).sort((a, b) => a.name.localeCompare(b.name)),
    [entities, draft.repositoryId],
  );
  const unlistedComponent = draft.componentId && !components.some((c) => c.id === draft.componentId) ? draft.componentId : null;
  const repositoryName = repositories.find((r) => r.id === draft.repositoryId)?.name ?? draft.repositoryId;

  const change = (patch: Partial<DefinitionDraft>) => {
    setDraft((d) => ({ ...d, ...patch }));
    setErrors({});
    setSaveError("");
  };

  const codeText = (code: string) => {
    const key = `operations.errorDefinitions.errors.${code}`;
    const text = t(key);
    return text === key ? code : text;
  };
  // A field the form has no input for (`definitions` when the repository is
  // full) may still have words of its own.
  const restText = (field: string, code: string) => {
    const key = `operations.errorDefinitions.errors.fields.${field}.${code}`;
    const text = t(key);
    return text === key ? `${field}: ${codeText(code)}` : text;
  };
  const errorText = (field: DraftField, code: string) => {
    const key = `operations.errorDefinitions.errors.fields.${field}.${code}`;
    const text = t(key);
    return text === key ? codeText(code) : text;
  };
  const errorId = (field: DraftField) => `errdef-${field}-error`;
  const fieldError = (field: DraftField) =>
    errors[field] ? (
      <p id={errorId(field)} className="text-caption text-destructive">
        {errorText(field, errors[field]!)}
      </p>
    ) : null;
  const describedBy = (field: DraftField, ...hints: Array<string | undefined>) => {
    const ids = [...hints, errors[field] ? errorId(field) : undefined].filter(Boolean);
    return ids.length > 0 ? ids.join(" ") : undefined;
  };
  const invalid = (field: DraftField) => (errors[field] ? true : undefined);

  const save = async (andAccept: boolean) => {
    const found = validateDefinitionDraft(draft);
    setErrors(found);
    setSaveError("");
    if (draftErrorCount(found) > 0) return;
    setSaving(true);
    let saved: ErrorDefinition | null = null;
    try {
      const body = definitionInput(draft, editing);
      saved = definition ? await api.updateErrorDefinition(definition.id, body) : await api.createErrorDefinition(body);
      onSaved(saved);
      if (andAccept) onSaved(await api.acceptErrorDefinition(saved.id));
      onOpenChange(false);
    } catch (err) {
      const fields = dataSourceFieldErrors(err);
      if (fields && !saved) {
        const { errors: inline, rest } = mapDefinitionFields(fields, RENDERED);
        setErrors(inline);
        setSaveError(
          rest.length > 0
            ? rest.map(([k, v]) => restText(k, v)).join(" ")
            : draftErrorCount(inline) > 0
              ? e("fixFields")
              : err instanceof Error
                ? err.message
                : t("common.actionFailed"),
        );
      } else if (!saved && isTooManyDefinitionsError(err)) {
        setSaveError(codeText("too_many_definitions"));
      } else {
        setSaveError(saved ? e("acceptFailed", { message: err instanceof Error ? err.message : "" }) : err instanceof Error ? err.message : t("common.actionFailed"));
      }
    } finally {
      setSaving(false);
    }
  };

  const thresholded = isThresholded(draft.action);
  const categoryHelpId = "errdef-category-help";
  const actionHelpId = "errdef-action-help";
  const regexHelpId = "errdef-regex-help";
  const endpointHelpId = "errdef-endpoint-help";
  const fingerprintHelpId = "errdef-fingerprint-help";

  return (
    <FormDialog
      open={open}
      onOpenChange={(next) => !saving && onOpenChange(next)}
      title={editing ? e("editTitle") : e("newTitle")}
      description={e("description")}
      className="sm:max-w-3xl"
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
            {t("common.cancel")}
          </Button>
          {suggested && (
            <Button variant="outline" onClick={() => void save(false)} disabled={saving}>
              {e("saveOnly")}
            </Button>
          )}
          <Button onClick={() => void save(suggested)} disabled={saving}>
            {saving ? e("saving") : suggested ? e("saveAndAccept") : e("save")}
          </Button>
        </>
      }
    >
      {saveError && (
        <Notice variant="error" title={e("saveFailed")}>
          <p className="break-words">{saveError}</p>
        </Notice>
      )}

      <div className="grid gap-4 sm:grid-cols-2">
        <div className="space-y-1.5">
          <Label htmlFor="errdef-repository">{e("repository")}</Label>
          {editing ? (
            <p id="errdef-repository" className="flex h-9 items-center truncate text-body font-medium">
              {repositoryName}
            </p>
          ) : (
            <Select value={draft.repositoryId} onValueChange={(v) => change({ repositoryId: v, componentId: "" })} disabled={saving}>
              <SelectTrigger id="errdef-repository" aria-invalid={invalid("repositoryId")} aria-describedby={describedBy("repositoryId")}>
                <SelectValue placeholder={e("repositoryPlaceholder")} />
              </SelectTrigger>
              <SelectContent>
                {repositories.map((r) => (
                  <SelectItem key={r.id} value={r.id}>
                    {r.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
          {fieldError("repositoryId")}
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="errdef-component">{e("component")}</Label>
          <Select
            value={draft.componentId || ALL}
            onValueChange={(v) => change({ componentId: v === ALL ? "" : v })}
            disabled={saving || !draft.repositoryId}
          >
            <SelectTrigger id="errdef-component" aria-invalid={invalid("componentId")} aria-describedby={describedBy("componentId")}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>{e("everyComponent")}</SelectItem>
              {components.map((c) => (
                <SelectItem key={c.id} value={c.id}>
                  {c.name}
                </SelectItem>
              ))}
              {unlistedComponent && (
                <SelectItem value={unlistedComponent}>
                  <span className="font-mono">{unlistedComponent.slice(0, 8)}</span>
                </SelectItem>
              )}
            </SelectContent>
          </Select>
          {fieldError("componentId")}
        </div>
      </div>

      <div className="space-y-1.5">
        <Label htmlFor="errdef-name">{e("name")}</Label>
        <Input
          id="errdef-name"
          value={draft.name}
          onChange={(ev) => change({ name: ev.target.value })}
          placeholder={e("namePlaceholder")}
          maxLength={200}
          disabled={saving}
          aria-invalid={invalid("name")}
          aria-describedby={describedBy("name")}
        />
        {fieldError("name")}
      </div>

      <fieldset className="space-y-3 rounded-lg border border-border p-3" aria-describedby={describedBy("match", "errdef-match-help")}>
        <legend className="px-1 text-body font-medium">{e("matchTitle")}</legend>
        <p id="errdef-match-help" className="text-caption text-muted-foreground">
          {e("matchHelp")}
        </p>
        {fieldError("match")}

        <div className="space-y-1.5">
          <Label htmlFor="errdef-regex">{e("messageRegex")}</Label>
          <Input
            id="errdef-regex"
            value={draft.messageRegex}
            onChange={(ev) => change({ messageRegex: ev.target.value })}
            placeholder={e("messageRegexPlaceholder")}
            disabled={saving}
            spellCheck={false}
            className="font-mono"
            aria-invalid={invalid("messageRegex")}
            aria-describedby={describedBy("messageRegex", regexHelpId)}
          />
          <p id={regexHelpId} className="text-caption text-muted-foreground">
            {e("messageRegexHelp")}
          </p>
          {fieldError("messageRegex")}
        </div>
        <RegexTestBox id="errdef-sample" pattern={draft.messageRegex} sample={sample} onSampleChange={setSample} disabled={saving} />

        <div className="grid gap-3 sm:grid-cols-2">
          <div className="space-y-1.5">
            <Label htmlFor="errdef-exception">{e("exceptionType")}</Label>
            <Input
              id="errdef-exception"
              value={draft.exceptionType}
              onChange={(ev) => change({ exceptionType: ev.target.value })}
              placeholder={e("exceptionTypePlaceholder")}
              disabled={saving}
              spellCheck={false}
              className="font-mono"
              aria-invalid={invalid("exceptionType")}
              aria-describedby={describedBy("exceptionType")}
            />
            {fieldError("exceptionType")}
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="errdef-endpoint">{e("endpoint")}</Label>
            <Input
              id="errdef-endpoint"
              value={draft.endpoint}
              onChange={(ev) => change({ endpoint: ev.target.value })}
              placeholder={e("endpointPlaceholder")}
              disabled={saving}
              spellCheck={false}
              className="font-mono"
              aria-invalid={invalid("endpoint")}
              aria-describedby={describedBy("endpoint", endpointHelpId)}
            />
            <p id={endpointHelpId} className="text-caption text-muted-foreground">
              {e("endpointHelp")}
            </p>
            {fieldError("endpoint")}
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="errdef-status">{e("status")}</Label>
            <Input
              id="errdef-status"
              type="number"
              inputMode="numeric"
              min={100}
              max={599}
              value={draft.status}
              onChange={(ev) => change({ status: ev.target.value })}
              placeholder="502"
              disabled={saving}
              className="font-mono tabular-nums"
              aria-invalid={invalid("status")}
              aria-describedby={describedBy("status")}
            />
            {fieldError("status")}
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="errdef-fingerprint">{e("fingerprint")}</Label>
            <Input
              id="errdef-fingerprint"
              value={draft.fingerprint}
              onChange={(ev) => change({ fingerprint: ev.target.value })}
              disabled={saving}
              spellCheck={false}
              className="font-mono"
              aria-invalid={invalid("fingerprint")}
              aria-describedby={describedBy("fingerprint", fingerprintHelpId)}
            />
            <p id={fingerprintHelpId} className="text-caption text-muted-foreground">
              {e("fingerprintHelp")}
            </p>
            {fieldError("fingerprint")}
          </div>
        </div>

        <div className="space-y-1.5">
          <KeyValueEditor
            label={e("attributes")}
            description={e("attributesHelp")}
            entries={draft.attributes}
            onEntriesChange={(attributes) => change({ attributes })}
          />
          {fieldError("attributes")}
        </div>
      </fieldset>

      <div className="grid gap-4 sm:grid-cols-3">
        <div className="space-y-1.5">
          <Label htmlFor="errdef-category">{e("category")}</Label>
          <Select value={draft.category} onValueChange={(v) => change({ category: v as ErrorCategory })} disabled={saving}>
            <SelectTrigger id="errdef-category" aria-invalid={invalid("category")} aria-describedby={describedBy("category", draft.category ? categoryHelpId : undefined)}>
              <SelectValue placeholder={e("categoryPlaceholder")} />
            </SelectTrigger>
            <SelectContent>
              {ERROR_CATEGORIES.map((c) => (
                <SelectItem key={c} value={c}>
                  {t(`operations.errorDefinitions.categories.${c}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {fieldError("category")}
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="errdef-severity">{e("severity")}</Label>
          <Select value={draft.severity} onValueChange={(v) => change({ severity: v as IncidentSeverity })} disabled={saving}>
            <SelectTrigger id="errdef-severity" aria-invalid={invalid("severity")} aria-describedby={describedBy("severity")}>
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
          {fieldError("severity")}
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="errdef-action">{e("action")}</Label>
          <Select
            value={draft.action}
            onValueChange={(v) => {
              setDraft((d) => switchAction(d, v as ErrorAction));
              setErrors({});
              setSaveError("");
            }}
            disabled={saving}
          >
            <SelectTrigger id="errdef-action" aria-invalid={invalid("action")} aria-describedby={describedBy("action", actionHelpId)}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {ERROR_ACTIONS.map((a) => (
                <SelectItem key={a} value={a}>
                  {t(`operations.errorDefinitions.actions.${a}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {fieldError("action")}
        </div>
      </div>
      {draft.category && (
        <p id={categoryHelpId} className="-mt-2 text-caption text-muted-foreground">
          {t(`operations.errorDefinitions.categoryHelp.${draft.category}`)}
        </p>
      )}
      <p id={actionHelpId} className="-mt-2 text-caption text-muted-foreground">
        {t(`operations.errorDefinitions.actionHelp.${draft.action}`)}
      </p>
      {fieldError("threshold")}

      {thresholded && (
        <div className="space-y-1.5">
          <p id="errdef-threshold-label" className="text-body font-medium">
            {e("threshold")}
          </p>
          <div className="flex flex-wrap items-center gap-2" role="group" aria-labelledby="errdef-threshold-label">
            <Label htmlFor="errdef-threshold-count" className="font-normal text-muted-foreground">
              {e("thresholdAtLeast")}
            </Label>
            <Input
              id="errdef-threshold-count"
              type="number"
              inputMode="numeric"
              min={1}
              value={draft.thresholdCount}
              onChange={(ev) => change({ thresholdCount: ev.target.value })}
              disabled={saving}
              className="w-28 font-mono tabular-nums"
              aria-invalid={invalid("thresholdCount")}
              aria-describedby={describedBy("thresholdCount")}
            />
            <Label htmlFor="errdef-threshold-window" className="font-normal text-muted-foreground">
              {e("thresholdWithin")}
            </Label>
            <DurationInput
              key={draft.action}
              id="errdef-threshold-window"
              seconds={draft.thresholdWindowSeconds}
              onChange={(seconds) => change({ thresholdWindowSeconds: seconds })}
              label={e("thresholdWithin")}
              invalid={Boolean(errors.thresholdWindow)}
              disabled={saving}
              describedBy={describedBy("thresholdWindow")}
            />
          </div>
          <p className="text-caption text-muted-foreground">{e(draft.action === "alert" ? "thresholdAlertHelp" : "thresholdIncidentHelp")}</p>
          {fieldError("thresholdCount")}
          {fieldError("thresholdWindow")}
        </div>
      )}

      <div className="space-y-1.5">
        <Label htmlFor="errdef-rationale">{e("rationale")}</Label>
        <Textarea
          id="errdef-rationale"
          value={draft.rationale}
          onChange={(ev) => change({ rationale: ev.target.value })}
          placeholder={e("rationalePlaceholder")}
          rows={3}
          maxLength={4000}
          disabled={saving}
          aria-invalid={invalid("rationale")}
          aria-describedby={describedBy("rationale")}
        />
        {fieldError("rationale")}
      </div>
    </FormDialog>
  );
}
