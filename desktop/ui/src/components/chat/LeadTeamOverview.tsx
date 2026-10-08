import { Link } from "react-router-dom";
import type { Agent } from "@/api";
import { AgentAvatar } from "@/components/agent/AgentAvatar";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { useI18n } from "@/hooks/useI18n";

type GroupKey = "plan" | "design" | "build" | "review" | "ship" | "custom";

// Keyed by catalog slug, not by name: a renamed built-in agent stays in its
// stage, and an agent that does two jobs (the architect plans and reviews)
// shows in both.
const GROUPS: { key: Exclude<GroupKey, "custom">; slugs: string[] }[] = [
  { key: "plan", slugs: ["product-manager", "system-architect"] },
  { key: "design", slugs: ["ui-designer"] },
  { key: "build", slugs: ["backend-developer", "frontend-developer", "mobile-developer", "data-scientist", "game-developer"] },
  { key: "review", slugs: ["system-architect", "security-agent"] },
  { key: "ship", slugs: ["qa-agent", "release-engineer"] },
];

const KNOWN_SLUGS = new Set(GROUPS.flatMap((g) => g.slugs));

function bySlugOrder(slugs: string[]) {
  return (a: Agent, b: Agent) => slugs.indexOf(a.catalog_slug ?? "") - slugs.indexOf(b.catalog_slug ?? "");
}

interface LeadTeamOverviewProps {
  agents: Agent[];
}

/** Who works at each stage of the flow, read from the agents this workspace actually has. */
export function LeadTeamOverview({ agents }: LeadTeamOverviewProps) {
  const { t } = useI18n();
  const base = "agentArea.chat.lead.welcome.team";
  const enabled = agents.filter((a) => a.enabled);

  const groups: { key: GroupKey; members: Agent[] }[] = GROUPS.map(({ key, slugs }) => ({
    key,
    members: enabled.filter((a) => a.catalog_slug && slugs.includes(a.catalog_slug)).sort(bySlugOrder(slugs)),
  }));
  groups.push({
    key: "custom",
    members: enabled.filter((a) => !a.catalog_slug || !KNOWN_SLUGS.has(a.catalog_slug)),
  });
  const shown = groups.filter((g) => g.members.length > 0);
  if (shown.length === 0) return null;

  return (
    <Card className="flex flex-col gap-3 p-4">
      <h3 className="text-heading font-semibold">{t(`${base}.title`)}</h3>
      <dl className="flex flex-col gap-3">
        {shown.map(({ key, members }) => (
          <div key={key} className="grid gap-1.5 sm:grid-cols-[9rem_1fr] sm:items-start sm:gap-3">
            <dt className="flex flex-col">
              <span className="text-caption font-semibold">{t(`${base}.groups.${key}.label`)}</span>
              <span className="text-micro text-muted-foreground">{t(`${base}.groups.${key}.detail`)}</span>
            </dt>
            <dd className="flex flex-wrap gap-1.5">
              {members.map((agent) => (
                <Button key={agent.id} variant="outline" size="sm" className="h-7 gap-1.5 rounded-full px-2" asChild>
                  <Link to={`/agents/${agent.id}/chat`}>
                    <AgentAvatar name={agent.name} lead={agent.catalog_slug === "product-manager"} size="xs" />
                    <span className="text-caption">{agent.name}</span>
                  </Link>
                </Button>
              ))}
            </dd>
          </div>
        ))}
      </dl>
    </Card>
  );
}
