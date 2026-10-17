import { CheckCircle2, MessageSquare, Sparkles, X, XCircle } from "lucide-react";
import { useState, type ReactNode } from "react";
import { Link, useNavigate } from "react-router-dom";
import { api, type AIJob, type AIJobRefusal } from "@/api";
import { MarkdownContent } from "@/components/markdown/MarkdownContent";
import { isJobRunning, jobItemHref, jobItems } from "@/components/operations/ai/aiJobs";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Elapsed } from "@/components/ui/elapsed";
import { Notice } from "@/components/ui/notice";
import { Spinner } from "@/components/ui/spinner";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

interface AIJobCardProps {
  job: AIJob | null;
  refusal: AIJobRefusal | null;
  startError: string | null;
  pollError: string | null;
  onDismiss: () => void;
  /** Buttons for a finished job, beside Dismiss (review the suggestions, say). */
  actions?: ReactNode;
  className?: string;
}

/** An AI job's progress: running (and for how long), then what the release
 * engineer stored, each linked to where it is reviewed, and its last reply;
 * or why it stopped. A refusal to start says what to do about it. */
export function AIJobCard({ job, refusal, startError, pollError, onDismiss, actions, className }: AIJobCardProps) {
  const { t } = useI18n();
  const j = (key: string, params?: Record<string, string | number>) => t(`operations.aiJob.${key}`, params);

  if (refusal) {
    return (
      <Notice variant="warning" title={j(`refusals.${refusal}.title`)} className={className}>
        <p>{j(`refusals.${refusal}.body`)}</p>
        <Button size="sm" variant="outline" className="mt-2" onClick={onDismiss}>
          {j("dismiss")}
        </Button>
      </Notice>
    );
  }
  if (startError) {
    return (
      <Notice variant="error" title={j("startFailed")} className={className}>
        <p className="break-words">{startError}</p>
        <Button size="sm" variant="outline" className="mt-2" onClick={onDismiss}>
          {j("dismiss")}
        </Button>
      </Notice>
    );
  }
  if (!job) return null;

  const running = isJobRunning(job);
  const waiting = running && job.started_at === null;
  const kindTitle = j(`kinds.${job.kind}`);
  return (
    <Card className={cn("space-y-3 p-4", className)} aria-live="polite" aria-busy={running || undefined}>
      <div className="flex flex-wrap items-start gap-3">
        <span className="mt-0.5 shrink-0">
          {running ? (
            <Spinner size="sm" />
          ) : job.status === "done" ? (
            <CheckCircle2 className="h-4 w-4 text-success" aria-hidden />
          ) : (
            <XCircle className="h-4 w-4 text-destructive" aria-hidden />
          )}
        </span>
        <div className="min-w-0 flex-1 space-y-0.5">
          <p className="flex flex-wrap items-center gap-2 text-body font-medium">
            <Sparkles className="h-3.5 w-3.5 text-info" aria-hidden />
            {kindTitle}
            <Badge variant={running ? "info" : job.status === "done" ? "success" : "destructive"} className="px-2 py-0 text-micro">
              {j(`statuses.${job.status}`)}
            </Badge>
          </p>
          {waiting ? (
            <p className="text-caption text-muted-foreground">
              {j("waitingFor")} <Elapsed since={Date.parse(job.created_at)} className="tabular-nums" />
              <span aria-hidden> · </span>
              {j("waitingHint")}
            </p>
          ) : running ? (
            <p className="text-caption text-muted-foreground">
              {j("runningFor")} <Elapsed since={Date.parse(job.started_at ?? job.created_at)} className="tabular-nums" />
              <span aria-hidden> · </span>
              {j("runningHint")}
            </p>
          ) : job.status === "done" ? (
            <p className="text-caption text-muted-foreground">{j("createdCount", { count: jobItems(job).length })}</p>
          ) : (
            <p className="text-caption break-words whitespace-pre-line text-destructive">{job.error || j("failedNoReason")}</p>
          )}
        </div>
        {!running && (
          <Button size="sm" variant="ghost" className="h-7 shrink-0" onClick={onDismiss} aria-label={j("dismissLabel")}>
            <X aria-hidden />
            {j("dismiss")}
          </Button>
        )}
      </div>

      {pollError && running && (
        <p className="text-caption text-warning" role="status">
          {j("pollFailed", { message: pollError })}
        </p>
      )}

      {!running && <JobResult job={job} />}

      {!running && (actions || (job.status === "failed" && job.session_id)) && (
        <div className="flex flex-wrap items-center gap-2">
          {actions}
          {job.status === "failed" && job.session_id && <OpenChatButton sessionId={job.session_id} />}
        </div>
      )}
    </Card>
  );
}

function JobResult({ job }: { job: AIJob }) {
  const { t } = useI18n();
  const j = (key: string, params?: Record<string, string | number>) => t(`operations.aiJob.${key}`, params);
  const href = useOpsHref();
  const items = jobItems(job);

  return (
    <div className="space-y-3">
      {job.status === "done" && items.length === 0 && <p className="text-caption text-muted-foreground">{j("createdNone")}</p>}
      {items.length > 0 && (
        <section aria-label={j("createdTitle")} className="space-y-1.5">
          <h3 className="text-micro font-medium tracking-wide text-muted-foreground uppercase">{j("createdTitle")}</h3>
          <ul className="divide-y divide-border rounded-lg border border-border">
            {items.map((item) => {
              const to = jobItemHref(item, job);
              return (
                <li key={`${item.kind}:${item.id}`} className="space-y-0.5 px-3 py-2">
                  <div className="flex min-w-0 flex-wrap items-center gap-2">
                    <Badge variant="outline" className="px-2 py-0 text-micro font-normal">
                      {j(`items.${item.kind}`)}
                    </Badge>
                    {to ? (
                      <Link to={href(to)} className="min-w-0 truncate text-caption font-medium text-info hover:underline">
                        {item.name}
                      </Link>
                    ) : (
                      <span className="min-w-0 truncate text-caption font-medium">{item.name}</span>
                    )}
                  </div>
                  {item.rationale && <p className="text-caption text-muted-foreground">{item.rationale}</p>}
                </li>
              );
            })}
          </ul>
        </section>
      )}
      {job.result_summary && (
        <section aria-label={j("summaryTitle")} className="space-y-1.5">
          <h3 className="text-micro font-medium tracking-wide text-muted-foreground uppercase">{j("summaryTitle")}</h3>
          <MarkdownContent content={job.result_summary} className="text-caption" />
        </section>
      )}
    </div>
  );
}

/** The job's chat names no agent; the session does, so it is read on the way there. */
function OpenChatButton({ sessionId }: { sessionId: string }) {
  const { t } = useI18n();
  const navigate = useNavigate();
  const [opening, setOpening] = useState(false);
  const [failed, setFailed] = useState(false);
  const open = async () => {
    setOpening(true);
    setFailed(false);
    try {
      const { session } = await api.getSession(sessionId);
      if (!session.agent_id) throw new Error("the session names no agent");
      navigate(`/agents/${encodeURIComponent(session.agent_id)}/chat/${encodeURIComponent(sessionId)}`);
    } catch {
      setFailed(true);
      setOpening(false);
    }
  };
  return (
    <>
      <Button size="sm" variant="outline" onClick={() => void open()} disabled={opening}>
        <MessageSquare aria-hidden />
        {t("operations.aiJob.openChat")}
      </Button>
      {failed && <span className="text-caption text-destructive">{t("operations.aiJob.openChatFailed")}</span>}
    </>
  );
}
