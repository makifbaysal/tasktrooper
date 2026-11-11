import { Check, Copy, LogIn, LogOut } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Spinner } from "@/components/ui/spinner";
import { useI18n } from "@/hooks/useI18n";
import { copyText } from "@/lib/clipboard";
import type { DesktopRemoteAccessHost, DesktopRemoteAccessState } from "@/lib/desktop-bridge";
import { isSubdomain } from "@/lib/remoteAccess";

interface CloudflareAccountProps {
  host: DesktopRemoteAccessHost;
  state: DesktopRemoteAccessState;
  onState: (state: DesktopRemoteAccessState) => void;
  /** The subdomain in front of the logged-in domain. */
  subdomain: string;
  onSubdomain: (value: string) => void;
  /** The whole address, when the domain could not be read. */
  typedHost: string;
  onTypedHost: (value: string) => void;
  /** The address these fields add up to: what the phone will use. */
  hostname: string;
}

/**
 * Cloudflare for "my own domain": a browser login instead of a pasted token.
 * The login is `cloudflared tunnel login`; what it leaves behind decides what
 * this shows (logged in as which domain), so there is nothing to copy by hand.
 */
export function CloudflareAccount({ host, state, onState, subdomain, onSubdomain, typedHost, onTypedHost, hostname }: CloudflareAccountProps) {
  const { t } = useI18n();
  const account = state.cloudflare;
  const running = state.wanted;
  const [busy, setBusy] = useState(false);
  const [confirmLogout, setConfirmLogout] = useState(false);
  const [copied, setCopied] = useState(false);

  const run = async (action: () => Promise<DesktopRemoteAccessState>, failure: string) => {
    setBusy(true);
    try {
      onState(await action());
    } catch (e) {
      toast.error(e instanceof Error ? e.message : failure);
    } finally {
      setBusy(false);
    }
  };

  const copyLink = async () => {
    if (!account.loginUrl) return;
    if (await copyText(account.loginUrl)) {
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    } else toast.error(t("remoteAccess.pair.copyFailed"));
  };

  const waiting = account.phase === "waiting";
  const subdomainInvalid = account.domain !== undefined && subdomain !== "" && !isSubdomain(subdomain.trim().toLowerCase());

  return (
    <div className="space-y-3 rounded-lg border p-3 sm:col-span-2" data-testid="cloudflare-account">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-sm font-medium">{t("remoteAccess.cloudflare.title")}</p>
        {account.loggedIn && (
          <div className="flex items-center gap-2">
            <Badge variant="success">{t("remoteAccess.cloudflare.loggedIn")}</Badge>
            {account.domain && <span className="text-sm text-muted-foreground">{t("remoteAccess.cloudflare.domain", { domain: account.domain })}</span>}
          </div>
        )}
      </div>

      {account.error && !waiting && (
        <Notice variant="error" title={t("remoteAccess.cloudflare.loginFailed")}>
          {account.error}
        </Notice>
      )}

      {!account.loggedIn && (
        <div className="space-y-3">
          <p className="text-sm text-muted-foreground">{t("remoteAccess.cloudflare.loggedOutHelp")}</p>
          {waiting ? (
            <div className="space-y-2">
              <div className="flex flex-wrap items-center gap-3">
                <span className="flex items-center gap-2 text-sm" role="status">
                  <Spinner size="sm" />
                  {t("remoteAccess.cloudflare.waiting")}
                </span>
                <Button variant="outline" size="sm" onClick={() => void run(() => host.cloudflareLoginCancel(), t("remoteAccess.cloudflare.loginFailed"))}>
                  {t("remoteAccess.cloudflare.cancel")}
                </Button>
              </div>
              <p className="text-xs text-muted-foreground">{t("remoteAccess.cloudflare.waitingHelp")}</p>
              {account.loginUrl && (
                <div className="space-y-1">
                  <p className="text-xs text-muted-foreground">{t("remoteAccess.cloudflare.linkHelp")}</p>
                  <div className="flex items-center gap-2">
                    <a
                      href={account.loginUrl}
                      target="_blank"
                      rel="noreferrer"
                      className="min-w-0 flex-1 truncate rounded bg-muted px-2 py-2 text-xs underline"
                      data-testid="cloudflare-login-url"
                    >
                      {account.loginUrl}
                    </a>
                    <Button variant="outline" size="sm" onClick={() => void copyLink()}>
                      {copied ? <Check className="mr-2 h-4 w-4" /> : <Copy className="mr-2 h-4 w-4" />}
                      {t("remoteAccess.cloudflare.copyLink")}
                    </Button>
                  </div>
                </div>
              )}
            </div>
          ) : (
            <Button onClick={() => void run(() => host.cloudflareLogin(), t("remoteAccess.cloudflare.loginFailed"))} disabled={busy}>
              <LogIn className="mr-2 h-4 w-4" />
              {t("remoteAccess.cloudflare.login")}
            </Button>
          )}
        </div>
      )}

      {account.loggedIn && (
        <div className="space-y-3">
          {account.domain ? (
            <div className="space-y-1">
              <Label htmlFor="cloudflare-subdomain">{t("remoteAccess.cloudflare.subdomain")}</Label>
              <div className="flex items-center gap-2">
                <Input
                  id="cloudflare-subdomain"
                  value={subdomain}
                  onChange={(e) => onSubdomain(e.target.value)}
                  autoComplete="off"
                  spellCheck={false}
                  disabled={running}
                  aria-invalid={subdomainInvalid}
                  className="max-w-xs"
                />
                <span className="truncate text-sm text-muted-foreground">.{account.domain}</span>
              </div>
              <p className={subdomainInvalid ? "text-xs text-destructive" : "text-xs text-muted-foreground"}>
                {subdomainInvalid ? t("remoteAccess.cloudflare.subdomainInvalid") : t("remoteAccess.cloudflare.subdomainHelp")}
              </p>
              {hostname !== "" && !subdomainInvalid && (
                <p className="break-all text-xs text-muted-foreground">{t("remoteAccess.cloudflare.preview", { url: `https://${hostname}` })}</p>
              )}
            </div>
          ) : (
            <div className="space-y-1">
              <Label htmlFor="cloudflare-hostname">{t("remoteAccess.cloudflare.fullHostname")}</Label>
              <Input
                id="cloudflare-hostname"
                value={typedHost}
                onChange={(e) => onTypedHost(e.target.value)}
                placeholder={t("remoteAccess.cloudflare.fullHostnamePlaceholder")}
                autoComplete="off"
                spellCheck={false}
                disabled={running}
              />
              <p className="text-xs text-muted-foreground">{t("remoteAccess.cloudflare.fullHostnameHelp", { reason: account.domainError ?? "?" })}</p>
            </div>
          )}
          <Button variant="outline" size="sm" onClick={() => setConfirmLogout(true)} disabled={busy || running}>
            <LogOut className="mr-2 h-4 w-4" />
            {t("remoteAccess.cloudflare.logout")}
          </Button>
        </div>
      )}

      <ConfirmDialog
        open={confirmLogout}
        onOpenChange={setConfirmLogout}
        title={t("remoteAccess.cloudflare.logoutTitle")}
        description={t("remoteAccess.cloudflare.logoutBody")}
        confirmLabel={t("remoteAccess.cloudflare.logout")}
        onConfirm={async () => {
          try {
            onState(await host.cloudflareLogout());
            toast.success(t("remoteAccess.cloudflare.loggedOutToast"));
            return true;
          } catch (e) {
            toast.error(e instanceof Error ? e.message : t("remoteAccess.cloudflare.logoutFailed"));
            return false;
          }
        }}
      />
    </div>
  );
}
