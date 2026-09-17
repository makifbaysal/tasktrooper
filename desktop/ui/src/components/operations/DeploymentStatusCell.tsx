import { Link } from "react-router-dom";
import { type MatrixCell } from "@/api";
import { Badge, type BadgeProps } from "@/components/ui/badge";
import { Spinner } from "@/components/ui/spinner";
import { useI18n } from "@/hooks/useI18n";
import { formatRelativeDate } from "@/lib/utils";

// Shared so the button and link wrappers below render pixel-identical cells.
const CELL_CLASS = "block w-full rounded-md p-2 text-left transition-colors hover:bg-muted/50";

interface DeploymentStatusCellProps {
  cell: MatrixCell;
  // Needed for the "not configured" link — MatrixCell carries no repository
  // reference of its own, only the parent MatrixRepo does.
  repositoryId: string;
  // Opens the detail drawer for a configured cell. The matrix used to own
  // the interactive element (a <button> wrapping this component) and this
  // component nested a <Link> inside it for the unconfigured case — invalid
  // HTML (a link inside a button) that breaks keyboard focus order and
  // screen-reader announcement. Now the cell owns its own single outer
  // element — a <button> here, or a <Link> when unconfigured — and the
  // matrix just tells it what to do when selected.
  onSelect: () => void;
}

/**
 * DeploymentStatusCell — one repo x env square in the deployment matrix:
 * a status badge, the short SHA and a relative timestamp for the last run,
 * or a straight line to configuring the environment when nothing is set up.
 */
export function DeploymentStatusCell({ cell, repositoryId, onSelect }: DeploymentStatusCellProps) {
  const { t } = useI18n();

  if (!cell.configured) {
    return (
      <Link to={`/repositories/${repositoryId}/deploy`} className={CELL_CLASS}>
        <span className="text-xs text-primary underline underline-offset-2">
          {t("operations.deployments.unconfigured")}
        </span>
      </Link>
    );
  }

  const run = cell.last_run;
  if (!run) {
    return (
      <button type="button" className={CELL_CLASS} onClick={onSelect}>
        <span className="text-sm text-muted-foreground">—</span>
      </button>
    );
  }

  let variant: BadgeProps["variant"] = "secondary";
  let label: string;
  let showSpinner = false;

  if (run.conclusion === "success") {
    variant = "success";
    label = t("operations.deployments.runStatus.success");
  } else if (run.conclusion === "failure") {
    variant = "destructive";
    label = t("operations.deployments.runStatus.failure");
  } else if (run.status !== "completed") {
    variant = "info";
    showSpinner = true;
    label =
      run.status === "queued"
        ? t("operations.deployments.runStatus.queued")
        : t("operations.deployments.runStatus.inProgress");
  } else {
    // Completed but neither success nor failure (e.g. cancelled) — a
    // catch-all so every value of domain.RunConclusion still renders.
    variant = "warning";
    label = t("operations.deployments.runStatus.other");
  }

  return (
    <button type="button" className={CELL_CLASS} onClick={onSelect}>
      <div className="flex flex-col gap-1">
        <Badge variant={variant} className="w-fit gap-1.5">
          {showSpinner && <Spinner size="sm" className="h-3 w-3" />}
          {label}
        </Badge>
        <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
          {/* A local run did not come from Actions; saying so on the grid is
              what stops "deployed" from reading as "deployed by CI". */}
          {run.trigger_source === "local" && (
            <>
              <span className="uppercase tracking-wide">{t("operations.deployments.localRun")}</span>
              <span>·</span>
            </>
          )}
          <span className="font-mono text-xs">{run.head_sha.slice(0, 6) || "—"}</span>
          <span>·</span>
          <span>{formatRelativeDate(run.started_at ?? run.completed_at ?? run.created_at)}</span>
        </div>
      </div>
    </button>
  );
}
