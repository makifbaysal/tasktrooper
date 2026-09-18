import { useMemo } from "react";
import { type Agent, type BoardColumn, type BoardTask } from "@/api";
import { ActivityFeedHeader, ActivityFeedItem } from "@/components/workspace/ActivityFeedItem";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Skeleton } from "@/components/ui/skeleton";
import { Activity } from "lucide-react";
import { useActivity } from "@/hooks/useActivity";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

interface ActivityFeedProps {
  className?: string;
  agents?: Agent[];
  tasks?: BoardTask[];
  columns?: BoardColumn[];
}

export function ActivityFeed({ className, agents = [], tasks = [], columns = [] }: ActivityFeedProps) {
  const { t } = useI18n();
  const { items, loading } = useActivity(40, 2000);

  const agentNameById = useMemo(() => {
    const map = new Map<string, string>();
    for (const a of agents) map.set(a.id, a.name);
    return map;
  }, [agents]);

  const taskLabelById = useMemo(() => {
    const map = new Map<string, string>();
    for (const t of tasks) map.set(t.id, `${t.key} · ${t.title}`);
    return map;
  }, [tasks]);

  if (loading) {
    return <Skeleton className={cn("h-64 rounded-xl", className)} />;
  }

  return (
    <Card className={cn("flex min-h-0 flex-col overflow-hidden", className)}>
      <ActivityFeedHeader live />
      <ScrollArea className="min-h-0 flex-1">
        <div className="space-y-2 p-3">
          {items.length === 0 ? (
            <EmptyState
              icon={Activity}
              title={t("chatArea.workspace.activityFeed.emptyTitle")}
              description={t("chatArea.workspace.activityFeed.emptyDescription")}
              className="py-10"
            />
          ) : (
            items.map((item) => (
              <ActivityFeedItem
                key={`${item.kind}-${item.id}`}
                item={item}
                agentName={item.agent_id ? agentNameById.get(item.agent_id) : undefined}
                taskLabel={item.task_id ? taskLabelById.get(item.task_id) : undefined}
                columns={columns}
                agentNameById={agentNameById}
              />
            ))
          )}
        </div>
      </ScrollArea>
    </Card>
  );
}
