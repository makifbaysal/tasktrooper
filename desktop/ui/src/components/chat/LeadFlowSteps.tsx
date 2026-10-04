import type { LucideIcon } from "lucide-react";
import { CheckCheck, Code2, FileSearch, FlaskConical, Rocket, User, UserCheck } from "lucide-react";
import type { Agent } from "@/api";
import { AgentAvatar } from "@/components/agent/AgentAvatar";
import { Card } from "@/components/ui/card";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

type StepKey = "you" | "lead" | "analysis" | "planApproval" | "build" | "qa" | "acceptance" | "approval" | "deploy";
/** Who acts at a step: the human (the request and the two approval stages), the lead, or the rest of the team. */
type Actor = "human" | "lead" | "team";

interface Step {
  key: StepKey;
  actor: Actor;
  icon?: LucideIcon;
}

// analiz_review and human_uat are the board's approval stages: the work waits
// there for the human, so they carry the human treatment, not the team's.
const STEPS: Step[] = [
  { key: "you", actor: "human", icon: User },
  { key: "lead", actor: "lead" },
  { key: "analysis", actor: "team", icon: FileSearch },
  { key: "planApproval", actor: "human", icon: UserCheck },
  { key: "build", actor: "team", icon: Code2 },
  { key: "qa", actor: "team", icon: FlaskConical },
  { key: "acceptance", actor: "lead", icon: CheckCheck },
  { key: "approval", actor: "human", icon: UserCheck },
  { key: "deploy", actor: "team", icon: Rocket },
];

const tileByActor: Record<Actor, string> = {
  // Opaque on purpose: a translucent tint lets the connector line show through the tile.
  human: "border-info/60 bg-[color-mix(in_oklch,var(--info)_12%,var(--card))] text-info",
  lead: "border-accent-warm/60 text-accent-warm",
  team: "border-border text-muted-foreground",
};

const detailByActor: Record<Actor, string> = {
  human: "text-info",
  lead: "text-muted-foreground",
  team: "text-muted-foreground",
};

interface LeadFlowStepsProps {
  agent: Agent;
}

export function LeadFlowSteps({ agent }: LeadFlowStepsProps) {
  const { t } = useI18n();
  const base = "agentArea.chat.lead.welcome.flow";

  return (
    <Card className="flex flex-col gap-3 p-4">
      <h3 className="text-heading font-semibold">{t(`${base}.title`)}</h3>
      {/* Vertical padding: overflow-x forces overflow-y too, which would clip the lead avatar's ring. */}
      <div className="-mx-1 overflow-x-auto px-1 py-1.5">
        <ol className="grid min-w-[640px] grid-cols-9">
          {STEPS.map(({ key, actor, icon: Icon }, index) => (
            <li key={key} className="relative flex flex-col items-center gap-2 text-center">
              {index < STEPS.length - 1 && (
                <span aria-hidden className="absolute top-4 left-1/2 z-0 h-px w-full bg-border" />
              )}
              {key === "lead" ? (
                <AgentAvatar name={agent.name} lead size="md" className="relative z-10 ring-offset-card" />
              ) : (
                <span
                  className={cn(
                    "relative z-10 flex h-8 w-8 items-center justify-center rounded-lg border bg-card",
                    tileByActor[actor],
                  )}
                >
                  {Icon && <Icon aria-hidden className="h-4 w-4" />}
                </span>
              )}
              <span className="flex flex-col whitespace-nowrap">
                <span className={cn("text-caption font-medium", actor !== "team" && "font-semibold")}>
                  {t(`${base}.steps.${key}.label`)}
                </span>
                <span className={cn("text-micro", detailByActor[actor])}>{t(`${base}.steps.${key}.detail`)}</span>
              </span>
            </li>
          ))}
        </ol>
      </div>
      <p className="text-caption text-muted-foreground">{t(`${base}.caption`)}</p>
    </Card>
  );
}
