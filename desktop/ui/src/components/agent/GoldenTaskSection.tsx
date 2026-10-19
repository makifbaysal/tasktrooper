import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { api, type GoldenTask, type GoldenTaskInput } from "@/api";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";

interface Draft {
  name: string;
  prompt: string;
  expected: string;
  enabled: boolean;
}

const emptyDraft = (): Draft => ({ name: "", prompt: "", expected: "", enabled: true });

function toInput(d: Draft): GoldenTaskInput {
  return {
    name: d.name.trim(),
    prompt: d.prompt.trim(),
    expected: d.expected
      .split("\n")
      .map((line) => line.trim())
      .filter(Boolean),
    enabled: d.enabled,
  };
}

function fromTask(task: GoldenTask): GoldenTaskInput {
  return { name: task.name, prompt: task.prompt, expected: task.expected ?? [], enabled: task.enabled };
}

export function GoldenTaskSection({ agentId }: { agentId: string }) {
  const { t } = useI18n();
  const [tasks, setTasks] = useState<GoldenTask[]>([]);
  const [draft, setDraft] = useState<Draft>(emptyDraft());
  const [editingId, setEditingId] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  const load = useCallback(async () => {
    try {
      const data = await api.listGoldenTasks(agentId);
      setTasks(data.golden_tasks ?? []);
    } catch {
      setTasks([]);
    }
  }, [agentId]);

  useEffect(() => {
    void load();
  }, [load]);

  const startEdit = (task: GoldenTask) => {
    setEditingId(task.id);
    setDraft({ name: task.name, prompt: task.prompt, expected: (task.expected ?? []).join("\n"), enabled: task.enabled });
  };

  const cancelEdit = () => {
    setEditingId(null);
    setDraft(emptyDraft());
  };

  const handleSubmit = async () => {
    const input = toInput(draft);
    if (!input.name || !input.prompt) {
      toast.error(t("agentArea.components.golden.toast.required"));
      return;
    }
    setSaving(true);
    try {
      if (editingId) {
        await api.updateGoldenTask(agentId, editingId, input);
        toast.success(t("agentArea.components.golden.toast.updated"));
        setEditingId(null);
      } else {
        await api.createGoldenTask(agentId, input);
        toast.success(t("agentArea.components.golden.toast.added"));
      }
      setDraft(emptyDraft());
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("agentArea.components.golden.toast.saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async (id: string) => {
    try {
      await api.deleteGoldenTask(agentId, id);
      toast.success(t("agentArea.components.golden.toast.deleted"));
      if (editingId === id) cancelEdit();
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("agentArea.components.golden.toast.deleteFailed"));
    }
  };

  const handleToggle = async (task: GoldenTask, enabled: boolean) => {
    try {
      await api.updateGoldenTask(agentId, task.id, { ...fromTask(task), enabled });
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("agentArea.components.golden.toast.toggleFailed"));
    }
  };

  return (
    <Card className="w-full space-y-4 p-6">
      <div>
        <h3 className="text-base font-semibold">{t("agentArea.components.golden.heading")}</h3>
        <p className="text-sm text-muted-foreground">{t("agentArea.components.golden.help")}</p>
      </div>
      {tasks.length > 0 ? (
        <div className="space-y-2">
          {tasks.map((task) => (
            <div key={task.id} className="flex items-center justify-between gap-3 rounded-md border p-3">
              <div className="min-w-0">
                <div className="truncate text-sm font-medium">{task.name}</div>
                <div className="truncate text-xs text-muted-foreground">{task.prompt}</div>
                <div className="text-xs text-muted-foreground">
                  {t("agentArea.components.golden.expectedCount", { count: (task.expected ?? []).length })}
                </div>
              </div>
              <div className="flex shrink-0 items-center gap-3">
                <Switch checked={task.enabled} onCheckedChange={(v) => handleToggle(task, v)} />
                <Button variant="ghost" size="sm" onClick={() => startEdit(task)}>
                  {t("agentArea.components.golden.edit")}
                </Button>
                <Button variant="ghost" size="sm" onClick={() => handleDelete(task.id)}>
                  {t("agentArea.components.golden.delete")}
                </Button>
              </div>
            </div>
          ))}
        </div>
      ) : (
        <p className="text-sm text-muted-foreground">{t("agentArea.components.golden.empty")}</p>
      )}
      <div className="space-y-3 rounded-md border p-3">
        <div className="text-sm font-medium">
          {editingId ? t("agentArea.components.golden.editTitle") : t("agentArea.components.golden.newTitle")}
        </div>
        <div className="space-y-1">
          <Label htmlFor="golden-name" className="text-xs">
            {t("agentArea.components.golden.name")}
          </Label>
          <Input
            id="golden-name"
            value={draft.name}
            onChange={(e) => setDraft((d) => ({ ...d, name: e.target.value }))}
          />
        </div>
        <div className="space-y-1">
          <Label htmlFor="golden-prompt" className="text-xs">
            {t("agentArea.components.golden.prompt")}
          </Label>
          <Textarea
            id="golden-prompt"
            rows={3}
            placeholder={t("agentArea.components.golden.promptPlaceholder")}
            value={draft.prompt}
            onChange={(e) => setDraft((d) => ({ ...d, prompt: e.target.value }))}
          />
        </div>
        <div className="space-y-1">
          <Label htmlFor="golden-expected" className="text-xs">
            {t("agentArea.components.golden.expected")}
          </Label>
          <Textarea
            id="golden-expected"
            rows={3}
            value={draft.expected}
            onChange={(e) => setDraft((d) => ({ ...d, expected: e.target.value }))}
          />
          <p className="text-xs text-muted-foreground">{t("agentArea.components.golden.expectedHelp")}</p>
        </div>
        <div className="flex items-center gap-2">
          <Button size="sm" onClick={handleSubmit} disabled={saving}>
            {saving
              ? t("common.saving")
              : editingId
                ? t("agentArea.components.golden.update")
                : t("agentArea.components.golden.add")}
          </Button>
          {editingId ? (
            <Button size="sm" variant="ghost" onClick={cancelEdit} disabled={saving}>
              {t("common.cancel")}
            </Button>
          ) : null}
        </div>
      </div>
    </Card>
  );
}
