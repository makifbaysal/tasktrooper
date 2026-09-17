import {
  CheckCircle2,
  Circle,
  ExternalLink,
  Loader2,
  MinusCircle,
  XCircle,
  type LucideIcon,
} from "lucide-react";
import { useState } from "react";
import type { TaskPipeline, TaskPipelineJob } from "@/api";
import { ScrollArea } from "@/components/ui/scroll-area";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

interface PipelineStagesProps {
  pipeline: TaskPipeline;
}

function jobStatusIcon(status: TaskPipelineJob["status"]): { Icon: LucideIcon; className: string } {
  switch (status) {
    case "success":
      return { Icon: CheckCircle2, className: "text-success" };
    case "failed":
      return { Icon: XCircle, className: "text-destructive" };
    case "running":
      return { Icon: Loader2, className: "text-info animate-spin" };
    case "skipped":
      return { Icon: MinusCircle, className: "text-muted-foreground" };
    case "pending":
    default:
      return { Icon: Circle, className: "text-muted-foreground" };
  }
}

/**
 * PipelineStages renders one pipeline run as a GitLab-style horizontal stage
 * chain: a circle icon per job connected by a line, job name below. Clicking
 * a finished (success/failed) job toggles a log block with its command and
 * output.
 */
export function PipelineStages({ pipeline }: PipelineStagesProps) {
  const { t } = useI18n();
  const [expandedJobId, setExpandedJobId] = useState<string | null>(null);
  const jobs = pipeline.jobs ?? [];
  const expandedJob = jobs.find((job) => job.id === expandedJobId) ?? null;

  if (jobs.length === 0) {
    return <p className="text-xs text-muted-foreground">{t("boardArea.components.pipelineStages.noStages")}</p>;
  }

  return (
    <div className="space-y-2">
      <div className="flex items-start overflow-x-auto pb-1">
        {jobs.map((job, idx) => {
          const { Icon, className } = jobStatusIcon(job.status);
          const clickable = job.status === "success" || job.status === "failed";
          const expanded = expandedJobId === job.id;
          return (
            <div key={job.id} className="flex items-start">
              <button
                type="button"
                disabled={!clickable}
                onClick={() => setExpandedJobId((prev) => (prev === job.id ? null : job.id))}
                title={job.name}
                className={cn(
                  "flex w-20 shrink-0 flex-col items-center gap-1 rounded-md py-1 text-center transition-colors",
                  clickable ? "cursor-pointer hover:bg-muted/40" : "cursor-default",
                  expanded && "bg-muted/60",
                )}
              >
                <Icon className={cn("h-5 w-5", className)} />
                <span className="w-full truncate px-1 text-[11px] text-muted-foreground">{job.name}</span>
                {job.coverage_pct != null && (
                  <span className="w-full truncate px-1 text-[10px] text-muted-foreground/70">
                    {job.coverage_pct.toFixed(1)}%
                  </span>
                )}
              </button>
              {idx < jobs.length - 1 && <div className="mt-2.5 h-px w-6 shrink-0 bg-border" />}
            </div>
          );
        })}
      </div>

      {expandedJob && (
        <div className="space-y-2 rounded-lg border border-border bg-muted/10 p-3">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <p className="text-xs font-medium text-muted-foreground">{expandedJob.name}</p>
            {expandedJob.run_url && (
              <a
                href={expandedJob.run_url}
                target="_blank"
                rel="noreferrer"
                className="inline-flex items-center gap-1 text-xs text-primary hover:underline"
              >
                <ExternalLink className="h-3 w-3" />
                {t("boardArea.components.pipelineStages.openOnGitHub")}
              </a>
            )}
          </div>
          <ScrollArea className="h-64">
            <pre className="overflow-x-auto whitespace-pre font-mono text-xs leading-relaxed">
              <span className="text-muted-foreground">$ {expandedJob.command}</span>
              {"\n"}
              {expandedJob.output || t("boardArea.components.pipelineStages.noOutput")}
            </pre>
          </ScrollArea>
        </div>
      )}
    </div>
  );
}
