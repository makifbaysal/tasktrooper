import { ExternalLink, Search } from "lucide-react";
import { useEffect, useMemo, useState, type MouseEvent } from "react";
import { toast } from "sonner";
import {
  api,
  ApiError,
  type BoardTask,
  type ExternalIssue,
  type IssueProvider,
  type Repository,
} from "@/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useI18n } from "@/hooks/useI18n";
import { desktopRunner } from "@/lib/desktop-bridge";

interface ImportIssueDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  repositories: Repository[];
  defaultRepositoryId?: string;
  onImported: (task: BoardTask) => void;
}

interface SearchResult {
  issues: ExternalIssue[];
  loading: boolean;
  error: string;
  notConfigured: boolean;
}

const EMPTY_RESULT: SearchResult = { issues: [], loading: false, error: "", notConfigured: false };

function hasGithubRemote(repo: Repository): boolean {
  return (repo.remote_url ?? "").includes("github.com");
}

export function ImportIssueDialog({
  open,
  onOpenChange,
  repositories,
  defaultRepositoryId,
  onImported,
}: ImportIssueDialogProps) {
  const { t } = useI18n();
  const [source, setSource] = useState<IssueProvider>("github");
  // GitHub issues can only land in the repository whose remote they came from;
  // a Jira issue can land in any repository, so the two tabs pick differently.
  const githubRepositories = useMemo(() => repositories.filter(hasGithubRemote), [repositories]);
  const [githubRepositoryId, setGithubRepositoryId] = useState("");
  const [jiraRepositoryId, setJiraRepositoryId] = useState("");
  const [project, setProject] = useState("");
  const [query, setQuery] = useState("");
  const [submittedQuery, setSubmittedQuery] = useState("");
  const [searchNonce, setSearchNonce] = useState(0);
  const [result, setResult] = useState<SearchResult>(EMPTY_RESULT);
  const [projects, setProjects] = useState<{ key: string; name: string }[]>([]);
  const [projectsError, setProjectsError] = useState("");
  const [importingKey, setImportingKey] = useState("");

  const targetRepositoryId = source === "github" ? githubRepositoryId : jiraRepositoryId;

  useEffect(() => {
    if (!open) return;
    const defaultIsGithub = githubRepositories.some((r) => r.id === defaultRepositoryId);
    setGithubRepositoryId(defaultIsGithub && defaultRepositoryId ? defaultRepositoryId : (githubRepositories[0]?.id ?? ""));
    setJiraRepositoryId(defaultRepositoryId ?? repositories[0]?.id ?? "");
    setQuery("");
    setSubmittedQuery("");
    setSearchNonce(0);
    setResult(EMPTY_RESULT);
  }, [open, defaultRepositoryId, repositories, githubRepositories]);

  useEffect(() => {
    if (!open || source !== "jira") return;
    let cancelled = false;
    setProjectsError("");
    api
      .listJiraProjects()
      .then((data) => {
        if (!cancelled) setProjects(data.projects ?? []);
      })
      .catch((e) => {
        if (cancelled) return;
        setProjects([]);
        setProjectsError(e instanceof Error ? e.message : t("issues.import.projectsFailed"));
      });
    return () => {
      cancelled = true;
    };
  }, [open, source, t]);

  // An empty query lists the open issues, so the dialog opens with something to
  // pick rather than an empty list behind a search box.
  useEffect(() => {
    if (!open) return;
    if (source === "github" ? !githubRepositoryId : !project || !jiraRepositoryId) {
      setResult(EMPTY_RESULT);
      return;
    }
    let cancelled = false;
    setResult({ ...EMPTY_RESULT, loading: true });
    api
      .searchIssues({
        provider: source,
        repositoryId: source === "github" ? githubRepositoryId : undefined,
        project: source === "jira" ? project : undefined,
        q: submittedQuery || undefined,
      })
      .then((data) => {
        if (!cancelled) setResult({ issues: data.issues ?? [], loading: false, error: "", notConfigured: false });
      })
      .catch((e) => {
        if (cancelled) return;
        // A source nobody connected is a different sentence from a source that
        // failed: the first has a fix, the second only has a retry.
        setResult({
          issues: [],
          loading: false,
          error: e instanceof Error ? e.message : "",
          notConfigured: e instanceof ApiError && e.type === "issue_source_not_configured",
        });
      });
    return () => {
      cancelled = true;
    };
  }, [open, source, githubRepositoryId, jiraRepositoryId, project, submittedQuery, searchNonce]);

  const runSearch = () => {
    setSubmittedQuery(query.trim());
    setSearchNonce((n) => n + 1);
  };

  const handleImport = async (issue: ExternalIssue) => {
    if (!targetRepositoryId) return;
    setImportingKey(issue.key);
    try {
      const { task, import: imported } = await api.importIssue(issue.provider, issue.key, targetRepositoryId);
      // A pending conversion replaces this task with the product manager's own,
      // so naming its key as the result would point at a card about to go.
      toast.success(
        imported?.conversion_status === "pending"
          ? t("issues.import.converting", { key: issue.key })
          : t("issues.import.done", { key: task.key }),
      );
      onImported(task);
      onOpenChange(false);
    } catch (e) {
      // The 409's task_key sits in the response body, which `request` folds into
      // the message; the key is already on the row as its "Imported as" badge.
      if (e instanceof ApiError && e.type === "issue_already_imported") {
        toast.error(t("issues.import.alreadyImported"));
      } else {
        toast.error(e instanceof Error ? e.message : t("issues.import.failed"));
      }
    } finally {
      setImportingKey("");
    }
  };

  // Same reason the pull-request link needs it: inside the desktop shell an
  // anchor's own navigation used to leave it on its loading screen with no way
  // back. In a browser there is no bridge, so `target="_blank"` applies.
  const handleOpen = (event: MouseEvent<HTMLAnchorElement>, url: string) => {
    const runner = desktopRunner();
    if (!runner) return;
    event.preventDefault();
    void runner.openExternal(url).then((opened) => {
      if (!opened) toast.error(t("issues.taskLink.linkFailed"));
    });
  };

  const renderResults = () => {
    if (result.loading) {
      return (
        <div className="space-y-2">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-14 w-full" />
          ))}
        </div>
      );
    }
    if (result.notConfigured) {
      return (
        <Notice variant="info" title={t("issues.import.sourceNotConnectedTitle")}>
          {t("issues.import.notConfigured", {
            source: t(source === "github" ? "issues.import.source.github" : "issues.import.source.jira"),
          })}
        </Notice>
      );
    }
    if (result.error) {
      return (
        <Notice variant="error" title={t("issues.import.searchFailed")}>
          <div className="space-y-2">
            <p>{result.error}</p>
            <Button variant="outline" size="sm" onClick={() => setSearchNonce((n) => n + 1)}>
              {t("issues.import.retry")}
            </Button>
          </div>
        </Notice>
      );
    }
    if (result.issues.length === 0) {
      return <p className="text-sm text-muted-foreground">{t("issues.import.noResults")}</p>;
    }
    return (
      <ul className="space-y-2">
        {result.issues.map((issue) => (
          <li
            key={`${issue.provider}:${issue.key}`}
            className="flex items-start justify-between gap-3 rounded-lg border border-border p-2.5"
          >
            <div className="min-w-0 space-y-1">
              <div className="flex flex-wrap items-center gap-1.5">
                <span className="font-mono text-caption text-muted-foreground">{issue.key}</span>
                <Badge variant={issue.state === "open" ? "info" : "secondary"}>{issue.state}</Badge>
                {issue.labels.slice(0, 3).map((label) => (
                  <Badge key={label} variant="outline">
                    {label}
                  </Badge>
                ))}
              </div>
              <p className="truncate text-sm">{issue.title}</p>
            </div>
            <div className="flex shrink-0 items-center gap-2">
              <a
                href={issue.url}
                target="_blank"
                rel="noreferrer"
                onClick={(e) => handleOpen(e, issue.url)}
                className="text-muted-foreground hover:text-foreground"
                aria-label={t("issues.import.openIssue")}
              >
                <ExternalLink className="h-3.5 w-3.5" />
              </a>
              {issue.imported_task ? (
                <Badge variant="secondary">
                  {t("issues.import.importedAs", { key: issue.imported_task.key })}
                </Badge>
              ) : (
                <Button
                  size="sm"
                  disabled={!targetRepositoryId || importingKey === issue.key}
                  onClick={() => void handleImport(issue)}
                >
                  {t("issues.import.import")}
                </Button>
              )}
            </div>
          </li>
        ))}
      </ul>
    );
  };

  const searchField = (
    <div className="flex items-end gap-2">
      <div className="flex-1 space-y-2">
        <Label htmlFor="issue-search">{t("issues.import.search")}</Label>
        <Input
          id="issue-search"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") runSearch();
          }}
          placeholder={t("issues.import.searchPlaceholder")}
        />
      </div>
      <Button variant="outline" onClick={runSearch}>
        <Search className="mr-2 h-3.5 w-3.5" />
        {t("issues.import.search")}
      </Button>
    </div>
  );

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t("issues.import.title")}</DialogTitle>
        </DialogHeader>
        <Tabs value={source} onValueChange={(v) => setSource(v as IssueProvider)}>
          <TabsList>
            <TabsTrigger value="github">{t("issues.import.source.github")}</TabsTrigger>
            <TabsTrigger value="jira">{t("issues.import.source.jira")}</TabsTrigger>
          </TabsList>

          <TabsContent value="github" className="space-y-3">
            {githubRepositories.length === 0 ? (
              <Notice variant="info" title={t("issues.import.noGithubRepositories")} />
            ) : (
              <>
                <div className="space-y-2">
                  <Label htmlFor="issue-github-repository">
                    {t("issues.import.targetRepository")}
                  </Label>
                  <Select value={githubRepositoryId} onValueChange={setGithubRepositoryId}>
                    <SelectTrigger id="issue-github-repository">
                      <SelectValue placeholder={t("issues.import.selectRepository")} />
                    </SelectTrigger>
                    <SelectContent>
                      {githubRepositories.map((repo) => (
                        <SelectItem key={repo.id} value={repo.id}>
                          {repo.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                {searchField}
                <div className="max-h-[55vh] overflow-y-auto pr-1">{renderResults()}</div>
              </>
            )}
          </TabsContent>

          <TabsContent value="jira" className="space-y-3">
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor="issue-jira-project">{t("issues.import.selectProject")}</Label>
                <Select value={project} onValueChange={setProject} disabled={projects.length === 0}>
                  <SelectTrigger id="issue-jira-project">
                    <SelectValue
                      placeholder={
                        projectsError
                          ? t("issues.import.projectsFailed")
                          : t("issues.import.selectProject")
                      }
                    />
                  </SelectTrigger>
                  <SelectContent>
                    {projects.map((p) => (
                      <SelectItem key={p.key} value={p.key}>
                        {p.key} · {p.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-2">
                <Label htmlFor="issue-jira-repository">{t("issues.import.targetRepository")}</Label>
                <Select value={jiraRepositoryId} onValueChange={setJiraRepositoryId}>
                  <SelectTrigger id="issue-jira-repository">
                    <SelectValue placeholder={t("issues.import.selectRepository")} />
                  </SelectTrigger>
                  <SelectContent>
                    {repositories.map((repo) => (
                      <SelectItem key={repo.id} value={repo.id}>
                        {repo.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>
            {searchField}
            <div className="max-h-[55vh] overflow-y-auto pr-1">{renderResults()}</div>
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
  );
}
