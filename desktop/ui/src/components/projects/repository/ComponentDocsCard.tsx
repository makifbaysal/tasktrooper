import { ExternalLink, GitPullRequest, Sparkles } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { api, type Component, type RepoDocKind, type RepoDocsTaskStatus, type RepositoryDocs } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
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
  { key: "coding_standards", labelKey: "repositoryPage.components.docsCodingStandards" },
  { key: "test_standards", labelKey: "repositoryPage.components.docsTestStandards" },
  { key: "architecture", labelKey: "repositoryPage.components.docsArchitecture" },
  { key: "local_run", labelKey: "repositoryPage.components.docsLocalRun" },
];

const DOCS_TASK_POLL_MS = 8000;
// Board columns that will never move again on their own — polling past them is
// a timer that never stops.
const TERMINAL_TASK_COLUMNS = new Set(["done", "released", "failed", "cancelled"]);

interface ComponentDocsCardProps {
  repositoryId: string;
  component: Component;
  onReload: () => void;
}

/**
 * The docs task is one per repository, not per component — a bundle can carry
 * items for several components, so every component's card on this tab reports
 * the same run.
 */
export function ComponentDocsCard({ repositoryId, component, onReload }: ComponentDocsCardProps) {
  const { t } = useI18n();
  const [docs, setDocs] = useState<RepositoryDocs>(component.docs ?? {});
  const [queued, setQueued] = useState<RepoDocKind[]>([]);
  const [task, setTask] = useState<RepoDocsTaskStatus | null>(null);
  const [saving, setSaving] = useState(false);
  const [creating, setCreating] = useState(false);
  const pollTimer = useRef<number | null>(null);

  useEffect(() => {
    setDocs(component.docs ?? {});
    setQueued([]);
  }, [component.id]);

  const loadTask = useCallback(async () => {
    try {
      const status = await api.getRepoDocsTask(repositoryId);
      setTask(status.task_id ? status : null);
    } catch {
      setTask(null);
    }
  }, [repositoryId]);

  useEffect(() => {
    void loadTask();
  }, [loadTask]);

  useEffect(() => {
    if (!task || task.pr_url || task.merged || TERMINAL_TASK_COLUMNS.has(task.column)) return;
    pollTimer.current = window.setTimeout(() => void loadTask(), DOCS_TASK_POLL_MS);
    return () => {
      if (pollTimer.current !== null) window.clearTimeout(pollTimer.current);
    };
  }, [task, loadTask]);

  const persist = useCallback((next: RepositoryDocs) => api.updateComponent(component.id, { docs: next }), [component.id]);

  const prURL = task?.pr_url ?? "";

  const handleSave = async () => {
    setSaving(true);
    try {
      await persist(docs);
      if (!prURL) {
        toast.success(t("repositoryPage.components.docsSaved"));
      } else {
        const { merged } = await api.mergeRepoDocsTask(repositoryId);
        if (merged) {
          toast.success(t("repositoryPage.components.docsMerged"));
        } else {
          toast.error(t("repositoryPage.components.docsMergeFailed"));
        }
      }
      onReload();
      await loadTask();
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
          component_id: component.id,
          path: docs[kind]?.trim() || undefined,
        })),
      );
      // The server records the default path for a blank field; mirror it so the
      // card shows what was saved without dropping unrelated in-progress edits.
      setDocs((prev) => {
        const next = { ...prev };
        for (const kind of queued) next[kind] = prev[kind]?.trim() || DEFAULT_DOC_PATH[kind];
        return next;
      });
      setQueued([]);
      toast.success(t("repositoryPage.components.docsBundleCreated"));
      onReload();
      await loadTask();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
    } finally {
      setCreating(false);
    }
  };

  return (
    <Card className="space-y-3 p-6">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h3 className="font-semibold">{t("repositoryPage.components.docsTitle")}</h3>
          {component.path !== "." && (
            <p className="text-micro text-muted-foreground">
              {t("repositoryPage.components.docsRelativeTo", { path: component.path })}
            </p>
          )}
        </div>
        {queued.length > 0 && (
          <Badge variant="outline">{t("repositoryPage.components.docsQueuedCount", { count: queued.length })}</Badge>
        )}
      </div>

      {task && !prURL && (
        <Notice variant="info" title={t("repositoryPage.components.docsTaskRunningTitle")}>
          {t("repositoryPage.components.docsTaskRunning", { status: task.column })}
        </Notice>
      )}
      {prURL && (
        <Notice variant="info" title={t("repositoryPage.components.docsPrReadyTitle")}>
          <div className="space-y-1">
            <p>{t("repositoryPage.components.docsPrReady")}</p>
            <a
              href={prURL}
              target="_blank"
              rel="noreferrer"
              className="inline-flex items-center gap-1.5 font-medium underline"
            >
              <ExternalLink className="h-3.5 w-3.5" />
              {t("repositoryPage.components.docsPrLink")}
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
                {isQueued ? t("repositoryPage.components.docsQueued") : t("repositoryPage.components.docsQueue")}
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
          {t("repositoryPage.components.docsGenerateBundle")}
        </Button>
        <Button size="sm" disabled={saving} onClick={() => void handleSave()}>
          {prURL && <GitPullRequest className="mr-1.5 h-3.5 w-3.5" />}
          {prURL ? t("repositoryPage.components.docsSaveAndMerge") : t("common.save")}
        </Button>
      </div>
    </Card>
  );
}
