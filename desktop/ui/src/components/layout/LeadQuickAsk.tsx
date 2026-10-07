import { useEffect, useRef, useState, type FormEvent } from "react";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";
import type { Agent } from "@/api";
import { AgentAvatar } from "@/components/agent/AgentAvatar";
import { Input } from "@/components/ui/input";
import { useI18n } from "@/hooks/useI18n";
import { startLeadConversation } from "@/lib/leadAgent";
import { shortcutModifier } from "@/lib/platform";

interface LeadQuickAskProps {
  agent: Agent;
}

export function LeadQuickAsk({ agent }: LeadQuickAskProps) {
  const { t } = useI18n();
  const navigate = useNavigate();
  const inputRef = useRef<HTMLInputElement>(null);
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        inputRef.current?.focus();
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const message = text.trim();
    if (!message) {
      navigate(`/agents/${agent.id}/chat`);
      return;
    }
    setBusy(true);
    try {
      await startLeadConversation(
        navigate,
        agent.id,
        message,
        t("agentArea.chat.newSessionTitle", { name: agent.name }),
      );
      setText("");
      inputRef.current?.blur();
    } catch {
      toast.error(t("frame.layout.header.leadAsk.failed"));
    } finally {
      setBusy(false);
    }
  };

  return (
    <form onSubmit={submit} className="relative hidden w-full max-w-md md:block">
      <AgentAvatar
        name={agent.name}
        lead
        size="xs"
        className="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2"
      />
      <Input
        ref={inputRef}
        value={text}
        disabled={busy}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Escape") inputRef.current?.blur();
        }}
        aria-label={t("frame.layout.header.leadAsk.label", { name: agent.name })}
        placeholder={t("frame.layout.header.leadAsk.placeholder")}
        className="pr-12 pl-10"
      />
      {!text && (
        <kbd className="pointer-events-none absolute top-1/2 right-2.5 -translate-y-1/2 rounded border border-border px-1.5 font-mono text-micro text-muted-foreground">
          {t("frame.layout.header.leadAsk.shortcut", { mod: shortcutModifier() })}
        </kbd>
      )}
    </form>
  );
}
