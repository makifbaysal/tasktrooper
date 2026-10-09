import { RefreshCw, Unlink } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { api, ApiError, type JiraStatus } from "@/api";
import { IntegrationCard } from "@/components/admin/IntegrationCard";
import { BrandIcon } from "@/components/ui/brand-icon";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

interface JiraCardProps {
  className?: string;
}

/**
 * A pasted API token rather than OAuth: a local server has no address Atlassian
 * could send the callback to. Only Jira Cloud is accepted, because the REST v3
 * API this uses has no self-hosted equivalent.
 */
export function JiraCard({ className }: JiraCardProps) {
  const { t } = useI18n();
  const [status, setStatus] = useState<JiraStatus | null>(null);
  const [checking, setChecking] = useState(false);
  const [saving, setSaving] = useState(false);
  const [siteUrl, setSiteUrl] = useState("");
  const [email, setEmail] = useState("");
  const [token, setToken] = useState("");

  const check = useCallback(async () => {
    setChecking(true);
    try {
      setStatus(await api.getJiraStatus());
    } catch (e) {
      setStatus(null);
      toast.error(e instanceof Error ? e.message : t("common.actionFailed"));
    } finally {
      setChecking(false);
    }
  }, []);

  useEffect(() => {
    void check();
  }, [check]);

  const handleConnect = async () => {
    setSaving(true);
    try {
      const connected = await api.setJira({
        site_url: siteUrl.trim(),
        email: email.trim(),
        api_token: token.trim(),
      });
      setStatus(connected);
      toast.success(
        t("issues.jira.connected", { name: connected.display_name || connected.email }),
      );
    } catch (e) {
      // A rejected site address and a rejected credential are both the user's
      // typo, and the server names which one it was.
      const message =
        e instanceof ApiError && e.type === "invalid_jira_site"
          ? t("issues.jira.invalidSite")
          : e instanceof ApiError && e.type === "jira_auth_failed"
            ? t("issues.jira.authFailed")
            : e instanceof Error
              ? e.message
              : t("issues.jira.connectFailed");
      toast.error(message);
    } finally {
      setToken("");
      setSaving(false);
    }
  };

  const handleDisconnect = async () => {
    setSaving(true);
    try {
      await api.disconnectJira();
      setStatus({ connected: false, site_url: "", email: "" });
      toast.success(t("issues.jira.disconnected"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.actionFailed"));
    } finally {
      setSaving(false);
    }
  };

  const statusLabel =
    status === null
      ? undefined
      : status.connected
        ? { tone: "connected" as const, label: t("settingsPages.integrations.status.connected") }
        : { tone: "idle" as const, label: t("settingsPages.integrations.status.notConnected") };

  return (
    <IntegrationCard
      className={className}
      icon={<BrandIcon brand="jira" className="h-5 w-5" />}
      name={t("issues.jira.title")}
      {...(statusLabel ? { status: statusLabel } : {})}
      actions={
        <>
          {status?.connected && (
            <Button variant="outline" size="sm" onClick={() => void handleDisconnect()} disabled={saving}>
              <Unlink className="mr-1.5 h-3.5 w-3.5" />
              {t("issues.jira.disconnect")}
            </Button>
          )}
          <Button
            variant="ghost"
            size="icon"
            className="h-8 w-8"
            onClick={() => void check()}
            disabled={checking}
            title={t("common.refresh")}
            aria-label={t("common.refresh")}
          >
            <RefreshCw className={cn("h-3.5 w-3.5", checking && "animate-spin")} />
          </Button>
        </>
      }
    >
      {status === null ? (
        <p className="text-sm text-muted-foreground">{t("issues.jira.statusUnavailable")}</p>
      ) : status.connected ? (
        <div className="min-w-0 space-y-0.5">
          <p className="truncate text-sm font-medium">{status.site_url}</p>
          <p className="truncate text-xs text-muted-foreground">
            {t("issues.jira.connectedAs", { email: status.email })}
          </p>
        </div>
      ) : (
        <div className="space-y-3">
          <div className="space-y-2">
            <Label htmlFor="jira-site-url">{t("issues.jira.siteUrl")}</Label>
            <Input
              id="jira-site-url"
              placeholder={t("issues.jira.siteUrlPlaceholder")}
              value={siteUrl}
              onChange={(e) => setSiteUrl(e.target.value)}
            />
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="jira-email">{t("issues.jira.email")}</Label>
              <Input id="jira-email" type="email" value={email} onChange={(e) => setEmail(e.target.value)} />
            </div>
            <div className="space-y-2">
              <Label htmlFor="jira-api-token">{t("issues.jira.apiToken")}</Label>
              <Input
                id="jira-api-token"
                type="password"
                autoComplete="off"
                value={token}
                onChange={(e) => setToken(e.target.value)}
              />
            </div>
          </div>
          <div className="flex flex-wrap items-center gap-3">
            <Button
              size="sm"
              onClick={() => void handleConnect()}
              disabled={saving || !siteUrl.trim() || !email.trim() || !token.trim()}
            >
              {t("issues.jira.connect")}
            </Button>
            <a
              href="https://id.atlassian.com/manage-profile/security/api-tokens"
              target="_blank"
              rel="noreferrer"
              className="text-xs text-primary hover:underline"
            >
              {t("issues.jira.tokenHelp")}
            </a>
          </div>
        </div>
      )}
    </IntegrationCard>
  );
}
