import { ChevronDown, ChevronRight, Download, Loader2, Plus, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import {
  api,
  type Agent,
  type AttachmentMeta,
  type BoardTask,
  type InitiativeProject,
  type Repository,
  type TaskColumn,
  type TaskPriority,
  type TaskType,
  type BoardColumn,
} from "@/api";
import { MultiSelectPicker } from "@/components/admin/MultiSelectPicker";
import { TaskAssigneeFields } from "@/components/board/TaskAssigneeFields";
import { AttachmentDropzone } from "@/components/attachments/AttachmentDropzone";
import { AttachmentList } from "@/components/attachments/AttachmentList";
import { ImportIssueDialog } from "@/components/issues/ImportIssueDialog";
import { MarkdownField } from "@/components/markdown/MarkdownField";
import { Button } from "@/components/ui/button";
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
import { useI18n } from "@/hooks/useI18n";
import { useTaskTypes } from "@/hooks/useTaskTypes";
import { TASK_PRIORITY_OPTIONS, taskTypeOptions } from "@/lib/project-board";

interface CriterionDraft {
  id: string;
  text: string;
}

interface DocumentDraft {
  id: string;
  title: string;
  content: string;
}

interface CreateTaskDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  repositories: Repository[];
  initiativeProjects: InitiativeProject[];
  columns: BoardColumn[];
  agents: Agent[];
  memberAgentIds: string[];
  defaultRepositoryId?: string;
  defaultInitiativeProjectId?: string;
  defaultColumn: TaskColumn;
  title?: string;
  onCreated: () => void;
}

function emptyCriterion(): CriterionDraft {
  return { id: crypto.randomUUID(), text: "" };
}

function emptyDocument(): DocumentDraft {
  return { id: crypto.randomUUID(), title: "", content: "" };
}

export function CreateTaskDialog({
  open,
  onOpenChange,
  repositories,
  initiativeProjects,
  columns,
  agents,
  memberAgentIds,
  defaultRepositoryId,
  defaultInitiativeProjectId,
  defaultColumn,
  title: dialogTitle,
  onCreated,
}: CreateTaskDialogProps) {
  const { t } = useI18n();
  const { taskTypes } = useTaskTypes();
  const typeOptions = taskTypeOptions(taskTypes);
  const [repositoryId, setRepositoryId] = useState("");
  const [taskTitle, setTaskTitle] = useState("");
  const [taskType, setTaskType] = useState<TaskType>("task");
  const [priority, setPriority] = useState<TaskPriority>("medium");
  const [description, setDescription] = useState("");
  const [technicalDescription, setTechnicalDescription] = useState("");
  const [initiativeProjectId, setInitiativeProjectId] = useState<string>("none");
  const [column, setColumn] = useState<TaskColumn>(defaultColumn);
  const [assigneeId, setAssigneeId] = useState<string>("none");
  const [criteria, setCriteria] = useState<CriterionDraft[]>([]);
  const [documents, setDocuments] = useState<DocumentDraft[]>([]);
  const [attachments, setAttachments] = useState<AttachmentMeta[]>([]);
  const [creating, setCreating] = useState(false);
  // Deploy runbook + shipping order. Collapsed by default: most tasks need
  // none of it, and four extra fields at the top of every create dialog is how
  // the fields that DO matter stop being read.
  const [deployOpen, setDeployOpen] = useState(false);
  const [beforeDeploy, setBeforeDeploy] = useState("");
  const [afterDeploy, setAfterDeploy] = useState("");
  const [rollbackPlan, setRollbackPlan] = useState("");
  const [deployDependsOn, setDeployDependsOn] = useState<string[]>([]);
  const [repoTasks, setRepoTasks] = useState<BoardTask[]>([]);
  const [repoTasksLoading, setRepoTasksLoading] = useState(false);
  const [importOpen, setImportOpen] = useState(false);

  const memberAgents = agents.filter((a) => memberAgentIds.includes(a.id));
  const columnOptions = columns.filter((c) => !c.is_backlog || c.slug === defaultColumn);

  useEffect(() => {
    if (!open) return;
    setRepositoryId(defaultRepositoryId ?? repositories[0]?.id ?? "");
    setTaskTitle("");
    setTaskType("task");
    setPriority("medium");
    setDescription("");
    setTechnicalDescription("");
    setInitiativeProjectId(defaultInitiativeProjectId || "none");
    setColumn(defaultColumn);
    setAssigneeId("none");
    setCriteria([]);
    setDocuments([]);
    setAttachments([]);
    setDeployOpen(false);
    setBeforeDeploy("");
    setAfterDeploy("");
    setRollbackPlan("");
    setDeployDependsOn([]);
    setRepoTasks([]);
  }, [open, defaultRepositoryId, defaultInitiativeProjectId, repositories, defaultColumn]);

  // The dependency picker's options are the selected repository's tasks, loaded
  // only once the deploy section is actually opened: a board can hold hundreds
  // of tasks and almost no create ever needs them.
  useEffect(() => {
    if (!open || !deployOpen || !repositoryId) return;
    let cancelled = false;
    setRepoTasksLoading(true);
    api
      .listRepositoryTasks(repositoryId)
      .then((data) => {
        if (!cancelled) setRepoTasks(data.tasks ?? []);
      })
      .catch(() => {
        if (!cancelled) setRepoTasks([]);
      })
      .finally(() => {
        if (!cancelled) setRepoTasksLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [open, deployOpen, repositoryId]);

  // Switching repository invalidates any dependency already picked: a package
  // and its dependencies release through one repository's workflows.
  useEffect(() => {
    setDeployDependsOn([]);
  }, [repositoryId]);

  const handleCreate = async () => {
    if (!repositoryId || !taskTitle.trim()) {
      toast.error(t("boardArea.components.createTask.repoAndTitleRequired"));
      return;
    }
    setCreating(true);
    try {
      const created = await api.createRepositoryTask(repositoryId, {
        title: taskTitle.trim(),
        task_type: taskType,
        description: description.trim(),
        technical_description: technicalDescription.trim(),
        initiative_project_id:
          initiativeProjectId !== "none" ? initiativeProjectId : undefined,
        column,
        priority,
        assignee_agent_id: assigneeId !== "none" ? assigneeId : undefined,
        acceptance_criteria: criteria
          .filter((c) => c.text.trim())
          .map((c, i) => ({ text: c.text.trim(), position: i })),
        documents: documents
          .filter((d) => d.title.trim())
          .map((d, i) => ({
            title: d.title.trim(),
            content: d.content.trim(),
            position: i,
          })),
        before_deploy: beforeDeploy.trim() || undefined,
        after_deploy: afterDeploy.trim() || undefined,
        rollback_plan: rollbackPlan.trim() || undefined,
        relations: deployDependsOn.length
          ? deployDependsOn.map((id) => ({
              target_task_id: id,
              relation_type: "deploy_depends_on" as const,
            }))
          : undefined,
      });
      // The create endpoint has no attachments field: uploads happen before
      // the task exists, so each id is linked afterwards (sequential is fine).
      for (const attachment of attachments) {
        try {
          await api.linkTaskAttachment(repositoryId, created.id, attachment.id);
        } catch {
          toast.error(t("boardArea.components.createTask.attachmentLinkFailed", { name: attachment.filename }));
        }
      }
      toast.success(t("boardArea.components.createTask.created"));
      onOpenChange(false);
      onCreated();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("boardArea.components.createTask.createFailed"));
    } finally {
      setCreating(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{dialogTitle ?? t("boardArea.components.createTask.defaultTitle")}</DialogTitle>
        </DialogHeader>
        <div className="space-y-4">
          {repositories.length > 1 && (
            <div className="space-y-2">
              <Label>{t("boardArea.components.createTask.repository")}</Label>
              <Select value={repositoryId} onValueChange={setRepositoryId}>
                <SelectTrigger>
                  <SelectValue placeholder={t("boardArea.components.createTask.selectRepo")} />
                </SelectTrigger>
                <SelectContent>
                  {repositories.map((r) => (
                    <SelectItem key={r.id} value={r.id}>
                      {r.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}

          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2 sm:col-span-2">
              <Label htmlFor="create-task-title">{t("boardArea.components.createTask.title")}</Label>
              <Input
                id="create-task-title"
                value={taskTitle}
                onChange={(e) => setTaskTitle(e.target.value)}
                placeholder={t("boardArea.components.createTask.titlePlaceholder")}
                autoFocus
              />
            </div>
            <div className="space-y-2">
              <Label>{t("boardArea.components.createTask.type")}</Label>
              <Select value={taskType} onValueChange={(v) => setTaskType(v as TaskType)}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {typeOptions.map((o) => (
                    <SelectItem key={o.value} value={o.value}>
                      {o.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label>{t("boardArea.components.createTask.priority")}</Label>
              <Select value={priority} onValueChange={(v) => setPriority(v as TaskPriority)}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {TASK_PRIORITY_OPTIONS.map((o) => (
                    <SelectItem key={o.value} value={o.value}>
                      {t(o.labelKey)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label>{t("boardArea.components.createTask.column")}</Label>
              <Select value={column} onValueChange={(v) => setColumn(v as TaskColumn)}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {columnOptions.map((col) => (
                    <SelectItem key={col.slug} value={col.slug}>
                      {col.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            {initiativeProjects.length > 0 && (
              <div className="space-y-2">
                <Label>{t("boardArea.components.createTask.project")}</Label>
                <Select value={initiativeProjectId} onValueChange={setInitiativeProjectId}>
                  <SelectTrigger>
                    <SelectValue placeholder={t("boardArea.components.createTask.select")} />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="none">{t("boardArea.components.createTask.none")}</SelectItem>
                    {initiativeProjects.map((p) => (
                      <SelectItem key={p.id} value={p.id}>
                        {p.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            )}
          </div>

          <TaskAssigneeFields
            agents={memberAgents}
            agentValue={assigneeId === "none" ? "" : assigneeId}
            onAgentChange={(id) => setAssigneeId(id || "none")}
            disabled={creating}
          />

          <div className="space-y-2">
            <Label>{t("boardArea.components.createTask.description")}</Label>
            <MarkdownField
              value={description}
              onChange={setDescription}
              placeholder={t("boardArea.components.createTask.descriptionPlaceholder")}
            />
          </div>

          <div className="space-y-2">
            <Label>{t("boardArea.components.createTask.technical")}</Label>
            <MarkdownField
              value={technicalDescription}
              onChange={setTechnicalDescription}
              placeholder={t("boardArea.components.createTask.technicalPlaceholder")}
            />
          </div>

          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <Label>{t("boardArea.components.createTask.criteria")}</Label>
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="gap-1"
                onClick={() => setCriteria((prev) => [...prev, emptyCriterion()])}
              >
                <Plus className="h-3.5 w-3.5" />
                {t("boardArea.components.createTask.add")}
              </Button>
            </div>
            {criteria.length === 0 ? (
              <p className="text-xs text-muted-foreground">{t("boardArea.components.createTask.criteriaHint")}</p>
            ) : (
              <div className="space-y-2">
                {criteria.map((c) => (
                  <div key={c.id} className="flex gap-2">
                    <Input
                      value={c.text}
                      onChange={(e) =>
                        setCriteria((prev) =>
                          prev.map((item) =>
                            item.id === c.id ? { ...item, text: e.target.value } : item,
                          ),
                        )
                      }
                      placeholder={t("boardArea.components.createTask.criterionPlaceholder")}
                    />
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon"
                      onClick={() => setCriteria((prev) => prev.filter((item) => item.id !== c.id))}
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </div>
                ))}
              </div>
            )}
          </div>

          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <Label>{t("boardArea.components.createTask.documents")}</Label>
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="gap-1"
                onClick={() => setDocuments((prev) => [...prev, emptyDocument()])}
              >
                <Plus className="h-3.5 w-3.5" />
                {t("boardArea.components.createTask.add")}
              </Button>
            </div>
            {documents.map((doc) => (
              <div key={doc.id} className="space-y-2 rounded-lg border border-border p-3">
                <div className="flex gap-2">
                  <Input
                    value={doc.title}
                    onChange={(e) =>
                      setDocuments((prev) =>
                        prev.map((item) =>
                          item.id === doc.id ? { ...item, title: e.target.value } : item,
                        ),
                      )
                    }
                    placeholder={t("boardArea.components.createTask.docTitlePlaceholder")}
                  />
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    onClick={() => setDocuments((prev) => prev.filter((item) => item.id !== doc.id))}
                  >
                    <Trash2 className="h-4 w-4" />
                  </Button>
                </div>
                <MarkdownField
                  value={doc.content}
                  onChange={(value) =>
                    setDocuments((prev) =>
                      prev.map((item) => (item.id === doc.id ? { ...item, content: value } : item)),
                    )
                  }
                  placeholder={t("boardArea.components.createTask.docContentPlaceholder")}
                  rows={4}
                />
              </div>
            ))}
          </div>

          <div className="space-y-2">
            <Label>{t("boardArea.components.createTask.attachments")}</Label>
            <AttachmentList
              attachments={attachments}
              onRemove={(meta) => {
                setAttachments((prev) => prev.filter((a) => a.id !== meta.id));
                // Not linked to anything yet — drop the orphan row too.
                void api.deleteAttachment(meta.id).catch(() => undefined);
              }}
            />
            <AttachmentDropzone
              repositoryId={repositoryId || undefined}
              onUploaded={(metas) => setAttachments((prev) => [...prev, ...metas])}
              disabled={creating}
            />
          </div>

          <div className="space-y-2 rounded-lg border border-border p-3">
            <Button
              type="button"
              variant="ghost"
              className="h-auto w-full justify-start gap-2 p-0 hover:bg-transparent"
              onClick={() => setDeployOpen((prev) => !prev)}
            >
              {deployOpen ? (
                <ChevronDown className="h-4 w-4" />
              ) : (
                <ChevronRight className="h-4 w-4" />
              )}
              <span className="text-sm font-medium">{t("boardArea.components.createTask.deploySection")}</span>
            </Button>
            {deployOpen && (
              <div className="space-y-4 pt-2">
                <p className="text-xs text-muted-foreground">
                  {t("boardArea.components.createTask.deployHint")}
                </p>
                <MultiSelectPicker
                  label={t("boardArea.components.createTask.deployDependsOn")}
                  options={repoTasks.map((task) => ({
                    value: task.id,
                    label: `${task.key} · ${task.title}`,
                    description: task.column,
                  }))}
                  selected={deployDependsOn}
                  onChange={setDeployDependsOn}
                  loading={repoTasksLoading}
                  emptyText={t("boardArea.components.createTask.deployDependsOnEmpty")}
                />
                <div className="space-y-2">
                  <Label>{t("boardArea.components.createTask.beforeDeploy")}</Label>
                  <MarkdownField
                    value={beforeDeploy}
                    onChange={setBeforeDeploy}
                    placeholder={t("boardArea.components.createTask.beforeDeployPlaceholder")}
                    rows={3}
                  />
                </div>
                <div className="space-y-2">
                  <Label>{t("boardArea.components.createTask.afterDeploy")}</Label>
                  <MarkdownField
                    value={afterDeploy}
                    onChange={setAfterDeploy}
                    placeholder={t("boardArea.components.createTask.afterDeployPlaceholder")}
                    rows={3}
                  />
                </div>
                <div className="space-y-2">
                  <Label>{t("boardArea.components.createTask.rollbackPlan")}</Label>
                  <MarkdownField
                    value={rollbackPlan}
                    onChange={setRollbackPlan}
                    placeholder={t("boardArea.components.createTask.rollbackPlanPlaceholder")}
                    rows={3}
                  />
                </div>
              </div>
            )}
          </div>
        </div>
        <DialogFooter>
          <Button variant="ghost" className="mr-auto" onClick={() => setImportOpen(true)}>
            <Download className="mr-2 h-4 w-4" />
            {t("issues.import.fromIssue")}
          </Button>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t("common.cancel")}
          </Button>
          <Button onClick={handleCreate} disabled={creating || !taskTitle.trim() || !repositoryId}>
            {creating && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            {t("boardArea.components.createTask.create")}
          </Button>
        </DialogFooter>
      </DialogContent>
      <ImportIssueDialog
        open={importOpen}
        onOpenChange={setImportOpen}
        repositories={repositories}
        defaultRepositoryId={repositoryId}
        onImported={() => {
          onCreated();
          onOpenChange(false);
        }}
      />
    </Dialog>
  );
}
