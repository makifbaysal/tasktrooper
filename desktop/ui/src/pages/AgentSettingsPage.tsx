import { useCallback, useEffect, useMemo, useState } from "react";
import { useNavigate, useOutletContext, useParams } from "react-router-dom";
import { toast } from "sonner";
import {
  api,
  type Agent,
  type AgentInput,
  type LLMEndpoint,
  type LLMProviderType,
  type LLMProviderView,
  type ToolPolicy,
} from "@/api";
import { PageContent } from "@/components/layout/PageContent";
import { PageHeader } from "@/components/admin/PageHeader";
import { ToolPolicyForm } from "@/components/admin/ToolPolicyForm";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { HelpTooltip } from "@/components/ui/help-tooltip";
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
import { ModelSelect } from "@/components/agent/ModelSelect";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";
import { isHostExecutedProvider, isProviderAvailable, SUBAGENT_TYPES } from "@/lib/constants";

/**
 * Stands in for the catalog entry whose id is the empty string ("let the CLI
 * pick its own model").
 *
 * Radix spells "nothing is selected" as value="" and throws on an item that
 * claims it, so the row cannot carry the real value. It carries this instead
 * and the change handler maps it back to "" before it reaches the form — which
 * is what the server stores, and what makes the runner omit --model.
 *
 * Shared by the normal AND the heavy model selects: both fields store "" the
 * same way, and both need the same workaround to offer the catalog's default
 * row rather than just a blank trigger.
 */
const CLI_DEFAULT_MODEL = "__cli_default__";

const emptyAgent = (): AgentInput => ({
  name: "",
  description: "",
  subagent_type: "generalPurpose",
  system_prompt: "",
  provider_type: "",
  model: "",
  model_heavy: "",
  tool_policy: {},
  enabled: true,
  self_evolution_enabled: false,
});

// The settings page is the two toggle gates' owner, so its draft carries them
// unconditionally even though AgentInput leaves them optional for partial-save
// callers that must never touch them (performance page's model swap).
type AgentDraft = AgentInput & Pick<Agent, "auto_pull_agent_updates" | "keep_skills_updated">;

const emptyAgentDraft = (): AgentDraft => ({
  ...emptyAgent(),
  auto_pull_agent_updates: true,
  keep_skills_updated: true,
});


type AgentOutletContext = {
  agent: Agent | null;
  refreshAgent: () => Promise<void>;
};

export function AgentSettingsPage() {
  const { t } = useI18n();
  const { agentId } = useParams();
  const navigate = useNavigate();
  const isNew = agentId === "new";
  const outlet = useOutletContext<AgentOutletContext | undefined>();
  const [form, setForm] = useState<AgentDraft>(emptyAgentDraft());
  // Whether this agent's definition came from the external agent catalog. The
  // two sync gates below are only meaningful then; a hand-made agent owns its
  // own prompt and no upstream will ever reach it.
  const [catalogManaged, setCatalogManaged] = useState(false);
  const [policy, setPolicy] = useState<ToolPolicy>({});
  const [loading, setLoading] = useState(!isNew);
  const [saving, setSaving] = useState(false);
  const [models, setModels] = useState<string[]>([]);
  // Label of the catalog's empty-id entry, or null when it sent none. Only a
  // host-executed provider offers one, and "" (entry present, server sent no
  // label) is a different answer from null (no such entry) — hence not a bool.
  const [defaultModelLabel, setDefaultModelLabel] = useState<string | null>(null);
  // Starts armed: the form paints before the first fetch is even dispatched, and
  // an unarmed flag would read as "asked, got nothing" for that frame — long
  // enough to flash the degraded free-text field at every host-executed agent.
  const [modelsLoading, setModelsLoading] = useState(true);
  const [providers, setProviders] = useState<LLMProviderView[]>([]);
  const [endpoints, setEndpoints] = useState<LLMEndpoint[]>([]);
  const [providersLoading, setProvidersLoading] = useState(true);

  // Built-in connected providers + host-executed providers + configured named
  // OpenAI-compatible endpoints.
  // Endpoints are keyed by their uuid (the value stored in agent.provider_type)
  // and the backend resolves that uuid to the endpoint's client for models/chat.
  //
  // A host-executed provider (Claude Code) is listed on the strength of its
  // definition alone: it can never be "configured", because there is no base URL
  // and no key to connect — the CLI on the runner host is the connection. This
  // agent picker is the only place it is offered; see LLMSettingsPage.
  //
  // A provider the server marks unavailable is dropped instead. It is declared
  // so the LLM settings page can show it is coming, and it has no executor: an
  // agent put on it would look healthy and then fail one task at a time. The
  // server refuses that save anyway — this filter is so the choice is never
  // offered, not so it is never made.
  const connectedProviders = useMemo(
    () => [
      ...providers
        .filter((p) => isProviderAvailable(p.definition.type, p.definition))
        .filter((p) => p.config.configured || isHostExecutedProvider(p.definition.type, p.definition))
        .map((p) => {
          const hostExecuted = isHostExecutedProvider(p.definition.type, p.definition);
          return {
            value: p.definition.type as string,
            label: hostExecuted && p.definition.type === "claude_code"
              ? t("agentArea.settings.provider.claudeCode.label")
              : p.definition.label,
            hint: hostExecuted && p.definition.type === "claude_code"
              ? t("agentArea.settings.provider.claudeCode.hint")
              : undefined,
            hostExecuted,
          };
        }),
      ...endpoints
        .filter((e) => e.configured)
        .map((e) => ({ value: e.id, label: e.name, hint: undefined, hostExecuted: false })),
    ],
    [providers, endpoints, t],
  );

  // Whether the selected provider's runs are executed by the CLI on the runner
  // host. It still shapes the model field — the CLI accepts no model at all, and
  // has no heavy one — but not the source of the list: the server curates the
  // models the local CLI supports and serves them from the same endpoint every
  // other provider is listed from.
  const hostExecutedSelected = isHostExecutedProvider(form.provider_type);

  // The picked provider's display name, for help text that used to hardcode
  // "Claude Code" even when the picker was showing OpenCode or Cursor's own
  // model field. Falls back to the raw provider type so a still-loading list
  // never leaves a sentence with a blank subject.
  const selectedProviderLabel =
    connectedProviders.find((p) => p.value === form.provider_type)?.label || form.provider_type;

  const loadProviders = useCallback(async () => {
    setProvidersLoading(true);
    try {
      const data = await api.listLLMProviders();
      setProviders(data.providers ?? []);
      setEndpoints(data.endpoints ?? []);
    } catch {
      setProviders([]);
      setEndpoints([]);
    } finally {
      setProvidersLoading(false);
    }
  }, []);

  // Every provider is asked the same question, host-executed ones included: the
  // server answers for the local CLI out of a curated list instead of proxying
  // a /v1/models call. A server too old to have that branch fails the request,
  // which lands as an empty catalog and degrades the field to free text.
  const loadModels = useCallback(async (provider: LLMProviderType | "") => {
    if (!provider) {
      setModels([]);
      setDefaultModelLabel(null);
      setModelsLoading(false);
      return;
    }
    setModelsLoading(true);
    try {
      const data = await api.listModels(provider);
      const entries = data.data ?? [];
      setModels(entries.map((m) => m.id).filter(Boolean));
      // An entry with no id is the provider saying "picking nothing is also an
      // answer" — the CLI default. It is dropped from the id list above on
      // purpose: it is not a model name, it is the absence of one.
      const cliDefault = entries.find((m) => !m.id);
      setDefaultModelLabel(cliDefault ? (cliDefault.label ?? "") : null);
    } catch {
      setModels([]);
      setDefaultModelLabel(null);
    } finally {
      setModelsLoading(false);
    }
  }, []);

  const loadAgent = useCallback(async () => {
    if (isNew || !agentId) {
      setLoading(false);
      return;
    }
    setLoading(true);
    try {
      const agent = outlet?.agent ?? (await api.getAgent(agentId));
      setForm({
        name: agent.name,
        description: agent.description,
        subagent_type: agent.subagent_type,
        system_prompt: agent.system_prompt,
        provider_type: agent.provider_type ?? "",
        model: agent.model,
        model_heavy: agent.model_heavy ?? "",
        tool_policy: agent.tool_policy ?? {},
        enabled: agent.enabled,
        self_evolution_enabled: agent.self_evolution_enabled ?? false,
        max_turns: agent.max_turns,
        effort: agent.effort,
        // Born connected to the catalog is the server's own default; a load
        // from an older server that predates these fields reads as "on".
        auto_pull_agent_updates: agent.auto_pull_agent_updates ?? true,
        keep_skills_updated: agent.keep_skills_updated ?? true,
      });
      setCatalogManaged(Boolean(agent.catalog_slug));
      setPolicy(agent.tool_policy ?? {});
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("agentArea.settings.toast.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, [agentId, isNew, outlet?.agent, t]);

  useEffect(() => {
    void loadProviders();
    void loadAgent();
  }, [loadAgent, loadProviders]);

  useEffect(() => {
    // Asked unconditionally: with no provider the empty answer clears both the
    // catalog left over from the previous pick and the armed loading flag.
    void loadModels(form.provider_type);
    if (form.provider_type) return;
    // Only a lone CONNECTED provider is picked for the user. Claude Code is
    // always in the list (it needs no connecting), and auto-selecting a provider
    // that runs on this host is not a default anybody asked for.
    const connectable = connectedProviders.filter((p) => !p.hostExecuted);
    if (connectable.length === 1) {
      const only = connectable[0].value as LLMProviderType;
      setForm((f) => ({ ...f, provider_type: only }));
    }
  }, [form.provider_type, connectedProviders, loadModels]);

  // Both stored names are listed even when the provider does not offer them:
  // a value with no matching item renders as an empty select, which is how an
  // agent kept a heavy model from a provider it had been moved off without
  // anyone seeing it. This applies uniformly now — the heavy field is offered
  // on every provider, host-executed included.
  const modelOptions = useMemo(() => {
    const ids = new Set(models);
    if (form.model) ids.add(form.model);
    if (form.model_heavy) ids.add(form.model_heavy);
    return [...ids];
  }, [models, form.model, form.model_heavy]);

  // The row is offered only where an empty model is actually allowed: the
  // server sent one AND the provider is host-executed. On an HTTP provider a
  // model is required, and a row that empties the field would leave the save
  // button dead with nothing on screen explaining why.
  const offersDefaultModel = hostExecutedSelected && defaultModelLabel !== null;

  // A select is worth showing as soon as there is anything in it — one lone
  // "CLI default" row still is a working control. Nothing at all on a
  // host-executed provider means the server predates its claude_code branch on
  // the models endpoint, and the field falls back to what 08ee142 shipped:
  // free text, which the runner passes through unchanged.
  //
  // Judged on what the SERVER sent, never on modelOptions: that set carries the
  // agent's own stored name, and one leftover value would otherwise pass for a
  // catalog and leave the user a one-row dropdown with nothing else to pick.
  const modelFreeText =
    hostExecutedSelected && !modelsLoading && models.length === 0 && !offersDefaultModel;

  // Nothing to open: no provider chosen, or a catalog with neither a model nor
  // the default row.
  const modelSelectDisabled =
    !form.provider_type || modelsLoading || (modelOptions.length === 0 && !offersDefaultModel);

  // A model name only means something to the provider it was picked from, so a
  // name this provider does not list is flagged rather than silently saved.
  const foreignModel = useCallback(
    (model: string) => Boolean(model) && models.length > 0 && !models.includes(model),
    [models],
  );

  // An agent that takes screenshots is judging pixels, and a text-only model
  // cannot look at them: the picture is stripped on the way to the provider (or
  // ignored by a model with no vision), and what comes back is a confident
  // verdict on a page nobody saw. A broken store badge shipped through a
  // developer and a QA round that way. An empty allowlist means unrestricted,
  // so it carries the browser tools too.
  const needsVisionModel = useMemo(() => {
    const allowed = policy.allow_tools;
    if (!allowed || allowed.length === 0) return true;
    return allowed.some((tool) => tool.startsWith("browser_") || tool.startsWith("mobile_"));
  }, [policy.allow_tools]);

  const handleProviderChange = (value: string) => {
    setForm((f) => ({ ...f, provider_type: value as LLMProviderType, model: "", model_heavy: "" }));
  };

  // Model is not required on a host-executed provider: an empty value lets the
  // CLI use the model the account defaults to, which is what a subscription
  // user expects (the executor only passes --model when one is set). That empty
  // value is a deliberate pick here, not an unfilled field — it is what the
  // catalog's "CLI default" row stores — so it must not fail the check.
  const canSave = isNew
    ? Boolean(form.name.trim() && form.provider_type && (hostExecutedSelected || form.model.trim()))
    : Boolean(form.name.trim());

  const handleSave = async () => {
    if (!canSave) {
      if (isNew) {
        toast.error(
          hostExecutedSelected
            ? t("agentArea.settings.toast.requiredNewNoModel")
            : t("agentArea.settings.toast.requiredNew"),
        );
      } else {
        toast.error(t("agentArea.settings.toast.requiredName"));
      }
      return;
    }
    setSaving(true);
    try {
      const payload = { ...form, tool_policy: policy };
      if (isNew) {
        const created = await api.createAgent(payload);
        toast.success(t("agentArea.settings.toast.created"));
        navigate(`/agents/${created.id}/settings`, { replace: true });
      } else if (agentId) {
        await api.updateAgent(agentId, payload);
        toast.success(t("agentArea.settings.toast.updated"));
        await outlet?.refreshAgent();
      }
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-8 w-48" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  return (
    <PageContent>
      <PageHeader
        title={isNew ? t("agentArea.settings.header.titleNew") : t("agentArea.settings.header.title")}
        description={t("agentArea.settings.header.description")}
      />
      <Card className="w-full space-y-4 p-6">
        <div className="grid gap-4 lg:grid-cols-2">
          <div className="space-y-2">
            <Label>{t("agentArea.settings.name")}</Label>
            <Input value={form.name} onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))} />
          </div>
          <div className="space-y-2">
            <div className="flex items-center gap-1.5">
              <Label>{t("agentArea.settings.provider.label")}</Label>
              {hostExecutedSelected && <HelpTooltip text={t("agentArea.settings.provider.claudeCode.hint")} />}
            </div>
            <Select
              value={form.provider_type || undefined}
              onValueChange={handleProviderChange}
              disabled={providersLoading || connectedProviders.length === 0}
            >
              <SelectTrigger>
                <SelectValue
                  placeholder={
                    providersLoading
                      ? t("agentArea.settings.provider.loading")
                      : connectedProviders.length === 0
                        ? t("agentArea.settings.provider.none")
                        : t("agentArea.settings.provider.select")
                  }
                />
              </SelectTrigger>
              <SelectContent>
                {connectedProviders.map((p) => (
                  <SelectItem key={p.value} value={p.value}>
                    <span className="flex flex-col items-start">
                      <span>{p.label}</span>
                      {p.hint && <span className="text-xs text-muted-foreground">{p.hint}</span>}
                    </span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-2 lg:col-span-2 lg:max-w-md">
            <div className="flex items-center gap-1.5">
              <Label>{t("agentArea.settings.model.label")}</Label>
              {hostExecutedSelected && (
                <HelpTooltip
                  text={
                    modelFreeText
                      ? t("agentArea.settings.model.claudeCode.fallbackHelp", {
                          provider: selectedProviderLabel,
                        })
                      : t("agentArea.settings.model.claudeCode.help", { provider: selectedProviderLabel })
                  }
                />
              )}
            </div>
            {modelFreeText ? (
              // Degraded path only: this server listed no models for the local
              // CLI, so the name is typed and passed through as-is. Empty stays
              // a real answer — the CLI then runs on the account's own default.
              <Input
                value={form.model}
                onChange={(e) => setForm((f) => ({ ...f, model: e.target.value }))}
                placeholder={t("agentArea.settings.model.claudeCode.placeholder", {
                  provider: selectedProviderLabel,
                })}
                className="font-mono text-sm"
              />
            ) : (
              <>
                <ModelSelect
                  // An empty model is a selection on a host-executed provider,
                  // not a blank field: show it as the default row rather than
                  // as the placeholder, so the trigger says what will happen.
                  value={offersDefaultModel && !form.model ? CLI_DEFAULT_MODEL : form.model}
                  onValueChange={(v) =>
                    setForm((f) => ({ ...f, model: v === CLI_DEFAULT_MODEL ? "" : v }))
                  }
                  models={modelOptions}
                  disabled={modelSelectDisabled}
                  placeholder={
                    !form.provider_type
                      ? t("agentArea.settings.model.selectProviderFirst")
                      : modelsLoading
                        ? t("agentArea.settings.model.loading")
                        : t("agentArea.settings.model.select")
                  }
                  extraItems={
                    offersDefaultModel ? (
                      <SelectItem value={CLI_DEFAULT_MODEL}>
                        {defaultModelLabel || t("agentArea.settings.model.claudeCode.cliDefault")}
                      </SelectItem>
                    ) : undefined
                  }
                />
                {foreignModel(form.model) &&
                  // On the CLI the curated list is a shortlist, not a contract:
                  // any name reaches `claude --model`. So a stored name it does
                  // not carry (typed into the free-text field this replaced) is
                  // pointed out, not condemned the way a wrong-provider name is.
                  (hostExecutedSelected ? (
                    <p className="text-xs text-muted-foreground">
                      {t("agentArea.settings.model.claudeCode.unlisted", { model: form.model })}
                    </p>
                  ) : (
                    <p className="text-xs text-destructive">
                      {t("agentArea.settings.model.foreign", { model: form.model })}
                    </p>
                  ))}
                {/* Every model the CLI runs can see images, so the warning that
                    guards against a text-only pick has nothing to warn about. */}
                {!hostExecutedSelected && needsVisionModel && (
                  <p className="text-xs text-amber-500">{t("agentArea.settings.model.visionRequired")}</p>
                )}
              </>
            )}
          </div>
          {/* Offered on every provider, host-executed included: the model for
              hard subtasks is an independent, optional pick from the model
              for ordinary turns above — never required, always clearable. */}
          <div className="space-y-2 lg:col-span-2 lg:max-w-md">
            <div className="flex items-center gap-1.5">
              <Label>{t("agentArea.settings.modelHeavy.label")}</Label>
              <HelpTooltip
                text={
                  modelFreeText
                    ? t("agentArea.settings.modelHeavy.claudeCode.fallbackHelp", {
                        provider: selectedProviderLabel,
                      })
                    : hostExecutedSelected
                      ? t("agentArea.settings.modelHeavy.claudeCode.help", { provider: selectedProviderLabel })
                      : t("agentArea.settings.modelHeavy.help")
                }
              />
            </div>
            {modelFreeText ? (
              // Degraded path only, mirroring the model field above: this
              // server listed no models for the local CLI, so the name is
              // typed and passed through as-is. Empty still saves cleanly —
              // hard subtasks then fall back to the model above.
              <Input
                value={form.model_heavy}
                onChange={(e) => setForm((f) => ({ ...f, model_heavy: e.target.value }))}
                placeholder={t("agentArea.settings.modelHeavy.claudeCode.placeholder")}
                className="font-mono text-sm"
              />
            ) : (
              <>
                <ModelSelect
                  // Same empty-id handling as the model field above: an empty
                  // heavy model is a deliberate pick (fall back to the model
                  // above, or its own CLI default when that is empty too),
                  // not an unfilled field, so a host-executed provider shows
                  // it as the catalog's own default row rather than a blank
                  // trigger.
                  value={
                    hostExecutedSelected && offersDefaultModel && !form.model_heavy
                      ? CLI_DEFAULT_MODEL
                      : form.model_heavy
                  }
                  onValueChange={(v) =>
                    setForm((f) => ({
                      ...f,
                      model_heavy: v === CLI_DEFAULT_MODEL || v === "__clear__" ? "" : v,
                    }))
                  }
                  models={modelOptions}
                  disabled={modelSelectDisabled}
                  placeholder={t("agentArea.settings.modelHeavy.placeholder")}
                  extraItems={
                    hostExecutedSelected && offersDefaultModel ? (
                      <SelectItem value={CLI_DEFAULT_MODEL}>
                        {defaultModelLabel || t("agentArea.settings.model.claudeCode.cliDefault")}
                      </SelectItem>
                    ) : form.model_heavy ? (
                      <SelectItem value="__clear__">{t("agentArea.settings.modelHeavy.clear")}</SelectItem>
                    ) : undefined
                  }
                />
                {foreignModel(form.model_heavy) &&
                  // Same softer wording as the model field above: on the CLI
                  // the curated list is a shortlist, not a contract.
                  (hostExecutedSelected ? (
                    <p className="text-xs text-muted-foreground">
                      {t("agentArea.settings.modelHeavy.claudeCode.unlisted", { model: form.model_heavy })}
                    </p>
                  ) : (
                    <p className="text-xs text-destructive">
                      {t("agentArea.settings.model.foreign", { model: form.model_heavy })}
                    </p>
                  ))}
              </>
            )}
          </div>
        </div>
        <div className="space-y-2">
          <Label>{t("agentArea.settings.description")}</Label>
          <Input
            value={form.description}
            onChange={(e) => setForm((f) => ({ ...f, description: e.target.value }))}
          />
        </div>
        <div className="space-y-2">
          <Label>{t("agentArea.settings.subagentType")}</Label>
          <Select value={form.subagent_type} onValueChange={(v) => setForm((f) => ({ ...f, subagent_type: v }))}>
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {SUBAGENT_TYPES.map((t) => (
                <SelectItem key={t} value={t}>
                  {t}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-2">
          <Label>{t("agentArea.settings.systemPrompt")}</Label>
          <Textarea
            rows={4}
            value={form.system_prompt}
            onChange={(e) => setForm((f) => ({ ...f, system_prompt: e.target.value }))}
          />
        </div>
        <ToolPolicyForm value={policy} onChange={setPolicy} />
        {catalogManaged && (
          <div className="space-y-4 border-t border-border pt-4">
            <div className="flex items-center justify-between gap-4">
              <div className="space-y-1">
                <Label>{t("agentArea.settings.catalog.autoPull.label")}</Label>
                <p className="text-xs text-muted-foreground">{t("agentArea.settings.catalog.autoPull.help")}</p>
              </div>
              <Switch
                checked={form.auto_pull_agent_updates}
                onCheckedChange={(v) => setForm((f) => ({ ...f, auto_pull_agent_updates: v }))}
              />
            </div>
            <div className="flex items-center justify-between gap-4">
              <div className="space-y-1">
                <Label>{t("agentArea.settings.catalog.keepUpdated.label")}</Label>
                <p className="text-xs text-muted-foreground">{t("agentArea.settings.catalog.keepUpdated.help")}</p>
              </div>
              <Switch
                checked={form.keep_skills_updated}
                onCheckedChange={(v) => setForm((f) => ({ ...f, keep_skills_updated: v }))}
              />
            </div>
          </div>
        )}
        <div className="flex items-center gap-2">
          <Switch checked={form.enabled} onCheckedChange={(v) => setForm((f) => ({ ...f, enabled: v }))} />
          <Label>{t("agentArea.settings.enabled")}</Label>
        </div>
        <Button onClick={handleSave} disabled={!canSave || saving}>
          {saving ? t("common.saving") : isNew ? t("agentArea.settings.create") : t("agentArea.settings.update")}
        </Button>
      </Card>
    </PageContent>
  );
}

