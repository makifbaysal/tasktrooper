import { AlertTriangle, Loader2 } from "lucide-react";
import { useEffect, useState } from "react";
import { api, type RepositoryModel } from "@/api";
import type { PendingRepo } from "@/components/projects/add/flow-types";
import { RoleBadge } from "@/components/projects/model/RoleBadge";
import { type CloneProgress, ScanProgressList } from "@/components/projects/model/ScanProgressList";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { useI18n } from "@/hooks/useI18n";
import { useScanProgress } from "@/hooks/useScanProgress";
import { effectiveRole } from "@/lib/project-model";

interface ScanRepoRowProps {
  repo: PendingRepo;
  onRetry: (localId: string) => void;
  /** Reports whether this row is done moving (import failed, or its scan
   * reached succeeded/failed) — the Scan step's Continue button is a
   * function of every row's settled state. */
  onSettledChange: (localId: string, settled: boolean) => void;
}

/** One row of the Scan step: the shared ScanProgressList (a GitHub import's
 * clone shown as its first stage, driven by the import call), then a summary
 * of what components were found. */
export function ScanRepoRow({ repo, onRetry, onSettledChange }: ScanRepoRowProps) {
  const { t } = useI18n();
  const { scan, finished } = useScanProgress(repo.repositoryId, { enabled: repo.status === "ready" });
  const [model, setModel] = useState<RepositoryModel | null>(null);
  const clones = repo.recipe.method === "github";
  const clone: CloneProgress | undefined = clones
    ? {
        state: repo.status === "importing" ? "running" : repo.status === "ready" ? "done" : "failed",
        error: repo.error,
      }
    : undefined;

  useEffect(() => {
    const settled = repo.status === "import_failed" || (repo.status === "ready" && finished);
    onSettledChange(repo.localId, settled);
  }, [repo.status, repo.localId, finished, onSettledChange]);

  useEffect(() => {
    if (repo.status === "ready" && finished && scan?.status === "succeeded" && repo.repositoryId) {
      void api
        .getRepositoryModel(repo.repositoryId)
        .then(setModel)
        .catch(() => setModel(null));
    }
  }, [repo.status, finished, scan?.status, repo.repositoryId]);

  const activeRoles = (model?.components ?? [])
    .filter((c) => c.status === "active")
    .map((c) => effectiveRole(c))
    .filter((r): r is NonNullable<typeof r> => Boolean(r));

  return (
    <Card>
      <CardContent className="space-y-2 py-4">
        <div className="flex items-center justify-between gap-3">
          <span className="truncate font-medium">{repo.label}</span>
          {repo.status === "importing" && !clones && (
            <span className="flex items-center gap-2 text-caption text-muted-foreground">
              <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden />
              {t("addRepository.scan.cloning")}
            </span>
          )}
        </div>

        {repo.status === "import_failed" && (
          <div className="flex items-start justify-between gap-3 rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2">
            <div className="flex items-start gap-2">
              <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-destructive" aria-hidden />
              <div className="flex flex-col">
                <span className="text-body font-medium text-destructive">{t("addRepository.scan.failedTitle")}</span>
                {repo.error && !clones && <span className="text-caption text-destructive/90">{repo.error}</span>}
              </div>
            </div>
            <Button size="sm" variant="outline" onClick={() => onRetry(repo.localId)}>
              {t("addRepository.scan.retry")}
            </Button>
          </div>
        )}

        {(repo.status === "ready" || clones) && (
          <>
            <ScanProgressList scan={repo.status === "ready" ? scan : null} clone={clone} />
            {finished && scan?.status === "failed" && (
              <p className="text-caption text-muted-foreground">{t("addRepository.scan.scanFailedHint")}</p>
            )}
            {finished && scan?.status === "succeeded" && model && (
              <div className="flex flex-wrap items-center gap-1.5 pt-1">
                <span className="text-caption text-muted-foreground">
                  {t("addRepository.scan.componentsFound", { count: model.components.filter((c) => c.status === "active").length })}
                </span>
                {activeRoles.map((role, i) => (
                  <RoleBadge key={`${role}-${i}`} role={role} />
                ))}
              </div>
            )}
          </>
        )}
      </CardContent>
    </Card>
  );
}
