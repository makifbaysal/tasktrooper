import { Folder, FolderOpen, GitBranch, Loader2 } from "lucide-react";
import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import { api, type GitHubOwner, type GitHubRepoInfo, type InitiativeProject } from "@/api";
import { GitHubRepoPicker } from "@/components/projects/add/GitHubRepoPicker";
import {
  EMPTY_NEW_REPOSITORY,
  NewRepositoryForm,
  type NewRepositoryDraft,
  repoNameError,
  sanitizeRepoName,
  toNewRepositoryInput,
} from "@/components/projects/add/NewRepositoryForm";
import { SourceModePicker } from "@/components/projects/add/SourceModePicker";
import type { NewRepositoryInput, ProjectChoice, SourceMode, SourceSelection } from "@/components/projects/add/flow-types";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useI18n } from "@/hooks/useI18n";
import { desktopRunner } from "@/lib/desktop-bridge";
import { exampleFolderPath, isWindowsPlatform } from "@/lib/platform";

// The server's 500 for a create that got past making the repository and then
// failed a later step: the name is taken now, so Retry would only collide.
const PARTIAL_CREATE = /^repository ".*?" was created, but /;

interface SourceStepProps {
  initialProjectId?: string;
  onScan: (choice: ProjectChoice, selection: SourceSelection) => Promise<void>;
  onCreate: (choice: ProjectChoice, input: NewRepositoryInput) => Promise<void>;
  onModeChange?: (mode: SourceMode | null) => void;
}

/**
 * Step 1: which project, then exactly one way in — a folder, GitHub, or a new
 * repository. Only the picked mode's details are shown. Folder and GitHub go
 * on to Scan; a new repository has no code to scan, so it is created here
 * and the flow jumps straight to Done.
 */
export function SourceStep({ initialProjectId, onScan, onCreate, onModeChange }: SourceStepProps) {
  const { t } = useI18n();

  const [projects, setProjects] = useState<InitiativeProject[]>([]);
  const [projectMode, setProjectMode] = useState<"existing" | "new">("existing");
  const [existingProjectId, setExistingProjectId] = useState(initialProjectId ?? "");
  const [newProjectName, setNewProjectName] = useState("");

  const [sourceMode, setSourceMode] = useState<SourceMode | null>(null);

  const [githubConnected, setGithubConnected] = useState<boolean | null>(null);
  const [owners, setOwners] = useState<GitHubOwner[]>([]);
  const [ownersError, setOwnersError] = useState("");
  const [owner, setOwner] = useState("");
  const [ownerRepos, setOwnerRepos] = useState<GitHubRepoInfo[]>([]);
  const [ownerReposLoading, setOwnerReposLoading] = useState(false);
  const [selectedGithub, setSelectedGithub] = useState<Set<string>>(new Set());
  const [existingRepoNames, setExistingRepoNames] = useState<Set<string>>(new Set());

  const [folderPath, setFolderPath] = useState("");
  const [draft, setDraft] = useState<NewRepositoryDraft>(EMPTY_NEW_REPOSITORY);

  const [submitting, setSubmitting] = useState(false);
  const [createError, setCreateError] = useState<{ message: string; partial: boolean; name: string } | null>(null);

  const runner = desktopRunner();
  const canBrowse = typeof runner?.chooseDirectory === "function";

  useEffect(() => {
    api
      .listInitiativeProjects()
      .then((d) => {
        const list = d.projects ?? [];
        setProjects(list);
        if (!initialProjectId && list.length === 0) setProjectMode("new");
      })
      .catch(() => {});
    // Only ever read once: the ?project= preselection is a starting point,
    // not a value this step re-syncs to on every render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    api
      .listRepositories()
      .then((d) => setExistingRepoNames(new Set((d.repositories ?? []).map((r) => r.name))))
      .catch(() => setExistingRepoNames(new Set()));
  }, []);

  useEffect(() => {
    api
      .githubStatus()
      .then((s) => setGithubConnected(s.connected))
      .catch(() => setGithubConnected(false));
  }, []);

  useEffect(() => {
    if (githubConnected !== true) return;
    api
      .githubOwners()
      .then((d) => {
        const list = d.owners ?? [];
        setOwners(list);
        setOwnersError("");
        if (list.length > 0) {
          setOwner((prev) => prev || list[0].login);
          setDraft((prev) => (prev.owner ? prev : { ...prev, owner: list[0].login }));
        }
      })
      .catch((e) => {
        setOwners([]);
        setOwnersError(e instanceof Error ? e.message : t("addRepository.source.githubOwnersFailed"));
      });
  }, [githubConnected, t]);

  useEffect(() => {
    if (!owner) return;
    setOwnerReposLoading(true);
    setSelectedGithub(new Set());
    api
      .githubOwnerRepos(owner)
      .then((d) => setOwnerRepos(d.repos ?? []))
      .catch(() => setOwnerRepos([]))
      .finally(() => setOwnerReposLoading(false));
  }, [owner]);

  useEffect(() => {
    onModeChange?.(sourceMode);
  }, [sourceMode, onModeChange]);

  const toggleGithub = (name: string) => {
    setSelectedGithub((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  };

  const projectValid = projectMode === "existing" ? existingProjectId !== "" : newProjectName.trim() !== "";
  const sourceValid =
    sourceMode === "folder"
      ? folderPath.trim() !== ""
      : sourceMode === "github"
        ? selectedGithub.size > 0
        : sourceMode === "new"
          ? repoNameError(draft.name) === null
          : false;
  const nameTaken = sourceMode === "new" && createError?.partial === true && createError.name === sanitizeRepoName(draft.name);
  const canSubmit = projectValid && sourceValid && !submitting && !nameTaken;

  const projectChoice = (): ProjectChoice =>
    projectMode === "existing"
      ? {
          mode: "existing",
          projectId: existingProjectId,
          projectName: projects.find((p) => p.id === existingProjectId)?.name ?? "",
        }
      : { mode: "new", name: newProjectName.trim() };

  const handleBrowse = async () => {
    if (!runner?.chooseDirectory) return;
    try {
      const chosen = await runner.chooseDirectory({
        title: t("addRepository.source.folderPathLabel"),
        defaultPath: folderPath.trim() || undefined,
      });
      if (chosen) setFolderPath(chosen);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("addRepository.source.newProjectFailed"));
    }
  };

  const handleImport = async () => {
    const selection: SourceSelection =
      sourceMode === "github"
        ? {
            github: {
              owner,
              repos: [...selectedGithub].map((name) => ({
                name,
                cloneUrl: ownerRepos.find((r) => r.name === name)?.clone_url,
              })),
            },
            folderPath: "",
          }
        : { github: null, folderPath };
    try {
      await onScan(projectChoice(), selection);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("addRepository.source.newProjectFailed"));
    }
  };

  const handleCreate = async () => {
    setCreateError(null);
    const input = toNewRepositoryInput(draft);
    try {
      await onCreate(projectChoice(), input);
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e);
      setCreateError({ message, partial: PARTIAL_CREATE.test(message), name: input.name });
    }
  };

  const handleSubmit = async () => {
    if (!canSubmit) return;
    setSubmitting(true);
    try {
      if (sourceMode === "new") await handleCreate();
      else await handleImport();
    } finally {
      setSubmitting(false);
    }
  };

  const submitLabel =
    sourceMode === "new"
      ? submitting
        ? t("addRepository.source.creating")
        : t("addRepository.source.createButton")
      : sourceMode === "github" && selectedGithub.size > 1
        ? t("addRepository.source.importCountButton", { count: selectedGithub.size })
        : t("addRepository.source.importButton");

  return (
    <div className="space-y-4">
      <Card>
        <CardContent className="space-y-3 pt-6">
          <CardTitle className="text-body">{t("addRepository.source.heading")}</CardTitle>
          <div className="flex flex-col gap-3 sm:flex-row sm:items-start">
            <Tabs
              value={projectMode}
              onValueChange={(v) => setProjectMode(v as "existing" | "new")}
              variant="pill"
              className="w-fit shrink-0"
            >
              <TabsList>
                <TabsTrigger value="existing">{t("addRepository.source.existingProject")}</TabsTrigger>
                <TabsTrigger value="new">{t("addRepository.source.newProject")}</TabsTrigger>
              </TabsList>
            </Tabs>
            <div className="flex-1">
              {projectMode === "existing" ? (
                <Select value={existingProjectId} onValueChange={setExistingProjectId}>
                  <SelectTrigger aria-label={t("addRepository.source.existingProjectLabel")}>
                    <SelectValue placeholder={t("addRepository.source.existingProjectPlaceholder")} />
                  </SelectTrigger>
                  <SelectContent>
                    {projects.map((p) => (
                      <SelectItem key={p.id} value={p.id}>
                        {p.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              ) : (
                <Input
                  value={newProjectName}
                  onChange={(e) => setNewProjectName(e.target.value)}
                  placeholder={t("addRepository.source.newProjectPlaceholder")}
                  aria-label={t("addRepository.source.newProjectLabel")}
                />
              )}
            </div>
          </div>
          {projectMode === "new" && (
            <p className="text-caption text-muted-foreground">{t("addRepository.source.newProjectHint")}</p>
          )}
        </CardContent>
      </Card>

      <div className="space-y-2">
        <h2 className="text-body font-semibold">{t("addRepository.source.modeHeading")}</h2>
        <SourceModePicker value={sourceMode} onChange={setSourceMode} disabled={submitting} />
      </div>

      {sourceMode === "folder" && (
        <Card>
          <CardHeader>
            <div className="flex items-center gap-2">
              <Folder className="h-4 w-4 text-muted-foreground" aria-hidden />
              <CardTitle>{t("addRepository.source.folderTitle")}</CardTitle>
            </div>
            <CardDescription>{t("addRepository.source.folderDescription")}</CardDescription>
          </CardHeader>
          <CardContent className="space-y-2">
            <Label>{t("addRepository.source.folderPathLabel")}</Label>
            <div className="flex gap-2">
              <Input
                value={folderPath}
                onChange={(e) => setFolderPath(e.target.value)}
                placeholder={isWindowsPlatform() ? exampleFolderPath() : t("addRepository.source.folderPathPlaceholder")}
                className="flex-1"
              />
              {canBrowse && (
                <Button type="button" variant="outline" className="shrink-0" onClick={handleBrowse}>
                  <FolderOpen className="mr-1.5 h-4 w-4" aria-hidden />
                  {t("addRepository.source.folderBrowse")}
                </Button>
              )}
            </div>
          </CardContent>
        </Card>
      )}

      {sourceMode === "github" && (
        <Card>
          <CardHeader>
            <div className="flex items-center gap-2">
              <GitBranch className="h-4 w-4 text-muted-foreground" aria-hidden />
              <CardTitle>{t("addRepository.source.githubTitle")}</CardTitle>
            </div>
            <CardDescription>{t("addRepository.source.githubDescription")}</CardDescription>
          </CardHeader>
          <CardContent>
            {githubConnected === false ? (
              <Notice variant="info" title={t("addRepository.source.githubNotConnectedTitle")}>
                <p>{t("addRepository.source.githubNotConnectedBody")}</p>
                <Link to="/settings/integrations" className="underline">
                  {t("addRepository.source.githubNotConnectedAction")}
                </Link>
              </Notice>
            ) : (
              <GitHubRepoPicker
                owners={owners}
                ownersError={ownersError}
                owner={owner}
                onOwnerChange={setOwner}
                repos={ownerRepos}
                loading={ownerReposLoading}
                selected={selectedGithub}
                onToggle={toggleGithub}
                existingNames={existingRepoNames}
              />
            )}
          </CardContent>
        </Card>
      )}

      {sourceMode === "new" && <NewRepositoryForm value={draft} onChange={setDraft} owners={owners} disabled={submitting} />}

      {(sourceMode === "folder" || sourceMode === "github") && (
        <p className="text-caption text-muted-foreground">{t("addRepository.source.importHint")}</p>
      )}

      {sourceMode === "new" && createError && !createError.partial && (
        <Notice variant="error" title={t("addRepository.newRepo.createFailed")}>
          <p>{createError.message}</p>
          <Button size="sm" variant="outline" className="mt-2" disabled={!canSubmit} onClick={handleSubmit}>
            {t("addRepository.newRepo.retry")}
          </Button>
        </Notice>
      )}

      {sourceMode === "new" && createError?.partial && (
        <Notice variant="warning" title={t("addRepository.newRepo.partialTitle")}>
          <p>{createError.message}</p>
          <Button size="sm" variant="outline" className="mt-2" asChild>
            {projectMode === "existing" && existingProjectId ? (
              <Link to={`/projects/${existingProjectId}`}>{t("addRepository.newRepo.openProject")}</Link>
            ) : (
              <Link to="/projects">{t("addRepository.newRepo.openProjects")}</Link>
            )}
          </Button>
        </Notice>
      )}

      {sourceMode && (
        <div className="flex justify-end">
          <Button size="lg" disabled={!canSubmit} onClick={handleSubmit}>
            {submitting && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />}
            {submitLabel}
          </Button>
        </div>
      )}
    </div>
  );
}
