import type { StoreAppView } from "@/api";
import { hasTracks, formatPercent, rolloutPercent } from "@/components/operations/releases/mobileReleases";
import { LifecyclePill, ReviewPill } from "@/components/operations/releases/StorePills";
import { StorePlatformIcon } from "@/components/operations/StoreTestBuildParts";
import { storePlatformLabelKey } from "@/components/operations/storeTestBuilds";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

interface StoreAppStripProps {
  apps: StoreAppView[];
  selectedId?: string;
  onSelect: (app: StoreAppView) => void;
}

/** The entity's store apps, one pressable row each: identity, lifecycle,
 * review, versions and how far production has reached. */
export function StoreAppStrip({ apps, selectedId, onSelect }: StoreAppStripProps) {
  const { t, lang } = useI18n();
  const r = (key: string, params?: Record<string, string | number>) => t(`operations.mobileReleases.${key}`, params);
  return (
    <div className="grid gap-2 @3xl:grid-cols-2" role="group" aria-label={r("appsLabel")}>
      {apps.map((app) => {
        const pressed = app.id === selectedId;
        const production = hasTracks(app) ? app.tracks?.production : undefined;
        const percent = rolloutPercent(production);
        return (
          <Button
            key={app.id}
            variant="outline"
            aria-pressed={pressed}
            onClick={() => onSelect(app)}
            className={cn(
              "h-auto min-w-0 items-start justify-start gap-3 bg-card px-3 py-2.5 text-left whitespace-normal shadow-[var(--shadow-raised)]",
              pressed && "border-ring bg-accent ring-1 ring-ring",
            )}
          >
            <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md bg-muted">
              <StorePlatformIcon platform={app.platform} />
            </span>
            <span className="flex min-w-0 flex-1 flex-col gap-1">
              <span className="flex min-w-0 flex-wrap items-baseline gap-x-2">
                <span className="text-body font-semibold">{t(storePlatformLabelKey(app.platform))}</span>
                <span className="min-w-0 truncate font-mono text-caption font-normal text-muted-foreground">{app.identifier || "—"}</span>
              </span>
              <span className="flex flex-wrap items-center gap-1.5">
                <LifecyclePill state={app.state} />
                {app.review_state && <ReviewPill state={app.review_state} />}
              </span>
              <span className="flex flex-wrap gap-x-3 font-mono text-micro font-normal text-muted-foreground tabular-nums">
                {app.last_released_version ? (
                  <span>{r("lastReleased", { version: app.last_released_version })}</span>
                ) : (
                  <span>{r("noVersion")}</span>
                )}
                {app.last_submitted_version && <span>{r("lastSubmitted", { version: app.last_submitted_version })}</span>}
              </span>
            </span>
            {percent !== null && (
              <span className="flex shrink-0 flex-col items-end">
                <span className="text-micro font-normal text-muted-foreground">{t("operations.apps.channels.production")}</span>
                <span className="font-mono text-title font-semibold tabular-nums">{formatPercent(percent, lang)}%</span>
              </span>
            )}
          </Button>
        );
      })}
    </div>
  );
}
