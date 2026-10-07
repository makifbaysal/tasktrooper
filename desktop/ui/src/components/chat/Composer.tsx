import { useState } from "react";
import { CircleStop, ListPlus, Paperclip, Send } from "lucide-react";
import { toast } from "sonner";
import type { AttachmentMeta, FileRecord } from "@/api";
import { AttachmentDropzone, uploadAttachmentFiles } from "@/components/attachments/AttachmentDropzone";
import { AttachmentList } from "@/components/attachments/AttachmentList";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Spinner } from "@/components/ui/spinner";
import type { MentionOption } from "@/components/chat/MentionMenu";
import { MentionTextarea } from "@/components/chat/MentionTextarea";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

interface ComposerProps {
  value: string;
  onChange: (value: string) => void;
  onSend: () => void;
  sending: boolean;
  files: FileRecord[];
  selectedFileIds: string[];
  onToggleFile: (id: string, checked: boolean) => void;
  mentionOptions?: MentionOption[];
  // Fired when the user picks an entity from the autocomplete, so the parent
  // can send the exact kind+id reference alongside the message text.
  onMentionSelect?: (option: MentionOption) => void;
  /** Binary attachments queued for the next message (removable chips). */
  pendingAttachments?: AttachmentMeta[];
  /** Present = real file attach is enabled (picker button + paste-to-attach). */
  onPendingAttachmentsChange?: (metas: AttachmentMeta[]) => void;
  /**
   * Present = the turn in flight can be stopped, and the send button becomes a
   * stop button while `sending`. Left out, the composer behaves as it always
   * did: the button stays disabled until the answer arrives.
   */
  onStop?: () => void;
  /** The stop request is in flight (keeps the button from being pressed twice). */
  stopping?: boolean;
  /**
   * Keep typing and attaching while `sending`: the send button queues the
   * message instead, and Stop sits beside it rather than replacing it.
   */
  queueing?: boolean;
}

export function Composer({
  value,
  onChange,
  onSend,
  sending,
  files,
  selectedFileIds,
  onToggleFile,
  mentionOptions = [],
  onMentionSelect,
  pendingAttachments = [],
  onPendingAttachmentsChange,
  onStop,
  stopping = false,
  queueing = false,
}: ComposerProps) {
  const { t } = useI18n();
  const [pastingAttachment, setPastingAttachment] = useState(false);
  const attachmentsEnabled = Boolean(onPendingAttachmentsChange);
  const locked = sending && !queueing;
  const queuesNext = sending && queueing;

  // Paste-into-textarea attach: files on the clipboard become pending
  // attachments through the same upload path the dropzone button uses.
  const handlePasteFiles = async (e: React.ClipboardEvent) => {
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

  return (
    <div className="border-t border-border bg-card/50 p-4" onPaste={(e) => void handlePasteFiles(e)}>
      {(pendingAttachments.length > 0 || pastingAttachment) && (
        <div className="mb-3 flex flex-wrap items-center gap-2">
          <AttachmentList
            compact
            attachments={pendingAttachments}
            onRemove={(meta) =>
              onPendingAttachmentsChange?.(pendingAttachments.filter((a) => a.id !== meta.id))
            }
          />
          {pastingAttachment && <Spinner size="sm" />}
        </div>
      )}
      {files.length > 0 && (
        <div className="mb-3 flex flex-wrap gap-2">
          <span className="flex items-center gap-1 text-caption text-muted-foreground">
            <Paperclip className="h-3 w-3" />
            {t("chatArea.chat.composer.ragFiles")}
          </span>
          {files.map((file) => {
            const selected = selectedFileIds.includes(file.id);
            return (
              <label
                key={file.id}
                className={cn(
                  "flex cursor-pointer items-center gap-2 rounded-full border px-3 py-1 text-caption transition-colors",
                  selected ? "border-primary bg-primary/10 text-primary" : "border-border hover:bg-muted",
                )}
              >
                <Checkbox
                  checked={selected}
                  onCheckedChange={(checked) => onToggleFile(file.id, checked === true)}
                />
                {file.filename}
              </label>
            );
          })}
        </div>
      )}

      {selectedFileIds.length > 0 && (
        <div className="mb-3">
          <Badge variant="secondary">{t("chatArea.chat.composer.filesSelected", { count: selectedFileIds.length })}</Badge>
        </div>
      )}

      <div className="flex items-end gap-2">
        <MentionTextarea
          wrapperClassName="flex-1"
          value={value}
          onChange={onChange}
          onSubmit={onSend}
          mentionOptions={mentionOptions}
          onMentionSelect={onMentionSelect}
          placeholder={t("chatArea.chat.composer.placeholder")}
          rows={3}
          className="min-h-[72px] w-full resize-none"
          disabled={locked}
        />
        {attachmentsEnabled && (
          <AttachmentDropzone
            variant="button"
            disabled={locked || pastingAttachment}
            onUploaded={(metas) =>
              onPendingAttachmentsChange?.([...pendingAttachments, ...metas])
            }
          />
        )}
        {/* One slot, two jobs: while the agent is answering, the only useful
            action on it is stopping — a disabled send button gave the user no way
            out of a turn that had gone wrong. A queueing composer needs both. */}
        {(!sending || queueing || !onStop) && (
          <Button
            onClick={onSend}
            disabled={locked || !value.trim()}
            size="icon"
            title={queuesNext ? t("chatArea.chat.composer.queue") : t("chatArea.chat.composer.send")}
            aria-label={queuesNext ? t("chatArea.chat.composer.queue") : t("chatArea.chat.composer.send")}
          >
            {queuesNext ? <ListPlus className="h-4 w-4" /> : <Send className="h-4 w-4" />}
          </Button>
        )}
        {sending && onStop && (
          <Button
            onClick={onStop}
            disabled={stopping}
            size="icon"
            variant="destructive"
            title={t("chatArea.chat.composer.runStop")}
            aria-label={t("chatArea.chat.composer.runStop")}
          >
            {stopping ? <Spinner size="sm" className="text-current" /> : <CircleStop className="h-4 w-4" />}
          </Button>
        )}
      </div>
    </div>
  );
}
