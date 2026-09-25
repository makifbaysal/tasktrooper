import { ExternalLink, PackageCheck } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { api, type Component, type Release } from "@/api";
import { CutReleaseDialog } from "@/components/projects/repository/deploy/CutReleaseDialog";
import { RELEASE_ACTIVE_STATUSES, RELEASE_STATUS_VARIANT, ReleaseDrawer } from "@/components/projects/repository/deploy/ReleaseDrawer";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { formatRelativeDate } from "@/lib/utils";

const POLL_MS = 15_000;

interface ReleasesCardProps {
  repositoryId: string;
  repositoryName: string;
  component: Component;
  className?: string;
}

/** One component's last 20 releases, newest first. Polls while any of them
 * is still moving (Watched()-like, or waiting on a verdict) so a merge does
 * not require a manual refresh to see settle. A batch component's draft
 * release (if any) is pinned first with a Cut release action. */
export function ReleasesCard({ repositoryId, repositoryName, component, className }: ReleasesCardProps) {
  const { t } = useI18n();
  const [releases, setReleases] = useState<Release[] | null>(null);
  const [openReleaseId, setOpenReleaseId] = useState<string | null>(null);
  const [cutReleaseId, setCutReleaseId] = useState<string | null>(null);

  const effective = component.delivery?.override ?? component.delivery?.detected ?? null;
  const isBatch = effective?.mode === "batch";

  const load = useCallback(async () => {
    try {
      const res = await api.listReleases(repositoryId, { componentId: component.id, limit: 20 });
      setReleases(res.releases);
    } catch (e) {
      setReleases([]);
      toast.error(e instanceof Error ? e.message : t("release.releases.loadFailed"));
    }
  }, [repositoryId, component.id, t]);

  useEffect(() => {
    setReleases(null);
    void load();
  }, [load]);

  useEffect(() => {
    if (!releases?.some((r) => RELEASE_ACTIVE_STATUSES.includes(r.status))) return;
    const id = setInterval(() => void load(), POLL_MS);
    return () => clearInterval(id);
  }, [releases, load]);

  const draft = isBatch ? releases?.find((r) => r.status === "draft") ?? null : null;
  const history = releases?.filter((r) => r.status !== "draft") ?? [];
  const empty = releases !== null && !draft && history.length === 0;

  return (
    <Card className={className}>
      <CardHeader>
        <CardTitle className="text-base">{t("release.releases.title")}</CardTitle>
      </CardHeader>
      <CardContent className="p-0">
        {releases === null ? (
          <div className="p-4">
            <Skeleton className="h-24 w-full" />
          </div>
        ) : empty ? (
          <EmptyState icon={PackageCheck} title={t("release.releases.empty")} description={t("release.releases.emptyDesc")} />
        ) : (
          <div className="divide-y divide-border">
            {draft && (
              <div className="flex flex-wrap items-center gap-3 px-4 py-3">
                <Badge variant="secondary">{t("release.releases.draftBadge")}</Badge>
                <span className="text-caption">{t("release.releases.draftLabel", { count: draft.tasks.length })}</span>
                {draft.tasks.length > 0 && (
                  <Button size="sm" className="ml-auto" onClick={() => setCutReleaseId(draft.id)}>
                    {t("release.releases.cutRelease")}
                  </Button>
                )}
              </div>
            )}
            {history.map((r) => (
              <button
                key={r.id}
                type="button"
                onClick={() => setOpenReleaseId(r.id)}
                className="flex w-full flex-wrap items-center gap-3 px-4 py-3 text-left transition-colors hover:bg-muted/40"
              >
                <span className="font-mono text-caption">{r.version}</span>
                <Badge variant={RELEASE_STATUS_VARIANT[r.status]}>{t(`release.statuses.${r.status}`)}</Badge>
                <span className="text-caption text-muted-foreground">{t("release.releases.tasksCount", { count: r.tasks.length })}</span>
                <span className="ml-auto text-caption text-muted-foreground">{formatRelativeDate(r.created_at)}</span>
                {r.deploy?.run_url && (
                  <a
                    href={r.deploy.run_url}
                    target="_blank"
                    rel="noreferrer"
                    onClick={(e) => e.stopPropagation()}
                    className="inline-flex items-center gap-1 text-caption text-info hover:underline"
                  >
                    {t("release.releases.deployRun")}
                    <ExternalLink className="h-3 w-3" />
                  </a>
                )}
              </button>
            ))}
          </div>
        )}
      </CardContent>

      {openReleaseId && (
        <ReleaseDrawer
          releaseId={openReleaseId}
          repositoryName={repositoryName}
          open={Boolean(openReleaseId)}
          onOpenChange={(open) => !open && setOpenReleaseId(null)}
          onChanged={() => void load()}
        />
      )}

      {cutReleaseId && (
        <CutReleaseDialog
          open={Boolean(cutReleaseId)}
          onOpenChange={(open) => !open && setCutReleaseId(null)}
          releaseId={cutReleaseId}
          repositoryName={repositoryName}
          tagPattern={effective?.tag_pattern}
          onCut={() => {
            setCutReleaseId(null);
            void load();
          }}
        />
      )}
    </Card>
  );
}
