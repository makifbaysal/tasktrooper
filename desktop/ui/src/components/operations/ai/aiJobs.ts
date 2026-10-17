import type { AIJob, AIJobItem } from "@/api";
import { entityPath } from "@/components/operations/console/opsEntities";
import { definitionPath } from "@/components/operations/errorDefinitions/errorDefinitions";

export const AI_JOB_POLL_MS = 3_000;

export function isJobRunning(job: AIJob | null | undefined): boolean {
  return job?.status === "running";
}

/** Where one thing a job stored is reviewed: a definition on Definitions, a
 * binding on its component's Signals, a rule (saved off) on Alerts. */
export function jobItemHref(item: AIJobItem, job: Pick<AIJob, "repository_id" | "component_id">): string | undefined {
  switch (item.kind) {
    case "error_definition":
      return definitionPath({ id: item.id, repository_id: job.repository_id });
    case "signal_binding":
      return job.component_id ? entityPath(job.component_id, "signals") : undefined;
    case "alert_rule":
      return `/operations/alerts?rule=${encodeURIComponent(item.id)}`;
    default:
      return undefined;
  }
}

export function jobItems(job: AIJob | null | undefined): AIJobItem[] {
  return job?.created ?? [];
}

export function countItems(job: AIJob | null | undefined, kind: AIJobItem["kind"]): number {
  return jobItems(job).filter((i) => i.kind === kind).length;
}
