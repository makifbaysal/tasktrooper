import { ExternalLink, GitPullRequest, Sparkles } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { api, type RepoDocKind, type RepoDocsTaskStatus, type RepositoryDocs } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";

// Mirrors domain.DefaultRepoDocPath — shown as a placeholder and used when a
// generation is requested with no path typed in.
const DEFAULT_DOC_PATH: Record<RepoDocKind, string> = {
  coding_standards: ".ai/coding-standards.md",
  test_standards: ".ai/test-standards.md",
  architecture: ".ai/architecture.md",
  local_run: ".ai/local-deploy.md",
};

const DOC_FIELDS: { key: RepoDocKind; labelKey: string }[] = [
  { key: "coding_standards", labelKey: "projectAdmin.initialSetup.docsCodingStandards" },
  { key: "test_standards", labelKey: "projectAdmin.initialSetup.docsTestStandards" },
  { key: "architecture", labelKey: "projectAdmin.initialSetup.docsArchitecture" },
  { key: "local_run", labelKey: "projectAdmin.initialSetup.docsLocalRun" },
];

const DOCS_TASK_POLL_MS = 8000;
// Board columns that will never move again on their own — polling past them is
// a timer that never stops.
const TERMINAL_TASK_COLUMNS = new Set(["done", "released", "failed", "cancelled"]);

interface RepoDocsCardProps {
  repositoryId: string;
  /** Overrides the card's heading — used to label which sub-project this is. */
  title?: string;
  className?: string;
}

/**
 * RepoDocsCard reads and writes a repository's reference-doc paths live,
 * since it edits a repository that already exists — an already-imported repo
 * has no other way to reach this feature.
 *
 * Generation is queued rather than fired per field: four separate tasks meant
 * four branches and four pull requests for one set of reference docs. The
 * per-field button now only marks a doc, and one bundle task writes every
 * marked doc in a single PR — which is why saving turns into "save and merge"
 * once that PR exists.
 */
export function RepoDocsCard({ repositoryId, title, className }: RepoDocsCardProps) {
  const { t } = useI18n();
  const [docs, setDocs] = useState<RepositoryDocs>({});
  const [queued, setQueued] = useState<RepoDocKind[]>([]);
  const [task, setTask] = useState<RepoDocsTaskStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [creating, setCreating] = useState(false);
  const pollTimer = useRef<number | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const repo = await api.getRepository(repositoryId);
      setDocs(repo.docs ?? {});
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
    } finally {
      setLoading(false);
    }
  }, [repositoryId, t]);

  // The docs task is one per repository, not per sub-project: a bundle can
  // carry items for several sub-projects, so every card on the page reports the
  // same run. A failure here is silent — no task is the normal state.
  const loadTask = useCallback(async () => {
    try {
      const status = await api.getRepoDocsTask(repositoryId);
      setTask(status.task_id ? status : null);
    } catch {
      setTask(null);
    }
  }, [repositoryId]);

  useEffect(() => {
    void load();
    void loadTask();
  }, [load, loadTask]);

  useEffect(() => {
    if (!task || task.pr_url || task.merged || TERMINAL_TASK_COLUMNS.has(task.column)) return;
    pollTimer.current = window.setTimeout(() => void loadTask(), DOCS_TASK_POLL_MS);
    return () => {
      if (pollTimer.current !== null) window.clearTimeout(pollTimer.current);
    };
  }, [task, loadTask]);

  const persist = useCallback(
    async (next: RepositoryDocs) => {
      const repo = await api.getRepository(repositoryId);
      await api.updateRepository(repositoryId, { name: repo.name, description: repo.description, docs: next });
    },
    [repositoryId],
  );

  const prURL = task?.pr_url ?? "";

  const handleSave = async () => {
    setSaving(true);
    try {
      await persist(docs);
      if (!prURL) {
        toast.success(t("projectAdmin.projectSettings.docsSaved"));
        return;
      }
      const { merged } = await api.mergeRepoDocsTask(repositoryId);
      if (merged) {
        toast.success(t("projectAdmin.projectSettings.docsMerged"));
        await Promise.all([load(), loadTask()]);
      } else {
        toast.error(t("projectAdmin.projectSettings.docsMergeFailed"));
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("common.saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  const handleGenerate = async () => {
    if (queued.length === 0) return;
    setCreating(true);
    try {
      await persist(docs);
      await api.createRepoDocsBundleTask(
        repositoryId,
        queued.map((kind) => ({
          kind,
          path: docs[kind]?.trim() || undefined,
        })),
      );
      setQueued([]);
      toast.success(t("projectAdmin.projectSettings.docsBundleCreated"));
      await Promise.all([load(), loadTask()]);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
    } finally {
      setCreating(false);
    }
  };

  const heading = title ?? t("projectAdmin.initialSetup.docsLabel");

  if (loading) {
    return (
      <Card className={className}>
        <CardHeader>
          <CardTitle className="text-base">{heading}</CardTitle>
        </CardHeader>
        <CardContent>
          <Skeleton className="h-40 w-full" />
        </CardContent>
      </Card>
    );
  }

  return (
    <Card className={className}>
      <CardHeader>
        <div className="flex flex-wrap items-center justify-between gap-2">
          <CardTitle className="text-base">{heading}</CardTitle>
          {queued.length > 0 && (
            <Badge variant="outline">
              {t("projectAdmin.projectSettings.docsQueuedCount", { count: queued.length })}
            </Badge>
          )}
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        {task && !prURL && (
          <Notice variant="info" title={t("projectAdmin.projectSettings.docsTaskRunningTitle")}>
            {t("projectAdmin.projectSettings.docsTaskRunning", { status: task.column })}
          </Notice>
        )}
        {prURL && (
          <Notice variant="info" title={t("projectAdmin.projectSettings.docsPrReadyTitle")}>
            <div className="space-y-1">
              <p>{t("projectAdmin.projectSettings.docsPrReady")}</p>
              <a
                href={prURL}
                target="_blank"
                rel="noreferrer"
                className="inline-flex items-center gap-1.5 font-medium underline"
              >
                <ExternalLink className="h-3.5 w-3.5" />
                {t("projectAdmin.projectSettings.docsPrLink")}
              </a>
            </div>
          </Notice>
        )}

        {DOC_FIELDS.map((f) => {
          const isQueued = queued.includes(f.key);
          return (
            <div key={f.key} className="space-y-1">
              <Label className="text-xs font-normal text-muted-foreground">{t(f.labelKey)}</Label>
              <div className="flex items-center gap-2">
                <Input
                  value={docs[f.key] ?? ""}
                  onChange={(e) => setDocs((prev) => ({ ...prev, [f.key]: e.target.value }))}
                  placeholder={DEFAULT_DOC_PATH[f.key]}
                  className="flex-1"
                />
                <Button
                  type="button"
                  variant={isQueued ? "default" : "outline"}
                  size="sm"
                  aria-pressed={isQueued}
                  onClick={() =>
                    setQueued((prev) => (prev.includes(f.key) ? prev.filter((k) => k !== f.key) : [...prev, f.key]))
                  }
                >
                  <Sparkles className="mr-1.5 h-3.5 w-3.5" />
                  {isQueued
                    ? t("projectAdmin.projectSettings.docsQueued")
                    : t("projectAdmin.projectSettings.docsQueue")}
                </Button>
              </div>
            </div>
          );
        })}

        <div className="flex flex-wrap items-center justify-end gap-2 pt-1">
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={creating || queued.length === 0}
            onClick={() => void handleGenerate()}
          >
            <Sparkles className="mr-1.5 h-3.5 w-3.5" />
            {t("projectAdmin.projectSettings.docsGenerateBundle")}
          </Button>
          <Button size="sm" disabled={saving} onClick={() => void handleSave()}>
            {prURL && <GitPullRequest className="mr-1.5 h-3.5 w-3.5" />}
            {prURL ? t("projectAdmin.projectSettings.docsSaveAndMerge") : t("common.save")}
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
