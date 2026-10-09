import { Loader2, LogIn, LogOut, UserRound } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { api } from "@/api";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { useI18n } from "@/hooks/useI18n";
import { desktopAccount } from "@/lib/desktop-bridge";

type AccountView = { mode: "local" | "account"; origin?: string };

/**
 * What stands between the button and the switch: nothing when no run is in
 * flight, the number of runs it would stop when some are, and a plain
 * question when that could not be found out.
 */
type Confirm = { kind: "runs"; count: number } | { kind: "plain" };

/**
 * Settings → General's account switch.
 *
 * Connecting stops this computer's local server and loads the account's
 * sign-in page in this window, so this page is gone once it works; a run the
 * local server is in the middle of stops with it, which is why the switch asks
 * first when the backend says one is active.
 */
export function AccountCard() {
  const { t } = useI18n();
  const account = desktopAccount();
  const [state, setState] = useState<AccountView | null>(null);
  const [confirm, setConfirm] = useState<Confirm | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!account) return;
    void account.state().then(setState, () => undefined);
  }, [account]);

  const connect = useCallback(async () => {
    if (!account) return;
    setConfirm(null);
    setBusy(true);
    try {
      await account.signIn();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("settings.account.failed"));
    } finally {
      setBusy(false);
    }
  }, [account, t]);

  const askThenConnect = useCallback(async () => {
    setBusy(true);
    let count: number | null;
    try {
      count = (await api.activeRuns()).runs?.length ?? 0;
    } catch {
      count = null;
    }
    setBusy(false);
    if (count === 0) {
      await connect();
      return;
    }
    setConfirm(count === null ? { kind: "plain" } : { kind: "runs", count });
  }, [connect]);

  const signOut = useCallback(async () => {
    if (!account) return;
    setBusy(true);
    try {
      await account.signOut();
      setState(await account.state());
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("settings.account.failed"));
    } finally {
      setBusy(false);
    }
  }, [account, t]);

  const title = (
    <Label className="flex items-center gap-2">
      <UserRound className="h-4 w-4" />
      {t("settings.account.title")}
    </Label>
  );

  if (!account) {
    return (
      <Card className="w-full space-y-2 p-6">
        {title}
        <p className="text-sm text-muted-foreground">{t("settings.account.unavailable")}</p>
      </Card>
    );
  }

  const signedIn = state?.mode === "account";

  return (
    <Card className="w-full space-y-4 p-6">
      <div className="space-y-1">
        {title}
        <p className="text-sm text-muted-foreground">
          {signedIn ? t("settings.account.signedIn", { origin: state.origin ?? "" }) : t("settings.account.localHelp")}
        </p>
      </div>

      {confirm ? (
        <Notice
          variant="warning"
          title={confirm.kind === "runs" ? t("settings.account.runsTitle", { count: confirm.count }) : t("settings.account.confirmTitle")}
        >
          <p>{confirm.kind === "runs" ? t("settings.account.runsBody") : t("settings.account.confirmBody")}</p>
          <div className="mt-3 flex flex-wrap gap-2">
            <Button size="sm" onClick={() => void connect()} disabled={busy}>
              {t("settings.account.confirm")}
            </Button>
            <Button size="sm" variant="outline" onClick={() => setConfirm(null)} disabled={busy}>
              {t("settings.account.cancel")}
            </Button>
          </div>
        </Notice>
      ) : signedIn ? (
        <Button variant="outline" onClick={() => void signOut()} disabled={busy || state === null}>
          {busy ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <LogOut className="mr-2 h-4 w-4" />}
          {busy ? t("settings.account.signingOut") : t("settings.account.signOut")}
        </Button>
      ) : (
        <Button onClick={() => void askThenConnect()} disabled={busy || state === null}>
          {busy ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <LogIn className="mr-2 h-4 w-4" />}
          {busy ? t("settings.account.connecting") : t("settings.account.connect")}
        </Button>
      )}
    </Card>
  );
}
