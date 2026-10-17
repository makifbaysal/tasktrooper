import { useEffect, useRef, useState } from "react";
import {
  api,
  ApiError,
  dataSourceFieldErrors,
  type CloudAccount,
  type DataSource,
  type DataSourceKindInfo,
  type StoreCredentialView,
} from "@/api";
import { FormDialog } from "@/components/admin/FormDialog";
import {
  buildCreateRequest,
  buildPatchRequest,
  emptyDraft,
  fieldErrorText,
  fieldLabel,
  kindDescription,
  kindLabel,
  secretStored,
  secretsToResupply,
  validateDraft,
  visibleFields,
  type DataSourceDraft,
  type FieldErrors,
  type HeaderRow,
} from "@/components/operations/sources/dataSourceSchema";
import { DataSourceFieldInput } from "@/components/operations/sources/DataSourceFieldInput";
import { CapabilityChips, DataSourceStatusBadge } from "@/components/operations/sources/DataSourceStatusBadge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

type Phase = "kind" | "form" | "result";

const SECRETS_REQUIRED = "secrets_required";

interface DataSourceFormProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  kinds: DataSourceKindInfo[];
  /** The source to edit; absent to add a new one. */
  source?: DataSource | null;
  onSaved: (source: DataSource) => void;
  /** The edited source no longer exists (404): the caller drops it and closes the form. */
  onGone?: (source: DataSource) => void;
  /** Connected cloud accounts, for a kind that reads through one. */
  cloudAccounts?: CloudAccount[];
  /** The store credential vault, for a kind that reads a store credential; null while unknown. */
  storeCredentials?: StoreCredentialView[] | null;
  /** A new source's kind and settings to start from (a connected cloud
   * account offered for metrics): the form opens on them, past the kind step. */
  initial?: { kind: string; values: Record<string, string> };
}

function blankDraft(source?: DataSource | null): DataSourceDraft {
  return { name: source?.name ?? "", values: {}, headers: {}, removed: [] };
}

function startingDraft(kind: DataSourceKindInfo | undefined, source: DataSource | null, initial?: { values: Record<string, string> }): DataSourceDraft {
  if (!kind) return blankDraft(source);
  const draft = emptyDraft(kind, source);
  return initial && !source ? { ...draft, values: { ...draft.values, ...initial.values } } : draft;
}

/**
 * Adds or edits a data source. Step one picks a kind; step two is a form
 * drawn entirely from that kind's `fields`, so a new kind on the server
 * needs no change here. Every save is followed by the server's verify, whose
 * outcome is the last step: a source that could not be reached is still
 * saved, with an error status. Mount it only while open — its state starts
 * from the props.
 */
export function DataSourceForm({
  open,
  onOpenChange,
  kinds,
  source = null,
  onSaved,
  onGone,
  cloudAccounts,
  storeCredentials,
  initial,
}: DataSourceFormProps) {
  const { t } = useI18n();
  const f = (key: string, params?: Record<string, string | number>) => t(`operations.sources.form.${key}`, params);
  const [target, setTarget] = useState<DataSource | null>(source);
  const prefilled = !source && initial ? initial : undefined;
  const [phase, setPhase] = useState<Phase>(source || prefilled ? "form" : "kind");
  const [kindKey, setKindKey] = useState(source?.kind ?? prefilled?.kind ?? "");
  const effectiveKindKey = kindKey || (kinds.length === 1 ? kinds[0].kind : "");
  const kind = kinds.find((k) => k.kind === effectiveKindKey);
  const [draftKind, setDraftKind] = useState((source || prefilled) && kind ? kind.kind : "");
  const [draft, setDraft] = useState<DataSourceDraft>(() =>
    (source || prefilled) && kind ? startingDraft(kind, source, prefilled) : blankDraft(source),
  );
  const [errors, setErrors] = useState<FieldErrors>({});
  const [formError, setFormError] = useState("");
  const [saving, setSaving] = useState(false);
  const [focusRequest, setFocusRequest] = useState(0);
  const formRef = useRef<HTMLDivElement>(null);
  const draftReady = Boolean(kind) && draftKind === kind?.kind;

  // After a refused save, the first field that needs attention takes focus
  // (in the form's order), so the error is where the keyboard and the screen
  // reader already are. Only a new request moves it; typing afterwards must not.
  const focusInputs = useRef({ kind, draft, errors });
  focusInputs.current = { kind, draft, errors };
  useEffect(() => {
    const { kind: k, draft: d, errors: found } = focusInputs.current;
    const root = formRef.current;
    if (focusRequest === 0 || !k || !root) return;
    const first = ["name", ...visibleFields(k, d).map((field) => field.key)].find((key) => found[key]);
    if (!first) return;
    const target =
      first === "name"
        ? root.querySelector<HTMLElement>("#ds-name")
        : root
            .querySelector(`[data-ds-field="${first}"]`)
            ?.querySelector<HTMLElement>('input:not([disabled]), textarea:not([disabled]), button:not([disabled])');
    target?.focus();
  }, [focusRequest]);

  // An edit opened before the kinds arrived starts from a blank draft; saving
  // that would send auth="" and the server would drop the stored token as a
  // hidden field. Build the real draft the moment the kind is known.
  useEffect(() => {
    if (!kind || draftKind || phase !== "form") return;
    setDraft((d) => ({ ...startingDraft(kind, target, prefilled), name: d.name || target?.name || "" }));
    setDraftKind(kind.kind);
  }, [kind, draftKind, phase, target, prefilled]);

  const clearError = (key: string) =>
    setErrors((prev) => {
      if (!(key in prev)) return prev;
      const next = { ...prev };
      delete next[key];
      return next;
    });

  const setValue = (key: string, value: string) => {
    setDraft((d) => ({ ...d, values: { ...d.values, [key]: value } }));
    clearError(key);
  };

  const setHeaders = (key: string, rows: HeaderRow[]) => {
    setDraft((d) => ({ ...d, headers: { ...d.headers, [key]: rows } }));
    clearError(key);
  };

  const setRemoved = (key: string, removed: boolean) => {
    setDraft((d) => ({
      ...d,
      removed: removed ? [...d.removed.filter((k) => k !== key), key] : d.removed.filter((k) => k !== key),
    }));
    clearError(key);
  };

  const toForm = () => {
    if (!kind) return;
    if (draftKind !== kind.kind) {
      setDraft((d) => ({ ...emptyDraft(kind, target), name: d.name }));
      setDraftKind(kind.kind);
    }
    setErrors({});
    setFormError("");
    setPhase("form");
  };

  const editSaved = () => {
    if (!kind || !target) return;
    setDraft(emptyDraft(kind, target));
    setDraftKind(kind.kind);
    setErrors({});
    setFormError("");
    setPhase("form");
  };

  const save = async () => {
    if (!kind || !draftReady) return;
    const found = validateDraft(kind, draft, target);
    setErrors(found);
    setFormError("");
    if (Object.keys(found).length > 0) {
      setFocusRequest((n) => n + 1);
      return;
    }
    setSaving(true);
    let refused = false;
    try {
      const saved = target
        ? await api.updateDataSource(target.id, buildPatchRequest(kind, draft, target))
        : await api.createDataSource(buildCreateRequest(kind, draft));
      setTarget(saved);
      setDraft(blankDraft(saved));
      setDraftKind("");
      onSaved(saved);
      setPhase("result");
    } catch (err) {
      if (target && err instanceof ApiError && err.status === 404 && onGone) {
        onGone(target);
        return;
      }
      const fields = dataSourceFieldErrors(err);
      if (fields) {
        await showFieldErrors(fields);
        refused = true;
      } else setFormError(err instanceof Error ? err.message : t("common.actionFailed"));
    } finally {
      setSaving(false);
      if (refused) setFocusRequest((n) => n + 1);
    }
  };

  const showFieldErrors = async (fields: FieldErrors) => {
    if (!kind) return;
    let next = draft;
    let latest = target;
    const resupplyAsked = fields.secrets === SECRETS_REQUIRED;
    if (resupplyAsked) {
      next = { ...draft, mustResupply: true };
      // The server knows of a stored secret this snapshot does not (one added
      // from elsewhere since the list was read): read the source again so the
      // field that needs it can ask for it.
      if (target && secretsToResupply(kind, next, target).length === 0) {
        try {
          latest = await api.getDataSource(target.id);
          setTarget(latest);
        } catch {
          latest = target;
        }
      }
      setDraft(next);
    }
    const shown = new Set(["name", ...visibleFields(kind, next).map((field) => field.key)]);
    const inline = Object.fromEntries(Object.entries(fields).filter(([key]) => shown.has(key)));
    const resupplyErrors = resupplyAsked ? validateDraft(kind, next, latest) : {};
    setErrors({ ...resupplyErrors, ...inline });
    const rest = Object.entries(fields).filter(
      ([key]) => !shown.has(key) && !(key === "secrets" && Object.keys(resupplyErrors).length > 0),
    );
    setFormError(
      rest
        .map(([key, code]) => {
          const field = kind.fields.find((x) => x.key === key);
          const text = fieldErrorText(t, key, code);
          return field ? `${fieldLabel(t, kind.kind, field)}: ${text}` : text;
        })
        .join(" "),
    );
  };

  const title = target ? f("editTitle", { name: target.name }) : f("addTitle");
  const resupply = phase === "form" && kind ? secretsToResupply(kind, draft, target) : [];
  const close = () => onOpenChange(false);

  let body;
  let footer;
  if (phase === "kind") {
    body = (
      <div className="space-y-3">
        <p id="ds-kind-label" className="text-body font-medium">
          {f("pickKind")}
        </p>
        {kinds.length === 0 ? (
          <p className="text-caption text-muted-foreground">{f("noKinds")}</p>
        ) : (
          <div role="radiogroup" aria-labelledby="ds-kind-label" className="grid gap-2">
            {kinds.map((k) => {
              const checked = k.kind === effectiveKindKey;
              return (
                <Button
                  key={k.kind}
                  role="radio"
                  aria-checked={checked}
                  variant="outline"
                  onClick={() => setKindKey(k.kind)}
                  className={cn(
                    "h-auto flex-col items-start gap-1.5 whitespace-normal px-4 py-3 text-left",
                    checked && "border-primary bg-primary/5 ring-2 ring-primary/20",
                  )}
                >
                  <span className="font-medium">{kindLabel(t, k)}</span>
                  <span className="text-caption font-normal text-muted-foreground">{kindDescription(t, k)}</span>
                  <CapabilityChips capabilities={k.capabilities ?? []} />
                </Button>
              );
            })}
          </div>
        )}
        <p className="text-caption text-muted-foreground">{f("moreKindsLater")}</p>
      </div>
    );
    footer = (
      <>
        <Button variant="outline" onClick={close}>
          {t("common.cancel")}
        </Button>
        <Button onClick={toForm} disabled={!kind}>
          {f("continue")}
        </Button>
      </>
    );
  } else if (phase === "form") {
    body = !draftReady || !kind ? (
      kinds.length === 0 || kind ? (
        <div className="space-y-3" aria-busy="true">
          <Skeleton className="h-9 w-full" />
          <Skeleton className="h-9 w-full" />
          <Skeleton className="h-9 w-2/3" />
        </div>
      ) : (
        <Notice variant="warning" title={f("kindUnavailableTitle")}>
          {f("kindUnavailable")}
        </Notice>
      )
    ) : (
      <div ref={formRef} className="space-y-4">
        {formError && (
          <Notice variant="error" title={f("saveFailed")}>
            <p className="break-words">{formError}</p>
          </Notice>
        )}
        <p className="text-caption text-muted-foreground">{f("kindLine", { kind: kindLabel(t, kind) })}</p>
        {resupply.length > 0 && (
          <Notice variant="warning" title={f("resupplyTitle")}>
            {f("resupplyBody")}
          </Notice>
        )}
        <div className="space-y-1.5">
          <Label htmlFor="ds-name">
            {f("name")}
            {!target && <span className="ml-1 font-normal text-muted-foreground">({f("optional")})</span>}
          </Label>
          <Input
            id="ds-name"
            value={draft.name}
            onChange={(e) => {
              const name = e.target.value;
              setDraft((d) => ({ ...d, name }));
              clearError("name");
            }}
            placeholder={
              target
                ? undefined
                : kind.fields.some((x) => x.type === "url")
                  ? f("namePlaceholder")
                  : kind.fields.some((x) => x.type === "store_credential")
                    ? t("operations.telemetry.sources.namePlaceholderStore")
                    : f("namePlaceholderCloud")
            }
            aria-invalid={errors.name ? true : undefined}
            aria-describedby={errors.name ? "ds-name-error" : undefined}
            autoComplete="off"
            disabled={saving}
          />
          {errors.name && (
            <p id="ds-name-error" className="text-caption text-destructive">
              {fieldErrorText(t, "name", errors.name)}
            </p>
          )}
        </div>
        {visibleFields(kind, draft).map((field) => (
          <DataSourceFieldInput
            key={field.key}
            kind={kind.kind}
            field={field}
            value={draft.values[field.key] ?? ""}
            onChange={(value) => setValue(field.key, value)}
            headerRows={draft.headers[field.key] ?? []}
            onHeadersChange={(rows) => setHeaders(field.key, rows)}
            stored={secretStored(target, field.key)}
            removed={draft.removed.includes(field.key)}
            onRemovedChange={(removed) => setRemoved(field.key, removed)}
            mustResupply={resupply.includes(field.key)}
            error={errors[field.key]}
            disabled={saving}
            cloudAccounts={cloudAccounts}
            storeCredentials={storeCredentials}
            secretsStored={(target?.secrets_set ?? []).length > 0}
          />
        ))}
      </div>
    );
    footer = (
      <>
        {!target && (
          <Button variant="ghost" className="sm:mr-auto" onClick={() => setPhase("kind")} disabled={saving}>
            {f("back")}
          </Button>
        )}
        <Button variant="outline" onClick={close} disabled={saving}>
          {t("common.cancel")}
        </Button>
        <Button onClick={() => void save()} disabled={saving || !draftReady}>
          {saving ? f("saving") : f("save")}
        </Button>
      </>
    );
  } else {
    const saved = target!;
    const version = saved.info?.version;
    body = (
      <div className="space-y-3" aria-live="polite">
        <div className="flex flex-wrap items-center gap-2">
          <DataSourceStatusBadge status={saved.status} />
          {version && <span className="text-caption text-muted-foreground">{t("operations.sources.version", { version })}</span>}
        </div>
        {saved.status === "ok" ? (
          <Notice variant="info" title={f("resultOkTitle")}>
            {version ? f("resultOkVersion", { name: saved.name, version }) : f("resultOk", { name: saved.name })}
          </Notice>
        ) : (
          <Notice variant="error" title={f("resultErrorTitle")}>
            {saved.status_detail && <p className="break-words font-mono text-caption">{saved.status_detail}</p>}
            <p>{f("resultErrorHint")}</p>
          </Notice>
        )}
      </div>
    );
    footer =
      saved.status === "ok" ? (
        <Button onClick={close}>{f("done")}</Button>
      ) : (
        <>
          <Button variant="outline" onClick={editSaved} disabled={!kind}>
            {f("editSettings")}
          </Button>
          <Button onClick={close}>{f("close")}</Button>
        </>
      );
  }

  return (
    <FormDialog open={open} onOpenChange={(next) => !saving && onOpenChange(next)} title={title} footer={footer}>
      {body}
    </FormDialog>
  );
}
