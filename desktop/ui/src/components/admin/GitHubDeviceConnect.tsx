import { Copy, ExternalLink, GitBranch, Loader2 } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { api, type GitHubDeviceFlow } from "@/api";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/notice";
import { useI18n } from "@/hooks/useI18n";

const MIN_POLL_MS = 2000;

interface GitHubDeviceConnectProps {
  /** Called once GitHub reports the code approved and the server stored the connection. */
  onConnected: () => void;
}

/**
 * "Connect with GitHub": the device flow `gh auth login` uses. The server asks
 * GitHub for a one-time code, the user types it on github.com, and this polls
 * until GitHub says yes — no client secret, no callback URL, no token to paste.
 */
export function GitHubDeviceConnect({ onConnected }: GitHubDeviceConnectProps) {
  const { t } = useI18n();
  const [flow, setFlow] = useState<GitHubDeviceFlow | null>(null);
  const [starting, setStarting] = useState(false);

  const start = async () => {
    setStarting(true);
    try {
      setFlow(await api.startGitHubDeviceFlow());
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("settings.github.connectFailedToast"));
    } finally {
      setStarting(false);
    }
  };

  useEffect(() => {
    if (!flow || flow.state !== "pending") return;
    const id = setTimeout(
      async () => {
        try {
          const next = await api.pollGitHubDeviceFlow(flow.id);
          setFlow(next);
          if (next.state === "authorized") {
            toast.success(t("settings.github.connectedToast"));
            onConnected();
          }
        } catch (e) {
          toast.error(e instanceof Error ? e.message : t("settings.github.connectFailedToast"));
          setFlow(null);
        }
      },
      Math.max(MIN_POLL_MS, flow.interval_seconds * 1000),
    );
    return () => clearTimeout(id);
  }, [flow, onConnected, t]);

  const copy = async () => {
    if (!flow) return;
    try {
      await navigator.clipboard.writeText(flow.user_code);
      toast.success(t("settings.github.codeCopied"));
    } catch {
      // Clipboard access can be refused; the code is on screen to type anyway.
    }
  };

  if (!flow || flow.state === "authorized") {
    return (
      <div className="space-y-2">
        <Button onClick={() => void start()} disabled={starting}>
          {starting ? <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" /> : <GitBranch className="mr-2 h-3.5 w-3.5" />}
          {t("settings.github.connectWithGitHub")}
        </Button>
        <p className="text-xs text-muted-foreground">{t("settings.github.connectWithGitHubHint")}</p>
      </div>
    );
  }

  if (flow.state !== "pending") {
    return (
      <Notice
        variant="warning"
        title={flow.state === "expired" ? t("settings.github.flowExpired") : t("settings.github.flowDenied")}
      >
        <Button size="sm" variant="outline" className="mt-1" onClick={() => void start()} disabled={starting}>
          {t("settings.github.tryAgain")}
        </Button>
      </Notice>
    );
  }

  return (
    <div className="space-y-3 rounded-md border border-border/60 p-4">
      <p className="text-sm font-medium">{t("settings.github.deviceCodeTitle")}</p>
      <div className="flex flex-wrap items-center gap-3">
        <code className="rounded-md bg-muted px-3 py-2 font-mono text-2xl font-semibold tracking-widest" data-testid="github-user-code">
          {flow.user_code}
        </code>
        <Button size="sm" variant="outline" onClick={() => void copy()}>
          <Copy className="mr-2 h-3.5 w-3.5" />
          {t("settings.github.copyCode")}
        </Button>
        <Button size="sm" asChild>
          <a href={flow.verification_uri} target="_blank" rel="noreferrer">
            <ExternalLink className="mr-2 h-3.5 w-3.5" />
            {t("settings.github.openGitHub")}
          </a>
        </Button>
      </div>
      <p className="text-xs text-muted-foreground">{t("settings.github.deviceCodeHint", { url: flow.verification_uri })}</p>
      <div className="flex items-center justify-between">
        <span className="inline-flex items-center gap-2 text-xs text-muted-foreground">
          <Loader2 className="h-3 w-3 animate-spin" />
          {t("settings.github.waitingForApproval")}
        </span>
        <Button size="sm" variant="ghost" onClick={() => setFlow(null)}>
          {t("settings.github.cancel")}
        </Button>
      </div>
    </div>
  );
}
