import { Plus, Trash2 } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { api, type IssueSyncSettings, type Repository } from "@/api";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

interface IssueSyncCardProps {
  className?: string;
}

type Mapping = IssueSyncSettings["jira_projects"][number];

/**
 * The Jira project mapping is per project because one global target cannot
 * say which of several repositories a given project's issues belong to.
 */
export function IssueSyncCard({ className }: IssueSyncCardProps) {
  const { t } = useI18n();
  const [settings, setSettings] = useState<IssueSyncSettings | null>(null);
  const [repositories, setRepositories] = useState<Repository[]>([]);
  const [projectKeys, setProjectKeys] = useState<string[]>([]);
  const [saving, setSaving] = useState(false);

  const load = useCallback(async () => {
    setSettings(null);
    try {
      const [sync, repos, projects] = await Promise.allSettled([
        api.getIssueSyncSettings(),
        api.listRepositories(),
        api.listJiraProjects(),
      ]);
      if (sync.status === "rejected") throw sync.reason;
      setSettings(sync.value);
      if (repos.status === "fulfilled") setRepositories(repos.value.repositories ?? []);
      // Without the list the mapping rows fall back to a free-text key, which
      // is what an install with no connected Jira needs anyway.
      if (projects.status === "fulfilled") {
        setProjectKeys((projects.value.projects ?? []).map((p) => p.key));
      }
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("issues.sync.loadFailed"));
    }
  }, [t]);

  useEffect(() => {
    void load();
  }, [load]);

  const patch = (next: Partial<IssueSyncSettings>) =>
    setSettings((current) => (current ? { ...current, ...next } : current));

  const patchMapping = (index: number, next: Partial<Mapping>) =>
    setSettings((current) =>
      current
        ? {
            ...current,
            jira_projects: current.jira_projects.map((row, i) =>
              i === index ? { ...row, ...next } : row,
            ),
          }
        : current,
    );

  const handleSave = async () => {
    if (!settings) return;
    const label = settings.label.trim();
    if (!label) {
      toast.error(t("issues.sync.labelHelp"));
      return;
    }
    setSaving(true);
    try {
      const saved = await api.updateIssueSyncSettings({ ...settings, label });
      setSettings(saved);
      toast.success(t("issues.sync.saved"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  const switches: {
    id: string;
    field: "convert_with_pm" | "github_auto_import" | "jira_auto_import" | "write_back";
    label: string;
    help?: string;
  }[] = [
    {
      id: "issue-sync-convert",
      field: "convert_with_pm",
      label: t("issues.sync.convertWithPm"),
      help: t("issues.sync.convertWithPmHelp"),
    },
    { id: "issue-sync-github", field: "github_auto_import", label: t("issues.sync.githubAuto") },
    { id: "issue-sync-jira", field: "jira_auto_import", label: t("issues.sync.jiraAuto") },
    { id: "issue-sync-write-back", field: "write_back", label: t("issues.sync.writeBack") },
  ];

  return (
    <Card className={cn("w-full space-y-4 p-6", className)}>
      <div className="space-y-1">
        <Label>{t("issues.sync.title")}</Label>
        <p className="text-sm text-muted-foreground">{t("issues.sync.description")}</p>
      </div>
      {!settings ? (
        <Skeleton className="h-64 w-full" />
      ) : (
        <>
          <div className="space-y-2">
            <Label htmlFor="issue-sync-label">{t("issues.sync.label")}</Label>
            <Input
              id="issue-sync-label"
              value={settings.label}
              onChange={(e) => patch({ label: e.target.value })}
            />
            <p className="text-xs text-muted-foreground">{t("issues.sync.labelHelp")}</p>
          </div>

          <div className="space-y-3">
            {switches.map((item) => (
              <div key={item.id} className="flex items-center justify-between gap-4">
                <div className="space-y-0.5">
                  <Label htmlFor={item.id} className="font-normal">
                    {item.label}
                  </Label>
                  {item.help && <p className="text-xs text-muted-foreground">{item.help}</p>}
                </div>
                <Switch
                  id={item.id}
                  checked={settings[item.field]}
                  disabled={saving}
                  onCheckedChange={(checked) => patch({ [item.field]: checked })}
                />
              </div>
            ))}
          </div>

          <div className="space-y-2">
            <Label>{t("issues.sync.jiraProjects")}</Label>
            {settings.jira_projects.map((row, index) => (
              <div key={index} className="flex items-center gap-2">
                {projectKeys.length > 0 ? (
                  <Select
                    value={row.project_key}
                    onValueChange={(v) => patchMapping(index, { project_key: v })}
                  >
                    <SelectTrigger className="flex-1">
                      <SelectValue placeholder={t("issues.import.selectProject")} />
                    </SelectTrigger>
                    <SelectContent>
                      {projectKeys.map((key) => (
                        <SelectItem key={key} value={key}>
                          {key}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                ) : (
                  <Input
                    className="flex-1"
                    value={row.project_key}
                    placeholder={t("issues.sync.projectPlaceholder")}
                    onChange={(e) => patchMapping(index, { project_key: e.target.value.toUpperCase() })}
                  />
                )}
                <Select
                  value={row.repository_id}
                  onValueChange={(v) => patchMapping(index, { repository_id: v })}
                >
                  <SelectTrigger className="flex-1">
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
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label={t("issues.sync.removeMapping")}
                  onClick={() =>
                    patch({
                      jira_projects: settings.jira_projects.filter((_, i) => i !== index),
                    })
                  }
                >
                  <Trash2 className="h-4 w-4" />
                </Button>
              </div>
            ))}
            <Button
              variant="outline"
              size="sm"
              onClick={() =>
                patch({
                  jira_projects: [
                    ...settings.jira_projects,
                    { project_key: "", repository_id: repositories[0]?.id ?? "" },
                  ],
                })
              }
            >
              <Plus className="h-3.5 w-3.5" />
              {t("issues.sync.addMapping")}
            </Button>
          </div>

          <Button onClick={() => void handleSave()} disabled={saving}>
            {saving ? t("common.saving") : t("common.save")}
          </Button>
        </>
      )}
    </Card>
  );
}
