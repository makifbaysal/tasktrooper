// lucide v1 dropped the brand marks, so GitHub is the same GitBranch the
// GitHub settings card already uses for it.
import { GitBranch, MessageSquare, Sparkles, Ticket } from "lucide-react";
import { useEffect, useState, type MouseEvent } from "react";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { api, ApiError, type IssueConversionStatus, type IssueImport, type IssueLink } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { useI18n } from "@/hooks/useI18n";
import { desktopRunner } from "@/lib/desktop-bridge";

interface TaskIssueLinkProps {
  repositoryId: string;
  taskId: string;
}

const STATUS_KEY: Record<Exclude<IssueConversionStatus, "">, string> = {
  pending: "issues.conversion.status.converting",
  converting: "issues.conversion.status.converting",
  converted: "issues.conversion.status.converted",
  needs_input: "issues.conversion.status.needsInput",
  failed: "issues.conversion.status.failed",
  skipped: "issues.conversion.status.skipped",
};

/**
 * The GitHub or Jira issue a task came from, and how far the product manager
 * got turning it into tasks. Nothing at all for a task that came from no issue:
 * an empty "Issue" heading on every card would be a question with one answer.
 */
export function TaskIssueLink({ repositoryId, taskId }: TaskIssueLinkProps) {
  const { t } = useI18n();
  const navigate = useNavigate();
  const [link, setLink] = useState<IssueLink | null>(null);
  const [imported, setImported] = useState<IssueImport | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let cancelled = false;
    api
      .getTaskIssueLink(repositoryId, taskId)
      .then((data) => {
        if (cancelled) return;
        setLink(data.link ?? null);
        setImported(data.import ?? null);
      })
      .catch(() => {
        if (!cancelled) setLink(null);
      });
    return () => {
      cancelled = true;
    };
  }, [repositoryId, taskId]);

  if (!link) return null;

  // The pull-request link's own reason: inside the desktop shell an anchor's own
  // navigation used to leave it on its loading screen with no way back.
  const handleClick = (event: MouseEvent<HTMLAnchorElement>) => {
    const runner = desktopRunner();
    if (!runner) return;
    event.preventDefault();
    void runner.openExternal(link.url).then((opened) => {
      if (!opened) toast.error(t("issues.taskLink.linkFailed"));
    });
  };

  const Icon = link.provider === "github" ? GitBranch : Ticket;
  const status = imported?.conversion_status ?? "";
  const sessionId = imported?.conversion_session_id ?? null;
  // Only the task the import opened straight from the issue can be handed to
  // the product manager; a task it already produced is the conversion's result.
  const canConvert =
    imported !== null &&
    imported.intake_task_id === taskId &&
    (status === "" || status === "failed" || status === "skipped");

  const openConversation = async () => {
    if (!sessionId) return;
    setBusy(true);
    try {
      const { session } = await api.getSession(sessionId);
      if (!session.agent_id) throw new Error(t("issues.conversion.openFailed"));
      navigate(`/agents/${session.agent_id}/chat/${session.id}`);
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t("issues.conversion.openFailed"));
    } finally {
      setBusy(false);
    }
  };

  const convert = async () => {
    if (!imported) return;
    setBusy(true);
    try {
      const { import: queued } = await api.convertIssueImport(imported.id);
      setImported(queued);
      toast.success(t("issues.conversion.queued"));
    } catch (e) {
      toast.error(
        e instanceof ApiError && e.type === "issue_conversion_unavailable"
          ? t("issues.conversion.unavailable")
          : e instanceof Error && e.message
            ? e.message
            : t("common.actionFailed"),
      );
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className="space-y-2">
      <Label className="flex items-center gap-2 text-xs text-muted-foreground">
        <Icon className="h-3.5 w-3.5" />
        {t("issues.taskLink.label")}
      </Label>
      <div className="flex flex-wrap items-center gap-2">
        <a
          href={link.url}
          target="_blank"
          rel="noreferrer"
          onClick={handleClick}
          className="inline-flex items-center gap-1 text-sm text-primary hover:underline"
        >
          {link.provider === "github" ? "GitHub" : "Jira"} {link.key}
        </a>
        {link.closed_at && <Badge variant="secondary">{t("issues.taskLink.closed")}</Badge>}
        {status !== "" && (
          <Badge variant={status === "failed" ? "destructive" : status === "converted" ? "secondary" : "info"}>
            {t(STATUS_KEY[status])}
          </Badge>
        )}
      </div>
      {status === "failed" && imported?.conversion_error && (
        <p className="text-xs text-muted-foreground">{imported.conversion_error}</p>
      )}
      {(sessionId || canConvert) && (
        <div className="flex flex-wrap gap-2">
          {sessionId && (
            <Button variant="outline" size="sm" disabled={busy} onClick={() => void openConversation()}>
              <MessageSquare className="mr-2 h-3.5 w-3.5" />
              {t("issues.conversion.openChat")}
            </Button>
          )}
          {canConvert && (
            <Button variant="outline" size="sm" disabled={busy} onClick={() => void convert()}>
              <Sparkles className="mr-2 h-3.5 w-3.5" />
              {t("issues.conversion.convert")}
            </Button>
          )}
        </div>
      )}
    </section>
  );
}
