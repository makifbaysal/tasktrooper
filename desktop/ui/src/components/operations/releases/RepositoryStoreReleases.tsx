import { KeyRound, RefreshCw, Settings2, Store } from "lucide-react";
import { Link, useSearchParams } from "react-router-dom";
import type { StoreAppView } from "@/api";
import { repositoryDeployPath } from "@/components/operations/console/opsEntities";
import { MobileAppReleases } from "@/components/operations/releases/MobileAppReleases";
import { INTEGRATIONS_PATH, pickApp, STORE_PROVIDER } from "@/components/operations/releases/mobileReleases";
import { StoreAppStrip } from "@/components/operations/releases/StoreAppStrip";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { useRepositoryStoreApps, useStoreCredentials } from "@/hooks/useMobileReleases";

const PLATFORM_PARAM = "platform";

interface RepositoryStoreReleasesProps {
  repositoryId: string;
  repositoryName: string;
  /** Where the store incidents of these apps are listed. */
  issuesTarget: { entityId?: string; repositoryId?: string };
}

/** A repository's App Store and Google Play apps, one picked through
 * `?platform=`: the channels each build walks, the production rollout and
 * the test builds. A mobile entity's Releases and the repository-level store
 * page both render this. */
export function RepositoryStoreReleases({ repositoryId, repositoryName, issuesTarget }: RepositoryStoreReleasesProps) {
  const { t } = useI18n();
  const r = (key: string, params?: Record<string, string | number>) => t(`operations.mobileReleases.${key}`, params);
  const { apps, loading, error, reload } = useRepositoryStoreApps(repositoryId);
  const { credentials, configured } = useStoreCredentials();
  const [params, setParams] = useSearchParams();
  const app = pickApp(apps, params.get(PLATFORM_PARAM));

  const select = (next: StoreAppView) =>
    setParams(
      (prev) => {
        const out = new URLSearchParams(prev);
        out.set(PLATFORM_PARAM, next.platform);
        return out;
      },
      { replace: true },
    );

  if (apps.length === 0) {
    if (loading) {
      return (
        <div className="space-y-3" aria-busy>
          <Skeleton className="h-20 w-full" />
          <Skeleton className="h-48 w-full" />
        </div>
      );
    }
    if (error) {
      return (
        <Notice variant="error" title={r("loadFailed")}>
          <p>{error}</p>
          <Button size="sm" variant="outline" className="mt-2" onClick={reload}>
            <RefreshCw aria-hidden />
            {r("retry")}
          </Button>
        </Notice>
      );
    }
    const noStore = credentials !== null && !credentials.some((c) => c.configured);
    return (
      <Card>
        <EmptyState
          icon={Store}
          title={r("empty.title")}
          description={`${r("empty.description", { repository: repositoryName })}${noStore ? ` ${r("empty.noCredentials")}` : ""}`}
          action={
            <div className="flex flex-wrap justify-center gap-2">
              {noStore && (
                <Button size="sm" asChild>
                  <Link to={INTEGRATIONS_PATH}>
                    <KeyRound aria-hidden />
                    {r("empty.credentialsAction")}
                  </Link>
                </Button>
              )}
              <Button size="sm" variant="outline" asChild>
                <Link to={repositoryDeployPath(repositoryId)}>
                  <Settings2 aria-hidden />
                  {r("empty.action")}
                </Link>
              </Button>
            </div>
          }
        />
      </Card>
    );
  }

  return (
    <div className="@container space-y-5">
      <StoreAppStrip apps={apps} selectedId={app?.id} onSelect={select} />
      {app && (
        <MobileAppReleases
          key={app.id}
          app={app}
          issuesTarget={issuesTarget}
          credentialMissing={configured(STORE_PROVIDER[app.platform]) === false}
          onChanged={reload}
        />
      )}
    </div>
  );
}
