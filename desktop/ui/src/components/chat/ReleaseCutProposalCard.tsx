import { PackageCheck } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { api, type Release, type SessionAction } from "@/api";
import { CutReleaseDialog } from "@/components/projects/repository/deploy/CutReleaseDialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

interface ReleaseCutProposalCardProps {
  action: SessionAction;
  className?: string;
}

/**
 * The approval an agent's proposed release cut waits on, inline in the chat
 * it was asked for in. The proposal ships nothing: "Review and cut" opens the
 * same dialog as the Deploy tab, repository-name confirmation included. The
 * card reads the release's current state, so an old card in the transcript
 * says what became of it instead of offering a stale approval.
 */
export function ReleaseCutProposalCard({ action, className }: ReleaseCutProposalCardProps) {
  const { t } = useI18n();
  const releaseId = action.entity_id;
  const [release, setRelease] = useState<Release | null>(null);
  const [repositoryName, setRepositoryName] = useState<string | null>(null);
  const [gone, setGone] = useState(false);
  const [cutOpen, setCutOpen] = useState(false);
  const [dismissing, setDismissing] = useState(false);

  const load = useCallback(async () => {
    if (!releaseId) return;
    try {
      const r = await api.getRelease(releaseId);
      setRelease(r);
      const repo = await api.getRepository(r.repository_id);
      setRepositoryName(repo.name);
    } catch {
      setGone(true);
    }
  }, [releaseId]);

  useEffect(() => {
    void load();
  }, [load]);

  const dismiss = async () => {
    if (!releaseId) return;
    setDismissing(true);
    try {
      setRelease(await api.dismissCutProposal(releaseId));
      toast.success(t("release.cutProposal.dismissedToast"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("release.cutProposal.dismissFailed"));
    } finally {
      setDismissing(false);
    }
  };

  const proposal = release?.status === "draft" ? release.cut_proposal : null;
  const waiting = Boolean(proposal);
  const version = proposal?.version ?? action.title ?? "";

  return (
    <Card className={cn(waiting ? "border-primary/40 bg-primary/5" : "border-border bg-muted/40", className)}>
      <CardContent className="space-y-2 p-3">
        <div className="flex flex-wrap items-center gap-2">
          <PackageCheck className={cn("h-4 w-4 shrink-0", waiting ? "text-primary" : "text-muted-foreground")} />
          <span className="text-xs font-medium text-muted-foreground">{t("release.cutProposal.title")}</span>
          {version && (
            <Badge variant="outline" className="font-mono text-micro">
              {version}
            </Badge>
          )}
        </div>

        {gone ? (
          <p className="text-caption text-muted-foreground">{t("release.cutProposal.gone")}</p>
        ) : !release ? (
          <Skeleton className="h-8 w-full" />
        ) : proposal ? (
          <>
            <p className="text-sm leading-snug">{t("release.cutProposal.body", { count: release.tasks.length })}</p>
            <div className="flex flex-wrap justify-end gap-2">
              <Button type="button" size="sm" variant="ghost" onClick={() => void dismiss()} disabled={dismissing}>
                {t("release.cutProposal.dismiss")}
              </Button>
              <Button type="button" size="sm" onClick={() => setCutOpen(true)} disabled={!repositoryName}>
                {t("release.cutProposal.review")}
              </Button>
            </div>
          </>
        ) : release.status === "draft" ? (
          <p className="text-caption text-muted-foreground">{t("release.cutProposal.dismissed")}</p>
        ) : (
          <p className="text-caption text-muted-foreground">
            {t("release.cutProposal.cut", { version: release.version, status: t(`release.statuses.${release.status}`) })}
          </p>
        )}
      </CardContent>

      {cutOpen && releaseId && repositoryName && (
        <CutReleaseDialog
          open={cutOpen}
          onOpenChange={setCutOpen}
          releaseId={releaseId}
          repositoryName={repositoryName}
          tagPattern={release?.profile?.tag_pattern}
          onCut={(cut) => setRelease(cut)}
        />
      )}
    </Card>
  );
}
