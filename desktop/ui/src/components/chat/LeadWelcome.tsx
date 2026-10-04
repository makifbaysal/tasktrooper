import { Fragment, useRef, useState, type FormEvent, type KeyboardEvent } from "react";
import { ArrowRight, ArrowUp } from "lucide-react";
import type { Agent } from "@/api";
import { AgentAvatar } from "@/components/agent/AgentAvatar";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";

interface LeadWelcomeProps {
  agent: Agent;
  onSubmit: (message: string) => void | Promise<void>;
  busy?: boolean;
}

const FLOW_STEPS = ["you", "lead", "analysis", "build", "qa", "acceptance", "approval"] as const;

export function LeadWelcome({ agent, onSubmit, busy = false }: LeadWelcomeProps) {
  const { t } = useI18n();
  const [value, setValue] = useState("");
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const canSend = value.trim() !== "" && !busy;

  const submit = () => {
    const text = value.trim();
    if (!text || busy) return;
    void onSubmit(text);
  };

  const handleSubmit = (event: FormEvent) => {
    event.preventDefault();
    submit();
  };

  const handleKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key !== "Enter" || event.shiftKey || event.nativeEvent.isComposing) return;
    event.preventDefault();
    submit();
  };

  const prefill = (message: string) => {
    setValue(message);
    const el = textareaRef.current;
    if (!el) return;
    el.focus();
    requestAnimationFrame(() => el.setSelectionRange(message.length, message.length));
  };

  const suggestions = [
    { key: "board", send: true },
    { key: "feature", send: false },
    { key: "bug", send: false },
    { key: "projects", send: true },
  ] as const;

  return (
    <div className="flex-1 overflow-y-auto">
      <div className="mx-auto flex w-full max-w-2xl flex-col gap-10 px-6 py-12">
        <div className="flex flex-col items-center gap-3 text-center">
          <AgentAvatar name={agent.name} lead size="xl" />
          <p className="text-heading font-semibold">{agent.name}</p>
          <p className="max-w-md text-muted-foreground">{t("agentArea.chat.lead.description")}</p>
          <h2 className="mt-2 text-display font-semibold tracking-tight">
            {t("agentArea.chat.lead.welcome.title")}
          </h2>
        </div>

        <Card className="p-3">
          <form onSubmit={handleSubmit} className="flex flex-col gap-2">
            <Textarea
              ref={textareaRef}
              value={value}
              onChange={(e) => setValue(e.target.value)}
              onKeyDown={handleKeyDown}
              rows={3}
              autoFocus
              aria-label={t("agentArea.chat.lead.welcome.label", { name: agent.name })}
              placeholder={t("agentArea.chat.lead.welcome.placeholder")}
              className="border-0 bg-transparent shadow-none focus-visible:ring-0"
            />
            <div className="flex items-center justify-between gap-3">
              <p className="text-caption text-muted-foreground">{t("agentArea.chat.lead.welcome.hint")}</p>
              <Button type="submit" size="icon" disabled={!canSend} aria-label={t("agentArea.chat.lead.welcome.send")}>
                <ArrowUp className="h-4 w-4" />
              </Button>
            </div>
          </form>
        </Card>

        <div className="flex flex-wrap justify-center gap-2">
          {suggestions.map(({ key, send }) => {
            const message = t(`agentArea.chat.lead.welcome.suggestions.${key}.message`);
            return (
              <Button
                key={key}
                type="button"
                variant="outline"
                size="sm"
                className="rounded-full"
                disabled={busy && send}
                onClick={() => (send ? void onSubmit(message) : prefill(message))}
              >
                {t(`agentArea.chat.lead.welcome.suggestions.${key}.label`)}
              </Button>
            );
          })}
        </div>

        <div className="flex flex-col items-center gap-3 text-center">
          <h3 className="text-heading font-semibold">{t("agentArea.chat.lead.welcome.flow.title")}</h3>
          <ol className="flex flex-wrap items-center justify-center gap-2">
            {FLOW_STEPS.map((step, index) => (
              <Fragment key={step}>
                {index > 0 && <ArrowRight aria-hidden className="h-3.5 w-3.5 text-muted-foreground" />}
                <li
                  className={
                    step === "lead"
                      ? "flex items-center gap-1.5 rounded-full border border-accent-warm/70 bg-accent-warm/15 py-1 pl-1.5 pr-3 text-caption font-medium text-foreground"
                      : "rounded-full border border-border bg-card px-3 py-1 text-caption"
                  }
                >
                  {step === "lead" ? (
                    <>
                      <AgentAvatar name={agent.name} lead size="xs" />
                      {agent.name}
                    </>
                  ) : (
                    t(`agentArea.chat.lead.welcome.flow.${step}`)
                  )}
                </li>
              </Fragment>
            ))}
          </ol>
          <p className="text-caption text-muted-foreground">{t("agentArea.chat.lead.welcome.flow.caption")}</p>
        </div>
      </div>
    </div>
  );
}
