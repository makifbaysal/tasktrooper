import { Hammer, KeyRound, OctagonAlert, RefreshCw, Settings2 } from "lucide-react";
import { Link } from "react-router-dom";
import { api, type StoreAppView } from "@/api";
import { issuesPath, repositoryDeployPath } from "@/components/operations/console/opsEntities";
import { MobileVitals } from "@/components/operations/releases/MobileVitals";
import { formatPercent, INTEGRATIONS_PATH, rolloutPercent, STORE_PROVIDER } from "@/components/operations/releases/mobileReleases";
import { OnboardingChecklist } from "@/components/operations/releases/OnboardingChecklist";
import { ProductionControls } from "@/components/operations/releases/ProductionControls";
import { ReleasePipeline } from "@/components/operations/releases/ReleasePipeline";
import { ReleaseSection } from "@/components/operations/releases/ReleaseSection";
import { TestBuildsTable } from "@/components/operations/releases/TestBuildsTable";
import { TestGroupsTable } from "@/components/operations/releases/TestGroupsTable";
import { useOpsHref, useOptionalOpsContext } from "@/components/operations/shell/OpsContext";
import { hasStoreChannels, isStoreAppLinked } from "@/components/operations/StoreReleaseControls";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { useStoreAction, useStoreTracks } from "@/hooks/useMobileReleases";
import { useStoreTestGroups } from "@/hooks/useStoreTestGroups";
import { cn, formatDate, formatRelativeTime } from "@/lib/utils";

interface MobileAppReleasesProps {
  app: StoreAppView;
  /** Where a store incident is listed: the mobile entity, else the repository. */
  issuesTarget: { entityId?: string; repositoryId?: string };
  /** True when the vault says this store's credential was never saved. */
  credentialMissing: boolean;
  onChanged: () => void;
}

const LINK = "inline-flex items-center gap-1 font-medium text-info hover:underline";

/** One store app's releases: its channels and production rollout, what can be
 * done to them, its crash and launch slots, then its test builds and groups. */
export function MobileAppReleases({ app, issuesTarget, credentialMissing, onChanged }: MobileAppReleasesProps) {
  const { t, lang } = useI18n();
  const r = (key: string, params?: Record<string, string | number>) => t(`operations.mobileReleases.${key}`, params);
  const href = useOpsHref();
  const env = useOptionalOpsContext()?.env;
  const linked = isStoreAppLinked(app);
  const channelsReady = hasStoreChannels(app);
  const { tracks, readAt, stale, loading, error, refresh } = useStoreTracks(app);
  const testGroups = useStoreTestGroups(app.repository_id, app.platform, channelsReady);
  const { busy, run } = useStoreAction();
  const store = r(`stores.${STORE_PROVIDER[app.platform]}`);
  const production = tracks?.production;
  const halted = Boolean(production?.has_release && production.status === "halted");
  const rejected = app.review_state === "rejected";
  const issuesHref = href(issuesPath(issuesTarget));

  const acted = () => {
    onChanged();
    void refresh();
  };

  const startBuild = () =>
    run(
      "build",
      () => api.startStoreBuild(app.repository_id, app.platform),
      (start) => r("pipeline.buildStarted", { engine: r(`engines.${start.engine}`) }),
    );

  const readAtIso = readAt !== null ? new Date(readAt).toISOString() : null;

  return (
    <div className="space-y-5">
      {credentialMissing && (
        <Notice variant="warning" title={r("credentialsMissing.title", { store })}>
          <span>{r("credentialsMissing.body", { store })} </span>
          <Link to={INTEGRATIONS_PATH} className={LINK}>
            <KeyRound className="h-3 w-3" aria-hidden />
            {r("credentialsMissing.action")}
          </Link>
        </Notice>
      )}
      {halted && production && (
        <Notice variant="error" title={r("incident.haltedTitle")}>
          <span>
            {r("incident.halted", {
              identifier: app.identifier,
              percent: formatPercent(rolloutPercent(production) ?? 0, lang),
            })}{" "}
          </span>
          <Link to={issuesHref} className={LINK}>
            <OctagonAlert className="h-3 w-3" aria-hidden />
            {r("incident.viewIssues")}
          </Link>
        </Notice>
      )}
      {rejected && (
        <Notice variant="error" title={r("incident.rejectedTitle")}>
          <span>{r("incident.rejected", { identifier: app.identifier, version: app.last_submitted_version || "—" })} </span>
          <Link to={issuesHref} className={LINK}>
            <OctagonAlert className="h-3 w-3" aria-hidden />
            {r("incident.viewIssues")}
          </Link>
        </Notice>
      )}

      <ReleaseSection
        title={r("pipeline.title")}
        actions={
          <>
            {channelsReady && (
              <span className="text-micro text-muted-foreground" title={readAtIso ? formatDate(readAtIso) : undefined}>
                {loading
                  ? r("pipeline.refreshing")
                  : readAtIso
                    ? r("pipeline.readAgo", { time: formatRelativeTime(readAtIso, lang) })
                    : r("pipeline.notRead")}
              </span>
            )}
            {channelsReady && (
              <Button
                size="icon"
                variant="ghost"
                className="h-8 w-8"
                disabled={loading}
                onClick={() => void refresh()}
                aria-label={r("pipeline.refresh")}
                title={r("pipeline.refresh")}
              >
                <RefreshCw className={cn(loading && "animate-spin")} aria-hidden />
              </Button>
            )}
            {linked && (
              <Button size="sm" variant="outline" disabled={busy !== null} onClick={() => void startBuild()}>
                <Hammer aria-hidden />
                {r("pipeline.startBuild")}
              </Button>
            )}
          </>
        }
      >
        {!linked && (
          <Notice variant="info" title={r("pipeline.notLinkedTitle")}>
            <span>{r("pipeline.notLinked")} </span>
            <Link to={repositoryDeployPath(app.repository_id)} className={LINK}>
              <Settings2 className="h-3 w-3" aria-hidden />
              {r("empty.action")}
            </Link>
          </Notice>
        )}
        {linked && !channelsReady && (
          <Notice variant="info" title={r("pipeline.onboardingTitle")}>
            {r("pipeline.onboarding")}
          </Notice>
        )}
        {error && (
          <Notice variant="warning" title={r("pipeline.readFailed")}>
            {error}
          </Notice>
        )}
        {stale && <p className="text-micro text-muted-foreground">{r("pipeline.staleNote")}</p>}
        {channelsReady && !tracks && loading && <Skeleton className="h-44 w-full" />}
        {channelsReady && tracks && <ReleasePipeline app={app} tracks={tracks} onPromoted={acted} />}
      </ReleaseSection>

      {linked && app.state !== "live" && <OnboardingChecklist app={app} onVerified={acted} />}

      {channelsReady && tracks && (
        <ReleaseSection title={r("production.title")}>
          <ProductionControls app={app} production={production} onActed={acted} />
        </ReleaseSection>
      )}

      <ReleaseSection title={r("vitals.title")}>
        <MobileVitals componentId={issuesTarget.entityId} env={env} />
      </ReleaseSection>

      {channelsReady && <TestBuildsTable app={app} groups={testGroups.groups} groupsError={testGroups.error} />}
      {channelsReady && <TestGroupsTable app={app} state={testGroups} />}
    </div>
  );
}
