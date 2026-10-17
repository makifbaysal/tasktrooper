import { Check, ListTodo, PencilLine, Siren, Trash2, X } from "lucide-react";
import { useMemo, useState, type ReactNode } from "react";
import { Link } from "react-router-dom";
import type { ErrorDefinition } from "@/api";
import { DetailFacts, DetailPanel, DetailSection, type DetailFact } from "@/components/operations/console/DetailPanel";
import { ActionLabel, CategoryPill, SourceBadge } from "@/components/operations/errorDefinitions/DefinitionBadges";
import { matchSummary, thresholdText } from "@/components/operations/errorDefinitions/errorDefinitions";
import { IssueSeverity } from "@/components/operations/issues/IssueSeverity";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { intlLocale } from "@/lib/languages";
import { formatDate, formatRelativeTime } from "@/lib/utils";

interface ErrorDefinitionDetailPanelProps {
  /** The `?definition=` id; null closes the panel. */
  definitionId: string | null;
  definition?: ErrorDefinition;
  loading: boolean;
  scope: string;
  busy: boolean;
  onOpenChange: (open: boolean) => void;
  onEdit: (def: ErrorDefinition) => void;
  onAccept: (def: ErrorDefinition) => void;
  onDismiss: (def: ErrorDefinition) => void;
  /** Resolves the server's refusal, which keeps the confirmation open, or null once it is gone. */
  onDelete: (def: ErrorDefinition) => Promise<string | null>;
}

/** One definition in the console's flyout: what it matches, field by field,
 * what it sets off, and why it was written (the rationale and evidence). */
export function ErrorDefinitionDetailPanel({
  definitionId,
  definition: def,
  loading,
  scope,
  busy,
  onOpenChange,
  onEdit,
  onAccept,
  onDismiss,
  onDelete,
}: ErrorDefinitionDetailPanelProps) {
  const { t } = useI18n();
  const d = (key: string, params?: Record<string, string | number>) => t(`operations.errorDefinitions.detail.${key}`, params);

  let title: ReactNode = d("loading");
  if (def) title = def.name;
  else if (!loading) title = d("notFound");

  return (
    <DetailPanel
      open={definitionId !== null}
      onOpenChange={onOpenChange}
      title={title}
      description={def ? scope : undefined}
      badges={
        def ? (
          <>
            {def.status !== "active" && (
              <Badge variant={def.status === "suggested" ? "info" : "secondary"} className="px-2 py-0 text-micro">
                {t(`operations.errorDefinitions.statuses.${def.status}`)}
              </Badge>
            )}
            <CategoryPill category={def.category} />
            <Badge variant="outline" className="px-2 py-0">
              <IssueSeverity severity={def.severity} className="text-micro" />
            </Badge>
            <SourceBadge source={def.source} />
            {def.firing_since && (
              <Badge variant="outline" className="gap-1 border-transparent bg-destructive/15 px-2 py-0 text-micro text-destructive">
                <Siren className="h-3 w-3" aria-hidden />
                {d("firing")}
              </Badge>
            )}
          </>
        ) : undefined
      }
      actions={
        def ? (
          <DefinitionActions key={def.id} def={def} busy={busy} onEdit={onEdit} onAccept={onAccept} onDismiss={onDismiss} onDelete={onDelete} />
        ) : undefined
      }
    >
      {!def && loading && (
        <div className="space-y-3" aria-busy>
          <Skeleton className="h-24 w-full" />
          <Skeleton className="h-40 w-full" />
        </div>
      )}
      {!def && !loading && (
        <Notice variant="info" title={d("notFound")}>
          {d("notFoundBody")}
        </Notice>
      )}
      {def && <DefinitionBody def={def} />}
    </DetailPanel>
  );
}

function DefinitionActions({
  def,
  busy,
  onEdit,
  onAccept,
  onDismiss,
  onDelete,
}: {
  def: ErrorDefinition;
  busy: boolean;
  onEdit: (def: ErrorDefinition) => void;
  onAccept: (def: ErrorDefinition) => void;
  onDismiss: (def: ErrorDefinition) => void;
  onDelete: (def: ErrorDefinition) => Promise<string | null>;
}) {
  const { t } = useI18n();
  const d = (key: string, params?: Record<string, string | number>) => t(`operations.errorDefinitions.detail.${key}`, params);
  const [confirming, setConfirming] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [refusal, setRefusal] = useState<string | null>(null);

  const remove = async (): Promise<boolean> => {
    setDeleting(true);
    const refused = await onDelete(def);
    setDeleting(false);
    setRefusal(refused);
    return refused === null;
  };

  return (
    <>
      {def.status === "suggested" && (
        <>
          <Button size="sm" onClick={() => onAccept(def)} disabled={busy}>
            <Check aria-hidden />
            {t("operations.errorDefinitions.accept")}
          </Button>
          <Button size="sm" variant="outline" onClick={() => onDismiss(def)} disabled={busy}>
            <X aria-hidden />
            {t("operations.errorDefinitions.dismiss")}
          </Button>
        </>
      )}
      {def.status !== "dismissed" && (
        <Button size="sm" variant="outline" onClick={() => onEdit(def)} disabled={busy}>
          <PencilLine aria-hidden />
          {d("edit")}
        </Button>
      )}
      <Button
        size="sm"
        variant="outline"
        className="border-destructive/40 text-destructive hover:text-destructive"
        disabled={busy}
        onClick={() => {
          setRefusal(null);
          setConfirming(true);
        }}
      >
        <Trash2 aria-hidden />
        {d("delete")}
      </Button>
      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title={d("deleteTitle", { name: def.name })}
        description={d("deleteDescription")}
        confirmLabel={d("delete")}
        variant="destructive"
        loading={deleting}
        error={refusal}
        onConfirm={remove}
      />
    </>
  );
}

function DefinitionBody({ def }: { def: ErrorDefinition }) {
  const { t, lang } = useI18n();
  const d = (key: string, params?: Record<string, string | number>) => t(`operations.errorDefinitions.detail.${key}`, params);
  const e = (key: string) => t(`operations.errorDefinitions.editor.${key}`);
  const ctx = useMemo(() => ({ locale: intlLocale(lang), t }), [lang, t]);
  const m = def.match;
  const threshold = thresholdText(def, ctx);
  const attributes = Object.entries(m.attributes ?? {});
  const evidence = def.evidence ?? [];

  const matchFacts: DetailFact[] = [];
  if (m.message_regex) matchFacts.push({ label: e("messageRegex"), value: <code className="font-mono break-all">/{m.message_regex}/</code> });
  if (m.exception_type) matchFacts.push({ label: e("exceptionType"), value: <code className="font-mono break-all">{m.exception_type}</code> });
  if (m.endpoint) matchFacts.push({ label: e("endpoint"), value: <code className="font-mono break-all">{m.endpoint}</code> });
  if (m.status) matchFacts.push({ label: e("status"), value: <span className="font-mono tabular-nums">{m.status}</span> });
  if (m.fingerprint) matchFacts.push({ label: e("fingerprint"), value: <code className="font-mono break-all">{m.fingerprint}</code> });
  if (attributes.length > 0) {
    matchFacts.push({
      label: e("attributes"),
      value: (
        <span className="flex flex-wrap gap-1">
          {attributes.map(([k, v]) => (
            <Badge key={k} variant="outline" className="px-1.5 py-0 font-mono text-micro font-normal">
              {k}={v}
            </Badge>
          ))}
        </span>
      ),
    });
  }

  const actionFacts: DetailFact[] = [{ label: d("action"), value: <ActionLabel action={def.action} /> }];
  if (threshold) actionFacts.push({ label: d("threshold"), value: threshold });
  if (def.action === "alert" || def.action === "incident") {
    actionFacts.push({
      label: d("firingSince"),
      value: def.firing_since ? (
        <span title={formatDate(def.firing_since)}>{formatRelativeTime(def.firing_since, lang)}</span>
      ) : (
        <span className="text-muted-foreground">{d("notFiring")}</span>
      ),
    });
  }
  if (def.task_id) {
    actionFacts.push({
      label: d("task"),
      value: (
        <Link to={`/board?task=${encodeURIComponent(def.task_id)}`} className="inline-flex items-center gap-1 text-info hover:underline">
          <ListTodo className="h-3.5 w-3.5" aria-hidden />
          {d("openTask")}
        </Link>
      ),
    });
  }

  return (
    <>
      <DetailSection title={d("matchTitle")}>
        <p className="text-caption">{matchSummary(def.match, ctx)}</p>
        {matchFacts.length > 0 && <DetailFacts items={matchFacts} />}
      </DetailSection>
      <DetailSection title={d("actionTitle")}>
        <DetailFacts items={actionFacts} />
        <p className="text-caption text-muted-foreground">{t(`operations.errorDefinitions.actionHelp.${def.action}`)}</p>
      </DetailSection>
      <DetailSection title={d("rationale")}>
        {def.rationale ? (
          <p className="text-caption whitespace-pre-wrap">{def.rationale}</p>
        ) : (
          <p className="text-caption text-muted-foreground">{d("noRationale")}</p>
        )}
      </DetailSection>
      {evidence.length > 0 && (
        <DetailSection title={d("evidence")}>
          <ul className="space-y-1.5">
            {evidence.map((item, i) => (
              <li key={i} className="rounded-md border border-border bg-muted/30 px-2.5 py-1.5 font-mono text-micro break-words whitespace-pre-wrap">
                {item}
              </li>
            ))}
          </ul>
        </DetailSection>
      )}
      <DetailSection title={d("factsTitle")}>
        <DetailFacts
          items={[
            { label: d("source"), value: t(`operations.errorDefinitions.sourceTitles.${def.source}`) },
            { label: d("created"), value: formatDate(def.created_at) },
            { label: d("updated"), value: formatDate(def.updated_at) },
          ]}
        />
      </DetailSection>
    </>
  );
}
