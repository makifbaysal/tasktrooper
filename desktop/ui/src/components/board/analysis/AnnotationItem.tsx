import { Bot, Pencil, RotateCcw, SearchX, Trash2 } from "lucide-react";
import { forwardRef, useEffect, useState } from "react";
import { ANNOTATION_BODY_MAX, type TaskAnnotation, type TaskAnnotationStatus } from "@/api";
import { Badge, type BadgeProps } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";
import { cn, formatRelativeDate } from "@/lib/utils";

const STATUS_VARIANT: Record<TaskAnnotationStatus, BadgeProps["variant"]> = {
  open: "warning",
  submitted: "info",
  resolved: "success",
};

interface AnnotationItemProps {
  annotation: TaskAnnotation;
  /** undefined until the frame has tried to anchor it. */
  found: boolean | undefined;
  active: boolean;
  onActivate: () => void;
  onEdit: (body: string) => Promise<boolean>;
  onDelete: () => void;
  onReopen: () => void;
  /** The variant it was made on, when the panel lists more than one document. */
  label?: string;
}

export const AnnotationItem = forwardRef<HTMLDivElement, AnnotationItemProps>(function AnnotationItem(
  { annotation, found, active, onActivate, onEdit, onDelete, onReopen, label },
  ref,
) {
  const { t } = useI18n();
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(annotation.body);
  const [saving, setSaving] = useState(false);
  const isOpen = annotation.status === "open";

  useEffect(() => {
    if (!isOpen) setEditing(false);
  }, [isOpen]);

  const startEdit = () => {
    setDraft(annotation.body);
    setEditing(true);
  };

  const saveEdit = async () => {
    const trimmed = draft.trim();
    if (!trimmed) return;
    if (trimmed === annotation.body) {
      setEditing(false);
      return;
    }
    setSaving(true);
    const ok = await onEdit(trimmed);
    setSaving(false);
    if (ok) setEditing(false);
  };

  return (
    <Card
      ref={ref}
      data-annotation-id={annotation.id}
      className={cn("space-y-2 p-3 transition-colors", active && "border-primary ring-1 ring-primary/40")}
    >
      <div className="flex items-center gap-2">
        <Badge variant={STATUS_VARIANT[annotation.status]}>
          {t(`analysisReview.panel.status.${annotation.status}`)}
        </Badge>
        {label && (
          <Badge variant="outline" className="max-w-40 truncate">
            {label}
          </Badge>
        )}
        <span className="text-micro text-muted-foreground">{formatRelativeDate(annotation.updated_at)}</span>
        {found === false && (
          <span className="ml-auto flex items-center gap-1 text-micro text-warning">
            <SearchX className="h-3 w-3" />
            {t("analysisReview.panel.notFound")}
          </span>
        )}
      </div>
      <button
        type="button"
        onClick={onActivate}
        className="block w-full text-left"
        aria-label={t("analysisReview.panel.showInDocument")}
      >
        <blockquote className="line-clamp-3 border-l-2 border-border pl-2 text-caption italic text-muted-foreground">
          {annotation.quote}
        </blockquote>
      </button>
      {editing ? (
        <div className="space-y-2">
          <Textarea
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            maxLength={ANNOTATION_BODY_MAX}
            rows={3}
            disabled={saving}
            autoFocus
            aria-label={t("analysisReview.panel.editLabel")}
          />
          <div className="flex justify-end gap-2">
            <Button variant="ghost" size="sm" onClick={() => setEditing(false)} disabled={saving}>
              {t("common.cancel")}
            </Button>
            <Button size="sm" onClick={saveEdit} disabled={saving || !draft.trim()}>
              {t("common.save")}
            </Button>
          </div>
        </div>
      ) : (
        <p className="whitespace-pre-wrap text-body">{annotation.body}</p>
      )}
      {annotation.reply && (
        <div className="space-y-1 rounded-md bg-muted/60 p-2">
          <p className="flex items-center gap-1 text-micro font-medium text-muted-foreground">
            <Bot className="h-3 w-3" />
            {t("analysisReview.panel.agentReply")}
          </p>
          <p className="whitespace-pre-wrap text-caption">{annotation.reply}</p>
        </div>
      )}
      {!editing && isOpen && (
        <div className="flex justify-end gap-1">
          <Button variant="ghost" size="sm" onClick={startEdit}>
            <Pencil />
            {t("analysisReview.panel.edit")}
          </Button>
          <Button variant="ghost" size="sm" onClick={onDelete}>
            <Trash2 />
            {t("analysisReview.panel.delete")}
          </Button>
        </div>
      )}
      {annotation.status === "resolved" && (
        <div className="flex justify-end">
          <Button variant="ghost" size="sm" onClick={onReopen}>
            <RotateCcw />
            {t("analysisReview.panel.reopen")}
          </Button>
        </div>
      )}
    </Card>
  );
});
