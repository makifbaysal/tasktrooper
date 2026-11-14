import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";
import type { Agent, AttachmentMeta } from "@/api";
import { AgentAvatar } from "@/components/agent/AgentAvatar";
import { AttachmentDropzone, uploadAttachmentFiles } from "@/components/attachments/AttachmentDropzone";
import { AttachmentList } from "@/components/attachments/AttachmentList";
import type { MentionOption } from "@/components/chat/MentionMenu";
import { MentionTextarea } from "@/components/chat/MentionTextarea";
import { Spinner } from "@/components/ui/spinner";
import { useI18n } from "@/hooks/useI18n";
import { useMentionOptions } from "@/hooks/useMentionOptions";
import { startLeadConversation } from "@/lib/leadAgent";
import { shortcutModifier } from "@/lib/platform";
import { agentRole, logEvent } from "@/lib/analytics";

interface LeadQuickAskProps {
  agent: Agent;
}

export function LeadQuickAsk({ agent }: LeadQuickAskProps) {
  const { t } = useI18n();
  const navigate = useNavigate();
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const mentionOptions = useMentionOptions();
  const pickedRef = useRef<MentionOption[]>([]);
  const [pendingAttachments, setPendingAttachments] = useState<AttachmentMeta[]>([]);
  const [pastingAttachment, setPastingAttachment] = useState(false);

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        textareaRef.current?.focus();
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  // Paste-into-textarea attach: files on the clipboard become pending
  // attachments through the same upload path the dropzone button uses.
  const handlePasteFiles = async (e: React.ClipboardEvent) => {
    const files = Array.from(e.clipboardData?.files ?? []);
    if (files.length === 0) return;
    e.preventDefault();
    setPastingAttachment(true);
    try {
      const metas = await uploadAttachmentFiles(files);
      setPendingAttachments((prev) => [...prev, ...metas]);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("frame.ui.attachments.uploadFailed"));
    } finally {
      setPastingAttachment(false);
    }
  };

  const submit = async () => {
    const message = text.trim();
    if (!message) {
      navigate(`/agents/${agent.id}/chat`);
      return;
    }
    const fold = message.toLocaleLowerCase("tr");
    const mentions = pickedRef.current
      .filter((m) => fold.includes(`@${m.name.toLocaleLowerCase("tr")}`))
      .map(({ kind, id, name }) => ({ kind, id, name }));
    setBusy(true);
    try {
      await startLeadConversation(
        navigate,
        agent.id,
        message,
        t("agentArea.chat.newSessionTitle", { name: agent.name }),
        mentions,
        pendingAttachments,
      );
      logEvent("chat_start", { agent_role: agentRole(agent), source: "home_quick_ask" });
      setText("");
      setPendingAttachments([]);
      pickedRef.current = [];
      textareaRef.current?.blur();
    } catch {
      toast.error(t("frame.layout.header.leadAsk.failed"));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div
      className="relative hidden w-full max-w-md items-center gap-1 md:flex"
      onPaste={(e) => void handlePasteFiles(e)}
    >
      <div className="relative min-w-0 flex-1">
        <AgentAvatar
          name={agent.name}
          slug={agent.catalog_slug}
          lead
          size="xs"
          className="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 z-10"
        />
        <MentionTextarea
          ref={textareaRef}
          value={text}
          onChange={setText}
          onSubmit={() => void submit()}
          disabled={busy}
          mentionOptions={mentionOptions}
          onMentionSelect={(option) => {
            if (!pickedRef.current.some((m) => m.kind === option.kind && m.id === option.id)) {
              pickedRef.current = [...pickedRef.current, option];
            }
          }}
          rows={1}
          aria-label={t("frame.layout.header.leadAsk.label", { name: agent.name })}
          placeholder={t("frame.layout.header.leadAsk.placeholder")}
          className="h-9 min-h-0 resize-none overflow-hidden py-1.5 pr-12 pl-10 leading-6"
        />
        {!text && (
          <kbd className="pointer-events-none absolute top-1/2 right-2.5 -translate-y-1/2 rounded border border-border px-1.5 font-mono text-micro text-muted-foreground">
            {t("frame.layout.header.leadAsk.shortcut", { mod: shortcutModifier() })}
          </kbd>
        )}
        {(pendingAttachments.length > 0 || pastingAttachment) && (
          <div className="absolute top-full left-0 right-0 z-50 mt-1 flex flex-wrap items-center gap-2 rounded-lg border border-border bg-card p-2 shadow-[var(--shadow-raised)]">
            <AttachmentList
              compact
              attachments={pendingAttachments}
              onRemove={(meta) => setPendingAttachments((prev) => prev.filter((a) => a.id !== meta.id))}
            />
            {pastingAttachment && <Spinner size="sm" />}
          </div>
        )}
      </div>
      <AttachmentDropzone
        variant="button"
        disabled={busy || pastingAttachment}
        onUploaded={(metas) => setPendingAttachments((prev) => [...prev, ...metas])}
      />
    </div>
  );
}
