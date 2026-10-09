import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Notice } from "@/components/ui/notice";
import { useI18n } from "@/hooks/useI18n";
import { writeFirstRunMode, type FirstRunMode } from "@/lib/firstRun";

/**
 * The first screen: local, or an account.
 *
 * The account option is offered only when the shell exposes
 * `account.signIn` — a browser tab and a shell older than account mode have
 * no such bridge, so the choice degrades to "local" instead of a button that
 * could never sign anyone in.
 */
export function FirstRunChoice({ onChosen }: { onChosen: (mode: FirstRunMode) => void }) {
  const { t } = useI18n();
  const [accountError, setAccountError] = useState("");
  const [signingIn, setSigningIn] = useState(false);
  const account = window.__tasktrooperDesktop?.account;

  const choose = (mode: FirstRunMode) => {
    writeFirstRunMode(mode);
    onChosen(mode);
  };

  const chooseAccount = async () => {
    if (!account?.signIn) return;
    setSigningIn(true);
    setAccountError("");
    try {
      await account.signIn();
      choose("account");
    } catch (e) {
      setAccountError(e instanceof Error ? e.message : t("setup.firstRun.account.failed"));
      setSigningIn(false);
    }
  };

  return (
    <div className="space-y-3">
      <div className="grid gap-3 sm:grid-cols-2">
        <Card className="flex flex-col gap-3 p-4">
          <div className="space-y-1">
            <p className="text-body font-medium">{t("setup.firstRun.local.title")}</p>
            <p className="text-sm text-muted-foreground">{t("setup.firstRun.local.body")}</p>
          </div>
          <Button className="mt-auto w-full" onClick={() => choose("local")}>
            {t("setup.firstRun.local.action")}
          </Button>
        </Card>

        {account?.signIn && (
          <Card className="flex flex-col gap-3 p-4">
            <div className="space-y-1">
              <p className="text-body font-medium">{t("setup.firstRun.account.title")}</p>
              <p className="text-sm text-muted-foreground">{t("setup.firstRun.account.body")}</p>
            </div>
            <Button
              variant="outline"
              className="mt-auto w-full"
              disabled={signingIn}
              onClick={() => void chooseAccount()}
            >
              {signingIn ? t("setup.firstRun.account.signingIn") : t("setup.firstRun.account.action")}
            </Button>
          </Card>
        )}
      </div>

      {accountError && (
        <Notice variant="error" title={t("setup.firstRun.account.failed")}>
          {accountError}
        </Notice>
      )}
    </div>
  );
}
