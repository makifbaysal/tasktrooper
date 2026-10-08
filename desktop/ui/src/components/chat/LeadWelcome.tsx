import { useRef, useState, type FormEvent } from "react";
import { ArrowUp } from "lucide-react";
import type { Agent, MessageMention } from "@/api";
import { AgentAvatar } from "@/components/agent/AgentAvatar";
import { LeadFlowSteps } from "@/components/chat/LeadFlowSteps";
import { LeadTeamOverview } from "@/components/chat/LeadTeamOverview";
import type { MentionOption } from "@/components/chat/MentionMenu";
import { MentionTextarea } from "@/components/chat/MentionTextarea";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { useI18n } from "@/hooks/useI18n";

interface LeadWelcomeProps {
  agent: Agent;
  /** `mentions` are the @-picked entities whose @Name is still in the message. */
  onSubmit: (message: string, mentions: MessageMention[]) => void | Promise<void>;
  busy?: boolean;
  mentionOptions?: MentionOption[];
  /** Every agent of the workspace, grouped by the stage of the flow it works. */
  team?: Agent[];
}


export function LeadWelcome({ agent, onSubmit, busy = false, mentionOptions = [], team = [] }: LeadWelcomeProps) {
  const { t } = useI18n();
  const [value, setValue] = useState("");
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const pickedRef = useRef<MentionOption[]>([]);
  const canSend = value.trim() !== "" && !busy;

  const submit = () => {
    const text = value.trim();
    if (!text || busy) return;
    const fold = text.toLocaleLowerCase("tr");
    const mentions = pickedRef.current
      .filter((m) => fold.includes(`@${m.name.toLocaleLowerCase("tr")}`))
      .map(({ kind, id, name }) => ({ kind, id, name }));
    void onSubmit(text, mentions);
  };

  const handleSubmit = (event: FormEvent) => {
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
      {/* min-h-full + justify-center: centred when it fits, scrolls only when the window is too short. */}
      <div className="mx-auto flex min-h-full w-full max-w-3xl flex-col justify-center gap-6 px-6 py-8">
        <div className="flex flex-col items-center gap-2 text-center">
          <AgentAvatar name={agent.name} lead size="lg" />
          <p className="text-heading font-semibold">{agent.name}</p>
          <p className="max-w-xl text-caption text-muted-foreground">{t("agentArea.chat.lead.description")}</p>
          <h2 className="mt-1 text-display font-semibold tracking-tight">
            {t("agentArea.chat.lead.welcome.title")}
          </h2>
        </div>

        <div className="flex flex-col gap-3">
          <Card className="p-3">
            <form onSubmit={handleSubmit} className="flex flex-col gap-2">
              <MentionTextarea
                ref={textareaRef}
                value={value}
                onChange={setValue}
                onSubmit={submit}
                mentionOptions={mentionOptions}
                onMentionSelect={(option) => {
                  if (!pickedRef.current.some((m) => m.kind === option.kind && m.id === option.id)) {
                    pickedRef.current = [...pickedRef.current, option];
                  }
                }}
                rows={2}
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
                  onClick={() => (send ? void onSubmit(message, []) : prefill(message))}
                >
                  {t(`agentArea.chat.lead.welcome.suggestions.${key}.label`)}
                </Button>
              );
            })}
          </div>
        </div>

        <LeadFlowSteps agent={agent} />
        <LeadTeamOverview agents={team} />
      </div>
    </div>
  );
}
