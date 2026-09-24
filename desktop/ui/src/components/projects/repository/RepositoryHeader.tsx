import { ChevronRight, ExternalLink, RefreshCw } from "lucide-react";
import { Link } from "react-router-dom";
import type { ProjectScan, Repository, RepositoryModel } from "@/api";
import { RepoShapeBadge } from "@/components/projects/model/RepoShapeBadge";
import { ScanProgressList } from "@/components/projects/model/ScanProgressList";
import { RepositoryGitNotice } from "@/components/projects/RepositoryGitNotice";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { useI18n } from "@/hooks/useI18n";
import { isScanFinished } from "@/lib/project-model";
import { formatRelativeDate } from "@/lib/utils";

/** github.com URLs only, in either the https or the git@ form — the only
 * remotes "Open on GitHub" makes sense for. */
function githubWebUrl(remoteUrl: string | undefined): string | null {
  if (!remoteUrl) return null;
  const ssh = remoteUrl.match(/^git@github\.com:(.+?)(\.git)?$/);
  if (ssh) return `https://github.com/${ssh[1]}`;
  const https = remoteUrl.match(/^https?:\/\/github\.com\/(.+?)(\.git)?$/);
  if (https) return `https://github.com/${https[1]}`;
  return null;
}

interface RepositoryHeaderProps {
  repository: Repository;
  model: RepositoryModel;
  projectId?: string;
  projectName?: string;
  scanning: boolean;
  scan: ProjectScan | null;
  onRescan: () => void;
  onGitRestored: () => void;
}

export function RepositoryHeader({
  repository,
  model,
  projectId,
  projectName,
  scanning,
  scan,
  onRescan,
  onGitRestored,
}: RepositoryHeaderProps) {
  const { t } = useI18n();
  const componentCount = model.components.filter((c) => c.status === "active").length;
  const githubUrl = githubWebUrl(repository.remote_url);
  const latestScan = scan ?? model.latest_scan;
  const showScanProgress = scanning || (latestScan && !isScanFinished(latestScan));

  return (
    <div className="mb-6 flex flex-col gap-3">
      <nav className="flex items-center gap-1.5 text-caption text-muted-foreground">
        <Link to="/projects" className="hover:text-foreground hover:underline">
          {t("frame.layout.sidebar.projects")}
        </Link>
        {projectName && (
          <>
            <ChevronRight className="h-3 w-3 shrink-0" aria-hidden />
            {projectId ? (
              <Link to={`/projects/${projectId}`} className="hover:text-foreground hover:underline">
                {projectName}
              </Link>
            ) : (
              <span>{projectName}</span>
            )}
          </>
        )}
        <ChevronRight className="h-3 w-3 shrink-0" aria-hidden />
        <span className="text-foreground">{repository.name}</span>
      </nav>

      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex flex-col gap-1">
          <h1 className="flex items-center gap-2 text-display font-semibold tracking-tight">
            {repository.name}
            <RepoShapeBadge shape={model.shape} />
            <span className="text-caption font-normal text-muted-foreground">
              {t("repositoryPage.header.componentCount", { count: componentCount })}
            </span>
          </h1>
          <div className="flex flex-wrap items-center gap-1.5 text-caption text-muted-foreground">
            <span>{repository.remote_url ?? t("repositoryPage.header.noRemote")}</span>
            <span aria-hidden>·</span>
            <span>
              {latestScan
                ? t("repositoryPage.header.lastScan", {
                    status: t(`repositoryPage.scanStatuses.${latestScan.status}`),
                    time: formatRelativeDate(latestScan.started_at),
                    trigger: t(`repositoryPage.scanTriggers.${latestScan.trigger}`),
                  })
                : t("repositoryPage.header.neverScanned")}
            </span>
          </div>
        </div>
        <div className="flex shrink-0 gap-2">
          <Button variant="outline" size="sm" onClick={onRescan} disabled={scanning} className="gap-1.5">
            <RefreshCw className={scanning ? "h-3.5 w-3.5 animate-spin" : "h-3.5 w-3.5"} aria-hidden />
            {scanning ? t("repositoryPage.header.rescanning") : t("repositoryPage.header.rescan")}
          </Button>
          {githubUrl && (
            <Button variant="outline" size="sm" asChild className="gap-1.5">
              <a href={githubUrl} target="_blank" rel="noreferrer">
                <ExternalLink className="h-3.5 w-3.5" aria-hidden />
                {t("repositoryPage.header.openOnGithub")}
              </a>
            </Button>
          )}
        </div>
      </div>

      <RepositoryGitNotice repository={repository} onRestored={onGitRestored} />

      {showScanProgress && (
        <Card className="p-4">
          <p className="mb-2 text-body font-medium">{t("repositoryPage.header.scanProgressTitle")}</p>
          <ScanProgressList scan={latestScan} />
        </Card>
      )}
    </div>
  );
}
