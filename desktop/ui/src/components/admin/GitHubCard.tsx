import { RefreshCw, Unlink } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { api, type GitHubConnectionStatus } from "@/api";
import { BrandIcon } from "@/components/ui/brand-icon";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Notice } from "@/components/ui/notice";
import { GitHubDeviceConnect } from "@/components/admin/GitHubDeviceConnect";
import { IntegrationCard, type IntegrationStatusTone } from "@/components/admin/IntegrationCard";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

interface GitHubCardProps {
  /**
   * Every outcome this card learns, for a caller deriving something from it.
   * A null status with a message is a check that could not run, which is never
   * the same as "not connected" — see the guided setup, which renders the two
   * differently.
   */
  onStatusChange?: (result: { status: GitHubConnectionStatus | null; error: string }) => void;
  className?: string;
}

/**
 * Connecting the GitHub account the agents push with.
 *
 * A pasted personal access token, not an OAuth hop: there is no gateway to hold
 * an OAuth app's client secret, and a local server cannot receive GitHub's
 * callback. The server verifies the token against the API before storing it
 * encrypted, so a typo fails here rather than at the first push.
 *
 * One component for two screens — Settings → Integrations and step 3 of the
 * guided setup — because it is one connection, and a second affordance for it
 * would be a second place to read a different answer from.
 */
export function GitHubCard({ onStatusChange, className }: GitHubCardProps) {
  const { t } = useI18n();
  const [status, setStatus] = useState<GitHubConnectionStatus | null>(null);
  const [checking, setChecking] = useState(false);
  const [saving, setSaving] = useState(false);
  const [token, setToken] = useState("");
  const [useToken, setUseToken] = useState(false);

  // Held in a ref, not a dependency: callers pass an inline arrow, and making
  // `check` depend on it would restart the mount effect on every render of
  // whatever owns this card.
  const onStatusChangeRef = useRef(onStatusChange);
  onStatusChangeRef.current = onStatusChange;

  const apply = useCallback((next: GitHubConnectionStatus | null, error: string) => {
    setStatus(next);
    onStatusChangeRef.current?.({ status: next, error });
  }, []);

  const check = useCallback(async () => {
    setChecking(true);
    try {
      apply(await api.githubStatus(), "");
    } catch (e) {
      // Null is "could not tell", not "not connected" — the render below says
      // exactly that, and the caller is handed the sentence to say it too.
      apply(null, e instanceof Error ? e.message : String(e));
    } finally {
      setChecking(false);
    }
  }, [apply]);

  useEffect(() => {
    void check();
  }, [check]);

  const handleConnect = async () => {
    const value = token.trim();
    if (!value) return;
    setSaving(true);
    try {
      apply(await api.setGitHubToken(value), "");
      setToken("");
      toast.success(t("settings.github.connectedToast"));
    } catch (e) {
      // The server's own sentence: it names what GitHub refused, which a
      // generic failure cannot.
      toast.error(e instanceof Error ? e.message : t("settings.github.connectFailedToast"));
    } finally {
      setSaving(false);
    }
  };

  const handleDisconnect = async () => {
    setSaving(true);
    try {
      apply(await api.disconnectGitHub(), "");
      toast.success(t("settings.github.disconnectedToast"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.actionFailed"));
    } finally {
      setSaving(false);
    }
  };

  const status_ = statusBadge(status, t);

  return (
    <IntegrationCard
      className={className}
      icon={<BrandIcon brand="github" className="h-5 w-5" />}
      name="GitHub"
      status={status_}
      actions={
        <>
          {status?.connected && (
            <Button variant="outline" size="sm" onClick={() => void handleDisconnect()} disabled={saving}>
              <Unlink className="mr-1.5 h-3.5 w-3.5" />
              {t("settings.github.disconnect")}
            </Button>
          )}
          <Button
            variant="ghost"
            size="icon"
            className="h-8 w-8"
            onClick={() => void check()}
            disabled={checking}
            title={t("settingsPages.integrations.refresh")}
            aria-label={t("settingsPages.integrations.refresh")}
          >
            <RefreshCw className={cn("h-3.5 w-3.5", checking && "animate-spin")} />
          </Button>
        </>
      }
    >
      {status === null ? (
        <p className="text-sm text-muted-foreground">{t("settings.github.statusUnavailable")}</p>
      ) : status.connected ? (
        <>
          <div className="flex items-center gap-3">
            <div className="flex h-8 w-8 items-center justify-center rounded-full bg-primary/10 text-sm font-semibold uppercase text-primary">
              {(status.login ?? "?").slice(0, 1)}
            </div>
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">{status.login}</p>
              {status.mode && (
                <p className="text-xs text-muted-foreground">
                  {status.mode === "app" ? t("settings.github.modeApp") : t("settings.github.modeToken")}
                </p>
              )}
            </div>
          </div>
          {status.needs_install && (
            <Notice variant="warning" title={t("settings.github.needsInstallTitle")}>
              <p>{t("settings.github.needsInstallBody")}</p>
              {status.install_url && (
                <Button size="sm" className="mt-2" asChild>
                  <a href={status.install_url} target="_blank" rel="noreferrer">
                    {t("settings.github.installApp")}
                  </a>
                </Button>
              )}
            </Notice>
          )}
          {status.missing_scopes && status.missing_scopes.length > 0 && (
            <Notice
              variant="warning"
              title={t("settings.github.missingScopesTitle", { scopes: status.missing_scopes.join(", ") })}
            >
              {status.missing_scopes.includes("workflow") ? t("settings.github.missingWorkflowScope") : null}
            </Notice>
          )}
          {status.fine_grained && <Notice variant="info" title={t("settings.github.fineGrainedHint")} />}
        </>
      ) : (
        <>
          {status.expired && (
            <Notice variant="warning" title={t("settings.github.expiredTitle")}>
              {t("settings.github.expiredBody")}
            </Notice>
          )}
          {status.detail && !status.expired && <p className="text-sm text-destructive">{status.detail}</p>}
          {status.app_available && !useToken ? (
            <>
              <GitHubDeviceConnect onConnected={() => void check()} />
              <Button variant="link" size="sm" className="h-auto px-0" onClick={() => setUseToken(true)}>
                {t("settings.github.useTokenInstead")}
              </Button>
            </>
          ) : (
            <div className="space-y-2">
              <div className="flex flex-wrap gap-2">
                <Input
                  type="password"
                  autoComplete="off"
                  className="min-w-[16rem] flex-1"
                  placeholder={t("settings.github.tokenPlaceholder")}
                  value={token}
                  onChange={(e) => setToken(e.target.value)}
                />
                <Button onClick={() => void handleConnect()} disabled={saving || !token.trim()}>
                  {t("settings.github.connect")}
                </Button>
              </div>
              <p className="text-xs text-muted-foreground">{t("settings.github.tokenHelp")}</p>
              {status.app_available && (
                <Button variant="link" size="sm" className="h-auto px-0" onClick={() => setUseToken(false)}>
                  {t("settings.github.useAppInstead")}
                </Button>
              )}
            </div>
          )}
        </>
      )}
    </IntegrationCard>
  );
}

function statusBadge(
  status: GitHubConnectionStatus | null,
  t: (key: string, params?: Record<string, string | number>) => string,
): { tone: IntegrationStatusTone; label: string } | undefined {
  if (status === null) return undefined;
  if (status.connected && (status.needs_install || (status.missing_scopes?.length ?? 0) > 0)) {
    return { tone: "attention", label: t("settingsPages.integrations.status.attention") };
  }
  if (status.connected) return { tone: "connected", label: t("settingsPages.integrations.status.connected") };
  if (status.expired) return { tone: "attention", label: t("settingsPages.integrations.status.expired") };
  return { tone: "idle", label: t("settingsPages.integrations.status.notConnected") };
}
