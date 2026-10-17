import { Plus, Webhook } from "lucide-react";
import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import { ApiError, api, type AlertToken } from "@/api";
import { ALERT_DOCS_URL, isLocalOnly, serverBase } from "@/components/operations/alerts/alertConnections";
import { AlertTokenTable, type AlertTokenGroup } from "@/components/operations/alerts/AlertTokenTable";
import { LocalOnlyNotice } from "@/components/operations/alerts/LocalOnlyNotice";
import { NewAlertConnectionDialog } from "@/components/operations/alerts/NewAlertConnectionDialog";
import { useOpsPageScope, useOptionalOpsContext } from "@/components/operations/shell/OpsContext";
import { useOpsDirectory, type OpsRepository } from "@/components/operations/shell/useOpsEntities";
import { ExternalAnchor } from "@/components/operations/StoreTestBuildParts";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { EmptyState } from "@/components/ui/empty-state";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useAlertTokens } from "@/hooks/useAlertTokens";
import { useI18n } from "@/hooks/useI18n";

interface DeleteTarget {
  repository: OpsRepository;
  token: AlertToken;
}

/** `/operations/alert-connections`: the alert ingest tokens that let an
 * alerting tool open issues on a repository. A new one's URL is shown once,
 * in the dialog that created it, and nowhere else. */
export default function OpsAlertConnectionsPage() {
  const { t } = useI18n();
  const a = (key: string, params?: Record<string, string | number>) => t(`operations.alertConnections.${key}`, params);
  useOpsPageScope({ time: false, env: false });
  const refreshKey = useOptionalOpsContext()?.refreshKey ?? 0;

  const { repositories, loaded, error: directoryError } = useOpsDirectory();
  const sorted = useMemo(() => [...repositories].sort((x, y) => x.name.localeCompare(y.name)), [repositories]);
  const ids = useMemo(() => sorted.map((r) => r.id), [sorted]);
  const { byRepository, errors, loading, add, remove } = useAlertTokens(ids, refreshKey);

  const [dialog, setDialog] = useState<{ repositoryId?: string } | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<DeleteTarget | null>(null);
  const [deleting, setDeleting] = useState(false);
  const base = serverBase();

  const groups: AlertTokenGroup[] = sorted.map((repository) => ({
    repository,
    tokens: byRepository[repository.id] ?? [],
    error: errors[repository.id],
  }));
  const total = groups.reduce((n, g) => n + g.tokens.length, 0);
  const anyError = groups.some((g) => g.error);

  const confirmDelete = async () => {
    if (!deleteTarget) return;
    const { repository, token } = deleteTarget;
    setDeleting(true);
    try {
      await api.deleteAlertToken(repository.id, token.id);
      remove(repository.id, token.id);
      toast.success(a("deleted", { label: token.label }));
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) {
        remove(repository.id, token.id);
        toast.success(a("deleted", { label: token.label }));
      } else {
        toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
      }
    } finally {
      setDeleting(false);
      setDeleteTarget(null);
    }
  };

  const addButton = (
    <Button onClick={() => setDialog({ repositoryId: sorted[0]?.id })}>
      <Plus aria-hidden />
      {a("add")}
    </Button>
  );

  const docsLink = <ExternalAnchor href={ALERT_DOCS_URL}>{a("docs")}</ExternalAnchor>;

  let body;
  if (!loaded && directoryError) {
    body = (
      <Notice variant="error" title={a("directoryFailed")}>
        {directoryError}
      </Notice>
    );
  } else if (!loaded) {
    body = <Skeleton className="h-48 w-full" />;
  } else if (sorted.length === 0) {
    body = (
      <Card>
        <EmptyState
          icon={Webhook}
          title={a("noRepositories.title")}
          description={a("noRepositories.description")}
          action={
            <Button asChild variant="outline">
              <Link to="/projects/new">{a("noRepositories.action")}</Link>
            </Button>
          }
        />
      </Card>
    );
  } else if (loading && total === 0 && !anyError) {
    body = <Skeleton className="h-48 w-full" />;
  } else if (total === 0 && !anyError) {
    body = (
      <Card>
        <EmptyState
          icon={Webhook}
          title={a("empty.title")}
          description={a("empty.description")}
          action={
            <div className="flex flex-col items-center gap-3">
              {addButton}
              {docsLink}
            </div>
          }
        />
      </Card>
    );
  } else {
    body = (
      <>
        {isLocalOnly(base) && <LocalOnlyNotice base={base} />}
        <AlertTokenTable
          groups={groups}
          onAdd={(repositoryId) => setDialog({ repositoryId })}
          onDelete={(repository, token) => setDeleteTarget({ repository, token })}
        />
      </>
    );
  }

  const listed = loaded && sorted.length > 0 && (total > 0 || anyError);

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="min-w-0 space-y-1">
          <p className="max-w-prose text-caption text-muted-foreground">{a("description")}</p>
          {listed && docsLink}
        </div>
        {listed && addButton}
      </div>

      {body}

      {dialog && (
        <NewAlertConnectionDialog
          open
          onOpenChange={(open) => !open && setDialog(null)}
          repositories={sorted}
          initialRepositoryId={dialog.repositoryId}
          onCreated={add}
        />
      )}

      <ConfirmDialog
        open={deleteTarget !== null}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title={a("deleteTitle", { label: deleteTarget?.token.label ?? "" })}
        description={a("deleteDescription", { repository: deleteTarget?.repository.name ?? "" })}
        confirmLabel={a("delete")}
        loading={deleting}
        onConfirm={confirmDelete}
      />
    </div>
  );
}
