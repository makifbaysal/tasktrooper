import { ArrowLeft, FolderGit2, SearchX } from "lucide-react";
import { Link, useParams } from "react-router-dom";
import { RepositoryStoreReleases } from "@/components/operations/releases/RepositoryStoreReleases";
import { useOpsHref, useOpsPageScope } from "@/components/operations/shell/OpsContext";
import { useOpsDirectory } from "@/components/operations/shell/useOpsEntities";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { useRepositoryStoreApps } from "@/hooks/useMobileReleases";

/** `/operations/mobile/app/:repositoryId`: a repository's store apps when no
 * mobile component carries them (nothing scanned as `mobile`, or only a
 * desktop or library one) — the same releases view an entity's tab shows. */
export default function OpsStoreAppsPage() {
  const { t } = useI18n();
  const r = (key: string, params?: Record<string, string | number>) => t(`operations.mobileReleases.repositoryPage.${key}`, params);
  useOpsPageScope({ time: false, env: false, note: t("operations.mobileReleases.scopeNote") });
  const { repositoryId = "" } = useParams();
  const href = useOpsHref();
  const { repositories, loaded } = useOpsDirectory();
  const { apps, loading } = useRepositoryStoreApps(repositoryId);
  const repository = repositories.find((repo) => repo.id === repositoryId);
  const name = repository?.name ?? apps[0]?.repository_name ?? "";

  if (!name) {
    if (!loaded || loading) {
      return (
        <div className="space-y-4" aria-busy>
          <Skeleton className="h-10 w-full max-w-md" />
          <Skeleton className="h-64 w-full" />
        </div>
      );
    }
    return (
      <Card>
        <EmptyState
          icon={SearchX}
          title={r("notFound.title")}
          description={r("notFound.description")}
          action={
            <Button variant="outline" size="sm" asChild>
              <Link to={href("/operations/mobile")}>{r("back")}</Link>
            </Button>
          }
        />
      </Card>
    );
  }

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
        <div className="min-w-0 flex-1">
          <h2 className="truncate text-title font-semibold">{r("title", { repository: name })}</h2>
          <p className="text-caption text-muted-foreground">{r("description")}</p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button size="sm" variant="ghost" asChild>
            <Link to={href("/operations/mobile")}>
              <ArrowLeft aria-hidden />
              {r("back")}
            </Link>
          </Button>
          <Button size="sm" variant="outline" asChild>
            <Link to={`/repositories/${encodeURIComponent(repositoryId)}`}>
              <FolderGit2 aria-hidden />
              {r("openRepository")}
            </Link>
          </Button>
        </div>
      </div>
      <RepositoryStoreReleases repositoryId={repositoryId} repositoryName={name} issuesTarget={{ repositoryId }} />
    </div>
  );
}
