import { Loader2, Plus, Save, Trash2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import {
  api,
  type AssigneeMode,
  type BehaviourSpec,
  type BoardColumn,
  type BoardSubscription,
  type Role,
  type TaskTypeDef,
  type WorkflowProblem,
  type WorkflowStage,
} from "@/api";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { BehaviourPicker } from "@/components/workflow/BehaviourPicker";
import { StageEditor } from "@/components/workflow/StageEditor";
import { useI18n } from "@/hooks/useI18n";
import { LEAD_ROLE_KEY } from "@/lib/leadAgent";
import { cn } from "@/lib/utils";

const ASSIGNEE_MODES: AssigneeMode[] = ["none", "default", "override"];

export function WorkflowSettingsPage() {
  const { t } = useI18n();
  const [loading, setLoading] = useState(true);
  const [taskTypes, setTaskTypes] = useState<TaskTypeDef[]>([]);
  const [roles, setRoles] = useState<Role[]>([]);
  const [columns, setColumns] = useState<BoardColumn[]>([]);
  const [behaviours, setBehaviours] = useState<BehaviourSpec[]>([]);
  const [subscriptions, setSubscriptions] = useState<BoardSubscription[]>([]);
  const [selectedKey, setSelectedKey] = useState<string | null>(null);

  const [draftType, setDraftType] = useState<TaskTypeDef | null>(null);
  const [draftStages, setDraftStages] = useState<WorkflowStage[]>([]);
  const [stagesLoading, setStagesLoading] = useState(false);
  const [savingType, setSavingType] = useState(false);
  const [savingStages, setSavingStages] = useState(false);
  const [problems, setProblems] = useState<WorkflowProblem[]>([]);

  const [createOpen, setCreateOpen] = useState(false);
  const [newKey, setNewKey] = useState("");
  const [newLabel, setNewLabel] = useState("");
  const [newPrefix, setNewPrefix] = useState("");
  const [cloneFrom, setCloneFrom] = useState<string>("none");
  const [creating, setCreating] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<TaskTypeDef | null>(null);
  const [deleting, setDeleting] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [typesRes, rolesRes, columnsRes, behavioursRes, subsRes] = await Promise.all([
        api.listTaskTypes(),
        api.listRoles(),
        api.listBoardColumns(),
        api.listWorkflowBehaviours(),
        api.listBoardSubscriptions(),
      ]);
      setTaskTypes((typesRes.task_types ?? []).sort((a, b) => a.position - b.position));
      setRoles(rolesRes.roles ?? []);
      setColumns(columnsRes.columns ?? []);
      setBehaviours(behavioursRes.behaviours ?? []);
      setSubscriptions(subsRes.subscriptions ?? []);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("settingsPages.workflows.loadFailed"));
    } finally {
      setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const selectedType = useMemo(
    () => taskTypes.find((tt) => tt.key === selectedKey) ?? null,
    [taskTypes, selectedKey],
  );

  const loadWorkflow = useCallback(
    async (key: string) => {
      setStagesLoading(true);
      setProblems([]);
      try {
        const wf = await api.getTaskTypeWorkflow(key);
        setDraftStages(wf.stages ?? []);
      } catch (e) {
        toast.error(e instanceof Error ? e.message : t("settingsPages.workflows.loadFailed"));
      } finally {
        setStagesLoading(false);
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [],
  );

  useEffect(() => {
    if (!selectedType) {
      setDraftType(null);
      setDraftStages([]);
      return;
    }
    setDraftType(selectedType);
    loadWorkflow(selectedType.key);
  }, [selectedType, loadWorkflow]);

  // Roles holding at least one agent subscribed to `columnSlug` — an
  // approximation (subscriptions aren't yet task-type-filter-aware here) that
  // matches the spec: "compute from /v1/board/subscriptions or per-agent
  // subscriptions".
  const hasSubscriber = useCallback(
    (roleId: string, columnSlug: string) => {
      const role = roles.find((r) => r.id === roleId);
      if (!role) return false;
      const roleAgentIds = new Set(role.assignments.map((a) => a.agent_id));
      return subscriptions.some((s) => s.column_slug === columnSlug && roleAgentIds.has(s.agent_id));
    },
    [roles, subscriptions],
  );

  const saveType = async () => {
    if (!draftType) return;
    setSavingType(true);
    try {
      const updated = await api.updateTaskType(draftType.key, {
        label: draftType.label,
        key_prefix: draftType.key_prefix,
        position: draftType.position,
        is_default: draftType.is_default,
        is_defect: draftType.is_defect,
        assignee_role_id: draftType.assignee_role_id ?? null,
        assignee_mode: draftType.assignee_mode,
        behaviours: draftType.behaviours,
      });
      setTaskTypes((prev) => prev.map((tt) => (tt.key === updated.key ? updated : tt)));
      setDraftType(updated);
      toast.success(t("settingsPages.workflows.savedToast"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("settingsPages.workflows.saveFailed"));
    } finally {
      setSavingType(false);
    }
  };

  const saveStages = async () => {
    if (!draftType) return;
    setSavingStages(true);
    try {
      const result = await api.updateTaskTypeWorkflow(draftType.key, draftStages);
      if (!result.saved) {
        setProblems(result.problems ?? []);
        toast.error(result.error ?? t("settingsPages.workflows.stagesInvalid"));
        return;
      }
      setProblems([]);
      if (result.stages) setDraftStages(result.stages);
      toast.success(t("settingsPages.workflows.stagesSavedToast"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("settingsPages.workflows.saveFailed"));
    } finally {
      setSavingStages(false);
    }
  };

  const createType = async () => {
    const key = newKey.trim();
    const label = newLabel.trim();
    const prefix = newPrefix.trim().toUpperCase();
    if (!key || !label || !prefix) return;
    setCreating(true);
    try {
      const created = await api.createTaskType({
        key,
        label,
        key_prefix: prefix,
        clone_from: cloneFrom === "none" ? undefined : cloneFrom,
      });
      setTaskTypes((prev) => [...prev, created].sort((a, b) => a.position - b.position));
      setSelectedKey(created.key);
      setCreateOpen(false);
      setNewKey("");
      setNewLabel("");
      setNewPrefix("");
      setCloneFrom("none");
      toast.success(t("settingsPages.workflows.createdToast"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("settingsPages.workflows.saveFailed"));
    } finally {
      setCreating(false);
    }
  };

  const deleteType = async () => {
    if (!deleteTarget) return;
    setDeleting(true);
    try {
      await api.deleteTaskType(deleteTarget.key);
      setTaskTypes((prev) => prev.filter((tt) => tt.key !== deleteTarget.key));
      if (selectedKey === deleteTarget.key) setSelectedKey(null);
      toast.success(t("settingsPages.workflows.deletedToast"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("settingsPages.workflows.deleteFailed"));
    } finally {
      setDeleting(false);
      setDeleteTarget(null);
    }
  };

  if (loading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-64 rounded-xl" />
      </div>
    );
  }

  const typeBehaviours = behaviours.filter((b) => b.scope === "type");

  return (
    <div className="space-y-6 pb-8">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="font-semibold">{t("settingsPages.workflows.title")}</h2>
          <p className="mt-0.5 text-sm text-muted-foreground">{t("settingsPages.workflows.subtitle")}</p>
        </div>
        <Button onClick={() => setCreateOpen(true)} className="gap-2">
          <Plus className="h-4 w-4" />
          {t("settingsPages.workflows.newType")}
        </Button>
      </div>

      <div className="grid gap-6 lg:grid-cols-[16rem_1fr]">
        <Card className="divide-y divide-border">
          {taskTypes.map((tt) => (
            <button
              key={tt.key}
              type="button"
              onClick={() => setSelectedKey(tt.key)}
              className={cn(
                "flex w-full items-center justify-between gap-2 px-4 py-3 text-left hover:bg-muted/30",
                selectedKey === tt.key && "bg-muted/50",
              )}
            >
              <span className="min-w-0">
                <span className="block truncate text-sm font-medium">{tt.label}</span>
                <span className="block truncate font-mono text-xs text-muted-foreground">
                  {tt.key} · {tt.key_prefix}
                </span>
              </span>
              {tt.is_default && (
                <span className="shrink-0 text-micro uppercase text-muted-foreground">
                  {t("settingsPages.workflows.defaultTag")}
                </span>
              )}
            </button>
          ))}
          {taskTypes.length === 0 && (
            <p className="px-4 py-6 text-center text-sm text-muted-foreground">{t("settingsPages.workflows.empty")}</p>
          )}
        </Card>

        {draftType ? (
          <div className="space-y-6">
            <Card className="space-y-4 p-6">
              <div className="flex items-start justify-between gap-3">
                <h3 className="text-sm font-semibold">{t("settingsPages.workflows.detailsTitle")}</h3>
                {!draftType.is_default && (
                  <button
                    type="button"
                    className="text-muted-foreground hover:text-destructive"
                    onClick={() => setDeleteTarget(draftType)}
                    title={t("settingsPages.workflows.deleteType")}
                  >
                    <Trash2 className="h-4 w-4" />
                  </button>
                )}
              </div>
              <div className="grid gap-3 sm:grid-cols-2">
                <div className="space-y-2">
                  <Label htmlFor="type-label">{t("settingsPages.workflows.labelLabel")}</Label>
                  <Input
                    id="type-label"
                    value={draftType.label}
                    onChange={(e) => setDraftType({ ...draftType, label: e.target.value })}
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="type-prefix">{t("settingsPages.workflows.prefixLabel")}</Label>
                  <Input
                    id="type-prefix"
                    value={draftType.key_prefix}
                    disabled={draftType.task_count > 0}
                    onChange={(e) => setDraftType({ ...draftType, key_prefix: e.target.value.toUpperCase() })}
                  />
                  {draftType.task_count > 0 && (
                    <p className="text-xs text-muted-foreground">{t("settingsPages.workflows.prefixLocked")}</p>
                  )}
                </div>
              </div>
              <div className="flex flex-wrap gap-4">
                <label className="flex items-center gap-2 text-sm">
                  <Checkbox
                    checked={draftType.is_default}
                    onCheckedChange={(v) => setDraftType({ ...draftType, is_default: v === true })}
                  />
                  {t("settingsPages.workflows.isDefaultLabel")}
                </label>
                <label className="flex items-center gap-2 text-sm">
                  <Checkbox
                    checked={draftType.is_defect}
                    onCheckedChange={(v) => setDraftType({ ...draftType, is_defect: v === true })}
                  />
                  {t("settingsPages.workflows.isDefectLabel")}
                </label>
              </div>
              <div className="grid gap-3 sm:grid-cols-2">
                <div className="space-y-2">
                  <Label>{t("settingsPages.workflows.assigneeModeLabel")}</Label>
                  <Select
                    value={draftType.assignee_mode}
                    onValueChange={(v) => setDraftType({ ...draftType, assignee_mode: v as AssigneeMode })}
                  >
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {ASSIGNEE_MODES.map((mode) => (
                        <SelectItem key={mode} value={mode}>
                          {t(`settingsPages.workflows.assigneeMode.${mode}`)}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <p className="text-xs text-muted-foreground">
                    {t(`settingsPages.workflows.assigneeModeHelp.${draftType.assignee_mode}`)}
                  </p>
                </div>
                {draftType.assignee_mode !== "none" && (
                  <div className="space-y-2">
                    <Label>{t("settingsPages.workflows.assigneeRoleLabel")}</Label>
                    <Select
                      value={draftType.assignee_role_id ?? "none"}
                      onValueChange={(v) => setDraftType({ ...draftType, assignee_role_id: v === "none" ? null : v })}
                    >
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="none">{t("settingsPages.roles.dutyNone")}</SelectItem>
                        {roles
                          .filter((role) => role.key !== LEAD_ROLE_KEY || role.id === draftType.assignee_role_id)
                          .map((role) => (
                            <SelectItem key={role.id} value={role.id}>
                              {role.name}
                            </SelectItem>
                          ))}
                      </SelectContent>
                    </Select>
                  </div>
                )}
              </div>

              <div className="space-y-1.5">
                <Label>{t("settingsPages.workflows.typeBehavioursLabel")}</Label>
                <BehaviourPicker
                  registry={typeBehaviours}
                  columns={columns}
                  selected={draftType.behaviours}
                  onChange={(behaviours) => setDraftType({ ...draftType, behaviours })}
                />
              </div>

              <Button onClick={saveType} disabled={savingType} className="gap-2">
                {savingType ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
                {t("common.save")}
              </Button>
            </Card>

            <Card className="space-y-4 p-6">
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div>
                  <h3 className="text-sm font-semibold">{t("settingsPages.workflows.stagesTitle")}</h3>
                  <p className="mt-0.5 text-xs text-muted-foreground">{t("settingsPages.workflows.stagesSubtitle")}</p>
                </div>
                <Button onClick={saveStages} disabled={savingStages || stagesLoading} className="gap-2">
                  {savingStages ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
                  {t("settingsPages.workflows.saveStages")}
                </Button>
              </div>
              {stagesLoading ? (
                <Skeleton className="h-48 rounded-lg" />
              ) : (
                <StageEditor
                  stages={draftStages}
                  onChange={setDraftStages}
                  columns={columns}
                  behaviourRegistry={behaviours}
                  roles={roles}
                  hasSubscriber={hasSubscriber}
                  problems={problems}
                  disabled={savingStages}
                />
              )}
            </Card>
          </div>
        ) : (
          <Card className="flex items-center justify-center p-10 text-sm text-muted-foreground">
            {t("settingsPages.workflows.selectType")}
          </Card>
        )}
      </div>

      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("settingsPages.workflows.newType")}</DialogTitle>
          </DialogHeader>
          <div className="space-y-3">
            <div className="space-y-2">
              <Label htmlFor="new-type-key">{t("settingsPages.workflows.keyLabel")}</Label>
              <Input id="new-type-key" value={newKey} onChange={(e) => setNewKey(e.target.value)} autoFocus />
            </div>
            <div className="space-y-2">
              <Label htmlFor="new-type-label">{t("settingsPages.workflows.labelLabel")}</Label>
              <Input id="new-type-label" value={newLabel} onChange={(e) => setNewLabel(e.target.value)} />
            </div>
            <div className="space-y-2">
              <Label htmlFor="new-type-prefix">{t("settingsPages.workflows.prefixLabel")}</Label>
              <Input
                id="new-type-prefix"
                value={newPrefix}
                onChange={(e) => setNewPrefix(e.target.value.toUpperCase())}
                placeholder={t("settingsPages.workflows.prefixPlaceholder")}
              />
            </div>
            <div className="space-y-2">
              <Label>{t("settingsPages.workflows.cloneFromLabel")}</Label>
              <Select value={cloneFrom} onValueChange={setCloneFrom}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="none">{t("settingsPages.workflows.cloneFromNone")}</SelectItem>
                  {taskTypes.map((tt) => (
                    <SelectItem key={tt.key} value={tt.key}>
                      {tt.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setCreateOpen(false)}>
              {t("common.cancel")}
            </Button>
            <Button onClick={createType} disabled={creating || !newKey.trim() || !newLabel.trim() || !newPrefix.trim()}>
              {t("settingsPages.workflows.create")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={deleteTarget !== null}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title={t("settingsPages.workflows.deleteType")}
        description={t("settingsPages.workflows.deleteTypeDescription", { label: deleteTarget?.label ?? "" })}
        loading={deleting}
        onConfirm={deleteType}
      />
    </div>
  );
}
