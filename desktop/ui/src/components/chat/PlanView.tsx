import { AlertCircle, CheckCircle2, Circle, Loader2, Target, XCircle } from "lucide-react";
import type { OrchestrationPlan } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { groupTasksByWaves, planStatusLabel, taskStatusVariant } from "@/lib/planUtils";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

interface PlanViewProps {
  plan: OrchestrationPlan;
  agentNameMap?: Record<string, string>;
}

function StatusIcon({ status, className }: { status: string; className?: string }) {
  const variant = taskStatusVariant(status);
  const size = cn("h-4 w-4", className);
  if (variant === "success") return <CheckCircle2 className={cn(size, "text-success")} />;
  if (variant === "destructive") return <XCircle className={cn(size, "text-destructive")} />;
  // Incomplete shares the warning colour with running but not the spinner: it
  // is finished, and the run moved on. A spinner here claimed an agent was
  // still working on a subtask that had already returned — and at plan level it
  // would claim the whole run was still going after it had settled.
  if (status.toLowerCase() === "incomplete") return <AlertCircle className={cn(size, "text-warning")} />;
  if (variant === "warning") return <Loader2 className={cn(size, "animate-spin text-warning")} />;
  return <Circle className={cn(size, "text-muted-foreground")} />;
}

function resolveAgentName(agentId: string, agentNameMap?: Record<string, string>): string {
  return agentNameMap?.[agentId] ?? agentId;
}

export function PlanView({ plan, agentNameMap }: PlanViewProps) {
  const { t } = useI18n();
  const waves = groupTasksByWaves(plan.tasks ?? []);

  return (
    <div className="space-y-4">
      {(plan.purpose || plan.goal) && (
        <Card className="border-border/60">
          <CardHeader className="py-3 px-4">
            <CardTitle className="flex items-center gap-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">
              <Target className="h-3.5 w-3.5" />
              {t("chatArea.chat.plan.goalGuidance")}
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-3 px-4 pb-4 pt-0">
            {plan.purpose && (
              <div>
                <p className="text-micro font-medium uppercase tracking-wide text-muted-foreground">{t("chatArea.chat.plan.purpose")}</p>
                <p className="mt-1 text-sm">{plan.purpose}</p>
              </div>
            )}
            {plan.goal && (
              <div>
                <p className="text-micro font-medium uppercase tracking-wide text-muted-foreground">{t("chatArea.chat.plan.goal")}</p>
                <p className="mt-1 text-sm">{plan.goal}</p>
              </div>
            )}
          </CardContent>
        </Card>
      )}

      <div className="flex items-start justify-between gap-2">
        <p className="text-sm text-muted-foreground">{plan.summary}</p>
        {/* The plan's own verdict. "incomplete" shares the warning colour with a
            running plan, so the icon carries the difference: an alert, never a
            spinner — this plan is over, it just was not confirmed. */}
        <Badge variant={taskStatusVariant(plan.status)} className="shrink-0 gap-1.5">
          <StatusIcon status={plan.status} className="h-3 w-3" />
          {planStatusLabel(plan.status)}
        </Badge>
      </div>

      {waves.map((wave, waveIndex) => (
        <Card key={waveIndex} className="border-border/60">
          <CardHeader className="py-3 px-4">
            <CardTitle className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
              {t("chatArea.chat.plan.wave", { number: waveIndex + 1 })}
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-2 px-4 pb-4 pt-0">
            {wave.map((task) => (
              <div
                key={task.id}
                className={cn(
                  "rounded-lg border border-border/60 p-3 transition-colors",
                  taskStatusVariant(task.status) === "warning" && "border-warning/30 bg-warning/5",
                  taskStatusVariant(task.status) === "success" && "border-success/30 bg-success/5",
                  taskStatusVariant(task.status) === "destructive" && "border-destructive/30 bg-destructive/5",
                )}
              >
                <div className="flex items-start gap-2">
                  <StatusIcon status={task.status} />
                  <div className="min-w-0 flex-1 space-y-1">
                    <div className="flex items-center justify-between gap-2">
                      <span className="text-sm font-medium">{task.title}</span>
                      <Badge variant={taskStatusVariant(task.status)} className="shrink-0 text-micro">
                        {task.status}
                      </Badge>
                    </div>
                    {task.description && <p className="text-xs text-muted-foreground">{task.description}</p>}
                    <div className="flex flex-wrap gap-2 text-micro text-muted-foreground">
                      {task.agent_id && <span>{t("chatArea.chat.plan.agent", { name: resolveAgentName(task.agent_id, agentNameMap) })}</span>}
                      {task.skill_ids?.length > 0 && <span>{t("chatArea.chat.plan.skills", { count: task.skill_ids.length })}</span>}
                      {task.depends_on?.length > 0 && <span>{t("chatArea.chat.plan.dependencies", { count: task.depends_on.length })}</span>}
                    </div>
                    {(task.tool_names?.length ?? 0) > 0 && (
                      <div className="flex flex-wrap gap-1 pt-1">
                        {task.tool_names!.map((name) => (
                          <Badge key={name} variant="outline" className="font-mono text-micro">
                            {name}
                          </Badge>
                        ))}
                      </div>
                    )}
                    {task.result && (
                      <pre className="mt-2 max-h-24 overflow-auto rounded bg-muted p-2 text-micro">{task.result}</pre>
                    )}
                    {task.error && (
                      <pre className="mt-2 max-h-24 overflow-auto rounded bg-destructive/10 p-2 text-micro text-destructive">
                        {task.error}
                      </pre>
                    )}
                  </div>
                </div>
              </div>
            ))}
          </CardContent>
        </Card>
      ))}

      {/* A verification the run did not pass is what settles the plan as
          "incomplete", so the card below is toned to match it: warning, not the
          old destructive red. Red said the run broke — it did not, it produced
          the work above and nothing confirmed it, which is a different thing to
          act on. */}
      {plan.verification && (
        <Card
          className={cn(
            "border-border/60",
            plan.verification.passed ? "border-success/40 bg-success/5" : "border-warning/40 bg-warning/5",
          )}
        >
          <CardHeader className="py-3 px-4">
            <CardTitle className="flex items-center gap-2 text-xs font-medium uppercase tracking-wide">
              {plan.verification.passed ? (
                <CheckCircle2 className="h-3.5 w-3.5 text-success" />
              ) : (
                <AlertCircle className="h-3.5 w-3.5 text-warning" />
              )}
              {t("chatArea.chat.plan.verification")}
              <Badge variant={plan.verification.passed ? "success" : "warning"} className="ml-auto text-micro">
                {plan.verification.passed ? t("chatArea.chat.plan.passed") : t("chatArea.chat.plan.failed")}
              </Badge>
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-2 px-4 pb-4 pt-0">
            {plan.verification.summary && (
              <p className="text-sm text-muted-foreground">{plan.verification.summary}</p>
            )}
            {(plan.verification.issues?.length ?? 0) > 0 && (
              <ul className="space-y-1">
                {plan.verification.issues.map((issue, index) => (
                  <li key={index} className="flex items-start gap-2 text-xs text-foreground">
                    <AlertCircle className="mt-0.5 h-3 w-3 shrink-0 text-warning" />
                    <span>{issue}</span>
                  </li>
                ))}
              </ul>
            )}
          </CardContent>
        </Card>
      )}
    </div>
  );
}
