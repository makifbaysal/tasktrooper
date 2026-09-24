import { Link2, Package, RefreshCw, Rocket } from "lucide-react";
import { useCallback, useEffect, useId, useRef, useState } from "react";
import { toast } from "sonner";
import {
  api,
  type BuildStart,
  type MobilePlatform,
  type MobileStoreApp,
  type MobileStorePlatform,
  type ReleaseEngine,
  type Repository,
  type StoreChannel,
  type StoreCredentialProvider,
  type StoreTracks,
  type TrackRelease,
} from "@/api";
import { StoreAppPickerDialog } from "@/components/admin/StoreAppPickerDialog";
import {
  ChannelPromoteButton,
  hasStoreChannels,
  isStoreAppLinked,
  isTracksCacheFresh,
  STORE_CHANNEL_ACCENT,
  STORE_CHANNELS,
  StoreReleaseControls,
  storeChannelLabelKey,
  trackStatusBadgeVariant,
  trackStatusLabelKey,
} from "@/components/operations/StoreReleaseControls";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Progress } from "@/components/ui/progress";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { tStatic, useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

const RELEASE_ENGINES: ReleaseEngine[] = ["auto", "github_actions", "local"];


export const CREDENTIAL_PROVIDER: Record<MobileStorePlatform, StoreCredentialProvider> = {
  ios: "asc",
  android: "google_play",
};

/** A cross-platform project ships to both stores, so it gets both sections. */
export function platformsFor(mobilePlatform: MobilePlatform): MobileStorePlatform[] {
  if (mobilePlatform === "ios") return ["ios"];
  if (mobilePlatform === "android") return ["android"];
  return ["ios", "android"];
}

interface ChannelCardProps {
  channel: StoreChannel;
  release: TrackRelease;
  actions?: React.ReactNode;
}

function ChannelCard({ channel, release, actions }: ChannelCardProps) {
  const { t } = useI18n();
  const fraction = release.user_fraction;
  const showRollout = channel === "production" && release.has_release && fraction !== undefined;
  const pct = Math.round((fraction ?? 0) * 100);

  return (
    <Card className="flex flex-col overflow-hidden">
      <div className={cn("h-1 w-full shrink-0", STORE_CHANNEL_ACCENT[channel])} />
      <div className="flex flex-1 flex-col gap-3 p-4">
        <div className="flex items-start justify-between gap-2">
          <h4 className="text-sm font-medium">{t(storeChannelLabelKey(channel))}</h4>
          <Badge variant={trackStatusBadgeVariant(release.status)}>{t(trackStatusLabelKey(release.status))}</Badge>
        </div>

        {release.has_release ? (
          <div className="space-y-2">
            <p className="font-mono text-sm tabular-nums">
              {release.version || "—"}
              {release.build && <span className="text-muted-foreground"> ({release.build})</span>}
            </p>
            {release.audience && (
              <p className="text-xs text-muted-foreground">
                {t("projectAdmin.mobileStore.audience")}: {release.audience}
              </p>
            )}
            {showRollout && (
              <div className="space-y-1">
                <div className="flex items-center justify-between text-xs text-muted-foreground">
                  <span>{t("projectAdmin.mobileStore.rollout")}</span>
                  <span className="tabular-nums">{pct}%</span>
                </div>
                <Progress value={pct} />
              </div>
            )}
          </div>
        ) : (
          <EmptyState
            icon={Package}
            title={t("projectAdmin.mobileStore.channelEmpty")}
            className="gap-2 px-0 py-4"
          />
        )}

        {actions && <div className="mt-auto pt-1">{actions}</div>}
      </div>
    </Card>
  );
}

interface StorePlatformSectionProps {
  repository: Repository;
  platform: MobileStorePlatform;
  app?: MobileStoreApp;
  tracks?: StoreTracks;
  tracksError?: string;
  /** Re-reads this platform's channels from the store console, not the cache. */
  onReload: () => void;
  refreshing: boolean;
  onPick: () => void;
  onBuilt: (build: BuildStart) => void;
}

function StorePlatformSection({
  repository,
  platform,
  app,
  tracks,
  tracksError,
  onReload,
  refreshing,
  onPick,
  onBuilt,
}: StorePlatformSectionProps) {
  const { t } = useI18n();
  const [building, setBuilding] = useState(false);
  const linked = isStoreAppLinked(app);
  const channelsReady = hasStoreChannels(app);
  const storeName = t(`projectAdmin.mobileStore.stores.${platform}`);

  const startBuild = async () => {
    setBuilding(true);
    try {
      const started = await api.startStoreBuild(repository.id, platform, repository.release_engine);
      onBuilt(started);
      toast.success(
        t("projectAdmin.mobileStore.buildStarted", {
          engine: t(`projectAdmin.mobileStore.engines.${started.engine}`),
        }),
      );
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
    } finally {
      setBuilding(false);
    }
  };

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-start justify-between gap-3 rounded-md border border-border/60 p-3">
        <div className="min-w-0 space-y-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-sm font-medium">{storeName}</span>
            <Badge variant={linked ? "success" : "outline"}>
              {linked ? t("projectAdmin.mobileStore.linked") : t("projectAdmin.mobileStore.notLinked")}
            </Badge>
          </div>
          <p className="truncate text-sm">{app?.app_name || app?.identifier || t("projectAdmin.mobileStore.noApp")}</p>
          {app?.identifier && <p className="truncate font-mono text-xs text-muted-foreground">{app.identifier}</p>}
          <p className="text-xs text-muted-foreground">
            {t("projectAdmin.mobileStore.storeAppId")}:{" "}
            <span className="font-mono">{app?.store_app_id || "—"}</span>
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button size="sm" variant="outline" onClick={onPick}>
            <Link2 className="mr-2 h-4 w-4" />
            {linked ? t("projectAdmin.mobileStore.changeApp") : t("projectAdmin.mobileStore.linkApp")}
          </Button>
          <Button size="sm" variant="outline" disabled={!linked || building} onClick={() => void startBuild()}>
            <Rocket className="mr-2 h-4 w-4" />
            {building ? t("projectAdmin.mobileStore.building") : t("projectAdmin.mobileStore.build")}
          </Button>
          <Button size="sm" variant="ghost" disabled={!channelsReady || refreshing} onClick={onReload}>
            <RefreshCw className={cn("mr-2 h-4 w-4", refreshing && "animate-spin")} />
            {t("projectAdmin.mobileStore.refreshChannels")}
          </Button>
        </div>
      </div>

      {!linked && (
        <p className="text-xs text-muted-foreground">{t("projectAdmin.mobileStore.channelsNeedApp")}</p>
      )}

      {linked && !channelsReady && (
        <Notice variant="info" title={t("projectAdmin.mobileStore.channelsOnboardingTitle")}>
          {t("projectAdmin.mobileStore.channelsOnboarding")}
        </Notice>
      )}

      {tracksError && (
        <Notice variant="warning" title={t("projectAdmin.mobileStore.tracksFailed")}>
          {tracksError}
        </Notice>
      )}

      {channelsReady && tracks && app && (
        <div className="grid gap-3 md:grid-cols-3">
          {STORE_CHANNELS.map((channel) => (
            <ChannelCard
              key={channel}
              channel={channel}
              release={tracks[channel]}
              actions={
                channel === "production" ? (
                  <StoreReleaseControls
                    app={{ ...app, repository_name: repository.name }}
                    onActed={onReload}
                  />
                ) : (
                  <ChannelPromoteButton
                    repositoryId={repository.id}
                    platform={platform}
                    from={channel}
                    release={tracks[channel]}
                    appState={app.state}
                    confirmPhrase={repository.name}
                    identifier={app.identifier}
                    onPromoted={onReload}
                  />
                )
              }
            />
          ))}
        </div>
      )}
    </div>
  );
}

interface MobileStorePanelProps {
  repositoryId: string;
  /** What this scope ships as; `cross_platform` and "" mean both stores. */
  mobilePlatform: MobilePlatform;
  className?: string;
}

/**
 * MobileStorePanel — the store half of a mobile scope's settings: which store
 * app this project publishes as, where its releases are built, and the three
 * channels a build walks through (internal → external → production).
 *
 * It loads the repository itself rather than taking it as a prop so the mount
 * sites stay a bare element, and so the release engine it writes back is echoed
 * from the same read (the repository PATCH clears an omitted description).
 */
export function MobileStorePanel({ repositoryId, mobilePlatform, className }: MobileStorePanelProps) {
  const { t } = useI18n();
  const engineHintId = useId();
  const platforms = platformsFor(mobilePlatform);

  const [repository, setRepository] = useState<Repository | null>(null);
  const [apps, setApps] = useState<MobileStoreApp[]>([]);
  const [tracks, setTracks] = useState<Partial<Record<MobileStorePlatform, StoreTracks>>>({});
  const [trackErrors, setTrackErrors] = useState<Partial<Record<MobileStorePlatform, string>>>({});
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [savingEngine, setSavingEngine] = useState(false);
  const [lastBuild, setLastBuild] = useState<BuildStart | null>(null);
  const [pickerFor, setPickerFor] = useState<MobileStorePlatform | null>(null);
  const [refreshing, setRefreshing] = useState<MobileStorePlatform | null>(null);

  // `platforms` is a fresh array every render, so the loader depends on its
  // joined form and splits it back — otherwise every render refetches.
  const platformKey = platforms.join(",");

  // Only the newest load may write state: a slow first pass must not land on
  // top of the refresh that overtook it.
  const requestRef = useRef(0);

  /**
   * `forceTracks` names the one platform whose channels have to come from a
   * live store read — the user asked, or an action just changed them. Every
   * other platform renders from `MobileStoreApp.tracks`, the cache the server
   * keeps on the row: a mount is not a reason to walk a store account.
   *
   * Not keyed on `t`: a language switch must not replay the whole store tour.
   */
  const load = useCallback(
    async (forceTracks?: MobileStorePlatform) => {
      const seq = ++requestRef.current;
      try {
        const [repo, appList] = await Promise.all([
          api.getRepository(repositoryId),
          api.listStoreApps(repositoryId),
        ]);
        if (seq !== requestRef.current) return;
        setRepository(repo);
        setApps(appList ?? []);
        setLoadError(null);

        const wanted = platformKey.split(",") as MobileStorePlatform[];
        const nextTracks: Partial<Record<MobileStorePlatform, StoreTracks>> = {};
        const nextErrors: Partial<Record<MobileStorePlatform, string>> = {};
        await Promise.all(
          wanted.map(async (platform) => {
            const app = (appList ?? []).find((row) => row.platform === platform);
            if (!hasStoreChannels(app)) return;
            if (forceTracks !== platform && app?.tracks && isTracksCacheFresh(app.tracks_synced_at)) {
              nextTracks[platform] = app.tracks;
              return;
            }
            try {
              nextTracks[platform] = await api.storeAppTracks(repositoryId, platform);
            } catch (err) {
              nextErrors[platform] =
                err instanceof Error ? err.message : tStatic("projectAdmin.mobileStore.tracksFailed");
            }
          }),
        );
        if (seq !== requestRef.current) return;
        setTracks(nextTracks);
        setTrackErrors(nextErrors);
      } catch (err) {
        if (seq !== requestRef.current) return;
        setLoadError(err instanceof Error ? err.message : tStatic("projectAdmin.mobileStore.loadFailed"));
      } finally {
        if (seq === requestRef.current) setLoading(false);
      }
    },
    [platformKey, repositoryId],
  );

  useEffect(() => {
    void load();
  }, [load]);

  const reloadTracks = useCallback(
    async (platform: MobileStorePlatform) => {
      setRefreshing(platform);
      try {
        await load(platform);
      } finally {
        setRefreshing(null);
      }
    },
    [load],
  );

  const engine = repository?.release_engine ?? "auto";

  const saveEngine = async (next: ReleaseEngine) => {
    if (!repository) return;
    const previous = repository.release_engine;
    setRepository({ ...repository, release_engine: next });
    setSavingEngine(true);
    try {
      // name/description are echoed back deliberately: the repository PATCH
      // applies them unconditionally and clears whatever it is not sent.
      await api.updateRepository(repositoryId, {
        name: repository.name,
        description: repository.description,
        release_engine: next,
      });
      toast.success(t("projectAdmin.mobileStore.engineSaved"));
    } catch (err) {
      setRepository((prev) => (prev ? { ...prev, release_engine: previous } : prev));
      toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
    } finally {
      setSavingEngine(false);
    }
  };

  if (loading) {
    return (
      <Card className={cn("w-full space-y-4 p-6", className)}>
        <Skeleton className="h-5 w-48" />
        <Skeleton className="h-24 w-full" />
      </Card>
    );
  }

  return (
    <Card className={cn("w-full space-y-5 p-6", className)}>
      <div>
        <h3 className="font-semibold">{t("projectAdmin.mobileStore.title")}</h3>
        <p className="text-sm text-muted-foreground">{t("projectAdmin.mobileStore.subtitle")}</p>
      </div>

      {loadError && (
        <Notice variant="error" title={t("projectAdmin.mobileStore.loadFailed")}>
          {loadError}
        </Notice>
      )}

      {repository && (
        <>
          <div className="space-y-4">
            {platforms.map((platform) => (
              <StorePlatformSection
                key={platform}
                repository={repository}
                platform={platform}
                app={apps.find((row) => row.platform === platform)}
                tracks={tracks[platform]}
                tracksError={trackErrors[platform]}
                onReload={() => void reloadTracks(platform)}
                refreshing={refreshing === platform}
                onPick={() => setPickerFor(platform)}
                onBuilt={setLastBuild}
              />
            ))}
          </div>

          <Separator />

          <div className="space-y-2">
            <Label htmlFor="release-engine">{t("projectAdmin.mobileStore.engineTitle")}</Label>
            <Select
              value={engine}
              disabled={savingEngine}
              onValueChange={(value) => void saveEngine(value as ReleaseEngine)}
            >
              <SelectTrigger id="release-engine" aria-describedby={engineHintId} className="max-w-72">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {RELEASE_ENGINES.map((value) => (
                  <SelectItem key={value} value={value}>
                    {t(`projectAdmin.mobileStore.engines.${value}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <p id={engineHintId} className="text-xs text-muted-foreground">
              {t(`projectAdmin.mobileStore.engineHints.${engine}`)}
            </p>
            <p className="text-xs text-muted-foreground">
              {lastBuild
                ? t("projectAdmin.mobileStore.lastRunEngine", {
                    engine: t(`projectAdmin.mobileStore.engines.${lastBuild.engine}`),
                    store: t(`projectAdmin.mobileStore.stores.${lastBuild.platform}`),
                  })
                : t("projectAdmin.mobileStore.lastRunNone")}
            </p>
          </div>
        </>
      )}

      {pickerFor && (
        <StoreAppPickerDialog
          provider={CREDENTIAL_PROVIDER[pickerFor]}
          platform={pickerFor}
          repositoryId={repositoryId}
          open
          onOpenChange={(open: boolean) => {
            if (!open) setPickerFor(null);
          }}
          onLinked={() => {
            setPickerFor(null);
            void load();
          }}
        />
      )}
    </Card>
  );
}
