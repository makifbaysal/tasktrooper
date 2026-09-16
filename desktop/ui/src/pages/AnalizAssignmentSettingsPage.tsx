import { Save } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { api, type Agent } from "@/api";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";

const DEFAULT_AGENT = "system-architect";

type Area = "backend" | "frontend" | "mobile";

interface AreaAssignment {
  backend: string;
  frontend: string;
  mobile: string;
}

const AREAS: Area[] = ["backend", "frontend", "mobile"];

export function AnalizAssignmentSettingsPage() {
  const { t } = useI18n();
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [agents, setAgents] = useState<Agent[]>([]);
  const [saved, setSaved] = useState<AreaAssignment>({
    backend: DEFAULT_AGENT,
    frontend: DEFAULT_AGENT,
    mobile: DEFAULT_AGENT,
  });
  const [selected, setSelected] = useState<AreaAssignment>({
    backend: DEFAULT_AGENT,
    frontend: DEFAULT_AGENT,
    mobile: DEFAULT_AGENT,
  });
  const [missingTools, setMissingTools] = useState<Record<string, string[]> | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [settings, agentsRes] = await Promise.all([api.getSettings(), api.listAgents()]);
      const next: AreaAssignment = {
        backend: settings.analiz_assignee_backend || DEFAULT_AGENT,
        frontend: settings.analiz_assignee_frontend || DEFAULT_AGENT,
        mobile: settings.analiz_assignee_mobile || DEFAULT_AGENT,
      };
      setSaved(next);
      setSelected(next);
      setAgents((agentsRes.agents ?? []).filter((a) => a.enabled));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("settingsPages.analizAssignment.loadFailed"));
    } finally {
      setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  // Only the areas the user actually touched travel in the request; "" is the
  // backend's own "leave this area unchanged" value, so an area nobody edited
  // is never re-validated against RequiredAnalizTools.
  const buildRequest = (confirm: boolean) => ({
    backend: selected.backend !== saved.backend ? selected.backend : "",
    frontend: selected.frontend !== saved.frontend ? selected.frontend : "",
    mobile: selected.mobile !== saved.mobile ? selected.mobile : "",
    confirm_grant_tools: confirm,
  });

  const save = async (confirm: boolean) => {
    const req = buildRequest(confirm);
    if (!req.backend && !req.frontend && !req.mobile) return;
    setSaving(true);
    try {
      const result = await api.updateAnalizAssignment(req);
      if (!result.saved) {
        setMissingTools(result.missing_tools ?? {});
        return;
      }
      const next: AreaAssignment = {
        backend: result.settings?.analiz_assignee_backend || saved.backend,
        frontend: result.settings?.analiz_assignee_frontend || saved.frontend,
        mobile: result.settings?.analiz_assignee_mobile || saved.mobile,
      };
      setSaved(next);
      setSelected(next);
      setMissingTools(null);
      toast.success(t("settingsPages.analizAssignment.savedToast"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("settingsPages.analizAssignment.saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  const cancelMissingTools = () => {
    setMissingTools(null);
    setSelected(saved);
    toast.info(t("settingsPages.analizAssignment.discardedToast"));
  };

  const areaLabel = (area: Area) => t(`settingsPages.analizAssignment.areas.${area}`);

  if (loading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-64 rounded-xl" />
      </div>
    );
  }

  return (
    <div className="space-y-6 pb-8">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="font-semibold">{t("settingsPages.analizAssignment.title")}</h2>
          <p className="mt-0.5 text-sm text-muted-foreground">
            {t("settingsPages.analizAssignment.subtitle")}
          </p>
        </div>
        <Button onClick={() => void save(false)} disabled={saving} className="gap-2">
          <Save className="h-4 w-4" />
          {saving ? t("common.saving") : t("common.save")}
        </Button>
      </div>

      <Card className="space-y-5 p-6">
        {AREAS.map((area) => (
          <div key={area} className="space-y-2">
            <Label htmlFor={`analiz-assignee-${area}`}>{areaLabel(area)}</Label>
            <Select
              value={selected[area]}
              onValueChange={(value) => setSelected((prev) => ({ ...prev, [area]: value }))}
            >
              <SelectTrigger id={`analiz-assignee-${area}`}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={DEFAULT_AGENT}>{DEFAULT_AGENT}</SelectItem>
                {agents
                  .filter((a) => a.name !== DEFAULT_AGENT)
                  .map((a) => (
                    <SelectItem key={a.id} value={a.name}>
                      {a.name}
                    </SelectItem>
                  ))}
              </SelectContent>
            </Select>
          </div>
        ))}
      </Card>

      <Dialog open={missingTools !== null} onOpenChange={(open) => !open && cancelMissingTools()}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("settingsPages.analizAssignment.missingToolsTitle")}</DialogTitle>
          </DialogHeader>
          <div className="space-y-3 text-sm">
            <p className="text-muted-foreground">{t("settingsPages.analizAssignment.missingToolsBody")}</p>
            {missingTools &&
              Object.entries(missingTools).map(([area, tools]) => (
                <div key={area} className="rounded-lg border border-border p-3">
                  <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                    {areaLabel(area as Area)}
                  </div>
                  <div className="mt-1 flex flex-wrap gap-1.5">
                    {tools.map((tool) => (
                      <code key={tool} className="rounded bg-muted px-1.5 py-0.5 text-xs">
                        {tool}
                      </code>
                    ))}
                  </div>
                </div>
              ))}
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={cancelMissingTools}>
              {t("common.cancel")}
            </Button>
            <Button onClick={() => void save(true)} disabled={saving}>
              {t("settingsPages.analizAssignment.grantTools")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
