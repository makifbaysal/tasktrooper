import { Loader2, Send, Trash2 } from "lucide-react";
import { useCallback, useEffect, useRef, useState, type FormEvent, type KeyboardEvent } from "react";
import { toast } from "sonner";
import { api, type AudienceChatMessage, type AudiencePersona } from "@/api";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Notice } from "@/components/ui/notice";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";
import { PersonaAvatar } from "./PersonaAvatar";

interface PersonaChatDialogProps {
  persona: Pick<AudiencePersona, "id" | "name" | "role" | "segment"> | null;
  /** When set, a question is asked as a follow-up of this study and listed under it. */
  studyId?: string;
  onOpenChange: (open: boolean) => void;
  /** Fires after a reply arrived, so a study page can refresh its follow-ups. */
  onAnswered?: () => void;
}

/**
 * A one-to-one conversation with a synthetic person. They stay in character,
 * remember the thread and what they told the studies; the thread is kept on
 * this computer, so it is still here tomorrow.
 */
export function PersonaChatDialog({ persona, studyId, onOpenChange, onAnswered }: PersonaChatDialogProps) {
  const { t } = useI18n();
  const c = (key: string, params?: Record<string, string | number>) => t(`audienceHub.chat.${key}`, params);
  const [messages, setMessages] = useState<AudienceChatMessage[]>([]);
  const [draft, setDraft] = useState("");
  const [loading, setLoading] = useState(false);
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [confirmClear, setConfirmClear] = useState(false);
  const bottomRef = useRef<HTMLDivElement>(null);
  const personaId = persona?.id;

  const load = useCallback(async () => {
    if (!personaId) return;
    setLoading(true);
    try {
      const out = await api.audienceThread(personaId);
      setMessages(out.messages);
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : c("loadFailed"));
    } finally {
      setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [personaId]);

  useEffect(() => {
    setMessages([]);
    setDraft("");
    setError(null);
    void load();
  }, [load]);

  useEffect(() => {
    bottomRef.current?.scrollIntoView?.({ block: "end" });
  }, [messages, sending]);

  const send = async (e?: FormEvent) => {
    e?.preventDefault();
    const text = draft.trim();
    if (!personaId || !text || sending) return;
    setSending(true);
    setError(null);
    // Shown at once; the thread is re-read when the reply lands.
    setMessages((prev) => [
      ...prev,
      { id: `pending-${prev.length}`, persona_id: personaId, role: "user", content: text, created_at: new Date().toISOString() },
    ]);
    setDraft("");
    try {
      if (studyId) await api.followUpAudiencePersona(studyId, personaId, text);
      else await api.sendAudienceMessage(personaId, text);
      await load();
      onAnswered?.();
    } catch (err) {
      // The thread is re-read to drop the optimistic message, which clears the
      // error line, so the error is set after it.
      await load();
      setError(err instanceof Error ? err.message : c("sendFailed"));
      setDraft(text);
    } finally {
      setSending(false);
    }
  };

  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault();
      void send();
    }
  };

  const clear = async () => {
    if (!personaId) return;
    try {
      await api.clearAudienceThread(personaId);
      setMessages([]);
      toast.success(c("cleared"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : c("clearFailed"));
    }
  };

  return (
    <>
      <Dialog open={persona !== null} onOpenChange={onOpenChange}>
        <DialogContent className="flex h-[min(720px,90vh)] flex-col gap-0 p-0 sm:max-w-xl">
          <DialogHeader className="border-b border-border p-4 pr-12">
            <div className="flex items-center gap-3">
              {persona && <PersonaAvatar name={persona.name} />}
              <div className="min-w-0 text-left">
                <DialogTitle className="truncate">{persona ? c("title", { name: persona.name }) : ""}</DialogTitle>
                <DialogDescription className="truncate">
                  {[persona?.role, persona?.segment].filter(Boolean).join(" · ") || c("synthetic")}
                </DialogDescription>
              </div>
              {messages.length > 0 && (
                <Button variant="ghost" size="icon" className="ml-auto shrink-0" onClick={() => setConfirmClear(true)} aria-label={c("clear")} title={c("clear")}>
                  <Trash2 />
                </Button>
              )}
            </div>
          </DialogHeader>

          <div className="flex-1 space-y-3 overflow-y-auto p-4" aria-live="polite">
            <Notice variant="info" title={c("syntheticTitle")} role={null}>
              {c("syntheticBody")}
            </Notice>
            {loading && messages.length === 0 && <p className="text-caption text-muted-foreground">{c("loading")}</p>}
            {!loading && messages.length === 0 && <p className="text-body text-muted-foreground">{c("empty", { name: persona?.name ?? "" })}</p>}
            {messages.map((m) => (
              <div key={m.id} className={cn("flex", m.role === "user" ? "justify-end" : "justify-start")}>
                <p
                  className={cn(
                    "max-w-[85%] whitespace-pre-wrap rounded-2xl px-3.5 py-2 text-body leading-relaxed",
                    m.role === "user" ? "bg-primary text-primary-foreground" : "bg-surface-sunken",
                  )}
                >
                  {m.content}
                </p>
              </div>
            ))}
            {sending && (
              <p className="flex items-center gap-2 text-caption text-muted-foreground">
                <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden />
                {c("thinking", { name: persona?.name ?? "" })}
              </p>
            )}
            {error && (
              <Notice variant="error" title={c("failedTitle")}>
                {error}
              </Notice>
            )}
            <div ref={bottomRef} />
          </div>

          <form onSubmit={(e) => void send(e)} className="flex items-end gap-2 border-t border-border p-3">
            <Textarea
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              onKeyDown={onKeyDown}
              rows={2}
              placeholder={c("placeholder")}
              aria-label={c("placeholder")}
              className="min-h-[44px]"
            />
            <Button type="submit" disabled={sending || draft.trim() === ""} aria-label={c("send")}>
              {sending ? <Loader2 className="animate-spin" /> : <Send />}
              {c("send")}
            </Button>
          </form>
        </DialogContent>
      </Dialog>
      <ConfirmDialog
        open={confirmClear}
        onOpenChange={setConfirmClear}
        title={c("clearTitle")}
        description={c("clearBody", { name: persona?.name ?? "" })}
        confirmLabel={c("clear")}
        variant="destructive"
        onConfirm={clear}
      />
    </>
  );
}
