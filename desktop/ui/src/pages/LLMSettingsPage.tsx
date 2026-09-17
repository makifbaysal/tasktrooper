import {
  CheckCircle2,
  Circle,
  Link2,
  Loader2,
  Pencil,
  Plug,
  Plus,
  Server,
  Trash2,
  Unplug,
} from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import {
  type AgentCLIFlavor,
  api,
  type ConnectLLMProviderRequest,
  type LLMEndpoint,
  type LLMProviderType,
  type LLMProviderView,
  type SaveLLMEndpointRequest,
} from "@/api";
import { EnvironmentPreflight, type EnvironmentBlocker } from "@/components/runner/EnvironmentPreflight";
import { LocalCliCard } from "@/components/runner/LocalCliCard";
import {
  type ConnectStep,
  connectAgentCli,
  connectFailure,
  disconnectAgentCli,
} from "@/components/runner/claudeCodeConnect";
import { useDesktopHost, useRunnerSnapshot } from "@/components/runner/useDesktopRunner";
import { useSetup } from "@/hooks/useSetup";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
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
import { useI18n } from "@/hooks/useI18n";
import { isHostExecutedProvider, isProviderAvailable } from "@/lib/constants";
import { cn } from "@/lib/utils";

// Native cards shown as fixed provider cards. local/openai/groq are hidden — they
// live as OpenAI-compatible endpoint presets below.
//
// This is an allowlist on purpose, and the host-executed providers are
// deliberately absent from it: they have no base URL and no API key to enter,
// and the server rejects connect/test/activate/set-embedding for them. They get
// their own section further down, which explains where their connection
// actually is instead of offering a form that cannot be submitted.
const NATIVE_CARD_TYPES: LLMProviderType[] = ["gemini", "anthropic"];

const providerAccent: Partial<Record<LLMProviderType, string>> = {
  gemini: "border-amber-500/40 bg-amber-500/5",
  anthropic: "border-violet-500/40 bg-violet-500/5",
  claude_code: "border-orange-500/40 bg-orange-500/5",
};

// Endpoint presets pre-fill the add form. "custom" leaves everything blank.
// labelKey is an i18n key resolved at render time.
type EndpointPreset = {
  id: string;
  labelKey: string;
  name: string;
  base_url: string;
  model: string;
  requiresKey?: boolean;
};

const ENDPOINT_PRESETS: EndpointPreset[] = [
  { id: "lmstudio", labelKey: "settingsPages.llm.presetLmStudio", name: "LM Studio", base_url: "http://127.0.0.1:1234/v1", model: "" },
  { id: "ollama", labelKey: "settingsPages.llm.presetOllama", name: "Ollama", base_url: "http://127.0.0.1:11434/v1", model: "" },
  { id: "openai", labelKey: "settingsPages.llm.presetOpenAI", name: "OpenAI", base_url: "https://api.openai.com/v1", model: "gpt-4o", requiresKey: true },
  { id: "groq", labelKey: "settingsPages.llm.presetGroq", name: "Groq", base_url: "https://api.groq.com/openai/v1", model: "llama-3.3-70b-versatile", requiresKey: true },
  { id: "openrouter", labelKey: "settingsPages.llm.presetOpenRouter", name: "OpenRouter", base_url: "https://openrouter.ai/api/v1", model: "", requiresKey: true },
  { id: "gemini-openai", labelKey: "settingsPages.llm.presetGeminiOpenAI", name: "Gemini OpenAI", base_url: "https://generativelanguage.googleapis.com/v1beta/openai/", model: "gemini-2.5-flash", requiresKey: true },
  { id: "custom", labelKey: "settingsPages.llm.presetCustom", name: "", base_url: "", model: "" },
];

function nativeStatusBadge(
  view: LLMProviderView,
  isDefault: boolean,
  t: (key: string, params?: Record<string, string | number>) => string,
) {
  if (isDefault && view.config.configured) {
    return <Badge variant="success">{t("settingsPages.llm.badgeDefault")}</Badge>;
  }
  if (view.config.configured) {
    return <Badge variant="secondary">{t("settingsPages.llm.badgeConnected")}</Badge>;
  }
  return <Badge variant="outline" className="whitespace-nowrap">{t("settingsPages.llm.badgeNotConnected")}</Badge>;
}

export function LLMSettingsPage() {
  const { t } = useI18n();
  // Non-null only inside the desktop shell. It is what draws the environment
  // preflight — the one part of these cards a browser tab cannot answer.
  const host = useDesktopHost();
  const snapshot = useRunnerSnapshot(host);

  const [loading, setLoading] = useState(true);
  const [providers, setProviders] = useState<LLMProviderView[]>([]);
  const [endpoints, setEndpoints] = useState<LLMEndpoint[]>([]);
  const [activeProvider, setActiveProvider] = useState<string | null>(null);

  // Native provider connect dialog (gemini / anthropic)
  const [connectTarget, setConnectTarget] = useState<LLMProviderView | null>(null);
  const [baseURL, setBaseURL] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [timeoutSeconds, setTimeoutSeconds] = useState(300);
  const [connecting, setConnecting] = useState(false);
  const [testing, setTesting] = useState(false);
  const [busyType, setBusyType] = useState<LLMProviderType | null>(null);

  // Endpoint dialog
  const [epDialogOpen, setEpDialogOpen] = useState(false);
  const [editingEndpoint, setEditingEndpoint] = useState<LLMEndpoint | null>(null);
  const [epPreset, setEpPreset] = useState<string>("custom");
  const [epName, setEpName] = useState("");
  const [epBaseURL, setEpBaseURL] = useState("");
  const [epModel, setEpModel] = useState("");
  const [epApiKey, setEpApiKey] = useState("");
  const [epTimeout, setEpTimeout] = useState(120);
  const [epTesting, setEpTesting] = useState(false);
  const [epSaving, setEpSaving] = useState(false);
  const [epBusyId, setEpBusyId] = useState<string | null>(null);
  const [deleteEndpointTarget, setDeleteEndpointTarget] = useState<LLMEndpoint | null>(null);

  // Local agent CLIs. Read from useSetup(), not a state of our own — that hook
  // is the single fetch of this row shared with the setup wizard, so a connect
  // made here and a connect made there can never show two different answers,
  // and its own focus-refresh keeps this page from freezing on whatever the
  // flag said at mount.
  const { cliState, refresh: refreshCliState } = useSetup();
  const [cliBusy, setCliBusy] = useState<AgentCLIFlavor | null>(null);
  // The failure stays ON THE CARD rather than in a toast. Connecting a CLI can
  // fail for reasons the user has to go and fix somewhere else — install the
  // binary, sign it in — and a toast is gone by the time they come back to see
  // what it said.
  const [cliError, setCliError] = useState<{ flavor: AgentCLIFlavor; message: string } | null>(null);
  // Where the connect flow is, so the card can narrate it. Null means nothing
  // is in flight.
  const [cliStep, setCliStep] = useState<ConnectStep | null>(null);
  // The environment preflight checklist lives only on the Claude Code card —
  // it is desktop-only and provider-specific, so its blocking state is kept
  // here rather than in EnvironmentPreflight itself, which has no idea which
  // card it is drawn on.
  const [envBlocker, setEnvBlocker] = useState<EnvironmentBlocker | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const data = await api.listLLMProviders();
      setProviders(data.providers ?? []);
      setEndpoints(data.endpoints ?? []);
      setActiveProvider(data.active_provider ?? null);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("settingsPages.llm.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    load();
  }, [load]);

  const applyResponse = (data: Awaited<ReturnType<typeof api.listLLMProviders>>) => {
    setProviders(data.providers ?? []);
    setEndpoints(data.endpoints ?? []);
    setActiveProvider(data.active_provider ?? null);
  };

  const nativeCards = providers.filter((p) => NATIVE_CARD_TYPES.includes(p.definition.type));

  // The local agent CLIs, in the order the server declares them. Unavailable
  // ones are kept: this section exists to say which CLIs the product runs, and
  // a list that quietly omits the one being built reads as a complete list.
  const cliCards = providers.filter((p) =>
    isHostExecutedProvider(p.definition.type, p.definition),
  );

  // --- Local agent CLIs (claude_code / cursor_agent) ---

  // The card is rendered from the provider list; the connect surface is keyed
  // by flavor. This is the one place the two vocabularies meet, and it reads
  // the server's own mapping rather than hardcoding a second copy of it — the
  // flavors array carries provider_type for exactly this.
  const flavorOf = (type: LLMProviderType): AgentCLIFlavor | null =>
    cliState?.flavors.find((f) => f.provider_type === type)?.flavor ?? null;

  // How a failure of the local flow reads on the card. The mapping itself
  // lives next to the flow (`connectFailure`), so the guided setup step — which
  // runs the same flow — cannot phrase a refusal differently from this card.
  const cliFailure = (flavor: AgentCLIFlavor, e: unknown) => {
    setCliError({ flavor, message: connectFailure(e).message });
  };

  const handleCliConnect = async (flavor: AgentCLIFlavor) => {
    setCliBusy(flavor);
    setCliError(null);
    setCliStep(null);
    try {
      await connectAgentCli({ flavor, api, onStep: setCliStep });
      toast.success(t("settingsPages.llm.cliConnectedToast"));
    } catch (e) {
      cliFailure(flavor, e);
    } finally {
      setCliBusy(null);
      setCliStep(null);
      await refreshCliState();
    }
  };

  const handleCliDisconnect = async (flavor: AgentCLIFlavor) => {
    setCliBusy(flavor);
    setCliError(null);
    setCliStep(null);
    try {
      await disconnectAgentCli({ flavor, api, onStep: setCliStep });
      toast.success(t("settingsPages.llm.cliDisconnectedToast"));
    } catch (e) {
      cliFailure(flavor, e);
    } finally {
      setCliBusy(null);
      setCliStep(null);
      await refreshCliState();
    }
  };

  // --- Native provider (gemini/anthropic) ---
  const openConnect = (view: LLMProviderView) => {
    setConnectTarget(view);
    setBaseURL(view.config.base_url || view.definition.default_base_url);
    setApiKey("");
    const timeout =
      view.config.timeout_seconds > 0
        ? view.config.timeout_seconds
        : view.definition.default_timeout_seconds;
    setTimeoutSeconds(timeout);
  };

  const closeConnect = () => {
    setConnectTarget(null);
    setBaseURL("");
    setApiKey("");
    setTimeoutSeconds(300);
  };

  const buildPayload = (): ConnectLLMProviderRequest => ({
    base_url: baseURL.trim(),
    api_key: apiKey.trim(),
    timeout_seconds: timeoutSeconds,
  });

  const handleTest = async () => {
    if (!connectTarget) return;
    setTesting(true);
    try {
      await api.testLLMProvider(connectTarget.definition.type, buildPayload());
      toast.success(t("settingsPages.llm.testSuccess"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("settingsPages.llm.testFailed"));
    } finally {
      setTesting(false);
    }
  };

  const handleConnect = async () => {
    if (!connectTarget) return;
    setConnecting(true);
    try {
      const data = await api.connectLLMProvider(connectTarget.definition.type, buildPayload());
      applyResponse(data);
      toast.success(t("settingsPages.llm.connectedToast", { provider: connectTarget.definition.label }));
      closeConnect();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("settingsPages.llm.connectFailed"));
    } finally {
      setConnecting(false);
    }
  };

  const handleActivate = async (type: LLMProviderType) => {
    setBusyType(type);
    try {
      const data = await api.activateLLMProvider(type);
      applyResponse(data);
      toast.success(t("settingsPages.llm.defaultUpdated"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("settingsPages.llm.activateFailed"));
    } finally {
      setBusyType(null);
    }
  };

  const handleDisconnect = async (type: LLMProviderType) => {
    setBusyType(type);
    try {
      const data = await api.disconnectLLMProvider(type);
      applyResponse(data);
      toast.success(t("settingsPages.llm.disconnectedToast"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("settingsPages.llm.disconnectFailed"));
    } finally {
      setBusyType(null);
    }
  };

  // --- Endpoints ---
  const openCreateEndpoint = () => {
    setEditingEndpoint(null);
    setEpPreset("custom");
    setEpName("");
    setEpBaseURL("");
    setEpModel("");
    setEpApiKey("");
    setEpTimeout(120);
    setEpDialogOpen(true);
  };

  const openEditEndpoint = (ep: LLMEndpoint) => {
    setEditingEndpoint(ep);
    setEpPreset("custom");
    setEpName(ep.name);
    setEpBaseURL(ep.base_url);
    setEpModel(ep.default_model);
    setEpApiKey("");
    setEpTimeout(ep.timeout_seconds > 0 ? ep.timeout_seconds : 120);
    setEpDialogOpen(true);
  };

  const onPresetChange = (value: string) => {
    setEpPreset(value);
    const preset = ENDPOINT_PRESETS.find((p) => p.id === value);
    if (!preset || value === "custom") return;
    setEpName(preset.name);
    setEpBaseURL(preset.base_url);
    setEpModel(preset.model);
  };

  const buildEndpointPayload = (): SaveLLMEndpointRequest => ({
    name: epName.trim(),
    base_url: epBaseURL.trim(),
    default_model: epModel.trim(),
    api_key: epApiKey.trim(),
    timeout_seconds: epTimeout,
  });

  const handleEndpointTest = async () => {
    setEpTesting(true);
    try {
      await api.testLLMEndpoint(editingEndpoint?.id ?? "", buildEndpointPayload());
      toast.success(t("settingsPages.llm.testSuccess"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("settingsPages.llm.testFailed"));
    } finally {
      setEpTesting(false);
    }
  };

  const handleEndpointSave = async () => {
    if (!epName.trim() || !epBaseURL.trim()) {
      toast.error(t("settingsPages.llm.nameUrlRequired"));
      return;
    }
    setEpSaving(true);
    try {
      const payload = buildEndpointPayload();
      const data = editingEndpoint
        ? await api.updateLLMEndpoint(editingEndpoint.id, payload)
        : await api.createLLMEndpoint(payload);
      applyResponse(data);
      toast.success(editingEndpoint ? t("settingsPages.llm.endpointUpdated") : t("settingsPages.llm.endpointAdded"));
      setEpDialogOpen(false);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("settingsPages.llm.saveFailed"));
    } finally {
      setEpSaving(false);
    }
  };

  const handleEndpointActivate = async (id: string) => {
    setEpBusyId(id);
    try {
      const data = await api.activateLLMEndpoint(id);
      applyResponse(data);
      toast.success(t("settingsPages.llm.defaultUpdated"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("settingsPages.llm.activateFailed"));
    } finally {
      setEpBusyId(null);
    }
  };

  const handleEndpointDelete = async () => {
    if (!deleteEndpointTarget) return;
    setEpBusyId(deleteEndpointTarget.id);
    try {
      const data = await api.deleteLLMEndpoint(deleteEndpointTarget.id);
      applyResponse(data);
      toast.success(t("settingsPages.llm.endpointDeleted"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("settingsPages.llm.deleteFailed"));
    } finally {
      setEpBusyId(null);
      setDeleteEndpointTarget(null);
    }
  };

  return (
    <>
      {loading ? (
        <div className="grid gap-4 md:grid-cols-2">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-44 w-full" />
          ))}
        </div>
      ) : (
        <>
          {/* OpenAI-compatible endpoints */}
          <div className="mb-3 flex items-center justify-between">
            <div>
              <h2 className="text-sm font-semibold">{t("settingsPages.llm.endpointsTitle")}</h2>
              <p className="text-xs text-muted-foreground">
                {t("settingsPages.llm.endpointsDesc")}
              </p>
            </div>
            <Button size="sm" onClick={openCreateEndpoint}>
              <Plus className="mr-1.5 h-4 w-4" />
              {t("settingsPages.llm.addEndpoint")}
            </Button>
          </div>
          <div className="mb-6 grid gap-4 md:grid-cols-2">
            {endpoints.length === 0 && (
              <Card className="col-span-full flex items-center gap-3 border-dashed p-6 text-sm text-muted-foreground">
                <Server className="h-5 w-5" />
                {t("settingsPages.llm.endpointsEmpty")}
              </Card>
            )}
            {endpoints.map((ep) => {
              const isDefault = ep.id === activeProvider;
              const busy = epBusyId === ep.id;
              return (
                <Card
                  key={ep.id}
                  className={cn(
                    "flex flex-col border p-4",
                    ep.configured && "border-emerald-500/40 bg-emerald-500/5",
                  )}
                >
                  <div className="mb-3 flex items-start justify-between gap-2">
                    <div className="min-w-0">
                      <div className="flex items-center gap-2">
                        <Server className="h-4 w-4 shrink-0 text-success" />
                        <h3 className="truncate font-semibold">{ep.name}</h3>
                      </div>
                    </div>
                    {isDefault ? (
                      <Badge variant="success">{t("settingsPages.llm.badgeDefault")}</Badge>
                    ) : (
                      <Badge variant="secondary">{t("settingsPages.llm.badgeConnected")}</Badge>
                    )}
                  </div>
                  <div className="mb-3 space-y-1 rounded-md bg-muted/40 p-2 text-xs text-muted-foreground">
                    <p className="truncate">
                      <span className="font-medium text-foreground">{t("settingsPages.llm.urlLabel")}</span> {ep.base_url}
                    </p>
                    {ep.default_model && (
                      <p className="truncate">
                        <span className="font-medium text-foreground">{t("settingsPages.llm.modelLabel")}</span> {ep.default_model}
                      </p>
                    )}
                    {ep.has_api_key && (
                      <p>
                        <span className="font-medium text-foreground">{t("settingsPages.llm.apiKeyLabel")}</span> {t("settingsPages.llm.apiKeyStored")}
                      </p>
                    )}
                    {ep.timeout_seconds > 0 && (
                      <p>
                        <span className="font-medium text-foreground">{t("settingsPages.llm.timeoutLabel")}</span>{" "}
                        {t("settingsPages.llm.secondsValue", { seconds: ep.timeout_seconds })}
                      </p>
                    )}
                  </div>
                  <div className="mt-auto flex flex-wrap gap-2">
                    <Button size="sm" variant="default" onClick={() => openEditEndpoint(ep)} disabled={busy}>
                      <Pencil className="mr-1.5 h-3.5 w-3.5" />
                      {t("settingsPages.llm.edit")}
                    </Button>
                    {!isDefault && (
                      <Button
                        size="sm"
                        variant="secondary"
                        onClick={() => handleEndpointActivate(ep.id)}
                        disabled={busy}
                      >
                        {busy ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : t("settingsPages.llm.makeDefault")}
                      </Button>
                    )}
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => setDeleteEndpointTarget(ep)}
                      disabled={busy}
                    >
                      <Trash2 className="mr-1.5 h-3.5 w-3.5" />
                      {t("settingsPages.llm.delete")}
                    </Button>
                  </div>
                </Card>
              );
            })}
          </div>

          {/* Native providers (fixed): Gemini + Anthropic */}
          <h2 className="mb-3 text-sm font-semibold">{t("settingsPages.llm.nativeTitle")}</h2>
          <div className="grid gap-4 md:grid-cols-2">
            {nativeCards.map((view) => {
              const type = view.definition.type;
              const busy = busyType === type;
              const isDefault = type === activeProvider;
              return (
                <Card
                  key={type}
                  className={cn(
                    "flex flex-col border p-4",
                    view.config.configured && providerAccent[type],
                  )}
                >
                  <div className="mb-3 flex items-start justify-between gap-2">
                    <div className="min-w-0">
                      <div className="flex items-center gap-2">
                        {view.config.configured ? (
                          <CheckCircle2 className="h-4 w-4 shrink-0 text-success" />
                        ) : (
                          <Circle className="h-4 w-4 shrink-0 text-muted-foreground" />
                        )}
                        <h3 className="font-semibold">{view.definition.label}</h3>
                      </div>
                      <p className="mt-1 text-sm text-muted-foreground">{view.definition.description}</p>
                    </div>
                    {nativeStatusBadge(view, isDefault, t)}
                  </div>

                  {view.config.configured && (
                    <div className="mb-3 space-y-1 rounded-md bg-muted/40 p-2 text-xs text-muted-foreground">
                      <p className="truncate">
                        <span className="font-medium text-foreground">{t("settingsPages.llm.urlLabel")}</span> {view.config.base_url}
                      </p>
                      {view.config.default_model && (
                        <p className="truncate">
                          <span className="font-medium text-foreground">{t("settingsPages.llm.modelLabel")}</span> {view.config.default_model}
                        </p>
                      )}
                      {view.config.has_api_key && (
                        <p>
                          <span className="font-medium text-foreground">{t("settingsPages.llm.apiKeyLabel")}</span> {t("settingsPages.llm.apiKeyStored")}
                        </p>
                      )}
                      {view.config.timeout_seconds > 0 && (
                        <p>
                          <span className="font-medium text-foreground">{t("settingsPages.llm.timeoutLabel")}</span>{" "}
                          {t("settingsPages.llm.secondsValue", { seconds: view.config.timeout_seconds })}
                        </p>
                      )}
                    </div>
                  )}

                  <div className="mt-auto flex flex-wrap gap-2">
                    <Button size="sm" variant="default" onClick={() => openConnect(view)} disabled={busy}>
                      <Plug className="mr-1.5 h-3.5 w-3.5" />
                      {view.config.configured ? t("settingsPages.llm.reconnect") : t("settingsPages.llm.connect")}
                    </Button>
                    {view.config.configured && !isDefault && (
                      <Button
                        size="sm"
                        variant="secondary"
                        onClick={() => handleActivate(type)}
                        disabled={busy}
                      >
                        {busy ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : t("settingsPages.llm.makeDefault")}
                      </Button>
                    )}
                    {view.config.configured && (
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => handleDisconnect(type)}
                        disabled={busy}
                      >
                        <Unplug className="mr-1.5 h-3.5 w-3.5" />
                        {t("settingsPages.llm.disconnectShort")}
                      </Button>
                    )}
                  </div>
                </Card>
              );
            })}
          </div>

          {/* Local agent CLIs (host-executed): Claude Code, Cursor, Antigravity,
              OpenCode. The environment checklist applies to this MACHINE, not
              to any one of these CLIs, so it renders once here rather than
              being duplicated onto every card. */}
          <h2 className="mb-1 mt-8 text-sm font-semibold">{t("settingsPages.llm.cliTitle")}</h2>
          <p className="mb-3 text-sm text-muted-foreground">{t("settingsPages.llm.cliDesc")}</p>

          <EnvironmentPreflight host={host} onBlockingChange={setEnvBlocker} />

          <div className="grid gap-4 md:grid-cols-2">
            {cliCards.map((view) => {
              const type = view.definition.type;
              const flavor = flavorOf(type);
              return (
                <LocalCliCard
                  key={type}
                  view={view}
                  flavor={flavor}
                  available={isProviderAvailable(type, view.definition)}
                  cliState={cliState}
                  busy={flavor !== null && cliBusy === flavor}
                  anyBusy={cliBusy !== null}
                  step={flavor !== null && cliBusy === flavor ? cliStep : null}
                  error={flavor !== null && cliError?.flavor === flavor ? cliError.message : ""}
                  host={host}
                  snapshot={snapshot}
                  onConnect={() => flavor && handleCliConnect(flavor)}
                  onDisconnect={() => flavor && handleCliDisconnect(flavor)}
                  {...(providerAccent[type] ? { accentClassName: providerAccent[type] } : {})}
                  {...(envBlocker ? { environmentBlockingLabel: envBlocker.label } : {})}
                />
              );
            })}
          </div>
        </>
      )}

      <ConfirmDialog
        open={deleteEndpointTarget !== null}
        onOpenChange={(open) => !open && setDeleteEndpointTarget(null)}
        title={t("settingsPages.llm.deleteEndpointTitle")}
        description={t("settingsPages.llm.deleteEndpointDesc", { name: deleteEndpointTarget?.name ?? "" })}
        confirmLabel={t("settingsPages.llm.delete")}
        loading={epBusyId === deleteEndpointTarget?.id}
        onConfirm={handleEndpointDelete}
      />

      {/* Native connect dialog */}
      <Dialog open={connectTarget !== null} onOpenChange={(open) => !open && closeConnect()}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>{t("settingsPages.llm.connectDialogTitle", { provider: connectTarget?.definition.label ?? "" })}</DialogTitle>
          </DialogHeader>
          {/* No host-executed branch. This dialog is the base-URL-and-key form,
              and the local CLIs never open it any more: their Connect button
              runs the real connect on the card itself. */}
          {connectTarget && (
            <div className="space-y-4">
              {connectTarget.definition.base_url_required && (
                <div className="space-y-2">
                  <Label htmlFor="llm-base-url">{t("settingsPages.llm.baseUrlLabel")}</Label>
                  <Input
                    id="llm-base-url"
                    value={baseURL}
                    onChange={(e) => setBaseURL(e.target.value)}
                    placeholder={connectTarget.definition.default_base_url}
                    className="font-mono text-sm"
                  />
                </div>
              )}
              {connectTarget.definition.requires_api_key && (
                <div className="space-y-2">
                  <Label htmlFor="llm-api-key">{t("settingsPages.llm.apiKeyFieldLabel")}</Label>
                  <Input
                    id="llm-api-key"
                    type="password"
                    value={apiKey}
                    onChange={(e) => setApiKey(e.target.value)}
                    placeholder={connectTarget.config.has_api_key ? t("settingsPages.llm.apiKeyChangePlaceholder") : ""}
                  />
                  {connectTarget.config.has_api_key && !apiKey && (
                    <p className="text-xs text-muted-foreground">{t("settingsPages.llm.apiKeyBlankHint")}</p>
                  )}
                </div>
              )}
              <div className="space-y-2">
                <Label htmlFor="llm-timeout">{t("settingsPages.llm.timeoutFieldLabel")}</Label>
                <Input
                  id="llm-timeout"
                  type="number"
                  min={30}
                  max={3600}
                  value={timeoutSeconds}
                  onChange={(e) => setTimeoutSeconds(Math.max(30, Number(e.target.value) || 0))}
                />
                <p className="text-xs text-muted-foreground">
                  {t("settingsPages.llm.timeoutHint")}
                </p>
              </div>
            </div>
          )}
          <DialogFooter className="gap-2 sm:gap-0">
            <Button type="button" variant="outline" onClick={handleTest} disabled={testing || connecting}>
              {testing ? <Loader2 className="h-4 w-4 animate-spin" /> : <Link2 className="mr-1.5 h-4 w-4" />}
              {t("settingsPages.llm.test")}
            </Button>
            <Button type="button" onClick={handleConnect} disabled={connecting || testing}>
              {connecting ? <Loader2 className="h-4 w-4 animate-spin" /> : t("settingsPages.llm.connect")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Endpoint add/edit dialog */}
      <Dialog open={epDialogOpen} onOpenChange={(open) => !open && setEpDialogOpen(false)}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>{editingEndpoint ? t("settingsPages.llm.endpointDialogEditTitle") : t("settingsPages.llm.endpointDialogAddTitle")}</DialogTitle>
          </DialogHeader>
          <div className="space-y-4">
            {!editingEndpoint && (
              <div className="space-y-2">
                <Label>{t("settingsPages.llm.presetLabel")}</Label>
                <Select value={epPreset} onValueChange={onPresetChange}>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {ENDPOINT_PRESETS.map((p) => (
                      <SelectItem key={p.id} value={p.id}>
                        {t(p.labelKey)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            )}
            <div className="space-y-2">
              <Label htmlFor="ep-name">{t("settingsPages.llm.nameLabel")}</Label>
              <Input
                id="ep-name"
                value={epName}
                onChange={(e) => setEpName(e.target.value)}
                placeholder={t("settingsPages.llm.namePlaceholder")}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="ep-base-url">{t("settingsPages.llm.baseUrlLabel")}</Label>
              <Input
                id="ep-base-url"
                value={epBaseURL}
                onChange={(e) => setEpBaseURL(e.target.value)}
                placeholder="http://127.0.0.1:1234/v1"
                className="font-mono text-sm"
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="ep-model">{t("settingsPages.llm.defaultModelLabel")}</Label>
              <Input
                id="ep-model"
                value={epModel}
                onChange={(e) => setEpModel(e.target.value)}
                placeholder={t("settingsPages.llm.defaultModelPlaceholder")}
                className="font-mono text-sm"
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="ep-api-key">{t("settingsPages.llm.apiKeyOptionalLabel")}</Label>
              <Input
                id="ep-api-key"
                type="password"
                value={epApiKey}
                onChange={(e) => setEpApiKey(e.target.value)}
                placeholder={editingEndpoint?.has_api_key ? t("settingsPages.llm.apiKeyChangePlaceholder") : ""}
              />
              {editingEndpoint?.has_api_key && !epApiKey && (
                <p className="text-xs text-muted-foreground">{t("settingsPages.llm.apiKeyBlankHint")}</p>
              )}
            </div>
            <div className="space-y-2">
              <Label htmlFor="ep-timeout">{t("settingsPages.llm.timeoutFieldLabel")}</Label>
              <Input
                id="ep-timeout"
                type="number"
                min={30}
                max={3600}
                value={epTimeout}
                onChange={(e) => setEpTimeout(Math.max(30, Number(e.target.value) || 0))}
              />
            </div>
          </div>
          <DialogFooter className="gap-2 sm:gap-0">
            <Button type="button" variant="outline" onClick={handleEndpointTest} disabled={epTesting || epSaving}>
              {epTesting ? <Loader2 className="h-4 w-4 animate-spin" /> : <Link2 className="mr-1.5 h-4 w-4" />}
              {t("settingsPages.llm.test")}
            </Button>
            <Button type="button" onClick={handleEndpointSave} disabled={epSaving || epTesting}>
              {epSaving ? <Loader2 className="h-4 w-4 animate-spin" /> : editingEndpoint ? t("common.save") : t("settingsPages.llm.add")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
