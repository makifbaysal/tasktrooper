import { RefreshCw } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { api, type StoreAppView, type StoreTracks } from "@/api";
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
  PendingVersionLine,
} from "@/components/operations/StoreReleaseControls";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Notice } from "@/components/ui/notice";
import { Progress } from "@/components/ui/progress";
import { Skeleton } from "@/components/ui/skeleton";
import { tStatic, useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

interface MobileAppDetailDrawerProps {
  app: StoreAppView | null;
  onOpenChange: (open: boolean) => void;
  onActed: () => void;
}

/**
 * MobileAppDetailDrawer — one mobile store app's full detail: identifier and
 * store app id, the onboarding checklist (read-only, re-verified against the
 * store console rather than trusted from the row), and the three release
 * channels a build walks through, in flow order.
 * `app` is looked up by the caller from its freshly-reloaded list on every
 * render, so an action here (verify, submit, promote, ...) shows up-to-date
 * state the moment the parent's refetch lands — no separate local copy to
 * keep in sync. The channels come from that same row's `tracks` cache; a live
 * store read happens only when the cache is stale, or on demand.
 */
export function MobileAppDetailDrawer({ app, onOpenChange, onActed }: MobileAppDetailDrawerProps) {
  const { t } = useI18n();
  const [verifying, setVerifying] = useState(false);
  const [tracks, setTracks] = useState<StoreTracks | null>(null);
  const [tracksError, setTracksError] = useState<string | null>(null);
  const [tracksLoading, setTracksLoading] = useState(false);

  const repositoryId = app?.repository_id;
  const platform = app?.platform;
  const linked = isStoreAppLinked(app ?? undefined);
  const channelsReady = hasStoreChannels(app ?? undefined);
  // The row already carries the server's own channel cache. Its freshness is a
  // primitive stamp, which is what the effects below can safely key on — the
  // `tracks` object itself is re-created by every refetch of the parent list.
  const hasFreshCache = channelsReady && Boolean(app?.tracks) && isTracksCacheFresh(app?.tracks_synced_at);
  const cachedTracks = hasFreshCache ? (app?.tracks ?? null) : null;

  // Only the newest read may write. Opening app A, closing it and opening B
  // otherwise lets A's late answer render under B's heading, silently.
  const requestRef = useRef(0);

  const fetchTracks = useCallback(async () => {
    if (!repositoryId || !platform) return;
    const seq = ++requestRef.current;
    setTracksLoading(true);
    try {
      const fresh = await api.storeAppTracks(repositoryId, platform);
      if (seq !== requestRef.current) return;
      setTracks(fresh);
      setTracksError(null);
    } catch (err) {
      if (seq !== requestRef.current) return;
      setTracksError(err instanceof Error ? err.message : tStatic("operations.apps.channelsFailed"));
    } finally {
      if (seq === requestRef.current) setTracksLoading(false);
    }
  }, [platform, repositoryId]);

  useEffect(() => {
    requestRef.current += 1;
    setTracks(null);
    setTracksError(null);
    setTracksLoading(false);
  }, [repositoryId, platform]);

  useEffect(() => {
    // Reaching the store console costs a dozen-odd calls (an Android read is a
    // write: insert an edit, list, delete it), so it happens only when nothing
    // already answers — never merely because this drawer opened, and never
    // again once it has an answer. Refreshing is the button's job, or an
    // action's.
    if (!repositoryId || !platform || !channelsReady || hasFreshCache || tracks !== null) return;
    void fetchTracks();
  }, [channelsReady, fetchTracks, hasFreshCache, platform, repositoryId, tracks]);

  const shownTracks = tracks ?? cachedTracks;

  const verify = useCallback(async () => {
    if (!app) return;
    setVerifying(true);
    try {
      await api.verifyStoreOnboarding(app.repository_id, app.platform);
      toast.success(t("operations.apps.verified"));
      onActed();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
    } finally {
      setVerifying(false);
    }
  }, [app, onActed, t]);

  // An action just changed the channels, so the cache is behind by definition.
  const acted = useCallback(() => {
    onActed();
    void fetchTracks();
  }, [fetchTracks, onActed]);

  const checklist = app?.checklist ?? [];

  return (
    <Dialog open={app !== null} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85vh] max-w-lg overflow-y-auto">
        {app && (
          <>
            <DialogHeader>
              <DialogTitle className="font-mono text-base">{app.identifier}</DialogTitle>
              <DialogDescription>
                {app.repository_name} · {t("operations.apps.storeAppIdLabel")}:{" "}
                <span className="font-mono">{app.store_app_id || "—"}</span>
              </DialogDescription>
            </DialogHeader>

            <div className="space-y-4">
              {checklist.length > 0 && (
                <section className="space-y-2">
                  <div className="flex items-center justify-between gap-2">
                    <h3 className="text-sm font-medium">{t("operations.apps.checklist")}</h3>
                    <Button size="sm" variant="outline" disabled={verifying} onClick={() => void verify()}>
                      {verifying ? t("operations.apps.verifying") : t("operations.apps.verify")}
                    </Button>
                  </div>
                  <ul className="space-y-2">
                    {checklist.map((item) => (
                      <li key={item.key} className="flex items-center gap-2">
                        <Checkbox checked={item.done} disabled />
                        <span className="text-sm">{item.title}</span>
                      </li>
                    ))}
                  </ul>
                </section>
              )}

              <section className="space-y-2">
                <div className="flex items-center justify-between gap-2">
                  <h3 className="text-sm font-medium">{t("operations.apps.channelsTitle")}</h3>
                  {channelsReady && (
                    <Button
                      size="sm"
                      variant="ghost"
                      disabled={tracksLoading}
                      onClick={() => void fetchTracks()}
                    >
                      <RefreshCw className={cn("mr-2 h-4 w-4", tracksLoading && "animate-spin")} />
                      {t("operations.apps.refreshChannels")}
                    </Button>
                  )}
                </div>

                {!linked && <p className="text-sm text-muted-foreground">{t("operations.apps.channelsNeedApp")}</p>}
                {linked && !channelsReady && (
                  <Notice variant="info" title={t("operations.apps.channelsOnboardingTitle")}>
                    {t("operations.apps.channelsOnboarding")}
                  </Notice>
                )}
                {tracksError && (
                  <Notice variant="warning" title={t("operations.apps.channelsFailed")}>
                    {tracksError}
                  </Notice>
                )}
                {channelsReady && tracksLoading && !shownTracks && <Skeleton className="h-40 w-full" />}

                {channelsReady && shownTracks && (
                  <ol className="space-y-0">
                    {STORE_CHANNELS.map((channel, index) => {
                      const release = shownTracks[channel];
                      const fraction = release.user_fraction;
                      const showRollout =
                        channel === "production" && release.has_release && fraction !== undefined;
                      const pct = Math.round((fraction ?? 0) * 100);
                      const last = index === STORE_CHANNELS.length - 1;

                      return (
                        <li key={channel} className="flex gap-3">
                          {/* The rail is one column of the row rather than an
                              absolutely-positioned overlay, so a node that grows
                              (production's controls) stretches the line with it. */}
                          <div className="flex flex-col items-center pt-1">
                            <span
                              className={cn("h-3 w-3 shrink-0 rounded-full", STORE_CHANNEL_ACCENT[channel])}
                              aria-hidden
                            />
                            {!last && <span className="w-px flex-1 bg-border" aria-hidden />}
                          </div>

                          <div className={cn("min-w-0 flex-1 space-y-2", last ? "pb-0" : "pb-5")}>
                            <div className="flex items-start justify-between gap-2">
                              <span className="text-sm font-medium">{t(storeChannelLabelKey(channel))}</span>
                              <Badge variant={trackStatusBadgeVariant(release.status)}>
                                {t(trackStatusLabelKey(release.status))}
                              </Badge>
                            </div>

                            {release.has_release ? (
                              <>
                                <p className="font-mono text-sm tabular-nums">
                                  {release.version || "—"}
                                  {release.build && (
                                    <span className="text-muted-foreground"> ({release.build})</span>
                                  )}
                                </p>
                                {release.audience && (
                                  <p className="text-xs text-muted-foreground">
                                    {t("operations.apps.channelAudience")}: {release.audience}
                                  </p>
                                )}
                                <PendingVersionLine release={release} />
                                {showRollout && (
                                  <div className="space-y-1">
                                    <div className="flex items-center justify-between text-xs text-muted-foreground">
                                      <span>{t("operations.apps.rollout")}</span>
                                      <span className="tabular-nums">{pct}%</span>
                                    </div>
                                    <Progress value={pct} />
                                  </div>
                                )}
                              </>
                            ) : (
                              <p className="text-xs text-muted-foreground">
                                {t("operations.apps.channelEmpty")}
                              </p>
                            )}

                            {channel === "production" ? (
                              <StoreReleaseControls app={app} onActed={acted} />
                            ) : (
                              <ChannelPromoteButton
                                repositoryId={app.repository_id}
                                platform={app.platform}
                                from={channel}
                                release={release}
                                appState={app.state}
                                confirmPhrase={app.repository_name}
                                identifier={app.identifier}
                                onPromoted={acted}
                                className="max-w-56"
                              />
                            )}
                          </div>
                        </li>
                      );
                    })}
                  </ol>
                )}

                {/* An app with no channels to walk — unlinked, or still
                    onboarding — never reaches the controls nested in the
                    production card, and those are the only way to act on it. */}
                {!channelsReady && <StoreReleaseControls app={app} onActed={acted} />}
              </section>
            </div>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
