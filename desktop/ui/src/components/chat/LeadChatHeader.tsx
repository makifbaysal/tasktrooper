import type { ReactNode } from "react";
import type { Agent } from "@/api";
import { AgentAvatar } from "@/components/agent/AgentAvatar";
import { useI18n } from "@/hooks/useI18n";

const MAX_TEAM_AVATARS = 6;

interface LeadChatHeaderProps {
  agent: Agent;
  team: Agent[];
  actions: ReactNode;
}

export function LeadChatHeader({ agent, team, actions }: LeadChatHeaderProps) {
  const { t } = useI18n();
  const shown = team.slice(0, MAX_TEAM_AVATARS);
  const hidden = team.length - shown.length;

  return (
    <div className="flex flex-wrap items-center gap-4">
      <AgentAvatar name={agent.name} lead size="lg" />
      <div className="min-w-0 flex-1">
        <h1 className="text-title font-semibold">{agent.name}</h1>
        <p className="text-caption text-muted-foreground">{t("agentArea.chat.lead.description")}</p>
      </div>
      {team.length > 0 && (
        <div className="flex items-center gap-2">
          <div className="flex items-center">
            {shown.map((member, index) => (
              <AgentAvatar
                key={member.id}
                name={member.name}
                size="sm"
                className={index === 0 ? "ring-2 ring-background" : "-ml-1.5 ring-2 ring-background"}
              />
            ))}
            {hidden > 0 && (
              <span className="-ml-1.5 flex h-6 min-w-6 items-center justify-center rounded-full bg-muted px-1 text-micro font-semibold text-muted-foreground ring-2 ring-background">
                +{hidden}
              </span>
            )}
          </div>
          <span className="text-caption text-muted-foreground">
            {t("agentArea.chat.lead.teamCount", { count: team.length })}
          </span>
        </div>
      )}
      {actions}
    </div>
  );
}
