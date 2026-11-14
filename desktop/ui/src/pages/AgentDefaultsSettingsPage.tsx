import { Activity, Package, Save } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { api } from "@/api";
import { SettingsSection } from "@/components/layout/SettingsSection";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { tStatic, useI18n } from "@/hooks/useI18n";

function BoilerplateCatalogCard() {
  const { t } = useI18n();
  const [repo, setRepo] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const data = await api.getSettings();
      setRepo(data.boilerplate_catalog_repo ?? "");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : tStatic("settings.boilerplate.loadFailed"));
    } finally {
      setLoading(false);
    }
    // Not keyed on `t`: a language switch must not discard the unsaved input.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const save = async (value: string) => {
    setSaving(true);
    try {
      const data = await api.updateSettings({ boilerplate_catalog_repo: value.trim() || "-" });
      setRepo(data.boilerplate_catalog_repo ?? "");
      toast.success(t("common.saved"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Card className="w-full space-y-3 p-6">
      <Label htmlFor="boilerplate-repo" className="flex items-center gap-2">
        <Package className="h-4 w-4" />
        {t("settings.boilerplate.title")}
      </Label>
      <p className="text-sm text-muted-foreground">
        {t("settings.boilerplate.descPrefix")}{" "}
        <code className="text-xs">.ai/catalog.yaml</code>{" "}
        {t("settings.boilerplate.descMid")} <code className="text-xs">owner/repo</code>,{" "}
        <code className="text-xs">github.com/owner/repo</code> {t("settings.boilerplate.descSuffix")}
      </p>
      {loading ? (
        <Skeleton className="h-10 w-full" />
      ) : (
        <div className="flex flex-wrap items-center gap-2">
          <Input
            id="boilerplate-repo"
            value={repo}
            onChange={(e) => setRepo(e.target.value)}
            placeholder="github.com/your-org/boilerplates"
            className="min-w-0 flex-1 basis-64"
          />
          <Button onClick={() => void save(repo)} disabled={saving}>
            <Save className="mr-2 h-4 w-4" />
            {saving ? t("common.saving") : t("common.save")}
          </Button>
          <Button variant="outline" size="sm" onClick={() => void save("")} disabled={saving}>
            {t("common.resetDefault")}
          </Button>
        </div>
      )}
    </Card>
  );
}

function ConcurrencyCard() {
  const { t } = useI18n();
  const [agents, setAgents] = useState("");
  const [tasks, setTasks] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const data = await api.getSettings();
      setAgents(String(data.max_concurrent_agents ?? 0));
      setTasks(String(data.max_concurrent_tasks ?? 0));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : tStatic("settings.concurrency.loadFailed"));
    } finally {
      setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const parse = (value: string) => {
    const n = Number.parseInt(value, 10);
    return Number.isFinite(n) && n >= 0 ? n : 0;
  };

  const save = async (nextAgents?: number, nextTasks?: number) => {
    setSaving(true);
    try {
      await api.updateSettings({
        max_concurrent_agents: nextAgents ?? parse(agents),
        max_concurrent_tasks: nextTasks ?? parse(tasks),
      });
      toast.success(t("common.saved"));
      void load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("settings.concurrency.saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Card className="w-full space-y-3 p-6">
      <Label className="flex items-center gap-2">
        <Activity className="h-4 w-4" />
        {t("settings.concurrency.title")}
      </Label>
      {loading ? (
        <Skeleton className="h-10 w-full" />
      ) : (
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-1">
            <Label htmlFor="max-concurrent-agents">{t("settings.concurrency.agentsLabel")}</Label>
            <Input
              id="max-concurrent-agents"
              type="number"
              min={0}
              value={agents}
              onChange={(e) => setAgents(e.target.value)}
              placeholder="0"
            />
            <p className="text-xs text-muted-foreground">
              {t("settings.concurrency.agentsHelp")}{" "}
              <span className="italic">{t("settings.concurrency.zeroHint")}</span>
            </p>
          </div>
          <div className="space-y-1">
            <Label htmlFor="max-concurrent-tasks">{t("settings.concurrency.tasksLabel")}</Label>
            <Input
              id="max-concurrent-tasks"
              type="number"
              min={0}
              value={tasks}
              onChange={(e) => setTasks(e.target.value)}
              placeholder="0"
            />
            <p className="text-xs text-muted-foreground">
              {t("settings.concurrency.tasksHelp")}{" "}
              <span className="italic">{t("settings.concurrency.zeroHint")}</span>
            </p>
          </div>
          <div className="flex flex-wrap items-center gap-2 sm:col-span-2">
            <Button onClick={() => void save()} disabled={saving}>
              <Save className="mr-2 h-4 w-4" />
              {saving ? t("common.saving") : t("common.save")}
            </Button>
            <Button variant="outline" size="sm" onClick={() => void save(0, 0)} disabled={saving}>
              {saving ? t("settings.concurrency.resetting") : t("settings.concurrency.reset")}
            </Button>
          </div>
        </div>
      )}
    </Card>
  );
}

export function AgentDefaultsSettingsPage() {
  const { t } = useI18n();
  return (
    <div className="max-w-3xl space-y-10">
      <SettingsSection title={t("settings.sections.agents.title")} description={t("settings.sections.agents.description")}>
        <ConcurrencyCard />
        <BoilerplateCatalogCard />
      </SettingsSection>
    </div>
  );
}
