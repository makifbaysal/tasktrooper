import { Bold, Eye, Heading2, Italic, Link2, List, ListOrdered, Pencil } from "lucide-react";
import { useRef, useState, type ClipboardEvent } from "react";
import { toast } from "sonner";
import type { AttachmentMeta } from "@/api";
import { AttachmentDropzone, uploadAttachmentFiles } from "@/components/attachments/AttachmentDropzone";
import { AttachmentList } from "@/components/attachments/AttachmentList";
import type { MentionOption } from "@/components/chat/MentionMenu";
import { MentionTextarea } from "@/components/chat/MentionTextarea";
import { MarkdownContent } from "@/components/markdown/MarkdownContent";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

interface MarkdownFieldProps {
  id?: string;
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  rows?: number;
  className?: string;
  /** @-mention autocomplete (agents/projects/repositories); omitted keeps today's plain textarea. */
  mentionOptions?: MentionOption[];
  onMentionSelect?: (option: MentionOption) => void;
  /** True = an attach button and paste-to-attach are offered, with pendingAttachments shown as removable chips. */
  attachmentsEnabled?: boolean;
  pendingAttachments?: AttachmentMeta[];
  onPendingAttachmentsChange?: (metas: AttachmentMeta[]) => void;
}

type WrapFn = (before: string, after: string, placeholder?: string) => void;

function ToolbarButton({
  label,
  onClick,
  children,
}: {
  label: string;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <Button type="button" variant="ghost" size="icon" className="h-7 w-7" title={label} onClick={onClick}>
      {children}
    </Button>
  );
}

export function MarkdownField({
  id,
  value,
  onChange,
  placeholder,
  rows = 6,
  className,
  mentionOptions,
  onMentionSelect,
  attachmentsEnabled = false,
  pendingAttachments = [],
  onPendingAttachmentsChange,
}: MarkdownFieldProps) {
  const { t } = useI18n();
  const [mode, setMode] = useState<"edit" | "preview">("edit");
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const [pastingAttachment, setPastingAttachment] = useState(false);

  const handlePasteFiles = async (e: ClipboardEvent) => {
    if (!attachmentsEnabled) return;
    const files = Array.from(e.clipboardData?.files ?? []);
    if (files.length === 0) return;
    e.preventDefault();
    setPastingAttachment(true);
    try {
      const metas = await uploadAttachmentFiles(files);
      onPendingAttachmentsChange?.([...pendingAttachments, ...metas]);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("frame.ui.attachments.uploadFailed"));
    } finally {
      setPastingAttachment(false);
    }
  };

  const wrapSelection: WrapFn = (before, after, insertPlaceholder) => {
    const el = textareaRef.current;
    if (!el) return;
    const start = el.selectionStart;
    const end = el.selectionEnd;
    const selected = value.slice(start, end) || insertPlaceholder || "";
    const next = value.slice(0, start) + before + selected + after + value.slice(end);
    onChange(next);
    requestAnimationFrame(() => {
      el.focus();
      const cursor = start + before.length + selected.length;
      el.setSelectionRange(cursor, cursor);
    });
  };

  const prefixLines = (prefix: string) => {
    const el = textareaRef.current;
    if (!el) return;
    const start = el.selectionStart;
    const end = el.selectionEnd;
    const before = value.slice(0, start);
    const selected = value.slice(start, end);
    const after = value.slice(end);
    const block = selected || "";
    const lines = block.split("\n").map((line) => (line ? `${prefix}${line}` : prefix.trimEnd()));
    const next = before + lines.join("\n") + after;
    onChange(next);
  };

  return (
    <div
      className={cn("overflow-hidden rounded-lg border border-border", className)}
      onPaste={(e) => void handlePasteFiles(e)}
    >
      <div className="flex flex-wrap items-center justify-between gap-1 border-b border-border bg-muted/30 px-2 py-1">
        <div className="flex flex-wrap items-center gap-0.5">
          <ToolbarButton label={t("chatArea.markdown.field.bold")} onClick={() => wrapSelection("**", "**", t("chatArea.markdown.field.boldPlaceholder"))}>
            <Bold className="h-3.5 w-3.5" />
          </ToolbarButton>
          <ToolbarButton label={t("chatArea.markdown.field.italic")} onClick={() => wrapSelection("*", "*", t("chatArea.markdown.field.italicPlaceholder"))}>
            <Italic className="h-3.5 w-3.5" />
          </ToolbarButton>
          <ToolbarButton label={t("chatArea.markdown.field.heading")} onClick={() => prefixLines("## ")}>
            <Heading2 className="h-3.5 w-3.5" />
          </ToolbarButton>
          <ToolbarButton label={t("chatArea.markdown.field.bulletList")} onClick={() => prefixLines("- ")}>
            <List className="h-3.5 w-3.5" />
          </ToolbarButton>
          <ToolbarButton label={t("chatArea.markdown.field.numberedList")} onClick={() => prefixLines("1. ")}>
            <ListOrdered className="h-3.5 w-3.5" />
          </ToolbarButton>
          <ToolbarButton label={t("chatArea.markdown.field.link")} onClick={() => wrapSelection("[", "](url)", t("chatArea.markdown.field.linkPlaceholder"))}>
            <Link2 className="h-3.5 w-3.5" />
          </ToolbarButton>
          {attachmentsEnabled && (
            <AttachmentDropzone
              variant="button"
              disabled={pastingAttachment}
              onUploaded={(metas) => onPendingAttachmentsChange?.([...pendingAttachments, ...metas])}
            />
          )}
        </div>
        <div className="flex items-center gap-0.5">
          <Button
            type="button"
            variant={mode === "edit" ? "secondary" : "ghost"}
            size="sm"
            className="h-7 gap-1 px-2 text-xs"
            onClick={() => setMode("edit")}
          >
            <Pencil className="h-3 w-3" />
            {t("chatArea.markdown.field.edit")}
          </Button>
          <Button
            type="button"
            variant={mode === "preview" ? "secondary" : "ghost"}
            size="sm"
            className="h-7 gap-1 px-2 text-xs"
            onClick={() => setMode("preview")}
          >
            <Eye className="h-3 w-3" />
            {t("chatArea.markdown.field.preview")}
          </Button>
        </div>
      </div>
      {mode === "edit" ? (
        mentionOptions ? (
          <MentionTextarea
            id={id}
            ref={textareaRef}
            value={value}
            onChange={onChange}
            submitOnEnter={false}
            mentionOptions={mentionOptions}
            onMentionSelect={onMentionSelect}
            placeholder={placeholder}
            rows={rows}
            className="min-h-[8rem] resize-y rounded-none border-0 bg-background focus-visible:ring-0"
          />
        ) : (
          <Textarea
            id={id}
            ref={textareaRef}
            value={value}
            onChange={(e) => onChange(e.target.value)}
            placeholder={placeholder}
            rows={rows}
            className="min-h-[8rem] resize-y rounded-none border-0 bg-background focus-visible:ring-0"
          />
        )
      ) : (
        <div className="min-h-[8rem] bg-background p-3">
          <MarkdownContent content={value} />
        </div>
      )}
      {attachmentsEnabled && (pendingAttachments.length > 0 || pastingAttachment) && (
        <div className="flex flex-wrap items-center gap-2 border-t border-border bg-muted/20 px-3 py-2">
          <AttachmentList
            compact
            attachments={pendingAttachments}
            onRemove={(meta) => onPendingAttachmentsChange?.(pendingAttachments.filter((a) => a.id !== meta.id))}
          />
          {pastingAttachment && <Spinner size="sm" />}
        </div>
      )}
    </div>
  );
}
