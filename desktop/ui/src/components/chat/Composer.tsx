import { useMemo, useRef, useState } from "react";
import { CircleStop, Paperclip, Send } from "lucide-react";
import { toast } from "sonner";
import type { AttachmentMeta, FileRecord } from "@/api";
import { AttachmentDropzone, uploadAttachmentFiles } from "@/components/attachments/AttachmentDropzone";
import { AttachmentList } from "@/components/attachments/AttachmentList";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Spinner } from "@/components/ui/spinner";
import { Textarea } from "@/components/ui/textarea";
import { MentionMenu, type MentionOption } from "@/components/chat/MentionMenu";
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
}

const MENTION_QUERY_MAX = 40;
const MENTION_RESULTS_MAX = 8;
const KIND_ORDER: Record<MentionOption["kind"], number> = { agent: 0, project: 1, repository: 2 };

function normalize(value: string) {
  return value.toLocaleLowerCase("tr");
}

// Active @token at the caret: the nearest '@' on the same line that sits at a
// word boundary. Returns null when the caret is not inside a mention.
function findMentionToken(value: string, caret: number): { start: number; query: string } | null {
  for (let i = caret - 1; i >= 0 && caret - i <= MENTION_QUERY_MAX + 1; i--) {
    const ch = value[i];
    if (ch === "\n") return null;
    if (ch === "@") {
      const before = i === 0 ? "" : value[i - 1];
      if (before && /[\p{L}\p{N}]/u.test(before)) return null;
      return { start: i, query: value.slice(i + 1, caret) };
    }
  }
  return null;
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
}: ComposerProps) {
  const { t } = useI18n();
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const [mention, setMention] = useState<{ start: number; query: string } | null>(null);
  const [activeIndex, setActiveIndex] = useState(0);
  const [pastingAttachment, setPastingAttachment] = useState(false);
  const attachmentsEnabled = Boolean(onPendingAttachmentsChange);

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

  const filtered = useMemo(() => {
    if (!mention || mentionOptions.length === 0) return [];
    const q = normalize(mention.query.trim());
    return mentionOptions
      .filter((o) => normalize(o.name).includes(q) || (o.description ? normalize(o.description).includes(q) : false))
      .sort((a, b) => KIND_ORDER[a.kind] - KIND_ORDER[b.kind] || a.name.localeCompare(b.name, "tr"))
      .slice(0, MENTION_RESULTS_MAX);
  }, [mention, mentionOptions]);

  const menuOpen = mention !== null && filtered.length > 0;

  const syncMention = (nextValue: string, caret: number) => {
    const token = findMentionToken(nextValue, caret);
    setMention(token);
    setActiveIndex(0);
  };

  const applyMention = (option: MentionOption) => {
    if (!mention) return;
    const caret = textareaRef.current?.selectionStart ?? value.length;
    const inserted = `@${option.name} `;
    const next = value.slice(0, mention.start) + inserted + value.slice(caret);
    onChange(next);
    onMentionSelect?.(option);
    setMention(null);
    const cursor = mention.start + inserted.length;
    requestAnimationFrame(() => {
      textareaRef.current?.focus();
      textareaRef.current?.setSelectionRange(cursor, cursor);
    });
  };

  return (
    <div className="border-t border-border bg-card/50 p-4 backdrop-blur-sm" onPaste={(e) => void handlePasteFiles(e)}>
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
        <div className="relative flex-1">
          {menuOpen && (
            <MentionMenu
              options={filtered}
              activeIndex={activeIndex}
              onSelect={applyMention}
              onHover={setActiveIndex}
            />
          )}
          <Textarea
            ref={textareaRef}
            value={value}
            onChange={(e) => {
              onChange(e.target.value);
              syncMention(e.target.value, e.target.selectionStart ?? e.target.value.length);
            }}
            onKeyDown={(e) => {
              if (menuOpen) {
                if (e.key === "ArrowDown") {
                  e.preventDefault();
                  setActiveIndex((i) => (i + 1) % filtered.length);
                  return;
                }
                if (e.key === "ArrowUp") {
                  e.preventDefault();
                  setActiveIndex((i) => (i - 1 + filtered.length) % filtered.length);
                  return;
                }
                if (e.key === "Enter" || e.key === "Tab") {
                  e.preventDefault();
                  applyMention(filtered[activeIndex] ?? filtered[0]);
                  return;
                }
                if (e.key === "Escape") {
                  e.preventDefault();
                  setMention(null);
                  return;
                }
              }
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                onSend();
              }
            }}
            onClick={(e) => syncMention(value, e.currentTarget.selectionStart ?? value.length)}
            onBlur={() => setMention(null)}
            placeholder={t("chatArea.chat.composer.placeholder")}
            rows={3}
            className="min-h-[72px] w-full resize-none"
            disabled={sending}
          />
        </div>
        {attachmentsEnabled && (
          <AttachmentDropzone
            variant="button"
            disabled={sending || pastingAttachment}
            onUploaded={(metas) =>
              onPendingAttachmentsChange?.([...pendingAttachments, ...metas])
            }
          />
        )}
        {/* One slot, two jobs: while the agent is answering, the only useful
            action on it is stopping — a disabled send button gave the user no way
            out of a turn that had gone wrong. */}
        {sending && onStop ? (
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
        ) : (
          <Button
            onClick={onSend}
            disabled={sending || !value.trim()}
            size="icon"
            title={t("chatArea.chat.composer.send")}
            aria-label={t("chatArea.chat.composer.send")}
          >
            <Send className="h-4 w-4" />
          </Button>
        )}
      </div>
    </div>
  );
}
