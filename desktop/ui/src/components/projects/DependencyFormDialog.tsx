import { Loader2 } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import {
  api,
  type DatabaseEngine,
  type DependencyTargetKind,
  type RepoDependency,
  type Repository,
  type SaveRepoDependencyRequest,
} from "@/api";
import { FormDialog } from "@/components/admin/FormDialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";
import { dependencyTargetRepositories } from "@/lib/dependencyTargets";

const TARGET_KINDS: DependencyTargetKind[] = ["repo", "sub_repo", "database"];
const DATABASE_ENGINES: DatabaseEngine[] = ["postgres", "mysql", "mongodb", "redis", "other"];
const DATABASE_ENVS: Array<"stage" | "prod"> = ["stage", "prod"];
const NO_ENGINE = "__none__";

interface DependencyFormDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  repositoryId: string;
  /** Every other repository, used to pick a `repo`/`sub_repo` target. */
  repositories: Repository[];
  /** The dependency being edited, or null to create one. */
  dependency?: RepoDependency | null;
  onSaved: (dependency: RepoDependency) => void;
}

/**
 * Create or edit one outgoing dependency edge: another repository, a
 * sub-project of one, or a manually-recorded database. The database secret
 * field only ever shows what the server sent back (masked or empty) — see
 * `RepoDependency.database_secret` — so leaving it untouched on an edit keeps
 * the stored value, and there is no way to make the real value appear here.
 */
export function DependencyFormDialog({
  open,
  onOpenChange,
  repositoryId,
  repositories,
  dependency = null,
  onSaved,
}: DependencyFormDialogProps) {
  const { t } = useI18n();
  const [targetKind, setTargetKind] = useState<DependencyTargetKind>("repo");
  const [targetRepositoryId, setTargetRepositoryId] = useState("");
  const [subProjectPath, setSubProjectPath] = useState("");
  const [databaseLabel, setDatabaseLabel] = useState("");
  const [databaseEngine, setDatabaseEngine] = useState<DatabaseEngine | "">("");
  const [databaseEnv, setDatabaseEnv] = useState<"stage" | "prod" | "">("");
  const [databaseHost, setDatabaseHost] = useState("");
  const [databasePort, setDatabasePort] = useState("");
  const [databaseName, setDatabaseName] = useState("");
  const [databaseUsername, setDatabaseUsername] = useState("");
  const [databaseSecret, setDatabaseSecret] = useState("");
  const [note, setNote] = useState("");
  const [saving, setSaving] = useState(false);

  // Seeded on open rather than from an initialiser: the same mounted dialog
  // is reopened for a different dependency, and useState would keep the
  // first one's values forever.
  useEffect(() => {
    if (!open) return;
    setTargetKind(dependency?.target_kind ?? "repo");
    setTargetRepositoryId(dependency?.target_repository_id ?? "");
    setSubProjectPath(dependency?.target_sub_project_path ?? "");
    setDatabaseLabel(dependency?.database_label ?? "");
    setDatabaseEngine(dependency?.database_engine ?? "");
    setDatabaseEnv((dependency?.database_env as "stage" | "prod" | undefined) ?? "");
    setDatabaseHost(dependency?.database_host ?? "");
    setDatabasePort(dependency?.database_port ? String(dependency.database_port) : "");
    setDatabaseName(dependency?.database_name ?? "");
    setDatabaseUsername(dependency?.database_username ?? "");
    setDatabaseSecret(dependency?.database_secret ?? "");
    setNote(dependency?.note ?? "");
  }, [open, dependency]);

  const targetRepositoryOptions = dependencyTargetRepositories(repositories, repositoryId, targetKind);
  const targetRepository = repositories.find((r) => r.id === targetRepositoryId) ?? null;
  const targetSubProjects = targetRepository?.sub_projects ?? [];

  const subProjectLabel = (path: string) => (path === "." ? t("projectAdmin.initialSetup.subProjectRoot") : path);

  const canSave =
    targetKind === "database"
      ? databaseLabel.trim().length > 0 && databaseEnv.length > 0
      : targetKind === "repo"
        ? targetRepositoryId.length > 0
        : targetRepositoryId.length > 0 && subProjectPath.length > 0;

  const save = async () => {
    if (!canSave) return;
    setSaving(true);
    try {
      const req: SaveRepoDependencyRequest = { target_kind: targetKind, note: note.trim() };
      if (targetKind === "repo" || targetKind === "sub_repo") {
        req.target_repository_id = targetRepositoryId;
      }
      if (targetKind === "sub_repo") {
        req.target_sub_project_path = subProjectPath;
      }
      if (targetKind === "database") {
        req.database_label = databaseLabel.trim();
        req.database_engine = databaseEngine || undefined;
        req.database_env = databaseEnv || undefined;
        req.database_host = databaseHost.trim();
        req.database_port = databasePort ? Number(databasePort) : undefined;
        req.database_name = databaseName.trim();
        req.database_username = databaseUsername.trim();
        req.database_secret = databaseSecret;
      }
      const saved = dependency
        ? await api.updateRepoDependency(repositoryId, dependency.id, req)
        : await api.createRepoDependency(repositoryId, req);
      toast.success(dependency ? t("projectAdmin.dependencies.updated") : t("projectAdmin.dependencies.created"));
      onOpenChange(false);
      onSaved(saved);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("projectAdmin.dependencies.saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <FormDialog
      open={open}
      onOpenChange={onOpenChange}
      title={dependency ? t("projectAdmin.dependencies.editTitle") : t("projectAdmin.dependencies.newTitle")}
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t("common.cancel")}
          </Button>
          <Button onClick={() => void save()} disabled={saving || !canSave}>
            {saving && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            {dependency ? t("common.save") : t("projectAdmin.dependencies.add")}
          </Button>
        </>
      }
    >
      <div className="space-y-2">
        <Label>{t("projectAdmin.dependencies.targetKindLabel")}</Label>
        <Select value={targetKind} onValueChange={(v) => setTargetKind(v as DependencyTargetKind)}>
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {TARGET_KINDS.map((k) => (
              <SelectItem key={k} value={k}>
                {t(`projectAdmin.dependencies.targetKinds.${k}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {(targetKind === "repo" || targetKind === "sub_repo") && (
        <div className="space-y-2">
          <Label>{t("projectAdmin.dependencies.targetRepositoryLabel")}</Label>
          <Select
            value={targetRepositoryId}
            onValueChange={(v) => {
              setTargetRepositoryId(v);
              setSubProjectPath("");
            }}
          >
            <SelectTrigger>
              <SelectValue placeholder={t("projectAdmin.dependencies.targetRepositoryPlaceholder")} />
            </SelectTrigger>
            <SelectContent>
              {targetRepositoryOptions.map((r) => (
                <SelectItem key={r.id} value={r.id}>
                  {r.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      )}

      {targetKind === "sub_repo" && (
        <div className="space-y-2">
          <Label>{t("projectAdmin.dependencies.subProjectLabel")}</Label>
          {targetSubProjects.length === 0 ? (
            <p className="text-sm text-muted-foreground">{t("projectAdmin.dependencies.noSubProjects")}</p>
          ) : (
            <Select value={subProjectPath} onValueChange={setSubProjectPath}>
              <SelectTrigger>
                <SelectValue placeholder={t("projectAdmin.dependencies.subProjectPlaceholder")} />
              </SelectTrigger>
              <SelectContent>
                {targetSubProjects.map((sp) => (
                  <SelectItem key={sp.path} value={sp.path}>
                    {subProjectLabel(sp.path)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
        </div>
      )}

      {targetKind === "database" && (
        <>
          <div className="space-y-2">
            <Label htmlFor="dep-db-label">{t("projectAdmin.dependencies.databaseLabelLabel")}</Label>
            <Input
              id="dep-db-label"
              value={databaseLabel}
              onChange={(e) => setDatabaseLabel(e.target.value)}
              placeholder={t("projectAdmin.dependencies.databaseLabelPlaceholder")}
            />
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <Label>{t("projectAdmin.dependencies.databaseEngineLabel")}</Label>
              <Select
                value={databaseEngine || NO_ENGINE}
                onValueChange={(v) => setDatabaseEngine(v === NO_ENGINE ? "" : (v as DatabaseEngine))}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={NO_ENGINE}>{t("projectAdmin.dependencies.databaseEngineNone")}</SelectItem>
                  {DATABASE_ENGINES.map((eng) => (
                    <SelectItem key={eng} value={eng}>
                      {t(`projectAdmin.dependencies.engines.${eng}`)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label>{t("projectAdmin.dependencies.databaseEnvLabel")}</Label>
              <Select value={databaseEnv} onValueChange={(v) => setDatabaseEnv(v as "stage" | "prod")}>
                <SelectTrigger>
                  <SelectValue placeholder={t("projectAdmin.dependencies.databaseEnvLabel")} />
                </SelectTrigger>
                <SelectContent>
                  {DATABASE_ENVS.map((env) => (
                    <SelectItem key={env} value={env}>
                      {t(`projectAdmin.dependencies.environments.${env}`)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="dep-db-host">{t("projectAdmin.dependencies.databaseHostLabel")}</Label>
              <Input id="dep-db-host" value={databaseHost} onChange={(e) => setDatabaseHost(e.target.value)} />
            </div>
            <div className="space-y-2">
              <Label htmlFor="dep-db-port">{t("projectAdmin.dependencies.databasePortLabel")}</Label>
              <Input
                id="dep-db-port"
                type="number"
                min={0}
                max={65535}
                value={databasePort}
                onChange={(e) => setDatabasePort(e.target.value)}
              />
            </div>
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="dep-db-name">{t("projectAdmin.dependencies.databaseNameLabel")}</Label>
              <Input id="dep-db-name" value={databaseName} onChange={(e) => setDatabaseName(e.target.value)} />
            </div>
            <div className="space-y-2">
              <Label htmlFor="dep-db-username">{t("projectAdmin.dependencies.databaseUsernameLabel")}</Label>
              <Input
                id="dep-db-username"
                value={databaseUsername}
                onChange={(e) => setDatabaseUsername(e.target.value)}
              />
            </div>
          </div>
          <div className="space-y-2">
            <Label htmlFor="dep-db-secret">{t("projectAdmin.dependencies.databaseSecretLabel")}</Label>
            <Input
              id="dep-db-secret"
              type="password"
              autoComplete="off"
              value={databaseSecret}
              onChange={(e) => setDatabaseSecret(e.target.value)}
            />
            <p className="text-xs text-muted-foreground">{t("projectAdmin.dependencies.databaseSecretHint")}</p>
          </div>
        </>
      )}

      <div className="space-y-2">
        <Label htmlFor="dep-note">{t("projectAdmin.dependencies.noteLabel")}</Label>
        <Textarea
          id="dep-note"
          value={note}
          onChange={(e) => setNote(e.target.value)}
          rows={2}
          placeholder={t("projectAdmin.dependencies.notePlaceholder")}
        />
      </div>
    </FormDialog>
  );
}
