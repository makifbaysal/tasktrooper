import { MessageSquareText, TextSelect } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { api, type TaskAnnotation } from "@/api";
import { AnnotationComposer } from "@/components/board/analysis/AnnotationComposer";
import { AnnotationItem } from "@/components/board/analysis/AnnotationItem";
import type { FrameSelection } from "@/components/board/analysis/srcdoc";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { EmptyState } from "@/components/ui/empty-state";
import { useI18n } from "@/hooks/useI18n";
import { annotationCounts } from "@/lib/analysis-review";
import { cn } from "@/lib/utils";

interface AnnotationsPanelProps {
  repositoryId: string;
  taskId: string;
  documentId: string | null;
  /** This document's annotations only. */
  annotations: TaskAnnotation[];
  anchored: Record<string, boolean>;
  activeId: string | null;
  pending: FrameSelection | null;
  canComment: boolean;
  onActivate: (id: string) => void;
  onCancelPending: () => void;
  onUpsert: (annotation: TaskAnnotation) => void;
  onRemove: (id: string) => void;
  /** Replaces "Commenting is paused while the agent revises the analysis." */
  pausedLabel?: string;
  className?: string;
}

const STATUS_ORDER: Record<TaskAnnotation["status"], number> = { open: 0, submitted: 1, resolved: 2 };

export function AnnotationsPanel({
  repositoryId,
  taskId,
  documentId,
  annotations,
  anchored,
  activeId,
  pending,
  canComment,
  onActivate,
  onCancelPending,
  onUpsert,
  onRemove,
  pausedLabel,
  className,
}: AnnotationsPanelProps) {
  const { t } = useI18n();
  const [creating, setCreating] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<TaskAnnotation | null>(null);
  const itemRefs = useRef(new Map<string, HTMLDivElement>());
  const counts = annotationCounts(annotations);
  const ordered = [...annotations].sort(
    (a, b) => STATUS_ORDER[a.status] - STATUS_ORDER[b.status] || a.created_at.localeCompare(b.created_at),
  );

  useEffect(() => {
    if (!activeId) return;
    itemRefs.current.get(activeId)?.scrollIntoView?.({ block: "nearest", behavior: "smooth" });
  }, [activeId]);

  const failure = (e: unknown, fallback: string) => toast.error(e instanceof Error ? e.message : t(fallback));

  const create = async (body: string) => {
    if (!pending || !documentId) return;
    setCreating(true);
    try {
      const created = await api.createTaskAnnotation(repositoryId, taskId, documentId, {
        quote: pending.quote,
        prefix: pending.prefix,
        suffix: pending.suffix,
        body,
      });
      onUpsert(created);
      onCancelPending();
      onActivate(created.id);
    } catch (e) {
      failure(e, "analysisReview.panel.createFailed");
    } finally {
      setCreating(false);
    }
  };

  const edit = async (annotation: TaskAnnotation, body: string): Promise<boolean> => {
    onUpsert({ ...annotation, body });
    try {
      onUpsert(await api.updateTaskAnnotation(repositoryId, taskId, annotation.id, { body }));
      return true;
    } catch (e) {
      onUpsert(annotation);
      failure(e, "analysisReview.panel.updateFailed");
      return false;
    }
  };

  const reopen = async (annotation: TaskAnnotation) => {
    onUpsert({ ...annotation, status: "open" });
    try {
      onUpsert(await api.updateTaskAnnotation(repositoryId, taskId, annotation.id, { status: "open" }));
    } catch (e) {
      onUpsert(annotation);
      failure(e, "analysisReview.panel.updateFailed");
    }
  };

  const remove = async (annotation: TaskAnnotation) => {
    onRemove(annotation.id);
    try {
      await api.deleteTaskAnnotation(repositoryId, taskId, annotation.id);
    } catch (e) {
      onUpsert(annotation);
      failure(e, "analysisReview.panel.deleteFailed");
    }
  };

  return (
    <aside className={cn("flex min-h-0 flex-col bg-background", className)}>
      <div className="space-y-1 border-b border-border px-4 py-3">
        <h2 className="flex items-center gap-2 text-body font-semibold">
          <MessageSquareText className="h-4 w-4 text-muted-foreground" />
          {t("analysisReview.panel.title")}
        </h2>
        <p className="text-caption text-muted-foreground">
          {t("analysisReview.panel.counts", {
            open: counts.open,
            submitted: counts.submitted,
            resolved: counts.resolved,
          })}
        </p>
      </div>
      <div className="min-h-0 flex-1 space-y-3 overflow-y-auto p-4 scrollbar-thin">
        {pending && canComment && (
          <AnnotationComposer
            quote={pending.quote}
            saving={creating}
            onSave={create}
            onCancel={onCancelPending}
          />
        )}
        {!pending && canComment && annotations.length > 0 && (
          <p className="flex items-center gap-2 text-caption text-muted-foreground">
            <TextSelect className="h-3.5 w-3.5 shrink-0" />
            {t("analysisReview.panel.selectHint")}
          </p>
        )}
        {!canComment && (
          <p className="text-caption text-muted-foreground">{pausedLabel ?? t("analysisReview.panel.commentingPaused")}</p>
        )}
        {annotations.length === 0 && !pending ? (
          <EmptyState
            icon={TextSelect}
            title={t("analysisReview.panel.empty")}
            description={t("analysisReview.panel.emptyBody")}
          />
        ) : (
          ordered.map((annotation) => (
            <AnnotationItem
              key={annotation.id}
              ref={(el) => {
                if (el) itemRefs.current.set(annotation.id, el);
                else itemRefs.current.delete(annotation.id);
              }}
              annotation={annotation}
              found={anchored[annotation.id]}
              active={annotation.id === activeId}
              onActivate={() => onActivate(annotation.id)}
              onEdit={(body) => edit(annotation, body)}
              onDelete={() => setDeleteTarget(annotation)}
              onReopen={() => void reopen(annotation)}
            />
          ))
        )}
      </div>
      <ConfirmDialog
        open={deleteTarget !== null}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title={t("analysisReview.panel.deleteTitle")}
        description={t("analysisReview.panel.deleteBody")}
        confirmLabel={t("analysisReview.panel.delete")}
        onConfirm={async () => {
          if (deleteTarget) await remove(deleteTarget);
        }}
      />
    </aside>
  );
}
