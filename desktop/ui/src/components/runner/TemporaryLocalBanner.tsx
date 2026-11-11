import { LogIn, LogOut } from "lucide-react";
import { useCallback, useState } from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/notice";
import { useI18n } from "@/hooks/useI18n";
import { useDesktopAccountState } from "@/hooks/useDesktopAccountState";
import { announceAccountChange, desktopAccount } from "@/lib/desktop-bridge";

/**
 * Says, on every page, that this computer is signed in to an account but runs
 * locally for now — the account's page could not be reached at launch — and
 * offers the way back. Nothing while the state is anything else, and nothing
 * outside the desktop shell.
 */
export function TemporaryLocalBanner() {
  const { t } = useI18n();
  const account = desktopAccount();
  const state = useDesktopAccountState();
  const [busy, setBusy] = useState(false);

  const back = useCallback(async () => {
    if (!account) return;
    setBusy(true);
    try {
      await account.signIn();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("settings.account.failed"));
    } finally {
      setBusy(false);
    }
  }, [account, t]);

  // Forgets the account: no sign-in attempt at the next launch either. The
  // banner goes the moment the shell answers that nothing is signed in.
  const signOut = useCallback(async () => {
    if (!account) return;
    setBusy(true);
    try {
      await account.signOut();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("settings.account.failed"));
    } finally {
      setBusy(false);
      announceAccountChange();
    }
  }, [account, t]);

  if (!account || state?.temporaryLocal !== true) return null;

  return (
    <div className="px-4 pt-3">
      <Notice variant="warning" title={t("settings.account.bannerTitle")}>
        <p>{t("settings.account.bannerBody", { origin: state.origin ?? "" })}</p>
        <Button size="sm" variant="outline" className="mt-2" onClick={() => void back()} disabled={busy}>
          <LogIn className="mr-2 h-4 w-4" />
          {t("settings.account.backToAccount")}
        </Button>
        <Button size="sm" variant="ghost" className="ml-2 mt-2" onClick={() => void signOut()} disabled={busy}>
          <LogOut className="mr-2 h-4 w-4" />
          {t("settings.account.signOut")}
        </Button>
      </Notice>
    </div>
  );
}
