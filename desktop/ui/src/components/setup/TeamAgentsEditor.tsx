import { useCallback, useEffect, useMemo, useState } from "react";
import { api, type Agent } from "@/api";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { useI18n } from "@/hooks/useI18n";
import {
  TEAM_TEMPLATES,
  agentUpdate,
  coreAgentIds,
  templateAgentIds,
  type TeamTemplateId,
} from "@/lib/teamTemplates";

const EMPTY_RETRY_MS = 2000;
const EMPTY_RETRIES = 15;

interface TeamAgentsEditorProps {
  /** First run starts from nothing and needs a template pick; editing starts from the current enabled set. */
  startFrom: "template" | "current";
  confirmLabel: string;
  /** "dialog" scrolls the body inside its parent's height and pins the save button below it. */
  layout?: "page" | "dialog";
  onSaved: () => void;
}

/**
 * Template presets plus one switch per catalog agent. Saving writes `enabled`
 * only for the agents whose flag changed, through the agents API; the core
 * agents are pinned on.
 */
export function TeamAgentsEditor({
  startFrom,
  confirmLabel,
  layout = "page",
  onSaved,
}: TeamAgentsEditorProps) {
  const { t } = useI18n();
  const [agents, setAgents] = useState<Agent[] | null>(null);
  const [loadError, setLoadError] = useState("");
  const [templateId, setTemplateId] = useState<TeamTemplateId | null>(null);
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set());
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState("");
  const [emptyRetries, setEmptyRetries] = useState(0);

  const load = useCallback(async () => {
    setLoadError("");
    try {
      const { agents: list } = await api.listAgents();
      const loaded = list ?? [];
      setAgents(loaded);
      if (startFrom === "current") {
        const ids = new Set(
          loaded.filter((agent) => agent.enabled).map((agent) => agent.id),
        );
        for (const id of coreAgentIds(loaded)) ids.add(id);
        setSelected(ids);
      }
    } catch (e) {
      setAgents(null);
      setLoadError(e instanceof Error ? e.message : t("setup.team.loadFailed"));
    }
  }, [t, startFrom]);

  useEffect(() => {
    void load();
  }, [load]);

  // A workspace created a moment ago gets its catalog agents a few seconds
  // later (the pod's first-sight sync), so an empty roster is asked again.
  useEffect(() => {
    if (!agents || agents.length > 0 || emptyRetries >= EMPTY_RETRIES) return;
    const timer = setTimeout(() => {
      setEmptyRetries((n) => n + 1);
      void load();
    }, EMPTY_RETRY_MS);
    return () => clearTimeout(timer);
  }, [agents, emptyRetries, load]);

  const preparing = agents !== null && agents.length === 0 && emptyRetries < EMPTY_RETRIES;

  const coreIds = useMemo(
    () => (agents ? new Set(coreAgentIds(agents)) : new Set<string>()),
    [agents],
  );

  const pickTemplate = (id: TeamTemplateId) => {
    if (!agents) return;
    const template = TEAM_TEMPLATES.find((entry) => entry.id === id);
    if (!template) return;
    const ids = new Set(templateAgentIds(agents, template));
    for (const coreId of coreIds) ids.add(coreId);
    setTemplateId(id);
    setSelected(ids);
    setSaveError("");
  };

  const toggle = (id: string, on: boolean) => {
    if (coreIds.has(id) && !on) return;
    setSelected((prev) => {
      const next = new Set(prev);
      if (on) next.add(id);
      else next.delete(id);
      return next;
    });
  };

  const ready = startFrom === "current" || templateId !== null;

  const confirm = async () => {
    if (!agents || !ready) return;
    setSaving(true);
    setSaveError("");
    try {
      const changed = agents.filter(
        (agent) => selected.has(agent.id) !== agent.enabled,
      );
      await Promise.all(
        changed.map((agent) =>
          api.updateAgent(agent.id, agentUpdate(agent, selected.has(agent.id))),
        ),
      );
      setSaving(false);
      onSaved();
    } catch (e) {
      setSaveError(e instanceof Error ? e.message : t("setup.team.saveFailed"));
      setSaving(false);
    }
  };

  const inDialog = layout === "dialog";

  return (
    <div
      className={inDialog ? "flex min-h-0 flex-1 flex-col gap-3" : "space-y-3"}
    >
      <div
        className={
          inDialog
            ? "min-h-0 flex-1 space-y-3 overflow-y-auto overflow-x-hidden pr-1"
            : "space-y-3"
        }
        data-testid="team-editor-body"
      >
        {loadError && (
          <Notice variant="error" title={t("setup.team.loadFailed")}>
            <p>{loadError}</p>
            <Button
              variant="outline"
              size="sm"
              className="mt-2"
              onClick={() => void load()}
            >
              {t("common.refresh")}
            </Button>
          </Notice>
        )}

        {agents === null && !loadError && (
          <Skeleton className="h-40 rounded-xl" />
        )}

        {agents !== null && (
          <>
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              {TEAM_TEMPLATES.map((template) => (
                <Button
                  key={template.id}
                  variant={templateId === template.id ? "default" : "outline"}
                  className="h-auto min-w-0 flex-col items-start gap-1 whitespace-normal break-words p-4 text-left"
                  onClick={() => pickTemplate(template.id)}
                >
                  <span className="text-body font-medium">
                    {t(template.nameKey)}
                  </span>
                  <span className="text-sm font-normal text-muted-foreground">
                    {t(template.descriptionKey)}
                  </span>
                </Button>
              ))}
            </div>

            {ready && (
              <Card className="divide-y divide-border">
                <div className="flex items-center justify-between gap-3 px-4 py-3">
                  <p className="min-w-0 truncate text-body font-medium">
                    {t("setup.team.agentsLabel")}
                  </p>
                  <p className="shrink-0 text-xs text-muted-foreground">
                    {t("setup.team.coreLocked")}
                  </p>
                </div>
                {preparing && (
                  <p className="px-4 py-3 text-sm text-muted-foreground">{t("setup.team.preparing")}</p>
                )}
                {agents.map((agent) => {
                  const locked = coreIds.has(agent.id);
                  const checked = locked || selected.has(agent.id);
                  return (
                    <div
                      key={agent.id}
                      className="flex min-w-0 items-center justify-between gap-3 px-4 py-3"
                    >
                      <div className="min-w-0">
                        <p className="truncate text-body">{agent.name}</p>
                        {agent.description && (
                          <p className="truncate text-xs text-muted-foreground">
                            {agent.description}
                          </p>
                        )}
                      </div>
                      <Switch
                        className="shrink-0"
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
          </>
        )}
      </div>

      {agents !== null && (
        <div
          className={
            inDialog
              ? "flex shrink-0 justify-end border-t border-border pt-3"
              : undefined
          }
        >
          <Button disabled={!ready || saving} onClick={() => void confirm()}>
            {confirmLabel}
          </Button>
        </div>
      )}
    </div>
  );
}
