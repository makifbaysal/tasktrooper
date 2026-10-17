import type { MobileStorePlatform, TrackRelease } from "@/api";
import { formatPercent, IOS_PHASED_DAYS, phasedReleaseDay, rolloutPercent } from "@/components/operations/releases/mobileReleases";
import { Progress, type ProgressTone } from "@/components/ui/progress";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

/** How far production has reached, shown first and largest: the share, the bar,
 * and for Apple's phased release the day of seven it is on. */
export function RolloutGauge({ platform, release }: { platform: MobileStorePlatform; release: TrackRelease }) {
  const { t, lang } = useI18n();
  const r = (key: string, params?: Record<string, string | number>) => t(`operations.mobileReleases.rollout.${key}`, params);
  const percent = rolloutPercent(release);
  const day = phasedReleaseDay(platform, release);
  const halted = release.status === "halted";
  const tone: ProgressTone = halted ? "destructive" : percent === 100 ? "success" : "warning";

  let caption: string;
  if (percent === null) caption = r("unknown");
  else if (halted) caption = r("halted", { percent: formatPercent(percent, lang) });
  else if (day !== null) caption = r("phasedDay", { day, total: IOS_PHASED_DAYS });
  else if (percent === 100) caption = r("full");
  else if (percent === 0) caption = r("notStarted");
  else caption = r(platform === "ios" ? "phased" : "staged");

  return (
    <div className="space-y-1.5" data-testid="rollout-gauge">
      <div className="flex items-baseline justify-between gap-2">
        <span className="text-micro font-medium tracking-wide text-muted-foreground uppercase">{r("label")}</span>
        <span className={cn("text-caption", halted ? "font-medium text-destructive" : "text-muted-foreground")}>{caption}</span>
      </div>
      <p className="font-mono text-display leading-none font-semibold tabular-nums">
        {percent === null ? "—" : formatPercent(percent, lang)}
        {percent !== null && <span className="text-title text-muted-foreground">%</span>}
      </p>
      <Progress
        value={percent ?? 0}
        tone={tone}
        label={percent === null ? r("unknown") : r("progressLabel", { percent: formatPercent(percent, lang) })}
      />
      {platform === "ios" && day !== null && (
        <ol className="grid grid-cols-7 gap-0.5" aria-hidden>
          {Array.from({ length: IOS_PHASED_DAYS }, (_, i) => (
            <li key={i} className={cn("h-1 rounded-full", i < day ? (halted ? "bg-destructive/70" : "bg-warning") : "bg-surface-sunken")} />
          ))}
        </ol>
      )}
    </div>
  );
}
