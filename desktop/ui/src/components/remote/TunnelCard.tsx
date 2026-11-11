import { Check, Copy, Download, Network, Play, RefreshCw, Save, Square } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { Badge, type BadgeProps } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
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
import { modeHelpKey, providerLabel } from "@/lib/remoteAccess";

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
}

export function TunnelCard({ host, state, onState }: TunnelCardProps) {
  const { t } = useI18n();
  const { config } = state;
  const [provider, setProvider] = useState<DesktopRemoteProvider>(config.provider);
  const [mode, setMode] = useState<DesktopRemoteMode>(config.mode);
  const [hostname, setHostname] = useState(config.hostname);
  const [serverName, setServerName] = useState(config.serverName);
  const [secret, setSecret] = useState("");
  const [busy, setBusy] = useState<"save" | "toggle" | "detect" | null>(null);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    setProvider(config.provider);
    setMode(config.mode);
    setHostname(config.hostname);
    setServerName(config.serverName);
  }, [config.provider, config.mode, config.hostname, config.serverName]);

  const secretKey = provider === "cloudflare" ? "cloudflareToken" : "ngrokAuthtoken";
  const secretStored = provider === "cloudflare" ? state.secrets.cloudflareToken : state.secrets.ngrokAuthtoken;
  const needsSecret = provider === "ngrok" || mode === "named";
  const needsHostname = mode === "named";
  const dirty =
    provider !== config.provider ||
    mode !== config.mode ||
    hostname.trim() !== config.hostname ||
    serverName.trim() !== config.serverName ||
    secret !== "";

  const running = state.wanted;
  const detection = state.detection;
  const service = `http://127.0.0.1:${state.remotePort ?? state.suggestedPort}`;

  const status = useMemo(() => t(`remoteAccess.tunnel.status.${state.tunnel}`), [state.tunnel, t]);

  const save = async () => {
    setBusy("save");
    try {
      const next = await host.configure({
        provider,
        mode,
        hostname: needsHostname ? hostname.trim() : "",
        serverName: serverName.trim(),
        ...(secret !== "" ? { [secretKey]: secret.trim() } : {}),
      });
      setSecret("");
      onState(next);
      toast.success(t("remoteAccess.tunnel.saved"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("remoteAccess.tunnel.saveFailed"));
    } finally {
      setBusy(null);
    }
  };

  const forget = async () => {
    setBusy("save");
    try {
      onState(await host.configure({ clearSecrets: [secretKey] }));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("remoteAccess.tunnel.saveFailed"));
    } finally {
      setBusy(null);
    }
  };

  const toggle = async () => {
    setBusy("toggle");
    try {
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
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="quick">{t("remoteAccess.tunnel.modes.quick")}</SelectItem>
              <SelectItem value="named">{t("remoteAccess.tunnel.modes.named")}</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <p className="text-sm text-muted-foreground sm:col-span-2">{t(modeHelpKey(provider, mode))}</p>

        {needsSecret && (
          <div className="space-y-1 sm:col-span-2">
            <Label htmlFor="remote-secret">
              {t(provider === "cloudflare" ? "remoteAccess.tunnel.cloudflareToken" : "remoteAccess.tunnel.ngrokAuthtoken")}
            </Label>
            <div className="flex items-center gap-2">
              <Input
                id="remote-secret"
                type="password"
                autoComplete="off"
                spellCheck={false}
                value={secret}
                onChange={(e) => setSecret(e.target.value)}
                placeholder={secretStored ? t("remoteAccess.tunnel.secretStored") : ""}
                disabled={running}
              />
              {secretStored && !running && (
                <Button type="button" variant="outline" size="sm" onClick={() => void forget()} disabled={busy !== null}>
                  {t("remoteAccess.tunnel.secretClear")}
                </Button>
              )}
            </div>
            <p className="text-xs text-muted-foreground">
              {t(provider === "cloudflare" ? "remoteAccess.tunnel.cloudflareTokenHelp" : "remoteAccess.tunnel.ngrokAuthtokenHelp")}
            </p>
          </div>
        )}

        {needsHostname && (
          <div className="space-y-1 sm:col-span-2">
            <Label htmlFor="remote-hostname">{t("remoteAccess.tunnel.hostname")}</Label>
            <Input
              id="remote-hostname"
              value={hostname}
              onChange={(e) => setHostname(e.target.value)}
              placeholder={t(
                provider === "cloudflare"
                  ? "remoteAccess.tunnel.hostnamePlaceholderCloudflare"
                  : "remoteAccess.tunnel.hostnamePlaceholderNgrok",
              )}
              autoComplete="off"
              spellCheck={false}
              disabled={running}
            />
            <p className="text-xs text-muted-foreground">
              {t(provider === "cloudflare" ? "remoteAccess.tunnel.hostnameHelpCloudflare" : "remoteAccess.tunnel.hostnameHelpNgrok")}
            </p>
            {provider === "cloudflare" && (
              <p className="text-xs text-muted-foreground">
                {t("remoteAccess.tunnel.cloudflareIngress", { service })}
              </p>
            )}
          </div>
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
        <Button variant="outline" onClick={() => void save()} disabled={!dirty || busy !== null || running}>
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
        <Button onClick={() => void toggle()} disabled={busy !== null} variant={running ? "outline" : "default"}>
          {running ? <Square className="mr-2 h-4 w-4" /> : <Play className="mr-2 h-4 w-4" />}
          {running ? t("remoteAccess.tunnel.stop") : t("remoteAccess.tunnel.start")}
        </Button>
        {state.restarts > 0 && running && (
          <span className="text-xs text-muted-foreground">{t("remoteAccess.tunnel.restarts", { count: state.restarts })}</span>
        )}
      </div>

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
