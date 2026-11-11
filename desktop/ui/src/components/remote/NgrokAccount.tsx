import { ExternalLink, Link2Off, Plug } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Spinner } from "@/components/ui/spinner";
import { useI18n } from "@/hooks/useI18n";
import type { DesktopRemoteAccessHost, DesktopRemoteAccessState } from "@/lib/desktop-bridge";

/** The select value for "type a domain myself". */
export const NGROK_OTHER_DOMAIN = "__other__";

interface NgrokAccountProps {
  host: DesktopRemoteAccessHost;
  state: DesktopRemoteAccessState;
  onState: (state: DesktopRemoteAccessState) => void;
  /** "My own domain" mode: a domain of the account is needed. */
  needsDomain: boolean;
  domain: string;
  onDomain: (value: string) => void;
  typedHost: string;
  onTypedHost: (value: string) => void;
}

/**
 * ngrok has no browser login for its command line. The honest version of
 * "log in": paste the authtoken once (it is proven with ngrok before it is
 * kept), and optionally an API key, which is the only thing that can list the
 * account's domains.
 */
export function NgrokAccount({ host, state, onState, needsDomain, domain, onDomain, typedHost, onTypedHost }: NgrokAccountProps) {
  const { t } = useI18n();
  const account = state.ngrok;
  const running = state.wanted;
  const connected = state.secrets.ngrokAuthtoken && account.verified;
  const checking = account.phase === "checking";
  const [authtoken, setAuthtoken] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [busy, setBusy] = useState(false);

  const run = async (action: () => Promise<DesktopRemoteAccessState>, failure: string, onDone?: () => void) => {
    setBusy(true);
    try {
      onState(await action());
      onDone?.();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : failure);
    } finally {
      setBusy(false);
    }
  };

  const connect = () => run(() => host.ngrokConnect({ authtoken: authtoken.trim() }), t("remoteAccess.ngrok.connectFailed"), () => setAuthtoken(""));
  const listDomains = () =>
    run(() => host.ngrokConnect(apiKey.trim() !== "" ? { apiKey: apiKey.trim() } : {}), t("remoteAccess.ngrok.apiKeyFailed"), () => setApiKey(""));

  const options = account.domains;
  const selectValue = domain === "" ? undefined : domain;

  return (
    <div className="space-y-3 rounded-lg border p-3 sm:col-span-2" data-testid="ngrok-account">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-sm font-medium">{t("remoteAccess.ngrok.title")}</p>
        {connected && <Badge variant="success">{t("remoteAccess.ngrok.connected")}</Badge>}
      </div>

      {account.error && (
        <Notice variant="error" title={t("remoteAccess.ngrok.connectFailed")}>
          {account.error}
        </Notice>
      )}

      {!connected ? (
        <div className="space-y-2">
          <p className="text-sm text-muted-foreground">{t("remoteAccess.ngrok.authtokenHelp")}</p>
          <Label htmlFor="ngrok-authtoken">{t("remoteAccess.ngrok.authtoken")}</Label>
          <div className="flex items-center gap-2">
            <Input
              id="ngrok-authtoken"
              type="password"
              autoComplete="off"
              spellCheck={false}
              value={authtoken}
              onChange={(e) => setAuthtoken(e.target.value)}
              disabled={busy || checking}
            />
            <Button onClick={() => void connect()} disabled={authtoken.trim() === "" || busy || checking}>
              <Plug className="mr-2 h-4 w-4" />
              {t("remoteAccess.ngrok.connect")}
            </Button>
          </div>
          {checking && (
            <p className="flex items-center gap-2 text-sm text-muted-foreground" role="status">
              <Spinner size="sm" />
              {t("remoteAccess.ngrok.checking")}
            </p>
          )}
          <a
            href="https://dashboard.ngrok.com/get-started/your-authtoken"
            target="_blank"
            rel="noreferrer"
            className="inline-flex items-center gap-1 text-xs underline"
          >
            <ExternalLink className="h-3 w-3" />
            {t("remoteAccess.ngrok.getToken")}
          </a>
        </div>
      ) : (
        <div className="space-y-3">
          {needsDomain && (
            <div className="space-y-1">
              <Label htmlFor="ngrok-domain">{t("remoteAccess.ngrok.domain")}</Label>
              <Select value={selectValue} onValueChange={onDomain} disabled={running}>
                <SelectTrigger id="ngrok-domain">
                  <SelectValue placeholder={t("remoteAccess.ngrok.domainNone")} />
                </SelectTrigger>
                <SelectContent>
                  {options.map((d) => (
                    <SelectItem key={d} value={d}>
                      {d}
                    </SelectItem>
                  ))}
                  <SelectItem value={NGROK_OTHER_DOMAIN}>{t("remoteAccess.ngrok.domainOther")}</SelectItem>
                </SelectContent>
              </Select>
              {domain === NGROK_OTHER_DOMAIN && (
                <Input
                  aria-label={t("remoteAccess.ngrok.domain")}
                  value={typedHost}
                  onChange={(e) => onTypedHost(e.target.value)}
                  placeholder={t("remoteAccess.ngrok.domainPlaceholder")}
                  autoComplete="off"
                  spellCheck={false}
                  disabled={running}
                />
              )}
              <p className="text-xs text-muted-foreground">{t("remoteAccess.ngrok.domainHelp")}</p>
              {options.length > 0 && <p className="text-xs text-muted-foreground">{t("remoteAccess.ngrok.domainsListed", { count: options.length })}</p>}
            </div>
          )}

          {needsDomain && (
            <div className="space-y-1">
              {account.apiKeyError && (
                <Notice variant="error" title={t("remoteAccess.ngrok.apiKeyFailed")}>
                  {account.apiKeyError}
                </Notice>
              )}
              <Label htmlFor="ngrok-api-key">{t("remoteAccess.ngrok.apiKey")}</Label>
              <div className="flex items-center gap-2">
                <Input
                  id="ngrok-api-key"
                  type="password"
                  autoComplete="off"
                  spellCheck={false}
                  value={apiKey}
                  onChange={(e) => setApiKey(e.target.value)}
                  placeholder={state.secrets.ngrokApiKey ? "••••••••" : ""}
                  disabled={busy || running}
                />
                <Button
                  variant="outline"
                  onClick={() => void listDomains()}
                  disabled={busy || running || (apiKey.trim() === "" && !state.secrets.ngrokApiKey)}
                >
                  {apiKey.trim() === "" && state.secrets.ngrokApiKey ? t("remoteAccess.ngrok.refreshDomains") : t("remoteAccess.ngrok.listDomains")}
                </Button>
              </div>
              <p className="text-xs text-muted-foreground">{t("remoteAccess.ngrok.apiKeyHelp")}</p>
            </div>
          )}

          <Button
            variant="outline"
            size="sm"
            onClick={() => void run(() => host.ngrokDisconnect(), t("remoteAccess.ngrok.disconnectFailed"))}
            disabled={busy || running}
          >
            <Link2Off className="mr-2 h-4 w-4" />
            {t("remoteAccess.ngrok.disconnect")}
          </Button>
        </div>
      )}
    </div>
  );
}
