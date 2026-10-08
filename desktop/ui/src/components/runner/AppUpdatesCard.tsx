import { ArrowUpCircle, Loader2, RefreshCw } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Progress } from "@/components/ui/progress";
import { useI18n } from "@/hooks/useI18n";
import { desktopUpdates, type DesktopUpdateStatus } from "@/lib/desktop-bridge";
import { formatRelativeTime } from "@/lib/utils";

/**
 * Settings → General's view of the shell's auto-updater.
 *
 * The app already checks every six hours and downloads in the background; this
 * card is where a person asks "am I current?" and installs what is staged
 * without waiting for the next quit.
 */
export function AppUpdatesCard() {
  const { t, lang } = useI18n();
  const updates = desktopUpdates();
  const [status, setStatus] = useState<DesktopUpdateStatus | null>(null);
  const [version, setVersion] = useState("");
  const [checking, setChecking] = useState(false);
  const [restarting, setRestarting] = useState(false);

  useEffect(() => {
    if (!updates) return;
    void updates.status().then(setStatus, () => undefined);
    void window.__tasktrooperDesktop?.info?.().then((info) => setVersion(info.version), () => undefined);
    return updates.subscribe(setStatus);
  }, [updates]);

  const check = useCallback(async () => {
    if (!updates) return;
    setChecking(true);
    try {
      setStatus(await updates.check());
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("settings.updates.checkFailed"));
    } finally {
      setChecking(false);
    }
  }, [updates, t]);

  // A successful restart never resolves into anything here — the app quits. A
  // status that leaves `ready` meanwhile means it did not, so let go of the button.
  useEffect(() => {
    if (status?.phase !== "ready") setRestarting(false);
  }, [status?.phase]);

  const restart = useCallback(async () => {
    if (!updates) return;
    setRestarting(true);
    try {
      await updates.restart();
    } catch (e) {
      setRestarting(false);
      toast.error(e instanceof Error ? e.message : t("settings.updates.restartFailed"));
    }
  }, [updates, t]);

  const title = (
    <Label className="flex items-center gap-2">
      <ArrowUpCircle className="h-4 w-4" />
      {t("settings.updates.title")}
    </Label>
  );

  if (!updates) {
    return (
      <Card className="w-full space-y-2 p-6">
        {title}
        <p className="text-sm text-muted-foreground">{t("settings.updates.unavailable")}</p>
      </Card>
    );
  }

  const phase = status?.phase;
  const busy = checking || phase === "checking";
  const canCheck = phase !== undefined && phase !== "unsupported" && phase !== "available" && phase !== "ready";

  return (
    <Card className="w-full space-y-4 p-6">
      <div className="space-y-1">
        {title}
        <p className="text-sm text-muted-foreground">
          {version ? t("settings.updates.currentVersion", { version }) : t("settings.updates.help")}
        </p>
      </div>

      {phase === "ready" ? (
        <div className="space-y-3">
          <p className="text-sm">
            {status?.version
              ? t("settings.updates.readyVersion", { version: status.version })
              : t("settings.updates.ready")}
          </p>
          <Button onClick={() => void restart()} disabled={restarting}>
            {restarting ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <ArrowUpCircle className="mr-2 h-4 w-4" />}
            {restarting ? t("settings.updates.restarting") : t("settings.updates.restart")}
          </Button>
          <p className="text-xs text-muted-foreground">{t("settings.updates.restartHelp")}</p>
        </div>
      ) : phase === "available" ? (
        <div className="space-y-2">
          <p className="text-sm">
            {status?.version
              ? t("settings.updates.downloadingVersion", { version: status.version, percent: status.percent ?? 0 })
              : t("settings.updates.downloading", { percent: status?.percent ?? 0 })}
          </p>
          <Progress value={status?.percent ?? 0} />
        </div>
      ) : phase === "unsupported" ? (
        <Notice variant="info" title={t("settings.updates.unsupported")}>
          {status?.detail}
        </Notice>
      ) : phase === "error" ? (
        <Notice variant="error" title={t("settings.updates.checkFailed")}>
          {status?.detail}
        </Notice>
      ) : phase === "current" ? (
        <p className="text-sm">{t("settings.updates.upToDate")}</p>
      ) : null}

      {canCheck ? (
        <div className="flex flex-wrap items-center gap-3">
          <Button variant="outline" onClick={() => void check()} disabled={busy}>
            {busy ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <RefreshCw className="mr-2 h-4 w-4" />}
            {busy ? t("settings.updates.checking") : t("settings.updates.check")}
          </Button>
          {status?.checkedAt ? (
            <span className="text-xs text-muted-foreground">
              {t("settings.updates.lastChecked", {
                when: formatRelativeTime(new Date(status.checkedAt).toISOString(), lang),
              })}
            </span>
          ) : null}
        </div>
      ) : null}
    </Card>
  );
}
