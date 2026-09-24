import { RefreshCw, Square, Trash2 } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { api, type InitiativeProject, type RepositoryModel } from "@/api";
import { MultiSelectPicker } from "@/components/admin/MultiSelectPicker";
import { ProjectIndexStatus } from "@/components/projects/ProjectIndexStatus";
import { RepoDocsCard } from "@/components/projects/RepoDocsCard";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { HelpTooltip } from "@/components/ui/help-tooltip";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";
import { useIndexProgress } from "@/hooks/useIndexProgress";
import { indexProgressPercent } from "@/lib/project-board";

interface SettingsTabProps {
  model: RepositoryModel;
  repositoryId: string;
  onReload: () => void;
}

export function SettingsTab({ model, repositoryId, onReload }: SettingsTabProps) {
  const { t } = useI18n();
  const navigate = useNavigate();
  const repository = model.repository;
  const [name, setName] = useState(repository.name);
  const [description, setDescription] = useState(repository.description);
  const [projectIds, setProjectIds] = useState<string[]>(repository.project_ids ?? []);
  const [initiativeProjects, setInitiativeProjects] = useState<InitiativeProject[]>([]);
  const [saving, setSaving] = useState(false);
  const [coverageThreshold, setCoverageThreshold] = useState(String(repository.coverage_threshold ?? ""));
  const [mutationThreshold, setMutationThreshold] = useState(String(repository.mutation_threshold ?? ""));
  const [watchingIndex, setWatchingIndex] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [creatingSetupTask, setCreatingSetupTask] = useState(false);

  const { index, percent, refresh: refreshIndex } = useIndexProgress(repositoryId, {
    forcePoll: watchingIndex,
    onIndexingFinished: () => setWatchingIndex(false),
  });

  useEffect(() => {
    api
      .listInitiativeProjects()
      .then((res) => setInitiativeProjects(res.projects ?? []))
      .catch(() => undefined);
  }, []);

  const patchRepo = useCallback(
    async (partial: Parameters<typeof api.updateRepository>[1]) => {
      try {
        await api.updateRepository(repositoryId, { name: repository.name, description: repository.description, ...partial });
        onReload();
      } catch (e) {
        toast.error(e instanceof Error ? e.message : t("common.saveFailed"));
      }
    },
    [repositoryId, repository.name, repository.description, onReload, t],
  );

  const patchGates = useCallback(
    async (partial: Parameters<typeof api.setLifecycleGates>[1]) => {
      try {
        await api.setLifecycleGates(repositoryId, partial);
        onReload();
      } catch (e) {
        toast.error(e instanceof Error ? e.message : t("common.saveFailed"));
      }
    },
    [repositoryId, onReload, t],
  );

  const saveGeneral = async () => {
    setSaving(true);
    try {
      await api.updateRepository(repositoryId, { name, description });
      await api.setRepositoryProjects(repositoryId, projectIds);
      onReload();
      toast.success(t("common.saved"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  const handleReindex = async () => {
    setWatchingIndex(true);
    try {
      await api.reindexRepository(repositoryId);
      await refreshIndex();
    } catch (e) {
      setWatchingIndex(false);
      toast.error(e instanceof Error ? e.message : t("common.actionFailed"));
    }
  };

  const handleStopIndex = async () => {
    try {
      await api.stopRepositoryIndex(repositoryId);
      setWatchingIndex(false);
      await refreshIndex();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.actionFailed"));
    }
  };

  const handleCreateSetupTask = async () => {
    setCreatingSetupTask(true);
    try {
      await api.createWorkflowSetupTask(repositoryId);
      toast.success(t("repositoryPage.settings.openSetupTask"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.actionFailed"));
    } finally {
      setCreatingSetupTask(false);
    }
  };

  const handleDelete = async () => {
    setDeleting(true);
    try {
      await api.deleteRepository(repositoryId);
      navigate("/projects");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.actionFailed"));
      setDeleting(false);
    }
  };

  const displayPercent = index ? indexProgressPercent(index.files_processed, index.files_total, index.status) : percent;
  const hasCIChecks = model.checks.some((c) => c.source === "ci");

  return (
    <div className="max-w-3xl space-y-6">
      <Card className="space-y-4 p-6">
        <div>
          <h2 className="font-semibold">{t("repositoryPage.settings.generalTitle")}</h2>
          <p className="text-caption text-muted-foreground">{t("repositoryPage.settings.generalDesc")}</p>
        </div>
        <div className="space-y-2">
          <Label>{t("repositoryPage.settings.name")}</Label>
          <Input value={name} onChange={(e) => setName(e.target.value)} />
        </div>
        <div className="space-y-2">
          <Label>{t("repositoryPage.settings.description")}</Label>
          <Textarea value={description} onChange={(e) => setDescription(e.target.value)} rows={3} />
        </div>
        {initiativeProjects.length > 0 && (
          <MultiSelectPicker
            label={t("repositoryPage.settings.projects")}
            options={initiativeProjects.map((p) => ({ value: p.id, label: p.name }))}
            selected={projectIds}
            onChange={setProjectIds}
          />
        )}
        <div className="flex justify-end">
          <Button onClick={saveGeneral} disabled={saving}>
            {t("common.save")}
          </Button>
        </div>
      </Card>

      <Card className="space-y-4 p-6">
        <div>
          <h2 className="font-semibold">{t("repositoryPage.settings.workflowTitle")}</h2>
          <p className="text-caption text-muted-foreground">{t("repositoryPage.settings.workflowDesc")}</p>
        </div>

        <GateRow
          label={t("repositoryPage.settings.requireHumanReview")}
          help={t("repositoryPage.settings.requireHumanReviewHelp")}
          checked={repository.require_human_review ?? false}
          onCheckedChange={(checked) => patchRepo({ require_human_review: checked })}
        />
        <GateRow
          label={t("repositoryPage.settings.autoRelease")}
          checked={repository.auto_release_on_done ?? false}
          onCheckedChange={(checked) => patchRepo({ auto_release_on_done: checked })}
        />
        <GateRow
          label={t("repositoryPage.settings.requireReviewChain")}
          checked={repository.require_review_chain ?? false}
          onCheckedChange={(checked) => patchGates({ require_review_chain: checked })}
        />
        <GateRow
          label={t("repositoryPage.settings.requireReleaseDeploy")}
          checked={repository.require_release_deploy ?? false}
          onCheckedChange={(checked) => patchGates({ require_release_deploy: checked })}
        />
        <GateRow
          label={t("repositoryPage.settings.requirePipelineForReview")}
          checked={repository.require_pipeline_for_review ?? true}
          onCheckedChange={(checked) => patchGates({ require_pipeline_for_review: checked })}
        />
        <GateRow
          label={t("repositoryPage.settings.requireOverallCoverage")}
          checked={repository.require_overall_coverage ?? false}
          onCheckedChange={(checked) => patchGates({ require_overall_coverage: checked })}
        />
        <div className="space-y-2">
          <Label>{t("repositoryPage.settings.coverageThreshold")}</Label>
          <Input
            type="number"
            min={0}
            max={100}
            value={coverageThreshold}
            onChange={(e) => setCoverageThreshold(e.target.value)}
            onBlur={() => {
              const n = Number(coverageThreshold);
              if (!Number.isNaN(n) && n !== repository.coverage_threshold) void patchGates({ coverage_threshold: n });
            }}
            className="max-w-32"
          />
        </div>
        <GateRow
          label={t("repositoryPage.settings.mutationGate")}
          checked={repository.mutation_enabled ?? false}
          onCheckedChange={(checked) => patchRepo({ mutation_enabled: checked })}
        />
        <div className="space-y-2">
          <Label>{t("repositoryPage.settings.mutationThreshold")}</Label>
          <Input
            type="number"
            min={0}
            max={100}
            value={mutationThreshold}
            onChange={(e) => setMutationThreshold(e.target.value)}
            onBlur={() => {
              const n = Number(mutationThreshold);
              if (!Number.isNaN(n) && n !== repository.mutation_threshold) void patchRepo({ mutation_threshold: n });
            }}
            className="max-w-32"
          />
        </div>
      </Card>

      <Card className="space-y-4 p-6">
        <div className="flex items-center justify-between gap-4">
          <h2 className="font-semibold">{t("repositoryPage.settings.codeIndexTitle")}</h2>
          <div className="flex items-center gap-2">
            <ProjectIndexStatus repositoryId={repositoryId} />
            {watchingIndex && (
              <Button variant="outline" size="sm" onClick={handleStopIndex}>
                <Square className="mr-2 h-4 w-4" />
                {t("repositoryPage.settings.stopIndex")}
              </Button>
            )}
            <Button variant="outline" size="sm" onClick={handleReindex} disabled={watchingIndex}>
              <RefreshCw className={watchingIndex ? "mr-2 h-4 w-4 animate-spin" : "mr-2 h-4 w-4"} />
              {t("repositoryPage.settings.reindex")}
            </Button>
          </div>
        </div>
        {index ? (
          <div className="grid grid-cols-3 gap-3 text-center text-body">
            <div>
              <p className="text-muted-foreground">{t("repositoryPage.settings.files")}</p>
              <p className="font-semibold">{index.file_count}</p>
            </div>
            <div>
              <p className="text-muted-foreground">{t("repositoryPage.settings.chunks")}</p>
              <p className="font-semibold">{index.chunk_count}</p>
            </div>
            <div>
              <p className="text-muted-foreground">{t("repositoryPage.settings.symbols")}</p>
              <p className="font-semibold">{index.symbol_count}</p>
            </div>
            <div className="col-span-3 text-caption text-muted-foreground">{displayPercent}%</div>
          </div>
        ) : (
          <p className="text-body text-muted-foreground">{t("repositoryPage.settings.noIndexYet")}</p>
        )}
      </Card>

      <RepoDocsCard repositoryId={repositoryId} title={t("repositoryPage.settings.docsTitle")} />

      {!hasCIChecks && (
        <Notice variant="warning" title={t("repositoryPage.settings.noWorkflowsTitle")}>
          <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <span>{t("repositoryPage.settings.noWorkflowsDesc")}</span>
            <Button size="sm" onClick={handleCreateSetupTask} disabled={creatingSetupTask} className="shrink-0">
              {t("repositoryPage.settings.openSetupTask")}
            </Button>
          </div>
        </Notice>
      )}

      <Card className="space-y-4 border-destructive/30 p-6">
        <h2 className="font-semibold text-destructive">{t("repositoryPage.settings.dangerTitle")}</h2>
        <p className="text-body text-muted-foreground">{t("repositoryPage.settings.deleteRepoWarning")}</p>
        <Button variant="destructive" onClick={() => setDeleteOpen(true)} className="gap-2">
          <Trash2 className="h-4 w-4" />
          {t("repositoryPage.settings.deleteRepo")}
        </Button>
      </Card>

      <ConfirmDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        title={t("repositoryPage.settings.deleteConfirmTitle", { name: repository.name })}
        description={t("repositoryPage.settings.deleteConfirmDesc")}
        confirmLabel={t("repositoryPage.settings.deleteRepo")}
        loading={deleting}
        onConfirm={handleDelete}
      />
    </div>
  );
}

function GateRow({
  label,
  help,
  checked,
  onCheckedChange,
}: {
  label: string;
  help?: string;
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
}) {
  return (
    <div className="flex items-center justify-between gap-4 rounded-md border border-border p-3">
      <div className="flex items-center gap-1.5">
        <Label>{label}</Label>
        {help && <HelpTooltip text={help} />}
      </div>
      <Switch checked={checked} onCheckedChange={onCheckedChange} />
    </div>
  );
}
