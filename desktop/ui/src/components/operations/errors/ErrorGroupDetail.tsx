import { BookMarked, BookPlus, Copy, Database, ExternalLink, ListPlus, Plug, X } from "lucide-react";
import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import type { OpsErrorGroup } from "@/api";
import { repositoryDeployPath } from "@/components/operations/console/opsEntities";
import { DefinitionChip } from "@/components/operations/errorDefinitions/DefinitionBadges";
import { canCreateTask, dataSourcePath, isBindingRow, isCloudProvider, providerLabel } from "@/components/operations/errors/errorGroups";
import type { EnvironmentRef } from "@/components/operations/query/opsQuery";
import type { OpsEntity } from "@/components/operations/shell/useOpsEntities";
import { ProviderIcon } from "@/components/projects/model/ProviderIcon";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Spinner } from "@/components/ui/spinner";
import { useI18n } from "@/hooks/useI18n";
import { cn, formatDate, formatRelativeTime } from "@/lib/utils";

interface ErrorGroupDetailProps {
  group: OpsErrorGroup;
  environment?: EnvironmentRef;
  entity?: OpsEntity;
  envLabel: string;
  entityHref?: (entity: OpsEntity) => string;
  creating: boolean;
  closeHref: string;
  onCreateTask: () => void;
  onCopyFingerprint: () => void;
  /** Where the group's error definition is shown, when it matches one. */
  definitionHref?: string;
  /** Opens the definition editor for this group; absent, no Define button. */
  onDefine?: () => void;
  className?: string;
}

function Field({ label, children, mono = false }: { label: string; children: ReactNode; mono?: boolean }) {
  return (
    <div className="space-y-0.5">
      <dt className="text-micro font-medium tracking-wide text-muted-foreground uppercase">{label}</dt>
      <dd className={cn("min-w-0 break-words text-caption", mono && "font-mono")}>{children}</dd>
    </div>
  );
}

/** The inbox's side panel: everything known about one error group, and what
 * to do about it. */
export function ErrorGroupDetail({
  group,
  environment,
  entity,
  envLabel,
  entityHref,
  creating,
  closeHref,
  onCreateTask,
  onCopyFingerprint,
  definitionHref,
  onDefine,
  className,
}: ErrorGroupDetailProps) {
  const { t, lang } = useI18n();
  const d = (key: string, params?: Record<string, string | number>) => t(`operations.errorsInbox.detail.${key}`, params);
  const e = (key: string, params?: Record<string, string | number>) => t(`operations.errorsInbox.${key}`, params);
  const provider = providerLabel(t, group.provider);
  const binding = isBindingRow(group);
  const taskable = canCreateTask(group);
  const number = new Intl.NumberFormat(lang);

  return (
    <Card className={cn("flex min-w-0 flex-col gap-4 p-4", className)} aria-labelledby="error-group-detail-title" role="region">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0 space-y-1">
          <div className="flex flex-wrap items-center gap-2">
            <h2 id="error-group-detail-title" className="text-body font-semibold">
              {d("title")}
            </h2>
            {group.new && (
              <Badge variant="warning" className="rounded-[4px] px-1.5 py-0 text-micro" title={e("newTitle")}>
                {e("newBadge")}
              </Badge>
            )}
          </div>
          <p className="font-mono text-micro text-muted-foreground tabular-nums">
            {d("summary", { count: number.format(group.count), time: formatRelativeTime(group.last_seen, lang) })}
          </p>
          {group.definition && <DefinitionChip definition={group.definition} />}
        </div>
        <Button variant="ghost" size="icon" className="h-7 w-7 shrink-0" asChild>
          <Link to={closeHref} aria-label={d("close")} title={d("close")}>
            <X aria-hidden />
          </Link>
        </Button>
      </div>

      <div className="flex flex-wrap gap-2">
        <Button
          size="sm"
          onClick={onCreateTask}
          disabled={creating || !taskable}
          title={taskable ? undefined : t("operations.telemetry.errors.noTask", { provider })}
        >
          {creating ? <Spinner size="sm" className="text-current" /> : <ListPlus aria-hidden />}
          {e("createTask")}
        </Button>
        {group.external_url && (
          <Button size="sm" variant="outline" asChild>
            <a href={group.external_url} target="_blank" rel="noreferrer">
              <ExternalLink aria-hidden />
              {e("openInProvider", { provider })}
            </a>
          </Button>
        )}
        <Button size="sm" variant="outline" onClick={onCopyFingerprint}>
          <Copy aria-hidden />
          {e("copyFingerprint")}
        </Button>
        {group.definition && definitionHref ? (
          <Button size="sm" variant="outline" asChild>
            <Link to={definitionHref}>
              <BookMarked aria-hidden />
              {t("operations.errorDefinitions.inbox.definitionLabel")}
            </Link>
          </Button>
        ) : (
          !group.definition &&
          onDefine && (
            <Button size="sm" variant="outline" onClick={onDefine}>
              <BookPlus aria-hidden />
              {t("operations.errorDefinitions.inbox.define")}
            </Button>
          )
        )}
      </div>

      <dl className="space-y-3">
        <Field label={d("message")}>
          <pre className="max-h-48 overflow-auto rounded-md bg-muted/50 p-2 font-mono text-caption whitespace-pre-wrap">
            {group.message || e("noMessage")}
          </pre>
        </Field>
        {group.sample && group.sample !== group.message && (
          <Field label={d("sample")}>
            <pre className="max-h-64 overflow-auto rounded-md bg-muted/50 p-2 font-mono text-micro whitespace-pre-wrap">{group.sample}</pre>
          </Field>
        )}
        {group.source && (
          <Field label={d("source")} mono>
            {group.source}
          </Field>
        )}
        <Field label={d("fingerprint")} mono>
          <span className="break-all">{group.fingerprint}</span>
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field label={d("occurrences")} mono>
            <span className="tabular-nums">{number.format(group.count)}</span>
          </Field>
          <Field label={d("provider")}>
            <span className="inline-flex items-center gap-1.5">
              {isCloudProvider(group.provider) ? (
                <ProviderIcon provider={group.provider} className="h-3.5 w-3.5" />
              ) : (
                <Database className="h-3.5 w-3.5 text-muted-foreground" aria-hidden />
              )}
              {provider}
            </span>
          </Field>
          <Field label={d("firstSeen")}>
            <span className="font-mono tabular-nums" title={formatDate(group.first_seen)}>
              {formatRelativeTime(group.first_seen, lang)}
            </span>
          </Field>
          <Field label={d("lastSeen")}>
            <span className="font-mono tabular-nums" title={formatDate(group.last_seen)}>
              {formatRelativeTime(group.last_seen, lang)}
            </span>
          </Field>
        </div>
        <Field label={d("entity")}>
          {entity ? (
            entityHref ? (
              <Link to={entityHref(entity)} className="font-medium text-info hover:underline">
                {entity.name}
              </Link>
            ) : (
              <span className="font-medium">{entity.name}</span>
            )
          ) : (
            <span className="text-muted-foreground">{t("operations.opsQuery.unavailable.unknownEntity")}</span>
          )}
        </Field>
        <Field label={d("environment")}>
          <span className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
            <span>{envLabel}</span>
            {environment?.resource && <span className="truncate font-mono text-muted-foreground">{environment.resource}</span>}
            {binding && group.data_source_id && (
              <Link
                to={dataSourcePath(group.data_source_id)}
                className="inline-flex items-center gap-1 text-micro font-medium text-info hover:underline"
              >
                <Database className="h-3 w-3" aria-hidden />
                {t("operations.telemetry.errors.openSource", { provider })}
              </Link>
            )}
            {entity && !binding && (
              <Link
                to={repositoryDeployPath(entity.repositoryId)}
                className="inline-flex items-center gap-1 text-micro font-medium text-info hover:underline"
              >
                <Plug className="h-3 w-3" aria-hidden />
                {d("deploySettings")}
              </Link>
            )}
          </span>
        </Field>
      </dl>
    </Card>
  );
}
