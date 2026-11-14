import { ArrowUpRight, CheckCircle2, Trash2 } from "lucide-react";
import type { SessionAction } from "@/api";
import { ReleaseCutProposalCard } from "@/components/chat/ReleaseCutProposalCard";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

interface SessionActionCardProps {
  action: SessionAction;
  /** Omitted for entity kinds with no detail view (comments, criteria, …). */
  onOpenTask?: (action: SessionAction) => void;
  className?: string;
}

/**
 * Renders one board record an agent touched, inline in the transcript. The chat
 * used to show only the agent's prose about what it did, so the task itself was
 * invisible from the conversation that created it.
 */
export function SessionActionCard({ action, onOpenTask, className }: SessionActionCardProps) {
  const { t } = useI18n();
  if (action.entity_kind === "release") {
    return <ReleaseCutProposalCard action={action} className={className} />;
  }
  const entityLabel = t(`chatArea.chat.actions.entity.${action.entity_kind}`);
  const verbLabel = t(`chatArea.chat.actions.verb.${action.verb}`);
  // A deleted record has no detail view left to open, and a success-green card
  // reads as "here it is" for something that is no longer there.
  const isDeletion = action.verb === "deleted";
  const canOpen = Boolean(onOpenTask) && action.entity_kind === "board_task" && !isDeletion;

  return (
    <Card
      className={cn(
        isDeletion ? "border-border bg-muted/40" : "border-success/40 bg-success/5",
        className,
      )}
    >
      <CardContent className="space-y-2 p-3">
        <div className="flex flex-wrap items-center gap-2">
          {isDeletion ? (
            <Trash2 className="h-4 w-4 shrink-0 text-muted-foreground" />
          ) : (
            <CheckCircle2 className="h-4 w-4 shrink-0 text-success" />
          )}
          <span className="text-xs font-medium text-muted-foreground">
            {entityLabel} {verbLabel}
          </span>
          {action.entity_key && (
            <Badge variant="outline" className="font-mono text-micro">
              {action.entity_key}
            </Badge>
          )}
        </div>

        {action.title && (
          <p
            className={cn(
              "text-sm font-medium leading-snug",
              isDeletion && "text-muted-foreground line-through",
            )}
          >
            {action.title}
          </p>
        )}

        <div className="flex flex-wrap items-center gap-2">
          {action.column && <Badge variant="secondary">{action.column}</Badge>}
          {action.priority && <Badge variant="outline">{action.priority}</Badge>}
          {canOpen && (
            <Button
              type="button"
              size="sm"
              variant="ghost"
              className="ml-auto h-7 px-2 text-xs"
              onClick={() => onOpenTask?.(action)}
            >
              {t("chatArea.chat.actions.openTask")}
              <ArrowUpRight className="ml-1 h-3.5 w-3.5" />
            </Button>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
