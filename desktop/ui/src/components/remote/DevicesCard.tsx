import { RefreshCw, Smartphone } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { api, type RemoteDevice } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Input } from "@/components/ui/input";
import { useI18n } from "@/hooks/useI18n";
import { relativeTime } from "@/lib/remoteAccess";

const REFRESH_MS = 10_000;

export function DevicesCard({ refreshKey, onChanged }: { refreshKey: number; onChanged: () => void }) {
  const { t } = useI18n();
  const [devices, setDevices] = useState<RemoteDevice[] | null>(null);
  const [failed, setFailed] = useState(false);
  const [renaming, setRenaming] = useState<{ id: string; name: string } | null>(null);
  const [revoking, setRevoking] = useState<RemoteDevice | null>(null);
  const now = Date.now();

  const load = useCallback(async () => {
    try {
      setDevices((await api.listRemoteDevices()).devices);
      setFailed(false);
    } catch {
      setFailed(true);
    }
  }, []);

  useEffect(() => {
    void load();
    const timer = window.setInterval(() => void load(), REFRESH_MS);
    return () => window.clearInterval(timer);
  }, [load, refreshKey]);

  const rename = async () => {
    if (!renaming || renaming.name.trim() === "") return;
    try {
      await api.renameRemoteDevice(renaming.id, { name: renaming.name.trim() });
      toast.success(t("remoteAccess.devices.renamed"));
      setRenaming(null);
      void load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("remoteAccess.devices.actionFailed"));
    }
  };

  const revoke = async (device: RemoteDevice): Promise<boolean> => {
    try {
      await api.revokeRemoteDevice(device.id);
      toast.success(t("remoteAccess.devices.revoked_toast"));
      void load();
      onChanged();
      return true;
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("remoteAccess.devices.actionFailed"));
      return false;
    }
  };

  return (
    <Card className="w-full space-y-4 p-6">
      <div className="flex items-start justify-between gap-3">
        <div>
          <h2 className="flex items-center gap-2 text-heading font-semibold">
            <Smartphone className="h-4 w-4" />
            {t("remoteAccess.devices.title")}
          </h2>
          <p className="text-caption text-muted-foreground">{t("remoteAccess.devices.description")}</p>
        </div>
        <Button variant="outline" size="sm" onClick={() => void load()}>
          <RefreshCw className="mr-2 h-4 w-4" />
          {t("remoteAccess.devices.refresh")}
        </Button>
      </div>

      {failed && <p className="text-sm text-destructive">{t("remoteAccess.devices.loadFailed")}</p>}
      {devices && devices.length === 0 && !failed && <p className="text-sm text-muted-foreground">{t("remoteAccess.devices.empty")}</p>}

      <ul className="space-y-2">
        {(devices ?? []).map((device) => (
          <li key={device.id} className="flex flex-wrap items-center justify-between gap-3 rounded-lg border p-3" data-testid="device-row">
            <div className="min-w-0 space-y-1">
              {renaming?.id === device.id ? (
                <div className="flex items-center gap-2">
                  <Input
                    aria-label={t("remoteAccess.devices.rename")}
                    value={renaming.name}
                    maxLength={80}
                    onChange={(e) => setRenaming({ id: device.id, name: e.target.value })}
                    onKeyDown={(e) => e.key === "Enter" && void rename()}
                  />
                  <Button size="sm" onClick={() => void rename()}>
                    {t("remoteAccess.devices.renameSave")}
                  </Button>
                </div>
              ) : (
                <p className={device.revoked_at ? "truncate font-medium text-muted-foreground line-through" : "truncate font-medium"}>
                  {device.name}
                </p>
              )}
              <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                <Badge variant={device.role === "operator" ? "info" : "secondary"}>{t(`remoteAccess.pair.roles.${device.role}`)}</Badge>
                {device.platform && <span>{device.platform}</span>}
                <span>
                  {device.last_seen_at
                    ? t("remoteAccess.devices.lastSeen", { time: relativeTime(device.last_seen_at, now, t) })
                    : t("remoteAccess.devices.neverSeen")}
                </span>
                <span>
                  {device.revoked_at
                    ? t("remoteAccess.devices.revoked", { time: relativeTime(device.revoked_at, now, t) })
                    : device.expires_at
                      ? t("remoteAccess.devices.expires", { time: relativeTime(device.expires_at, now, t) })
                      : t("remoteAccess.devices.noExpiry")}
                </span>
              </div>
            </div>
            {!device.revoked_at && (
              <div className="flex gap-2">
                <Button variant="outline" size="sm" onClick={() => setRenaming({ id: device.id, name: device.name })}>
                  {t("remoteAccess.devices.rename")}
                </Button>
                <Button variant="outline" size="sm" onClick={() => setRevoking(device)}>
                  {t("remoteAccess.devices.revoke")}
                </Button>
              </div>
            )}
          </li>
        ))}
      </ul>

      <ConfirmDialog
        open={revoking !== null}
        onOpenChange={(open) => !open && setRevoking(null)}
        title={t("remoteAccess.devices.revokeTitle", { name: revoking?.name ?? "" })}
        description={t("remoteAccess.devices.revokeBody")}
        confirmLabel={t("remoteAccess.devices.revoke")}
        variant="destructive"
        onConfirm={async () => (revoking ? revoke(revoking) : false)}
      />
    </Card>
  );
}
