import { FilePlus, Folder, FolderOpen, Loader2 } from "lucide-react";
import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import { api, type GitHubOwner, type GitHubRepoInfo, type InitiativeProject } from "@/api";
import { GitHubRepoPicker } from "@/components/projects/add/GitHubRepoPicker";
import type { ProjectChoice, SourceSelection } from "@/components/projects/add/flow-types";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useI18n } from "@/hooks/useI18n";
import { desktopRunner } from "@/lib/desktop-bridge";

interface SourceStepProps {
  initialProjectId?: string;
  onScan: (choice: ProjectChoice, selection: SourceSelection) => Promise<void>;
}

/**
 * Step 1: which project, then one or more sources. The three source cards
 * are not exclusive tabs — GitHub repos, a folder and an empty repository
 * can all be queued together in one "Scan (N)" batch, matching the flow's
 * per-repo rows in step 2.
 */
export function SourceStep({ initialProjectId, onScan }: SourceStepProps) {
  const { t } = useI18n();

  const [projects, setProjects] = useState<InitiativeProject[]>([]);
  const [mode, setMode] = useState<"existing" | "new">("existing");
  const [existingProjectId, setExistingProjectId] = useState(initialProjectId ?? "");
  const [newProjectName, setNewProjectName] = useState("");

  const [githubConnected, setGithubConnected] = useState<boolean | null>(null);
  const [owners, setOwners] = useState<GitHubOwner[]>([]);
  const [ownersError, setOwnersError] = useState("");
  const [owner, setOwner] = useState("");
  const [ownerRepos, setOwnerRepos] = useState<GitHubRepoInfo[]>([]);
  const [ownerReposLoading, setOwnerReposLoading] = useState(false);
  const [selectedGithub, setSelectedGithub] = useState<Set<string>>(new Set());
  const [existingRepoNames, setExistingRepoNames] = useState<Set<string>>(new Set());

  const [folderPath, setFolderPath] = useState("");
  const [emptyName, setEmptyName] = useState("");
  const [emptyOwner, setEmptyOwner] = useState("");

  const [submitting, setSubmitting] = useState(false);

  const runner = desktopRunner();
  const canBrowse = typeof runner?.chooseDirectory === "function";

  useEffect(() => {
    api
      .listInitiativeProjects()
      .then((d) => {
        const list = d.projects ?? [];
        setProjects(list);
        if (!initialProjectId && list.length === 0) setMode("new");
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
          setEmptyOwner((prev) => prev || list[0].login);
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

  const toggleGithub = (name: string) => {
    setSelectedGithub((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  };

  const count = selectedGithub.size + (folderPath.trim() ? 1 : 0) + (emptyName.trim() ? 1 : 0);
  const projectValid = mode === "existing" ? existingProjectId !== "" : newProjectName.trim() !== "";
  const canScan = projectValid && count > 0 && !submitting;

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

  const handleScan = async () => {
    if (!canScan) return;
    setSubmitting(true);
    try {
      const choice: ProjectChoice =
        mode === "existing"
          ? { mode: "existing", projectId: existingProjectId, projectName: projects.find((p) => p.id === existingProjectId)?.name ?? "" }
          : { mode: "new", name: newProjectName.trim() };
      const selection: SourceSelection = {
        github:
          selectedGithub.size > 0
            ? {
                owner,
                repos: [...selectedGithub].map((name) => ({
                  name,
                  cloneUrl: ownerRepos.find((r) => r.name === name)?.clone_url,
                })),
              }
            : null,
        folderPath,
        empty: emptyName.trim() ? { name: emptyName.trim(), owner: emptyOwner || undefined } : null,
      };
      await onScan(choice, selection);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("addRepository.source.newProjectFailed"));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="space-y-4">
      <Card>
        <CardContent className="space-y-3 pt-6">
          <CardTitle className="text-body">{t("addRepository.source.heading")}</CardTitle>
          <div className="flex flex-col gap-3 sm:flex-row sm:items-start">
            <Tabs value={mode} onValueChange={(v) => setMode(v as "existing" | "new")} variant="pill" className="w-fit shrink-0">
              <TabsList>
                <TabsTrigger value="existing">{t("addRepository.source.existingProject")}</TabsTrigger>
                <TabsTrigger value="new">{t("addRepository.source.newProject")}</TabsTrigger>
              </TabsList>
            </Tabs>
            <div className="flex-1">
              {mode === "existing" ? (
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
          {mode === "new" && <p className="text-caption text-muted-foreground">{t("addRepository.source.newProjectHint")}</p>}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <div className="flex items-center gap-2">
            <FolderOpen className="h-4 w-4 text-muted-foreground" aria-hidden />
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
              placeholder={t("addRepository.source.folderPathPlaceholder")}
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

      <Card>
        <CardHeader>
          <div className="flex items-center gap-2">
            <FilePlus className="h-4 w-4 text-muted-foreground" aria-hidden />
            <CardTitle>{t("addRepository.source.emptyTitle")}</CardTitle>
          </div>
          <CardDescription>{t("addRepository.source.emptyDescription")}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="space-y-1.5">
            <Label>{t("addRepository.source.emptyNameLabel")}</Label>
            <Input
              value={emptyName}
              onChange={(e) => setEmptyName(e.target.value)}
              placeholder={t("addRepository.source.emptyNamePlaceholder")}
            />
          </div>
          {owners.length > 0 && (
            <div className="space-y-1.5">
              <Label>{t("addRepository.source.emptyOwnerLabel")}</Label>
              <Select value={emptyOwner} onValueChange={setEmptyOwner}>
                <SelectTrigger>
                  <SelectValue placeholder={t("addRepository.source.emptyOwnerPlaceholder")} />
                </SelectTrigger>
                <SelectContent>
                  {owners.map((o) => (
                    <SelectItem key={o.login} value={o.login}>
                      {o.login} {o.type === "org" ? t("addRepository.source.githubOrgSuffix") : ""}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}
        </CardContent>
      </Card>

      <p className="text-caption text-muted-foreground">{t("addRepository.source.descriptionHint")}</p>

      <div className="flex justify-end">
        <Button size="lg" disabled={!canScan} onClick={handleScan}>
          {submitting && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />}
          {t("addRepository.source.scanButton", { count })}
        </Button>
      </div>
    </div>
  );
}
