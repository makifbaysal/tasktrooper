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

/** A pending batch release Deploy was never attempted on (no deploy_started_at)
 * may be re-cut with a different version — e.g. after ErrReleaseTagExists, or
 * simply a human changing their mind before anything shipped. */
function recuttable(r: Release): boolean {
  return r.status === "pending" && !r.deploy_started_at;
}

/** One component's last 20 releases, newest first. Polls while any of them
 * is still moving (Watched()-like, or waiting on a verdict) so a merge does
 * not require a manual refresh to see settle. A batch component's draft
 * release (if any), or a pending one still eligible for a re-cut, is pinned
 * first with a Cut/Re-cut release action. */
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
  const recut = !draft && isBatch ? releases?.find((r) => r.mode === "batch" && recuttable(r)) ?? null : null;
  const pinned = draft ?? recut;
  const history = releases?.filter((r) => r.id !== pinned?.id) ?? [];
  const empty = releases !== null && !pinned && history.length === 0;

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
            {pinned && (
              <div className="flex flex-wrap items-center gap-3 px-4 py-3">
                <Badge variant="secondary">
                  {pinned.status === "draft" ? t("release.releases.draftBadge") : t(`release.statuses.${pinned.status}`)}
                </Badge>
                <span className="text-caption">
                  {pinned.status === "draft"
                    ? t("release.releases.draftLabel", { count: pinned.tasks.length })
                    : t("release.releases.recutLabel", { version: pinned.version })}
                </span>
                {pinned.tasks.length > 0 && (
                  <Button size="sm" className="ml-auto" onClick={() => setCutReleaseId(pinned.id)}>
                    {pinned.status === "draft" ? t("release.releases.cutRelease") : t("release.releases.recutRelease")}
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
