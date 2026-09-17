import { useCallback, useEffect, useState } from "react";
import { api, type SessionStep } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { usePolling } from "@/hooks/usePolling";
import { cn } from "@/lib/utils";

interface TaskRunStepsProps {
  runId: string;
}

export function TaskRunSteps({ runId }: TaskRunStepsProps) {
  const { t } = useI18n();
  const [steps, setSteps] = useState<SessionStep[]>([]);
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    try {
      const data = await api.runSteps(runId);
      setSteps(data.steps ?? []);
    } catch {
      setSteps([]);
    } finally {
      setLoading(false);
    }
  }, [runId]);

  useEffect(() => {
    setLoading(true);
  }, [runId]);
  // Visibility-gated: a hidden tab stops polling entirely and refreshes once
  // when it comes back to the foreground.
  usePolling(load, 2000, true);

  if (loading) {
    return (
      <div className="space-y-2">
        <Skeleton className="h-6 w-full" />
        <Skeleton className="h-6 w-3/4" />
      </div>
    );
  }

  if (steps.length === 0) {
    return <p className="text-xs text-muted-foreground">{t("boardArea.components.taskRunSteps.empty")}</p>;
  }

  return (
    <div className="space-y-1.5">
      {steps.map((s, index) => (
        <div
          key={s.id}
          className={cn(
            "flex items-center gap-2 rounded-md px-compact py-1.5 text-xs",
            index === steps.length - 1 ? "bg-primary/5" : "bg-muted/30",
          )}
        >
          <span className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-muted text-[10px] font-medium">
            {index + 1}
          </span>
          <Badge variant="outline" className="font-mono text-[10px]">
            {s.step_type}
          </Badge>
        </div>
      ))}
    </div>
  );
}
