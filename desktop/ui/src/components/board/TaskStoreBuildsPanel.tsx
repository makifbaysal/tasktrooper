import { Hammer, Loader2, Smartphone } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { api, type MobileStoreApp, type MobileStorePlatform } from "@/api";
import { TaskPlatformTestBuild } from "@/components/board/TaskPlatformTestBuild";
import { hasStoreChannels } from "@/components/operations/StoreReleaseControls";
import { buildsByPlatform } from "@/components/operations/storeTestBuilds";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { useStoreTestBuilds } from "@/hooks/useStoreTestBuilds";

interface TaskStoreBuildsPanelProps {
  repositoryId: string;
  taskId: string;
  /** The repository's linked store apps, iOS first. */
  apps: MobileStoreApp[];
}

/**
 * The human UAT box's mobile test builds: per platform, the task's latest
 * TestFlight / internal app sharing build and its older attempts. The server
 * starts one on its own when the task reaches Human UAT; this is where a
 * reviewer installs it, opens it to more testers, or builds it again. Renders
 * nothing when test builds are not configured on this install.
 */
export function TaskStoreBuildsPanel({ repositoryId, taskId, apps }: TaskStoreBuildsPanelProps) {
  const { t } = useI18n();
  const { builds, error, unavailable, merge } = useStoreTestBuilds(repositoryId, { taskId, limit: 30 });
  const [starting, setStarting] = useState<MobileStorePlatform | "all" | null>(null);

  if (unavailable) return null;

  const start = async (platform?: MobileStorePlatform) => {
    setStarting(platform ?? "all");
    try {
      const res = await api.startStoreTestBuilds(repositoryId, {
        task_id: taskId,
        platforms: platform ? [platform] : undefined,
      });
      merge(res.builds ?? []);
      if (res.error) toast.warning(t("operations.storeTest.buildStartedPartial", { error: res.error }));
      else toast.success(t("operations.storeTest.buildStarted"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.actionFailed"));
    } finally {
      setStarting(null);
    }
  };

  const split = buildsByPlatform(builds ?? []);
  const hasAny = (builds?.length ?? 0) > 0;
  const anyTestable = apps.some((app) => hasStoreChannels(app));

  return (
    <div className="space-y-2 rounded-lg border border-border bg-background/60 p-3">
      <div className="flex flex-wrap items-center gap-2">
        <Smartphone className="h-4 w-4 text-muted-foreground" aria-hidden />
        <span className="text-sm font-medium">{t("operations.storeTest.panelTitle")}</span>
        {builds !== null && !hasAny && anyTestable && (
          <Button
            size="sm"
            variant="outline"
            className="ml-auto"
            disabled={starting !== null}
            onClick={() => void start()}
          >
            {starting === "all" ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Hammer className="h-3.5 w-3.5" />}
            {t("operations.storeTest.buildForTest")}
          </Button>
        )}
      </div>

      {builds === null ? (
        <Skeleton className="h-12 w-full" />
      ) : (
        <>
          {error && <p className="text-xs text-destructive">{error}</p>}
          <div className="divide-y divide-border">
            {apps.map((app) => (
              <TaskPlatformTestBuild
                key={app.platform}
                repositoryId={repositoryId}
                app={app}
                latest={split[app.platform].latest}
                older={split[app.platform].older}
                offerFirstBuild={hasAny}
                starting={starting === app.platform || starting === "all"}
                onBuild={() => void start(app.platform)}
                onChanged={(build) => merge([build])}
              />
            ))}
          </div>
        </>
      )}
    </div>
  );
}
