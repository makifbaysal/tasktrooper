import { Check, Copy, Download, Network, Play, RefreshCw, Save, Square } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { Badge, type BadgeProps } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { CloudflareAccount } from "@/components/remote/CloudflareAccount";
import { NGROK_OTHER_DOMAIN, NgrokAccount } from "@/components/remote/NgrokAccount";
import { PairPhone } from "@/components/remote/PairPhone";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { useI18n } from "@/hooks/useI18n";
import { copyText } from "@/lib/clipboard";
import {
  type DesktopRemoteAccessHost,
  type DesktopRemoteAccessState,
  type DesktopRemoteMode,
  type DesktopRemoteProvider,
  type DesktopRemoteTunnelState,
} from "@/lib/desktop-bridge";
import { isSubdomain, modeHelpKey, modeLabelKey, pairableUrl, providerLabel, subdomainOf } from "@/lib/remoteAccess";

const STATUS_VARIANT: Record<DesktopRemoteTunnelState, BadgeProps["variant"]> = {
  stopped: "secondary",
  starting: "info",
  connected: "success",
  reconnecting: "warning",
  error: "destructive",
  binary_missing: "warning",
};

interface TunnelCardProps {
  host: DesktopRemoteAccessHost;
  state: DesktopRemoteAccessState;
  onState: (state: DesktopRemoteAccessState) => void;
  /** A phone was paired or disconnected. */
  onChanged: () => void;
}

/** The domain choice of the saved hostname, in the shape each provider's form needs it. */
function initialDomain(hostname: string, domains: readonly string[]): string {
  if (hostname === "") return "";
  return domains.includes(hostname) ? hostname : NGROK_OTHER_DOMAIN;
}

export function TunnelCard({ host, state, onState, onChanged }: TunnelCardProps) {
  const { t } = useI18n();
  const { config } = state;
  const [provider, setProvider] = useState<DesktopRemoteProvider>(config.provider);
  const [mode, setMode] = useState<DesktopRemoteMode>(config.mode);
  const [serverName, setServerName] = useState(config.serverName);
  // The address, in the parts each provider's form edits: a subdomain on the
  // logged-in Cloudflare domain, a chosen ngrok domain, or a typed hostname.
  const [subdomain, setSubdomain] = useState("");
  const [ngrokDomain, setNgrokDomain] = useState("");
  const [typedHost, setTypedHost] = useState("");
  const [busy, setBusy] = useState<"save" | "toggle" | "detect" | null>(null);
  const [copied, setCopied] = useState(false);

  const cloudflareDomain = state.cloudflare.domain;
  const suggestedSubdomain = state.cloudflare.suggestedSubdomain;
  const knownDomains = state.ngrok.domains.join(",");

  // The form follows what is saved, and only that: a login finishing or a
  // domain arriving must not throw away a choice made but not saved yet.
  useEffect(() => {
    setProvider(config.provider);
    setMode(config.mode);
    setServerName(config.serverName);
  }, [config.provider, config.mode, config.serverName]);

  useEffect(() => {
    const sub = cloudflareDomain ? subdomainOf(config.hostname, cloudflareDomain) : null;
    setSubdomain(sub ?? (config.hostname === "" ? suggestedSubdomain : ""));
    const pick = initialDomain(config.hostname, knownDomains === "" ? [] : knownDomains.split(","));
    setNgrokDomain(pick);
    setTypedHost(config.hostname !== "" && (pick === NGROK_OTHER_DOMAIN || (config.provider === "cloudflare" && sub === null)) ? config.hostname : "");
  }, [config.provider, config.mode, config.hostname, config.serverName, cloudflareDomain, suggestedSubdomain, knownDomains]);

  const named = mode === "named";
  const cloudflareNamed = provider === "cloudflare" && named;
  const subdomainValue = subdomain.trim().toLowerCase();
  // What the form adds up to. "" means the person has not chosen an address (yet).
  let hostname = "";
  if (cloudflareNamed) hostname = cloudflareDomain ? (subdomainValue !== "" ? `${subdomainValue}.${cloudflareDomain}` : "") : typedHost.trim().toLowerCase();
  else if (provider === "ngrok" && named) hostname = ngrokDomain === NGROK_OTHER_DOMAIN ? typedHost.trim().toLowerCase() : ngrokDomain;
  const invalid = cloudflareNamed && cloudflareDomain !== undefined && subdomainValue !== "" && !isSubdomain(subdomainValue);
  const dirty =
    provider !== config.provider || mode !== config.mode || hostname !== config.hostname || serverName.trim() !== config.serverName;

  const running = state.wanted;
  const detection = state.detection;

  const status = useMemo(() => t(`remoteAccess.tunnel.status.${state.tunnel}`), [state.tunnel, t]);

  const settings = () => ({ provider, mode, hostname: named ? hostname : "", serverName: serverName.trim() });

  const save = async () => {
    setBusy("save");
    try {
      onState(await host.configure(settings()));
      toast.success(t("remoteAccess.tunnel.saved"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("remoteAccess.tunnel.saveFailed"));
    } finally {
      setBusy(null);
    }
  };

  const toggle = async () => {
    setBusy("toggle");
    try {
      if (!running && dirty) await host.configure(settings());
      onState(running ? await host.stop() : await host.start());
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(running ? "remoteAccess.tunnel.stopFailed" : "remoteAccess.tunnel.startFailed"));
    } finally {
      setBusy(null);
    }
  };

  const setAutoStart = async (autoStart: boolean) => {
    try {
      onState(await host.configure({ autoStart }));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("remoteAccess.tunnel.saveFailed"));
    }
  };

  const recheck = async () => {
    setBusy("detect");
    try {
      await host.detect(config.provider);
    } finally {
      setBusy(null);
    }
  };

  const copy = async () => {
    if (!state.url) return;
    if (await copyText(state.url)) {
      setCopied(true);
      toast.success(t("remoteAccess.tunnel.copied"));
      window.setTimeout(() => setCopied(false), 1500);
    } else toast.error(t("remoteAccess.tunnel.copyFailed"));
  };

  return (
    <Card className="w-full space-y-5 p-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="min-w-0">
          <h2 className="flex items-center gap-2 text-heading font-semibold">
            <Network className="h-4 w-4" />
            {t("remoteAccess.tunnel.title")}
          </h2>
          <p className="text-caption text-muted-foreground">{t("remoteAccess.tunnel.description")}</p>
        </div>
        <Badge variant={STATUS_VARIANT[state.tunnel]} data-testid="tunnel-status">
          {status}
        </Badge>
      </div>

      <div className="grid gap-4 sm:grid-cols-2">
        <div className="space-y-1">
          <Label htmlFor="remote-provider">{t("remoteAccess.tunnel.provider")}</Label>
          <Select value={provider} onValueChange={(v) => setProvider(v as DesktopRemoteProvider)} disabled={running}>
            <SelectTrigger id="remote-provider">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="cloudflare">{t("remoteAccess.tunnel.providers.cloudflare")}</SelectItem>
              <SelectItem value="ngrok">{t("remoteAccess.tunnel.providers.ngrok")}</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-1">
          <Label htmlFor="remote-mode">{t("remoteAccess.tunnel.mode")}</Label>
          <Select value={mode} onValueChange={(v) => setMode(v as DesktopRemoteMode)} disabled={running}>
            <SelectTrigger id="remote-mode">
              <SelectValue>{t(modeLabelKey(provider, mode))}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="quick">{t(modeLabelKey(provider, "quick"))}</SelectItem>
              <SelectItem value="named">{t(modeLabelKey(provider, "named"))}</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <p className="text-sm text-muted-foreground sm:col-span-2">{t(modeHelpKey(provider, mode))}</p>

        {cloudflareNamed && (
          <CloudflareAccount
            host={host}
            state={state}
            onState={onState}
            subdomain={subdomain}
            onSubdomain={setSubdomain}
            typedHost={typedHost}
            onTypedHost={setTypedHost}
            hostname={hostname}
          />
        )}

        {provider === "ngrok" && (
          <NgrokAccount
            host={host}
            state={state}
            onState={onState}
            needsDomain={named}
            domain={ngrokDomain}
            onDomain={setNgrokDomain}
            typedHost={typedHost}
            onTypedHost={setTypedHost}
          />
        )}

        <div className="space-y-1 sm:col-span-2">
          <Label htmlFor="remote-server-name">{t("remoteAccess.tunnel.serverName")}</Label>
          <Input
            id="remote-server-name"
            value={serverName}
            onChange={(e) => setServerName(e.target.value)}
            placeholder={t("remoteAccess.tunnel.serverNamePlaceholder")}
            maxLength={80}
            disabled={running}
          />
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <Button variant="outline" onClick={() => void save()} disabled={!dirty || invalid || busy !== null || running}>
          <Save className="mr-2 h-4 w-4" />
          {t("remoteAccess.tunnel.save")}
        </Button>
      </div>

      <div className="space-y-2 rounded-lg border p-3" data-testid="tunnel-binary">
        <p className="text-sm font-medium">{t("remoteAccess.binary.title")}</p>
        {detection?.installed ? (
          <p className="text-sm text-muted-foreground">
            {detection.version
              ? t("remoteAccess.binary.installed", { name: providerLabel(config.provider), version: detection.version })
              : t("remoteAccess.binary.installedNoVersion", { name: providerLabel(config.provider) })}
          </p>
        ) : detection ? (
          <div className="space-y-2">
            <p className="text-sm text-muted-foreground">{t("remoteAccess.binary.missing", { name: providerLabel(config.provider) })}</p>
            {detection.installCommand && (
              <>
                <p className="text-xs text-muted-foreground">{t("remoteAccess.binary.howToInstall")}</p>
                <code className="block break-all rounded bg-muted px-2 py-1 text-xs">{detection.installCommand}</code>
              </>
            )}
            <div className="flex flex-wrap gap-2">
              <Button variant="outline" size="sm" onClick={() => void host.openInstallPage(config.provider)}>
                <Download className="mr-2 h-4 w-4" />
                {t("remoteAccess.binary.openPage")}
              </Button>
              <Button variant="outline" size="sm" onClick={() => void recheck()} disabled={busy === "detect"}>
                <RefreshCw className="mr-2 h-4 w-4" />
                {busy === "detect" ? t("remoteAccess.binary.checking") : t("remoteAccess.binary.recheck")}
              </Button>
            </div>
          </div>
        ) : (
          <Button variant="outline" size="sm" onClick={() => void recheck()} disabled={busy === "detect"}>
            <RefreshCw className="mr-2 h-4 w-4" />
            {busy === "detect" ? t("remoteAccess.binary.checking") : t("remoteAccess.binary.recheck")}
          </Button>
        )}
      </div>

      {state.error && (
        <Notice variant={state.tunnel === "error" ? "error" : "warning"} title={state.problem ? t(`remoteAccess.tunnel.problems.${state.problem}`) : t("remoteAccess.tunnel.status.error")}>
          {state.error}
        </Notice>
      )}

      <div className="flex flex-wrap items-center gap-3">
        <Button onClick={() => void toggle()} disabled={busy !== null || (!running && invalid)} variant={running ? "outline" : "default"}>
          {running ? <Square className="mr-2 h-4 w-4" /> : <Play className="mr-2 h-4 w-4" />}
          {running ? t("remoteAccess.tunnel.stop") : t("remoteAccess.tunnel.start")}
        </Button>
        {state.restarts > 0 && running && (
          <span className="text-xs text-muted-foreground">{t("remoteAccess.tunnel.restarts", { count: state.restarts })}</span>
        )}
      </div>

      <PairPhone url={pairableUrl(state)} serverName={config.serverName} onChanged={onChanged} />

      {state.url && (
        <div className="space-y-1">
          <Label>{t("remoteAccess.tunnel.address")}</Label>
          <div className="flex items-center gap-2">
            <code className="min-w-0 flex-1 truncate rounded bg-muted px-2 py-2 text-sm" data-testid="tunnel-url">
              {state.url}
            </code>
            <Button variant="outline" size="sm" onClick={() => void copy()}>
              {copied ? <Check className="mr-2 h-4 w-4" /> : <Copy className="mr-2 h-4 w-4" />}
              {t("remoteAccess.tunnel.copy")}
            </Button>
          </div>
        </div>
      )}

      <div className="flex items-start justify-between gap-4">
        <div className="space-y-1">
          <Label htmlFor="remote-autostart">{t("remoteAccess.tunnel.autoStart")}</Label>
          <p className="text-xs text-muted-foreground">{t("remoteAccess.tunnel.autoStartHelp")}</p>
        </div>
        <Switch id="remote-autostart" checked={config.autoStart} onCheckedChange={(on) => void setAutoStart(on)} />
      </div>

      <details className="text-sm">
        <summary className="cursor-pointer text-muted-foreground">{t("remoteAccess.tunnel.logs")}</summary>
        <pre className="mt-2 max-h-56 overflow-auto rounded bg-muted p-2 text-xs" data-testid="tunnel-logs">
          {state.logTail.length > 0 ? state.logTail.join("\n") : t("remoteAccess.tunnel.logsEmpty")}
        </pre>
      </details>
    </Card>
  );
}
