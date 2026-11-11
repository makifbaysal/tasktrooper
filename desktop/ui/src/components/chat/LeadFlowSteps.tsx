import type { LucideIcon } from "lucide-react";
import { CheckCheck, Code2, FileSearch, FlaskConical, Palette, Rocket, ShieldCheck, User, UserCheck } from "lucide-react";
import type { Agent } from "@/api";
import { AgentAvatar } from "@/components/agent/AgentAvatar";
import { Card } from "@/components/ui/card";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

type StepKey =
  | "you"
  | "lead"
  | "analysis"
  | "planApproval"
  | "design"
  | "build"
  | "review"
  | "qa"
  | "acceptance"
  | "approval"
  | "deploy";
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
  { key: "design", actor: "team", icon: Palette },
  { key: "build", actor: "team", icon: Code2 },
  { key: "review", actor: "team", icon: ShieldCheck },
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

// From sm up the flow snakes: six steps run left to right, a curve on the
// right edge turns down, and the last five come back right to left. The grid
// has 30 tracks so both rows span the full width — six steps of five tracks
// above, five of six below — and their ends line up. Literal class names, so
// Tailwind sees every one of them.
const ROW_LENGTH = 6;
const SNAKE_CELL = [
  "sm:col-span-5 sm:col-start-1 sm:row-start-1",
  "sm:col-span-5 sm:col-start-6 sm:row-start-1",
  "sm:col-span-5 sm:col-start-11 sm:row-start-1",
  "sm:col-span-5 sm:col-start-[16] sm:row-start-1",
  "sm:col-span-5 sm:col-start-[21] sm:row-start-1",
  "sm:col-span-5 sm:col-start-[26] sm:row-start-1",
  "sm:col-span-6 sm:col-start-[25] sm:row-start-2",
  "sm:col-span-6 sm:col-start-[19] sm:row-start-2",
  "sm:col-span-6 sm:col-start-[13] sm:row-start-2",
  "sm:col-span-6 sm:col-start-7 sm:row-start-2",
  "sm:col-span-6 sm:col-start-1 sm:row-start-2",
];

function Connector({ index }: { index: number }) {
  if (index === STEPS.length - 1) return null;
  return (
    <>
      {/* Below sm the steps stack: a line down to the next tile. */}
      <span aria-hidden className="absolute top-8 -bottom-3 left-4 z-0 w-px bg-border sm:hidden" />
      {index < ROW_LENGTH - 1 && (
        <span aria-hidden className="absolute top-4 left-1/2 z-0 hidden h-px w-full bg-border sm:block" />
      )}
      {index === ROW_LENGTH - 1 && (
        <span
          aria-hidden
          className="absolute top-4 left-1/2 z-0 hidden h-[calc(100%+1.75rem)] w-[calc(50%+0.5rem)] rounded-r-2xl border border-l-0 border-border sm:block"
        />
      )}
      {index >= ROW_LENGTH && (
        <span aria-hidden className="absolute top-4 right-1/2 z-0 hidden h-px w-full bg-border sm:block" />
      )}
    </>
  );
}

interface LeadFlowStepsProps {
  agent: Agent;
}

export function LeadFlowSteps({ agent }: LeadFlowStepsProps) {
  const { t } = useI18n();
  const base = "agentArea.chat.lead.welcome.flow";

  return (
    <Card className="flex flex-col gap-4 p-4">
      <h3 className="text-heading font-semibold">{t(`${base}.title`)}</h3>
      <ol className="flex flex-col gap-3 py-1 sm:grid sm:grid-cols-[repeat(30,minmax(0,1fr))] sm:gap-x-0 sm:gap-y-7 sm:pr-3">
        {STEPS.map(({ key, actor, icon: Icon }, index) => (
          <li
            key={key}
            data-testid={`flow-step-${key}`}
            className={cn(
              "relative flex items-center gap-3 sm:flex-col sm:items-center sm:gap-2 sm:px-1 sm:text-center",
              SNAKE_CELL[index],
            )}
          >
            <Connector index={index} />
            {key === "lead" ? (
              <AgentAvatar name={agent.name} lead size="md" className="relative z-10 shrink-0 ring-offset-card" />
            ) : (
              <span
                className={cn(
                  "relative z-10 flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border bg-card",
                  tileByActor[actor],
                )}
              >
                {Icon && <Icon aria-hidden className="h-4 w-4" />}
              </span>
            )}
            <span className="flex min-w-0 flex-col">
              <span className={cn("text-caption font-medium", actor !== "team" && "font-semibold")}>
                <span className="mr-1 tabular-nums text-muted-foreground">{index + 1}.</span>
                {t(`${base}.steps.${key}.label`)}
              </span>
              <span className={cn("text-micro", detailByActor[actor])}>{t(`${base}.steps.${key}.detail`)}</span>
            </span>
          </li>
        ))}
      </ol>
    </Card>
  );
}
