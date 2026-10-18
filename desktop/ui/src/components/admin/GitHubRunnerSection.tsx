import { Copy } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { api, ApiError, type GitHubRunnerRepoStatus, type GitHubRunnerStatus } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import { usePolling } from "@/hooks/usePolling";
import { useI18n } from "@/hooks/useI18n";

const ERROR_PREFIX = "github_runner_";
const KNOWN_ERRORS = new Set([
  "github_not_connected",
  "forbidden",
  "not_found",
  "public_repo",
  "needs_admin",
  "invalid_repo",
  "too_many_repos",
  "unsupported_platform",
  "download_failed",
  "start_failed",
  "exited",
  "computer_away",
  "computer_not_ready",
  "computer_outdated",
  "github_error",
]);

type T = (key: string, params?: Record<string, string | number>) => string;

function errorText(t: T, code: string | undefined, repo: string): string {
  const known = code && KNOWN_ERRORS.has(code) ? code : "github_error";
  return t(`settings.github.runner.errors.${known}`, { repo });
}

function stateBadge(t: T, r: GitHubRunnerRepoStatus) {
  switch (r.state) {
    case "idle":
      return <Badge variant="success">{t("settings.github.runner.states.idle")}</Badge>;
    case "busy":
      return <Badge variant="info">{t("settings.github.runner.states.busy", { job: r.job ?? "" })}</Badge>;
    case "error":
      return <Badge variant="warning">{t("settings.github.runner.states.error")}</Badge>;
    case "installing":
    case "starting":
      return <Badge variant="secondary">{t(`settings.github.runner.states.${r.state}`)}</Badge>;
    default:
      return <Badge variant="outline">{t("settings.github.runner.states.stopped")}</Badge>;
  }
}

/**
 * This computer as a self-hosted GitHub Actions runner for repositories the
 * person picks. Every change is saved at once; the server checks a newly
 * added repository (private, administered by this connection) before it
 * keeps the list, so a refusal comes back here as the reason.
 */
export function GitHubRunnerSection() {
  const { t } = useI18n();
  const [status, setStatus] = useState<GitHubRunnerStatus | null>(null);
  const [saving, setSaving] = useState(false);
  // A refresh that started before a change answers with the old list; this
  // drops it instead of flashing the switch back.
  const version = useRef(0);

  const load = useCallback(async () => {
    const started = version.current;
    try {
      const next = await api.githubRunnerStatus();
      if (version.current === started) setStatus(next);
    } catch {
      // Keep the last answer; the card above already says when GitHub is unreachable.
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  usePolling(load, 4000, Boolean(status?.enabled), { leading: false });

  const save = async (next: { enabled: boolean; repos: string[] }, added?: string) => {
    if (!status) return;
    const previous = status;
    version.current += 1;
    setSaving(true);
    setStatus({
      ...status,
      enabled: next.enabled,
      repos: next.repos.map(
        (repo) =>
          status.repos.find((r) => r.repo === repo) ?? { repo, state: next.enabled ? "starting" : "stopped" },
      ),
    });
    try {
      const answer = await api.setGitHubRunner(next);
      version.current += 1;
      setStatus(answer);
      if (next.enabled !== previous.enabled) {
        toast.success(t(next.enabled ? "settings.github.runner.enabledToast" : "settings.github.runner.disabledToast"));
      }
    } catch (e) {
      version.current += 1;
      setStatus(previous);
      if (e instanceof ApiError && e.type.startsWith(ERROR_PREFIX)) {
        toast.error(errorText(t, e.type.slice(ERROR_PREFIX.length), added ?? ""));
      } else {
        toast.error(e instanceof Error ? e.message : t("common.actionFailed"));
      }
    } finally {
      setSaving(false);
    }
  };

  if (!status) return null;
  if (!status.supported) {
    return (
      <>
        <Separator />
        <p className="text-xs text-muted-foreground">{t("settings.github.runner.unsupported")}</p>
      </>
    );
  }

  const selected = status.repos.map((r) => r.repo);
  const runsOn = `runs-on: [${status.labels.filter((l) => l !== "tasktrooper").join(", ")}]`;

  const toggleRepo = (repo: string, on: boolean) => {
    const repos = on ? [...selected, repo] : selected.filter((r) => r !== repo);
    void save({ enabled: status.enabled, repos }, on ? repo : undefined);
  };

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(runsOn);
      toast.success(t("settings.github.runner.snippetCopied"));
    } catch {
      toast.error(t("common.actionFailed"));
    }
  };

  return (
    <>
      <Separator />
      <div className="space-y-3">
        <div className="flex items-start justify-between gap-4">
          <div className="min-w-0 space-y-1">
            <Label htmlFor="github-runner-switch" className="text-sm font-medium">
              {t("settings.github.runner.title")}
            </Label>
            <p className="text-xs text-muted-foreground">{t("settings.github.runner.description")}</p>
          </div>
          <Switch
            id="github-runner-switch"
            checked={status.enabled}
            disabled={saving}
            onCheckedChange={(on) => void save({ enabled: on, repos: selected })}
          />
        </div>

        <Notice variant="info" title={t("settings.github.runner.warning")} />

        <div className="space-y-2">
          <p className="text-xs font-medium text-muted-foreground">{t("settings.github.runner.repositories")}</p>
          {status.candidates.length === 0 ? (
            <p className="text-xs text-muted-foreground">{t("settings.github.runner.noCandidates")}</p>
          ) : (
            <div className="divide-y rounded-md border">
              {status.candidates.map((repo) => {
                const row = status.repos.find((r) => r.repo === repo);
                const id = `github-runner-repo-${repo.replace(/[^A-Za-z0-9]/g, "-")}`;
                return (
                  <div key={repo} className="space-y-1 px-3 py-2">
                    <div className="flex items-center justify-between gap-3">
                      <div className="flex min-w-0 items-center gap-2">
                        <Checkbox
                          id={id}
                          checked={Boolean(row)}
                          disabled={saving}
                          onCheckedChange={(on) => toggleRepo(repo, on === true)}
                        />
                        <Label htmlFor={id} className="truncate font-mono text-xs">
                          {repo}
                        </Label>
                      </div>
                      {row && status.enabled && stateBadge(t, row)}
                    </div>
                    {row && status.enabled && row.state === "error" && (
                      <p className="pl-6 text-xs text-muted-foreground">
                        {errorText(t, row.error_code, repo)}
                        {row.error_detail ? ` ${row.error_detail.split("\n")[0]}` : ""}
                      </p>
                    )}
                    {row?.last_job && (
                      <p className="pl-6 text-xs text-muted-foreground">
                        {t("settings.github.runner.lastJob", { job: row.last_job, result: row.last_result ?? "" })}
                      </p>
                    )}
                  </div>
                );
              })}
            </div>
          )}
          <p className="text-xs text-muted-foreground">{t("settings.github.runner.diskNote")}</p>
        </div>

        <div className="space-y-1">
          <p className="text-xs text-muted-foreground">{t("settings.github.runner.snippetHint")}</p>
          <div className="flex items-center gap-2">
            <code className="min-w-0 flex-1 truncate rounded-md bg-muted px-2 py-1 font-mono text-xs">{runsOn}</code>
            <Button
              variant="ghost"
              size="icon"
              className="h-7 w-7"
              onClick={() => void copy()}
              title={t("settings.github.runner.copySnippet")}
              aria-label={t("settings.github.runner.copySnippet")}
            >
              <Copy className="h-3.5 w-3.5" />
            </Button>
          </div>
        </div>
      </div>
    </>
  );
}
