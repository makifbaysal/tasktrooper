import { Lock, Pencil, Plus, Trash2, Unlock } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { api, type ComponentRole, type ProjectNote, type RepositoryModel } from "@/api";
import { NoteDialog } from "@/components/projects/repository/NoteDialog";
import { EvidenceList } from "@/components/projects/model/EvidenceList";
import { MarkdownContent } from "@/components/markdown/MarkdownContent";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { componentLabel, factValue } from "@/lib/project-model";

interface KnowledgeTabProps {
  model: RepositoryModel;
  repositoryId: string;
  onReload: () => void;
}

const ALL_SCOPE = "all";
const componentScope = (id: string) => `component:${id}`;
const roleScope = (role: ComponentRole) => `role:${role}`;

export function KnowledgeTab({ model, repositoryId, onReload }: KnowledgeTabProps) {
  const { t } = useI18n();
  const [scope, setScope] = useState(ALL_SCOPE);
  const [brief, setBrief] = useState<string | null>(null);
  const [loadingBrief, setLoadingBrief] = useState(true);
  const [addOpen, setAddOpen] = useState(false);
  const [editing, setEditing] = useState<ProjectNote | null>(null);
  const [deleting, setDeleting] = useState<ProjectNote | null>(null);

  const roles = useMemo(() => {
    const set = new Set<ComponentRole>();
    for (const c of model.components) {
      const role = factValue(c.role);
      if (role) set.add(role);
    }
    return [...set];
  }, [model.components]);

  useEffect(() => {
    setLoadingBrief(true);
    const params = scope.startsWith("component:")
      ? { componentId: scope.slice("component:".length) }
      : scope.startsWith("role:")
        ? { area: scope.slice("role:".length) as ComponentRole }
        : {};
    api
      .getRepositoryBrief(repositoryId, params)
      .then((res) => setBrief(res.brief))
      .catch((e) => toast.error(e instanceof Error ? e.message : t("common.actionFailed")))
      .finally(() => setLoadingBrief(false));
  }, [repositoryId, scope, t]);

  const repoNotes = model.notes.filter((n) => !n.component_id);
  const notesByComponent = new Map<string, ProjectNote[]>();
  for (const note of model.notes) {
    if (!note.component_id) continue;
    const list = notesByComponent.get(note.component_id) ?? [];
    list.push(note);
    notesByComponent.set(note.component_id, list);
  }

  const toggleLock = async (note: ProjectNote) => {
    try {
      await api.updateNote(note.id, { locked: !note.locked });
      onReload();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.actionFailed"));
    }
  };

  return (
    <div className="grid gap-6 lg:grid-cols-[1.1fr_1fr]">
      <Card className="space-y-3 p-6">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h3 className="font-semibold">{t("repositoryPage.knowledge.briefTitle")}</h3>
          <Select value={scope} onValueChange={setScope}>
            <SelectTrigger className="w-56">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL_SCOPE}>{t("repositoryPage.knowledge.scopeAll")}</SelectItem>
              {model.components.map((c) => (
                <SelectItem key={c.id} value={componentScope(c.id)}>
                  {componentLabel(c, model.repository.name)}
                </SelectItem>
              ))}
              {roles.map((role) => (
                <SelectItem key={role} value={roleScope(role)}>
                  {t("repositoryPage.knowledge.scopeRoleArea", { role: t(`projectModel.roles.${role}`) })}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        {loadingBrief ? (
          <Skeleton className="h-96 w-full" />
        ) : (
          <pre className="h-96 overflow-auto whitespace-pre-wrap rounded-md bg-muted p-4 font-mono text-caption">{brief}</pre>
        )}
      </Card>

      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <h3 className="font-semibold">{t("repositoryPage.knowledge.notesTitle")}</h3>
          <Button size="sm" onClick={() => setAddOpen(true)}>
            <Plus className="mr-1.5 h-3.5 w-3.5" />
            {t("repositoryPage.knowledge.addNote")}
          </Button>
        </div>

        {model.notes.length === 0 ? (
          <p className="text-body text-muted-foreground">{t("repositoryPage.knowledge.empty")}</p>
        ) : (
          <div className="space-y-3">
            {repoNotes.map((note) => (
              <NoteCard key={note.id} note={note} onEdit={() => setEditing(note)} onToggleLock={() => toggleLock(note)} onDelete={() => setDeleting(note)} />
            ))}
            {model.components.map((component) => {
              const notes = notesByComponent.get(component.id);
              if (!notes || notes.length === 0) return null;
              return (
                <div key={component.id} className="space-y-2">
                  <p className="font-mono text-caption text-muted-foreground">{componentLabel(component, model.repository.name)}</p>
                  {notes.map((note) => (
                    <NoteCard key={note.id} note={note} onEdit={() => setEditing(note)} onToggleLock={() => toggleLock(note)} onDelete={() => setDeleting(note)} />
                  ))}
                </div>
              );
            })}
          </div>
        )}
      </div>

      <NoteDialog
        open={addOpen}
        onOpenChange={setAddOpen}
        repositoryId={repositoryId}
        repositoryName={model.repository.name}
        components={model.components}
        onSaved={onReload}
      />
      {editing && (
        <NoteDialog
          open={Boolean(editing)}
          onOpenChange={(open) => !open && setEditing(null)}
          repositoryId={repositoryId}
          repositoryName={model.repository.name}
          components={model.components}
          initial={editing}
          onSaved={onReload}
        />
      )}
      <ConfirmDialog
        open={Boolean(deleting)}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={t("repositoryPage.knowledge.deleteConfirmTitle")}
        description={t("repositoryPage.knowledge.deleteConfirmDesc")}
        confirmLabel={t("repositoryPage.knowledge.delete")}
        onConfirm={async () => {
          if (!deleting) return;
          try {
            await api.deleteNote(deleting.id);
            onReload();
          } catch (e) {
            toast.error(e instanceof Error ? e.message : t("common.actionFailed"));
          }
        }}
      />
    </div>
  );
}

function NoteCard({ note, onEdit, onToggleLock, onDelete }: { note: ProjectNote; onEdit: () => void; onToggleLock: () => void; onDelete: () => void }) {
  const { t } = useI18n();
  return (
    <Card className="space-y-2 p-4">
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-1.5">
          <span className="font-medium">{t(`projectModel.noteTopics.${note.topic}`)}</span>
          <Badge variant={note.author === "agent" ? "info" : "secondary"}>
            {t(note.author === "agent" ? "repositoryPage.knowledge.authorAgent" : "repositoryPage.knowledge.authorYou")}
          </Badge>
          {note.stale && <Badge variant="warning">{t("repositoryPage.knowledge.stale")}</Badge>}
        </div>
        <div className="flex items-center gap-1">
          <Button variant="ghost" size="icon" className="h-7 w-7" onClick={onEdit}>
            <Pencil className="h-3.5 w-3.5" />
          </Button>
          <Button variant="ghost" size="icon" className="h-7 w-7" onClick={onToggleLock}>
            {note.locked ? <Lock className="h-3.5 w-3.5" /> : <Unlock className="h-3.5 w-3.5" />}
          </Button>
          <Button variant="ghost" size="icon" className="h-7 w-7 text-muted-foreground hover:text-destructive" onClick={onDelete}>
            <Trash2 className="h-3.5 w-3.5" />
          </Button>
        </div>
      </div>
      <MarkdownContent content={note.body_md} />
      {(note.evidence?.length ?? 0) > 0 && <EvidenceList evidence={note.evidence ?? []} />}
    </Card>
  );
}
