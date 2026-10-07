import { useCallback, useId, useState } from "react";
import { toast } from "sonner";
import {
  api,
  type MobileStoreApp,
  type MobileStorePlatform,
  type MobileStoreState,
  type StoreAppView,
  type StoreChannel,
  type TrackRelease,
  type TrackStatus,
} from "@/api";
import type { BadgeProps } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useI18n } from "@/hooks/useI18n";

type AndroidTrack = "internal" | "production";
type ConfirmKind = "submit" | "release" | "promote" | "halt";

/**
 * The channel vocabulary, in flow order. Everything that renders a channel or a
 * track status reads its label and colour from here — `MobileStorePanel`
 * (project settings) and `MobileAppDetailDrawer` (Operations) must not grow
 * their own copies of these switches.
 */
export const STORE_CHANNELS: StoreChannel[] = ["internal", "external", "production"];

/**
 * The single step forward the server permits out of each channel. Skipping a
 * channel is refused server-side, and `production` is terminal — from there the
 * only moves are the rollout actions below.
 */
export const NEXT_STORE_CHANNEL: Record<StoreChannel, StoreChannel | null> = {
  internal: "external",
  external: "production",
  production: null,
};

export const storeChannelLabelKey = (channel: StoreChannel) => `operations.apps.channels.${channel}`;

/**
 * Every track status the dictionary has a sentence for. A store can answer with
 * a status this build has never heard of; interpolating it straight into the
 * key would print the raw key, and falling back to `none` would be worse — that
 * one claims the channel is empty when it is not.
 */
const KNOWN_TRACK_STATUSES = new Set<string>([
  "none",
  "draft",
  "in_review",
  "rolling_out",
  "halted",
  "live",
  "unknown",
]);

export const trackStatusLabelKey = (status?: TrackStatus) => {
  if (!status) return "operations.apps.trackStatus.none";
  return KNOWN_TRACK_STATUSES.has(status)
    ? `operations.apps.trackStatus.${status}`
    : "operations.apps.trackStatus.unknown";
};

/** Top stripe per channel: neutral, then warning, then success as risk rises. */
export const STORE_CHANNEL_ACCENT: Record<StoreChannel, string> = {
  internal: "bg-muted-foreground/40",
  external: "bg-warning",
  production: "bg-success",
};

export function trackStatusBadgeVariant(status?: TrackStatus): BadgeProps["variant"] {
  switch (status) {
    case "live":
      return "success";
    case "rolling_out":
    case "in_review":
      return "warning";
    case "halted":
      return "destructive";
    default:
      // `none`, `draft` and anything this build does not recognise: a coloured
      // pill would assert a verdict the store never gave.
      return "outline";
  }
}

/**
 * Whether this row is actually bound to an app in the store console.
 *
 * Only App Store Connect gives an app a second identity — Play is keyed by the
 * package name alone, so `store_app_id` stays empty there by design and testing
 * it would call every linked Android app unlinked forever.
 */
export function isStoreAppLinked(
  app?: Pick<MobileStoreApp, "platform" | "identifier" | "store_app_id">,
): boolean {
  if (!app) return false;
  return app.platform === "ios" ? Boolean(app.store_app_id) : Boolean(app.identifier);
}

/**
 * Whether this app has channels to read at all. Linked is not enough: nothing
 * sits on any channel until onboarding finishes, and Play refuses
 * `edits.insert` outright before an app's first upload — the whole reason the
 * `play_first_upload` checklist item exists — so asking would fail on every
 * try rather than answer "empty". The server draws the same line for both
 * stores: its monitor syncs `tracks` only for `test_ready` and `live` rows.
 *
 * Distinct from `isStoreAppLinked`: an onboarding app IS linked, it just has
 * no channels yet, and the two must not be reported with the same sentence.
 */
export function hasStoreChannels(
  app?: Pick<MobileStoreApp, "platform" | "identifier" | "store_app_id" | "state">,
): boolean {
  if (!isStoreAppLinked(app)) return false;
  return app?.state === "test_ready" || app?.state === "live";
}

/**
 * How old `MobileStoreApp.tracks` may be before a screen reads the channels
 * live instead. The server's store monitor refreshes that cache once per sweep
 * (a minute by default), so a stamp older than this means the sweep is not
 * running and the cached channels cannot be trusted.
 */
const TRACKS_CACHE_MAX_AGE_MS = 5 * 60 * 1000;

/**
 * Whether the row's cached channel view is recent enough to render as-is.
 * `syncedAt` absent means the cache has never been filled — distinct from a
 * cache that was filled and found nothing, which is why the stamp is the test.
 */
export function isTracksCacheFresh(syncedAt?: string): boolean {
  if (!syncedAt) return false;
  const at = Date.parse(syncedAt);
  return Number.isFinite(at) && Date.now() - at < TRACKS_CACHE_MAX_AGE_MS;
}

interface ChannelPromoteButtonProps {
  repositoryId: string;
  platform: MobileStorePlatform;
  from: StoreChannel;
  release: TrackRelease;
  /** The app's own lifecycle state — production is closed unless it is `live`. */
  appState: MobileStoreState;
  /**
   * What the user must retype to promote into production. The server rejects a
   * production promote whose `confirm` does not match, so this is the value we
   * both display and send — not merely a UI brake.
   */
  confirmPhrase: string;
  identifier: string;
  onPromoted: () => void;
  className?: string;
}

/**
 * The one-step-forward promote out of `from`. Renders nothing on `production`,
 * which has no channel left to move to. When the promote is not available the
 * button is disabled and the reason is rendered as visible text wired up with
 * `aria-describedby` — a disabled button takes no focus, so a `title` alone
 * would never reach a screen reader.
 */
export function ChannelPromoteButton({
  repositoryId,
  platform,
  from,
  release,
  appState,
  confirmPhrase,
  identifier,
  onPromoted,
  className,
}: ChannelPromoteButtonProps) {
  const { t } = useI18n();
  const [busy, setBusy] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const reasonId = useId();

  const to = NEXT_STORE_CHANNEL[from];
  if (!to) return null;

  const toLabel = t(storeChannelLabelKey(to));
  const toProduction = to === "production";
  // storeops.PromoteChannel refuses a production promote from anything but a
  // live app, on top of needing something on the source channel — mirror both
  // rather than offer a button whose only outcome is the server's 409.
  const blockedKey = !release.has_release
    ? "projectAdmin.mobileStore.promoteDisabledEmpty"
    : toProduction && appState !== "live"
      ? "projectAdmin.mobileStore.promoteDisabledNotLive"
      : null;
  const blocked = blockedKey !== null;

  const promote = async () => {
    setBusy(true);
    try {
      await api.promoteStoreChannel(repositoryId, platform, from, to, toProduction ? confirmPhrase : undefined);
      toast.success(t("projectAdmin.mobileStore.promoteSucceeded", { channel: toLabel }));
      onPromoted();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className={className}>
      <Button
        size="sm"
        variant={toProduction ? "default" : "outline"}
        className="w-full"
        disabled={blocked || busy}
        aria-describedby={blocked ? reasonId : undefined}
        onClick={() => setConfirming(true)}
      >
        {t("projectAdmin.mobileStore.promoteTo", { channel: toLabel })}
      </Button>
      {blockedKey && (
        <p id={reasonId} className="mt-1.5 text-xs text-muted-foreground">
          {t(blockedKey)}
        </p>
      )}

      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title={t("projectAdmin.mobileStore.promoteTitle", { channel: toLabel })}
        description={t("projectAdmin.mobileStore.promoteDescription", {
          identifier,
          from: t(storeChannelLabelKey(from)),
          to: toLabel,
        })}
        confirmLabel={t("projectAdmin.mobileStore.promoteConfirm")}
        confirmPhrase={toProduction ? confirmPhrase : undefined}
        variant={toProduction ? "destructive" : "default"}
        loading={busy}
        onConfirm={promote}
      />
    </div>
  );
}

interface StoreReleaseControlsProps {
  app: StoreAppView;
  onActed: () => void;
}

/**
 * StoreReleaseControls — the store-side release actions for one mobile app:
 * iOS submit/release, Android promote/rollout/halt/resume. Enable/disable
 * conditions mirror the state the server itself checks (release.go), so a
 * user rarely hits the server's own 409 — but the server enforces its rule
 * independently regardless of what this component allows through. Every
 * production-class action (submit, release, promote-to-production, halt)
 * goes through ui/confirm-dialog with a typed confirmPhrase; the reversible
 * ones (rollout dial, resume) call straight through.
 */
export function StoreReleaseControls({ app, onActed }: StoreReleaseControlsProps) {
  const { t } = useI18n();
  // One base per instance, suffixed per action: a disabled button takes no
  // focus, so each "why is this off" sentence has to be visible text the
  // button points at with aria-describedby rather than a title tooltip.
  const reasonId = useId();
  const [busy, setBusy] = useState(false);
  const [confirmKind, setConfirmKind] = useState<ConfirmKind | null>(null);
  const [track, setTrack] = useState<AndroidTrack>("internal");
  const [promoteFraction, setPromoteFraction] = useState(1);
  const [rolloutFraction, setRolloutFraction] = useState(1);

  const run = useCallback(
    async (action: () => Promise<void>, successKey: string) => {
      setBusy(true);
      try {
        await action();
        toast.success(t(successKey));
        onActed();
      } catch (err) {
        toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
      } finally {
        setBusy(false);
      }
    },
    [onActed, t],
  );

  const trackLabel = (value: AndroidTrack) =>
    value === "internal" ? t("operations.apps.trackInternal") : t("operations.apps.trackProduction");

  if (app.platform === "ios") {
    // The server (storeops.SubmitIOS, release.go) only ever permits
    // submitting a `live` app — half-onboarded apps have no confirmed store
    // app id to submit a version against. Mirror that here so the button
    // isn't enabled in a state the server rejects with a 409.
    const canSubmit = app.state === "live";
    const canRelease = app.review_state === "approved";

    return (
      <div className="space-y-3">
        <div className="flex flex-wrap items-start gap-2">
          <div>
            <Button
              size="sm"
              disabled={!canSubmit || busy}
              aria-describedby={!canSubmit ? `${reasonId}-submit` : undefined}
              onClick={() => setConfirmKind("submit")}
            >
              {t("operations.apps.submit")}
            </Button>
            {!canSubmit && (
              <p id={`${reasonId}-submit`} className="mt-1.5 max-w-56 text-xs text-muted-foreground">
                {t("operations.apps.submitDisabled")}
              </p>
            )}
          </div>
          <div>
            <Button
              size="sm"
              variant="outline"
              disabled={!canRelease || busy}
              aria-describedby={!canRelease ? `${reasonId}-release` : undefined}
              onClick={() => setConfirmKind("release")}
            >
              {t("operations.apps.release")}
            </Button>
            {!canRelease && (
              <p id={`${reasonId}-release`} className="mt-1.5 max-w-56 text-xs text-muted-foreground">
                {t("operations.apps.releaseDisabled")}
              </p>
            )}
          </div>
        </div>

        <ConfirmDialog
          open={confirmKind === "submit"}
          onOpenChange={(open) => !open && setConfirmKind(null)}
          title={t("operations.apps.submitTitle")}
          description={t("operations.apps.submitDescription", { identifier: app.identifier })}
          confirmLabel={t("operations.apps.submit")}
          confirmPhrase={app.repository_name}
          loading={busy}
          onConfirm={() =>
            run(
              () => api.submitIOS(app.repository_id, app.repository_name),
              "operations.apps.submitSucceeded",
            )
          }
        />
        <ConfirmDialog
          open={confirmKind === "release"}
          onOpenChange={(open) => !open && setConfirmKind(null)}
          title={t("operations.apps.releaseTitle")}
          description={t("operations.apps.releaseDescription", { identifier: app.identifier })}
          confirmLabel={t("operations.apps.release")}
          confirmPhrase={app.repository_name}
          loading={busy}
          onConfirm={() =>
            run(
              () => api.releaseIOS(app.repository_id, app.repository_name),
              "operations.apps.releaseSucceeded",
            )
          }
        />
      </div>
    );
  }

  // Android.
  const isLive = app.state === "live";
  const promoteReady = app.state === "live" || (track === "internal" && app.state === "test_ready");

  return (
    <div className="space-y-4">
      <div className="space-y-1">
        <Label className="text-xs font-normal text-muted-foreground">{t("operations.apps.promote")}</Label>
        <div className="flex flex-wrap items-end gap-2">
          <div className="space-y-1">
            <Label htmlFor="promote-track" className="text-xs font-normal text-muted-foreground">
              {t("operations.apps.targetTrack")}
            </Label>
            <Select value={track} onValueChange={(v) => setTrack(v as AndroidTrack)}>
              <SelectTrigger id="promote-track" className="w-36">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="internal">{t("operations.apps.trackInternal")}</SelectItem>
                <SelectItem value="production">{t("operations.apps.trackProduction")}</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-1">
            <Label htmlFor="promote-fraction" className="text-xs font-normal text-muted-foreground">
              {t("operations.apps.rollout")}
            </Label>
            <Input
              id="promote-fraction"
              type="number"
              min={0}
              max={1}
              step={0.05}
              value={promoteFraction}
              onChange={(e) => setPromoteFraction(Number(e.target.value))}
              className="w-24"
            />
          </div>
          <Button
            size="sm"
            disabled={!promoteReady || busy}
            aria-describedby={!promoteReady ? `${reasonId}-promote` : undefined}
            onClick={() => setConfirmKind("promote")}
          >
            {t("operations.apps.promote")}
          </Button>
        </div>
        {!promoteReady && (
          <p id={`${reasonId}-promote`} className="text-xs text-muted-foreground">
            {t("operations.apps.promoteDisabled")}
          </p>
        )}
      </div>

      <div className="space-y-1">
        <Label htmlFor="rollout-fraction" className="text-xs font-normal text-muted-foreground">
          {t("operations.apps.rollout")}
        </Label>
        <div className="flex items-center gap-2">
          <Input
            id="rollout-fraction"
            type="number"
            min={0}
            max={1}
            step={0.05}
            value={rolloutFraction}
            disabled={!isLive}
            onChange={(e) => setRolloutFraction(Number(e.target.value))}
            className="w-24"
          />
          <Button
            size="sm"
            variant="outline"
            disabled={!isLive || busy}
            aria-describedby={!isLive ? `${reasonId}-rollout` : undefined}
            onClick={() =>
              void run(
                () => api.setAndroidRollout(app.repository_id, rolloutFraction),
                "operations.apps.rolloutSucceeded",
              )
            }
          >
            {t("operations.apps.apply")}
          </Button>
        </div>
        {!isLive && (
          <p id={`${reasonId}-rollout`} className="text-xs text-muted-foreground">
            {t("operations.apps.rolloutDisabled")}
          </p>
        )}
      </div>

      <div className="flex flex-wrap items-start gap-2">
        <div>
          <Button
            size="sm"
            variant="destructive"
            disabled={!isLive || busy}
            aria-describedby={!isLive ? `${reasonId}-halt` : undefined}
            onClick={() => setConfirmKind("halt")}
          >
            {t("operations.apps.halt")}
          </Button>
          {!isLive && (
            <p id={`${reasonId}-halt`} className="mt-1.5 max-w-56 text-xs text-muted-foreground">
              {t("operations.apps.haltDisabled")}
            </p>
          )}
        </div>
        <div>
          <Button
            size="sm"
            variant="outline"
            disabled={!isLive || busy}
            aria-describedby={!isLive ? `${reasonId}-resume` : undefined}
            onClick={() => void run(() => api.resumeAndroid(app.repository_id), "operations.apps.resumeSucceeded")}
          >
            {t("operations.apps.resume")}
          </Button>
          {!isLive && (
            <p id={`${reasonId}-resume`} className="mt-1.5 max-w-56 text-xs text-muted-foreground">
              {t("operations.apps.resumeDisabled")}
            </p>
          )}
        </div>
      </div>

      <ConfirmDialog
        open={confirmKind === "promote"}
        onOpenChange={(open) => !open && setConfirmKind(null)}
        title={t("operations.apps.promoteTitle", { track: trackLabel(track) })}
        description={t("operations.apps.promoteDescription", {
          identifier: app.identifier,
          track: trackLabel(track),
          fraction: String(promoteFraction),
        })}
        confirmLabel={t("operations.apps.promote")}
        confirmPhrase={track === "production" ? app.repository_name : undefined}
        variant={track === "production" ? "destructive" : "default"}
        loading={busy}
        onConfirm={() =>
          run(
            () =>
              api.promoteAndroid(app.repository_id, {
                to_track: track,
                user_fraction: promoteFraction,
                confirm: track === "production" ? app.repository_name : undefined,
              }),
            "operations.apps.promoteSucceeded",
          )
        }
      />
      <ConfirmDialog
        open={confirmKind === "halt"}
        onOpenChange={(open) => !open && setConfirmKind(null)}
        title={t("operations.apps.haltTitle")}
        description={t("operations.apps.haltDescription", { identifier: app.identifier })}
        confirmLabel={t("operations.apps.halt")}
        confirmPhrase={app.repository_name}
        loading={busy}
        onConfirm={() =>
          run(() => api.haltAndroid(app.repository_id, app.repository_name), "operations.apps.haltSucceeded")
        }
      />
    </div>
  );
}

/** The version queued behind what a channel holds, e.g. "Next: 1.5.0 · In review". */
export function PendingVersionLine({ release }: { release: TrackRelease }) {
  const { t } = useI18n();
  if (!release.pending_version) return null;
  return (
    <p className="text-xs text-muted-foreground">
      {t("operations.apps.pendingVersion", {
        version: release.pending_version,
        status: t(trackStatusLabelKey(release.pending_status)),
      })}
    </p>
  );
}
