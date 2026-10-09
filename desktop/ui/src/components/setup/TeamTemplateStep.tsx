import { useCallback, useEffect, useMemo, useState } from "react";
import { api, type Agent } from "@/api";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { useI18n } from "@/hooks/useI18n";
import { writeFirstRunTeamDone } from "@/lib/firstRun";
import {
  PRODUCT_MANAGER_SLUG,
  TEAM_TEMPLATES,
  agentUpdate,
  findAgentBySlug,
  templateAgentIds,
  type TeamTemplateId,
} from "@/lib/teamTemplates";

/**
 * The team step: a template preselects the catalog agents the user starts with,
 * and every agent can be toggled before the choice is written.
 *
 * Toggling writes `enabled` through the agents API; the product manager is
 * pinned on because the board has no lead without one. The list stays editable
 * afterwards on each agent's settings page.
 */
export function TeamTemplateStep({ onDone }: { onDone: () => void }) {
  const { t } = useI18n();
  const [agents, setAgents] = useState<Agent[] | null>(null);
  const [loadError, setLoadError] = useState("");
  const [templateId, setTemplateId] = useState<TeamTemplateId | null>(null);
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set());
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState("");

  const load = useCallback(async () => {
    setLoadError("");
    try {
      const { agents: list } = await api.listAgents();
      setAgents(list ?? []);
    } catch (e) {
      setAgents(null);
      setLoadError(e instanceof Error ? e.message : t("setup.team.loadFailed"));
    }
  }, [t]);

  useEffect(() => {
    void load();
  }, [load]);

  const pmId = useMemo(
    () => (agents ? (findAgentBySlug(agents, PRODUCT_MANAGER_SLUG)?.id ?? null) : null),
    [agents],
  );

  const pickTemplate = (id: TeamTemplateId) => {
    if (!agents) return;
    const template = TEAM_TEMPLATES.find((entry) => entry.id === id);
    if (!template) return;
    const ids = new Set(templateAgentIds(agents, template));
    if (pmId) ids.add(pmId);
    setTemplateId(id);
    setSelected(ids);
    setSaveError("");
  };

  const toggle = (id: string, on: boolean) => {
    if (id === pmId && !on) return;
    setSelected((prev) => {
      const next = new Set(prev);
      if (on) next.add(id);
      else next.delete(id);
      return next;
    });
  };

  const confirm = async () => {
    if (!agents || !templateId) return;
    setSaving(true);
    setSaveError("");
    try {
      const changed = agents.filter((agent) => selected.has(agent.id) !== agent.enabled);
      await Promise.all(changed.map((agent) => api.updateAgent(agent.id, agentUpdate(agent, selected.has(agent.id)))));
      writeFirstRunTeamDone();
      onDone();
    } catch (e) {
      setSaveError(e instanceof Error ? e.message : t("setup.team.saveFailed"));
      setSaving(false);
    }
  };

  return (
    <div className="space-y-3">
      {loadError && (
        <Notice variant="error" title={t("setup.team.loadFailed")}>
          <p>{loadError}</p>
          <Button variant="outline" size="sm" className="mt-2" onClick={() => void load()}>
            {t("common.refresh")}
          </Button>
        </Notice>
      )}

      {agents === null && !loadError && <Skeleton className="h-40 rounded-xl" />}

      {agents !== null && (
        <>
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {TEAM_TEMPLATES.map((template) => (
              <Button
                key={template.id}
                variant={templateId === template.id ? "default" : "outline"}
                className="h-auto flex-col items-start gap-1 whitespace-normal p-4 text-left"
                onClick={() => pickTemplate(template.id)}
              >
                <span className="text-body font-medium">{t(template.nameKey)}</span>
                <span className="text-sm font-normal text-muted-foreground">{t(template.descriptionKey)}</span>
              </Button>
            ))}
          </div>

          {templateId && (
            <Card className="divide-y divide-border">
              <div className="flex items-center justify-between gap-3 px-4 py-3">
                <p className="text-body font-medium">{t("setup.team.agentsLabel")}</p>
                <p className="text-xs text-muted-foreground">{t("setup.team.pmLocked")}</p>
              </div>
              {agents.map((agent) => {
                const locked = agent.id === pmId;
                const checked = locked || selected.has(agent.id);
                return (
                  <div key={agent.id} className="flex items-center justify-between gap-3 px-4 py-3">
                    <div className="min-w-0">
                      <p className="truncate text-body">{agent.name}</p>
                      {agent.description && (
                        <p className="truncate text-xs text-muted-foreground">{agent.description}</p>
                      )}
                    </div>
                    <Switch
                      checked={checked}
                      disabled={locked}
                      aria-label={agent.name}
                      onCheckedChange={(on) => toggle(agent.id, on)}
                    />
                  </div>
                );
              })}
            </Card>
          )}

          {saveError && (
            <Notice variant="error" title={t("setup.team.saveFailed")}>
              {saveError}
            </Notice>
          )}

          <Button disabled={!templateId || saving} onClick={() => void confirm()}>
            {t("setup.team.confirm")}
          </Button>
        </>
      )}
    </div>
  );
}
