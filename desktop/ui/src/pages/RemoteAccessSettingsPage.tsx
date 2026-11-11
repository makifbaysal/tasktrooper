import { useCallback, useState } from "react";
import { AuditCard } from "@/components/remote/AuditCard";
import { DevicesCard } from "@/components/remote/DevicesCard";
import { PairPhoneCard } from "@/components/remote/PairPhoneCard";
import { TunnelCard } from "@/components/remote/TunnelCard";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { useRemoteAccess } from "@/hooks/useRemoteAccess";

/**
 * Settings → Remote access. The tunnel (Cloudflare or ngrok), pairing a phone,
 * the paired phones and their activity log. Desktop shell, local mode only:
 * the tunnel is a program this computer runs, so a browser has nothing to show.
 */
export function RemoteAccessSettingsPage() {
  const { t } = useI18n();
  const { host, state, setState } = useRemoteAccess();
  const [refreshKey, setRefreshKey] = useState(0);
  const changed = useCallback(() => setRefreshKey((n) => n + 1), []);

  if (!host) {
    return (
      <div className="max-w-2xl">
        <Notice variant="info" title={t("remoteAccess.unavailable")} role={null} />
      </div>
    );
  }

  return (
    <div className="max-w-3xl space-y-6">
      <div>
        <h2 className="text-heading font-semibold">{t("remoteAccess.title")}</h2>
        <p className="text-caption text-muted-foreground">{t("remoteAccess.description")}</p>
      </div>
      <Notice variant="warning" title={t("remoteAccess.security.title")} role={null}>
        {t("remoteAccess.security.body")}
      </Notice>
      {state ? (
        <>
          <TunnelCard host={host} state={state} onState={setState} />
          <PairPhoneCard state={state} onChanged={changed} />
          <DevicesCard refreshKey={refreshKey} onChanged={changed} />
          <AuditCard refreshKey={refreshKey} />
        </>
      ) : (
        <Skeleton className="h-64 w-full" />
      )}
    </div>
  );
}
