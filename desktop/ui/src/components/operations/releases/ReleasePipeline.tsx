import { ArrowRight, ChevronRight } from "lucide-react";
import { Fragment, useId, useState } from "react";
import { api, type StoreAppView, type StoreChannel, type StoreTracks, type TrackRelease } from "@/api";
import { STORE_CHANNELS, storeChannelLabelKey, trackStatusLabelKey } from "@/components/operations/StoreReleaseControls";
import { parseRolloutTarget, promoteStep, type PromoteStep } from "@/components/operations/releases/mobileReleases";
import { RolloutGauge } from "@/components/operations/releases/RolloutGauge";
import { TrackStatusPill } from "@/components/operations/releases/StorePills";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useI18n } from "@/hooks/useI18n";
import { useStoreAction } from "@/hooks/useMobileReleases";
import { cn, formatDate, formatRelativeTime } from "@/lib/utils";

const STAGE_ACCENT: Record<StoreChannel, string> = {
  internal: "border-t-muted-foreground/40",
  external: "border-t-warning",
  production: "border-t-success",
};

/** Where a Play production promote starts its staged rollout unless changed. */
const DEFAULT_PROMOTE_SHARE = "10";

interface ReleasePipelineProps {
  app: StoreAppView;
  tracks: StoreTracks;
  onPromoted: () => void;
}

/** The three channels a build walks, left to right, each with what it holds;
 * a channel promotes only into the one right after it. */
export function ReleasePipeline({ app, tracks, onPromoted }: ReleasePipelineProps) {
  const { t } = useI18n();
  const [pending, setPending] = useState<PromoteStep | null>(null);
  const [confirming, setConfirming] = useState(false);
  const [share, setShare] = useState(DEFAULT_PROMOTE_SHARE);
  const shareId = useId();
  const shareHintId = useId();
  const { busy, run, refusal, clearRefusal } = useStoreAction();
  const channel = (c: StoreChannel) => t(storeChannelLabelKey(c));
  // Play takes production as a staged rollout; App Store has its own phased release.
  const staged = pending?.to === "production" && app.platform === "android";
  const target = parseRolloutTarget(share);

  const promote = async (typed: string) => {
    if (!pending) return true;
    if (staged && !target) return false;
    const { from, to, needsPhrase } = pending;
    const ok = await run(
      "promote",
      () =>
        api.promoteStoreChannel(
          app.repository_id,
          app.platform,
          from,
          to,
          needsPhrase ? typed : undefined,
          staged && target ? target.fraction : undefined,
        ),
      t("operations.mobileReleases.promote.succeeded", { channel: channel(to) }),
    );
    if (ok) onPromoted();
    return ok;
  };

  const source = pending ? tracks[pending.from] : undefined;
  const description = !pending
    ? ""
    : pending.to === "production"
      ? t(`operations.mobileReleases.promote.production.${app.platform}`, { identifier: app.identifier, version: versionText(source) })
      : t("operations.mobileReleases.promote.description", {
          identifier: app.identifier,
          from: channel(pending.from),
          to: channel(pending.to),
          version: versionText(source),
        });

  return (
    <>
      <ol
        className="grid gap-2 @4xl:grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)_auto_minmax(0,1fr)]"
        aria-label={t("operations.mobileReleases.pipeline.flowLabel")}
      >
        {STORE_CHANNELS.map((c, index) => (
          <Fragment key={c}>
            {index > 0 && (
              <li aria-hidden className="hidden items-center justify-center text-muted-foreground @4xl:flex">
                <ChevronRight className="h-5 w-5" />
              </li>
            )}
            <ChannelStage
              app={app}
              channel={c}
              release={tracks[c]}
              step={promoteStep(app, tracks, c)}
              busy={busy !== null}
              onPromote={(next) => {
                clearRefusal();
                setPending(next);
                setShare(DEFAULT_PROMOTE_SHARE);
                setConfirming(true);
              }}
            />
          </Fragment>
        ))}
      </ol>
      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title={pending ? t("operations.mobileReleases.promote.title", { channel: channel(pending.to) }) : ""}
        description={description}
        confirmLabel={t("operations.mobileReleases.promote.confirm")}
        confirmPhrase={pending?.needsPhrase ? app.repository_name : undefined}
        confirmDisabled={staged && !target}
        variant={pending?.needsPhrase ? "destructive" : "default"}
        loading={busy === "promote"}
        error={refusal ? refusal.detail || refusal.title : null}
        errorTitle={refusal?.title}
        onConfirm={promote}
      >
        {staged && (
          <div className="space-y-1">
            <Label htmlFor={shareId}>{t("operations.mobileReleases.promote.share")}</Label>
            <div className="flex items-center gap-1.5">
              <Input
                id={shareId}
                inputMode="decimal"
                className="h-8 w-20 font-mono tabular-nums"
                value={share}
                aria-describedby={shareHintId}
                aria-invalid={!target}
                onChange={(e) => setShare(e.target.value)}
              />
              <span className="text-caption text-muted-foreground">%</span>
            </div>
            <p id={shareHintId} className="text-micro text-muted-foreground">
              {t(target ? "operations.mobileReleases.promote.shareHint" : "operations.mobileReleases.promote.shareInvalid")}
            </p>
          </div>
        )}
      </ConfirmDialog>
    </>
  );
}

function versionText(release: TrackRelease | undefined): string {
  if (!release?.has_release) return "—";
  if (release.version && release.build) return `${release.version} (${release.build})`;
  return release.version || release.build || "—";
}

interface ChannelStageProps {
  app: StoreAppView;
  channel: StoreChannel;
  release: TrackRelease;
  step: PromoteStep | null;
  busy: boolean;
  onPromote: (step: PromoteStep) => void;
}

function ChannelStage({ app, channel, release, step, busy, onPromote }: ChannelStageProps) {
  const { t, lang } = useI18n();
  const p = (key: string, params?: Record<string, string | number>) => t(`operations.mobileReleases.pipeline.${key}`, params);
  const reasonId = useId();
  const label = t(storeChannelLabelKey(channel));

  return (
    <li className="min-w-0">
      <Card
        className={cn("flex h-full min-w-0 flex-col gap-2 border-t-4 p-3", STAGE_ACCENT[channel])}
        aria-label={p("stageLabel", { channel: label })}
        role="group"
      >
        <div className="flex items-start justify-between gap-2">
          <div className="min-w-0">
            <h3 className="text-body font-semibold">{label}</h3>
            <p className="truncate text-micro text-muted-foreground">{p(`where.${app.platform}.${channel}`)}</p>
          </div>
          <TrackStatusPill status={release.has_release ? release.status : "none"} />
        </div>

        {release.has_release ? (
          <dl className="space-y-1 text-caption">
            <div>
              <dt className="sr-only">{p("version")}</dt>
              <dd className="flex min-w-0 items-baseline gap-1.5 font-mono tabular-nums">
                <span className="truncate text-title font-semibold">{release.version || "—"}</span>
                {release.build && <span className="shrink-0 text-muted-foreground">({release.build})</span>}
              </dd>
            </div>
            {release.audience && (
              <div className="flex gap-1.5">
                <dt className="text-muted-foreground">{p("audience")}</dt>
                <dd className="min-w-0 truncate">{release.audience}</dd>
              </div>
            )}
            {release.updated_at && (
              <div className="flex gap-1.5">
                <dt className="text-muted-foreground">{p("updated")}</dt>
                <dd title={formatDate(release.updated_at)}>{formatRelativeTime(release.updated_at, lang)}</dd>
              </div>
            )}
            {release.pending_version && (
              <div className="flex gap-1.5">
                <dt className="text-muted-foreground">{p("nextLabel")}</dt>
                <dd className="min-w-0 truncate">
                  <span className="font-mono">{release.pending_version}</span> · {t(trackStatusLabelKey(release.pending_status))}
                </dd>
              </div>
            )}
          </dl>
        ) : (
          <p className="text-caption text-muted-foreground">{p("channelEmpty")}</p>
        )}

        {channel === "production" && release.has_release && <RolloutGauge platform={app.platform} release={release} />}

        {step && (
          <div className="mt-auto pt-1">
            <Button
              size="sm"
              variant={step.needsPhrase ? "default" : "outline"}
              className="w-full"
              disabled={step.blocked !== null || busy}
              aria-describedby={step.blocked ? reasonId : undefined}
              onClick={() => onPromote(step)}
            >
              {t("operations.mobileReleases.promote.to", { channel: t(storeChannelLabelKey(step.to)) })}
              <ArrowRight aria-hidden />
            </Button>
            {step.blocked && (
              <p id={reasonId} className="mt-1 text-micro text-muted-foreground">
                {t(`operations.mobileReleases.promote.blocked.${step.blocked}`, { channel: label })}
              </p>
            )}
          </div>
        )}
      </Card>
    </li>
  );
}
