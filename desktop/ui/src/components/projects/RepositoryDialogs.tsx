import { FolderOpen, Loader2, Lock } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { api, type GitHubOwner, type GitHubRepoInfo, type InitiativeProject } from "@/api";
import { desktopRunner } from "@/lib/desktop-bridge";
import { MultiSelectPicker } from "@/components/admin/MultiSelectPicker";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";

// GitHub owner (hesap/org) seçimi — import ve create dialoglarında ortak.
function useGitHubOwners(open: boolean) {
  const { t } = useI18n();
  const [owners, setOwners] = useState<GitHubOwner[]>([]);
  const [owner, setOwner] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    if (!open) return;
    api
      .githubOwners()
      .then((d) => {
        const list = d.owners ?? [];
        setOwners(list);
        setError("");
        if (list.length > 0) setOwner((prev) => prev || list[0].login);
      })
      .catch((e) => {
        setOwners([]);
        setError(e instanceof Error ? e.message : t("projectAdmin.components.githubOwnersFailed"));
      });
  }, [open, t]);

  return { owners, owner, setOwner, error };
}

function OwnerSelect({
  owners,
  owner,
  onChange,
}: {
  owners: GitHubOwner[];
  owner: string;
  onChange: (v: string) => void;
}) {
  const { t } = useI18n();
  return (
    <div className="space-y-2">
      <Label>{t("projectAdmin.components.githubAccountOrg")}</Label>
      <Select value={owner} onValueChange={onChange}>
        <SelectTrigger>
          <SelectValue placeholder={t("projectAdmin.components.selectAccount")} />
        </SelectTrigger>
        <SelectContent>
          {owners.map((o) => (
            <SelectItem key={o.login} value={o.login}>
              {o.login} {o.type === "org" ? t("projectAdmin.components.orgSuffix") : ""}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}

interface ImportGitHubDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSuccess: (repositoryId: string) => void;
  /**
   * Opened from inside a project: the import is linked to it automatically,
   * with no project picker step. Omitted, this behaves exactly as before —
   * unlinked to any project.
   */
  lockedProjectId?: string;
  /** See RepositoryAnalyzingDialog. Both optional: NoProjectsNotice renders
   * OpenRepositoryDialog without them, and the analyzing overlay is then
   * simply never shown for that caller. */
  onAnalyzeStart?: (label: string) => void;
  onAnalyzeEnd?: () => void;
}

export function ImportGitHubDialog({
  open,
  onOpenChange,
  onSuccess,
  lockedProjectId,
  onAnalyzeStart,
  onAnalyzeEnd,
}: ImportGitHubDialogProps) {
  const { t } = useI18n();
  const { owners, owner, setOwner, error } = useGitHubOwners(open);
  const [repos, setRepos] = useState<GitHubRepoInfo[]>([]);
  const [loadingRepos, setLoadingRepos] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [search, setSearch] = useState("");
  const [importing, setImporting] = useState(false);
  const [existingNames, setExistingNames] = useState<Set<string>>(new Set());

  useEffect(() => {
    if (!open) return;
    api
      .listRepositories()
      .then((d) => setExistingNames(new Set((d.repositories ?? []).map((r) => r.name))))
      .catch(() => setExistingNames(new Set()));
  }, [open]);

  const loadRepos = useCallback(async (login: string) => {
    if (!login) return;
    setLoadingRepos(true);
    setSelected(new Set());
    try {
      const d = await api.githubOwnerRepos(login);
      setRepos(d.repos ?? []);
    } catch (e) {
      setRepos([]);
      toast.error(e instanceof Error ? e.message : t("projectAdmin.components.repoListFailed"));
    } finally {
      setLoadingRepos(false);
    }
  }, []);

  useEffect(() => {
    if (open && owner) void loadRepos(owner);
  }, [open, owner, loadRepos]);

  const toggle = (name: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  };

  const handleImport = async () => {
    if (selected.size === 0) return;
    setImporting(true);
    let lastId = "";
    let fail = 0;
    try {
      for (const name of selected) {
        const repo = repos.find((r) => r.name === name);
        onAnalyzeStart?.(name);
        try {
          const created = await api.importGitHubRepository({
            owner,
            name,
            clone_url: repo?.clone_url,
            description: repo?.description || `GitHub: ${owner}/${name}`,
            ...(lockedProjectId ? { project_ids: [lockedProjectId] } : {}),
          });
          lastId = created.id;
        } catch (e) {
          fail += 1;
          toast.error(`${name}: ${e instanceof Error ? e.message : t("projectAdmin.components.importFailed")}`);
        }
      }
    } finally {
      // Ended once, after the whole batch — not per iteration, or the
      // analyzing dialog would flicker closed and reopen between repos.
      onAnalyzeEnd?.();
    }
    setImporting(false);
    const okCount = selected.size - fail;
    if (okCount > 0) {
      toast.success(t("projectAdmin.components.reposImported", { count: okCount }));
      onOpenChange(false);
      setSelected(new Set());
      onSuccess(lastId);
    }
  };

  const visible = repos.filter(
    (r) => !search.trim() || r.name.toLowerCase().includes(search.trim().toLowerCase()),
  );

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>{t("projectAdmin.components.importFromGitHub")}</DialogTitle>
        </DialogHeader>
        <div className="space-y-4">
          {error ? (
            <p className="text-sm text-destructive">{error}</p>
          ) : (
            <>
              <OwnerSelect owners={owners} owner={owner} onChange={setOwner} />
              <Input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder={t("projectAdmin.components.searchRepo")}
              />
              <ScrollArea className="h-64 rounded-md border">
                {loadingRepos ? (
                  <p className="p-4 text-sm text-muted-foreground">{t("projectAdmin.components.loading")}</p>
                ) : visible.length === 0 ? (
                  <p className="p-4 text-sm text-muted-foreground">{t("projectAdmin.components.noReposFound")}</p>
                ) : (
                  <div className="divide-y divide-border/60">
                    {visible.map((r) => {
                      const imported = existingNames.has(r.name);
                      return (
                        <label
                          key={r.full_name}
                          className={`flex cursor-pointer items-center gap-3 px-3 py-2 hover:bg-muted/40 ${imported ? "opacity-50" : ""}`}
                        >
                          <Checkbox
                            checked={selected.has(r.name)}
                            disabled={imported}
                            onCheckedChange={() => toggle(r.name)}
                          />
                          <span className="flex-1 truncate text-sm">{r.name}</span>
                          {r.private && <Lock className="h-3 w-3 text-muted-foreground" />}
                          {imported && <Badge variant="outline">{t("projectAdmin.components.added")}</Badge>}
                        </label>
                      );
                    })}
                  </div>
                )}
              </ScrollArea>
              <p className="text-xs text-muted-foreground">
                {t("projectAdmin.components.importHelp")}
              </p>
            </>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t("common.cancel")}
          </Button>
          <Button onClick={handleImport} disabled={importing || selected.size === 0}>
            {importing && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            {importing ? t("projectAdmin.components.importing") : t("projectAdmin.components.importCount", { count: selected.size })}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

interface OpenRepositoryDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSuccess: (repositoryId: string) => void;
  /** See ImportGitHubDialogProps.lockedProjectId. */
  lockedProjectId?: string;
  /** See ImportGitHubDialogProps.onAnalyzeStart/onAnalyzeEnd. */
  onAnalyzeStart?: (label: string) => void;
  onAnalyzeEnd?: () => void;
}

export function OpenRepositoryDialog({
  open,
  onOpenChange,
  onSuccess,
  lockedProjectId,
  onAnalyzeStart,
  onAnalyzeEnd,
}: OpenRepositoryDialogProps) {
  const { t } = useI18n();
  const [rootPath, setRootPath] = useState("");
  const [description, setDescription] = useState("");
  const [projectIds, setProjectIds] = useState<string[]>([]);
  const [initiativeProjects, setInitiativeProjects] = useState<InitiativeProject[]>([]);
  const [loading, setLoading] = useState(false);
  const runner = desktopRunner();
  const canBrowse = typeof runner?.chooseDirectory === "function";

  const handleBrowse = async () => {
    if (!runner?.chooseDirectory) return;
    try {
      const chosen = await runner.chooseDirectory({
        title: t("projectAdmin.components.chooseFolderTitle"),
        defaultPath: rootPath.trim() || undefined,
      });
      if (chosen) {
        setRootPath(chosen);
      }
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to open folder picker");
    }
  };

  useEffect(() => {
    if (!open || lockedProjectId) return;
    api
      .listInitiativeProjects()
      .then((d) => setInitiativeProjects(d.projects ?? []))
      .catch(() => setInitiativeProjects([]));
  }, [open, lockedProjectId]);

  const handleSubmit = async () => {
    if (!rootPath.trim()) {
      toast.error(t("projectAdmin.components.folderPathRequired"));
      return;
    }
    if (!description.trim()) {
      toast.error(t("projectAdmin.components.repoDescriptionRequired"));
      return;
    }
    setLoading(true);
    onAnalyzeStart?.(rootPath.trim());
    try {
      const repo = await api.openRepository(
        rootPath.trim(),
        description.trim(),
        lockedProjectId ? [lockedProjectId] : projectIds.length > 0 ? projectIds : undefined,
      );
      toast.success(t("projectAdmin.components.repoOpened"));
      onOpenChange(false);
      onSuccess(repo.id);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("projectAdmin.components.repoOpenFailed"));
    } finally {
      setLoading(false);
      onAnalyzeEnd?.();
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("projectAdmin.components.openRepoTitle")}</DialogTitle>
        </DialogHeader>
        <div className="space-y-4">
          <div className="space-y-2">
            <Label>{t("projectAdmin.components.folder")}</Label>
            <div className="flex gap-2">
              <Input
                value={rootPath}
                onChange={(e) => setRootPath(e.target.value)}
                placeholder="/path/to/project"
                className="flex-1"
              />
              {canBrowse && (
                <Button
                  type="button"
                  variant="outline"
                  onClick={handleBrowse}
                  title={t("projectAdmin.components.browseFolder")}
                  className="shrink-0"
                >
                  <FolderOpen className="h-4 w-4 mr-1.5" />
                  {t("projectAdmin.components.browseFolder")}
                </Button>
              )}
            </div>
          </div>
          <div className="space-y-2">
            <Label>{t("projectAdmin.components.repoAbout")}</Label>
            <Textarea
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder={t("projectAdmin.components.repoAboutPlaceholder")}
              rows={4}
            />
          </div>
          {/* Opened from inside a project, the link is automatic — not a
              separate step — so the picker that would otherwise ask for it
              is skipped entirely rather than shown pre-selected. */}
          {!lockedProjectId && initiativeProjects.length > 0 && (
            <MultiSelectPicker
              label={t("projectAdmin.components.linkedProjects")}
              options={initiativeProjects.map((p) => ({ value: p.id, label: p.name }))}
              selected={projectIds}
              onChange={setProjectIds}
              emptyText={t("projectAdmin.components.noProjectsYet")}
            />
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t("common.cancel")}
          </Button>
          <Button onClick={handleSubmit} disabled={loading}>
            {loading && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            {t("projectAdmin.components.open")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

interface CreateRepositoryDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSuccess: (repositoryId: string) => void;
  /** See ImportGitHubDialogProps.lockedProjectId. */
  lockedProjectId?: string;
}

export function CreateRepositoryDialog({
  open,
  onOpenChange,
  onSuccess,
  lockedProjectId,
}: CreateRepositoryDialogProps) {
  const { t } = useI18n();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [projectIds, setProjectIds] = useState<string[]>([]);
  const [initiativeProjects, setInitiativeProjects] = useState<InitiativeProject[]>([]);
  const [loading, setLoading] = useState(false);
  const { owners, owner, setOwner, error: ownersError } = useGitHubOwners(open);

  useEffect(() => {
    if (!open || lockedProjectId) return;
    api
      .listInitiativeProjects()
      .then((d) => setInitiativeProjects(d.projects ?? []))
      .catch(() => setInitiativeProjects([]));
  }, [open, lockedProjectId]);

  const handleSubmit = async () => {
    if (!name.trim()) {
      toast.error(t("projectAdmin.components.repoNameRequired"));
      return;
    }
    if (!description.trim()) {
      toast.error(t("projectAdmin.components.repoDescriptionRequired"));
      return;
    }
    setLoading(true);
    try {
      const repo = await api.createRepository(
        name.trim(),
        // The parent directory was a desktop-only field (native picker); the
        // browser has always sent it empty and the bridge picks its own root.
        "",
        description.trim(),
        lockedProjectId ? [lockedProjectId] : projectIds.length > 0 ? projectIds : undefined,
        owner || undefined,
      );
      toast.success(t("projectAdmin.components.repoCreated"));
      onOpenChange(false);
      onSuccess(repo.id);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("projectAdmin.components.repoCreateFailed"));
    } finally {
      setLoading(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("projectAdmin.components.createRepoTitle")}</DialogTitle>
        </DialogHeader>
        <div className="space-y-4">
          <div className="space-y-2">
            <Label>{t("projectAdmin.components.repoName")}</Label>
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="my-app" />
          </div>
          {ownersError ? (
            <p className="text-sm text-destructive">
              {t("projectAdmin.components.githubRequired", { error: ownersError })}
            </p>
          ) : (
            <OwnerSelect owners={owners} owner={owner} onChange={setOwner} />
          )}
          <div className="space-y-2">
            <Label>{t("projectAdmin.components.repoAbout")}</Label>
            <Textarea
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder={t("projectAdmin.components.repoAboutPlaceholder")}
              rows={4}
            />
          </div>
          {!lockedProjectId && initiativeProjects.length > 0 && (
            <MultiSelectPicker
              label={t("projectAdmin.components.linkedProjects")}
              options={initiativeProjects.map((p) => ({ value: p.id, label: p.name }))}
              selected={projectIds}
              onChange={setProjectIds}
              emptyText={t("projectAdmin.components.noProjectsYet")}
            />
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t("common.cancel")}
          </Button>
          <Button onClick={handleSubmit} disabled={loading}>
            {loading && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            {t("projectAdmin.components.create")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export { OpenRepositoryDialog as OpenProjectDialog, CreateRepositoryDialog as CreateProjectDialog };
