import { Bot } from "lucide-react";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useI18n } from "@/hooks/useI18n";
import { useWorkspaceOutlet } from "@/hooks/useWorkspaceOutlet";

// Radix will not take "" as an item value, so "nobody" needs a sentinel — the
// same one the rest of the board already uses for an empty select.
const NONE = "none";

interface AgentOption {
  id: string;
  name: string;
}

interface TaskAssigneeFieldsProps {
  /** The agents this board may hand the card to. Empty hides the picker. */
  agents: AgentOption[];
  agentValue?: string | null;
  /** "" unassigns. */
  onAgentChange: (agentId: string) => void;
  /**
   * Name for an `agentValue` that is not in `agents` — an agent that has since
   * left the board still has to be readable in the trigger rather than showing
   * a blank one.
   */
  agentFallbackName?: string | null;
  disabled?: boolean;
}

/** The agent that works a board task. */
export function TaskAssigneeFields({
  agents,
  agentValue,
  onAgentChange,
  agentFallbackName,
  disabled,
}: TaskAssigneeFieldsProps) {
  const { t } = useI18n();
  const unassignable = useWorkspaceOutlet()?.unassignableAgentIds;
  const options = unassignable?.length ? agents.filter((a) => !unassignable.includes(a.id)) : agents;
  if (options.length === 0) return null;

  const agentMissing = Boolean(agentValue) && !options.some((a) => a.id === agentValue);

  return (
    <div className="space-y-1.5">
      <Label className="flex items-center gap-2 text-xs text-muted-foreground">
        <Bot className="h-3.5 w-3.5" />
        {t("boardArea.components.memberAssignee.agentLabel")}
      </Label>
      <Select
        value={agentValue || NONE}
        onValueChange={(value) => onAgentChange(value === NONE ? "" : value)}
        disabled={disabled}
      >
        <SelectTrigger>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={NONE}>{t("boardArea.components.memberAssignee.unassigned")}</SelectItem>
          {agentMissing && agentValue && (
            <SelectItem value={agentValue}>
              {agentFallbackName || t("boardArea.components.memberAssignee.unknown")}
            </SelectItem>
          )}
          {options.map((agent) => (
            <SelectItem key={agent.id} value={agent.id}>
              {agent.name}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
