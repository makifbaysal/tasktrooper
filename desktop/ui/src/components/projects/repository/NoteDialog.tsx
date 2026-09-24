import { useEffect, useState } from "react";
import { toast } from "sonner";
import { api, NOTE_TOPICS, type Component, type NoteTopic, type ProjectNote } from "@/api";
import { FormDialog } from "@/components/admin/FormDialog";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";
import { componentLabel } from "@/lib/project-model";

const REPO_LEVEL = "__repo__";

interface NoteDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  repositoryId: string;
  repositoryName: string;
  components: Component[];
  /** Present = editing (topic/component are fixed, saveNote overwrites the same note); absent = a new note. */
  initial?: ProjectNote;
  onSaved: () => void;
}

/** Both "Add note" and "Edit note" go through PUT saveNote — a note is
 * identified by (repository, component, topic), so editing sends the same
 * topic/component back with a new body; the author becomes you either way. */
export function NoteDialog({ open, onOpenChange, repositoryId, repositoryName, components, initial, onSaved }: NoteDialogProps) {
  const { t } = useI18n();
  const [topic, setTopic] = useState<NoteTopic>("purpose");
  const [componentId, setComponentId] = useState(REPO_LEVEL);
  const [body, setBody] = useState("");
  const [locked, setLocked] = useState(false);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!open) return;
    setTopic(initial?.topic ?? "purpose");
    setComponentId(initial?.component_id ?? REPO_LEVEL);
    setBody(initial?.body_md ?? "");
    setLocked(initial?.locked ?? false);
  }, [open, initial]);

  const submit = async () => {
    if (!body.trim()) return;
    setSaving(true);
    try {
      await api.saveNote(repositoryId, {
        component_id: componentId === REPO_LEVEL ? undefined : componentId,
        topic,
        body_md: body,
        locked,
      });
      onSaved();
      onOpenChange(false);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <FormDialog
      open={open}
      onOpenChange={onOpenChange}
      title={initial ? t("repositoryPage.addNote.editTitle") : t("repositoryPage.addNote.title")}
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
            {t("common.cancel")}
          </Button>
          <Button onClick={submit} disabled={saving || !body.trim()}>
            {t("repositoryPage.addNote.submit")}
          </Button>
        </>
      }
    >
      <div className="grid gap-3 sm:grid-cols-2">
        <div className="space-y-2">
          <Label>{t("repositoryPage.addNote.topic")}</Label>
          <Select value={topic} onValueChange={(v) => setTopic(v as NoteTopic)} disabled={Boolean(initial)}>
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {NOTE_TOPICS.map((tp) => (
                <SelectItem key={tp} value={tp}>
                  {t(`projectModel.noteTopics.${tp}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-2">
          <Label>{t("repositoryPage.addNote.component")}</Label>
          <Select value={componentId} onValueChange={setComponentId} disabled={Boolean(initial)}>
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={REPO_LEVEL}>{t("repositoryPage.knowledge.repoLevel")}</SelectItem>
              {components.map((c) => (
                <SelectItem key={c.id} value={c.id}>
                  {componentLabel(c, repositoryName)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>
      <div className="space-y-2">
        <Label>{t("repositoryPage.addNote.body")}</Label>
        <Textarea value={body} onChange={(e) => setBody(e.target.value)} rows={8} className="font-mono" />
      </div>
      <div className="flex items-center justify-between gap-3 rounded-md border border-border p-3">
        <div>
          <Label htmlFor="note-locked">{t("repositoryPage.addNote.locked")}</Label>
          <p className="text-micro text-muted-foreground">{t("repositoryPage.addNote.lockedHelp")}</p>
        </div>
        <Switch id="note-locked" checked={locked} onCheckedChange={setLocked} />
      </div>
    </FormDialog>
  );
}
