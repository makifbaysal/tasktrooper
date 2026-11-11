import { Smartphone } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { api, type RemoteDeviceRole, type RemotePairing } from "@/api";
import { QrCode, canDrawQr } from "@/components/remote/QrCode";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useI18n } from "@/hooks/useI18n";
import { EXPIRY_KEYS, PAIRING_EXPIRY_DAYS, pairableUrl, spanLabel } from "@/lib/remoteAccess";

interface PairPhoneCardProps {
  state: { tunnel: string; url?: string } | null;
  /** Called when a pairing was created or a phone showed up, so the lists refresh. */
  onChanged: () => void;
}

const POLL_MS = 2_000;

export function PairPhoneCard({ state, onChanged }: PairPhoneCardProps) {
  const { t } = useI18n();
  const url = pairableUrl(state);
  const [label, setLabel] = useState("");
  const [role, setRole] = useState<RemoteDeviceRole>("operator");
  const [days, setDays] = useState<number>(30);
  const [busy, setBusy] = useState(false);
  const [pairing, setPairing] = useState<RemotePairing | null>(null);
  const [pairedName, setPairedName] = useState<string | null>(null);
  const [now, setNow] = useState(() => Date.now());
  const knownDevices = useRef<Set<string>>(new Set());

  const expiresAt = pairing ? Date.parse(pairing.expires_at) : 0;
  const expired = pairing !== null && expiresAt <= now;

  useEffect(() => {
    if (!pairing || pairedName) return;
    const tick = window.setInterval(() => setNow(Date.now()), 1_000);
    return () => window.clearInterval(tick);
  }, [pairing, pairedName]);

  // The phone shows up as a device that was not there when the code was made.
  useEffect(() => {
    if (!pairing || pairedName || expired) return;
    let alive = true;
    const poll = window.setInterval(() => {
      void api.listRemoteDevices().then(
        ({ devices }) => {
          if (!alive) return;
          const fresh = devices.find((d) => !d.revoked_at && !knownDevices.current.has(d.id));
          if (fresh) {
            setPairedName(fresh.name);
            onChanged();
          }
        },
        () => undefined,
      );
    }, POLL_MS);
    return () => {
      alive = false;
      window.clearInterval(poll);
    };
  }, [pairing, pairedName, expired, onChanged]);

  // A stopped tunnel invalidates the address a code was made for.
  useEffect(() => {
    if (!url) setPairing(null);
  }, [url]);

  const create = async () => {
    if (!url) return;
    setBusy(true);
    try {
      const existing = await api.listRemoteDevices().catch(() => ({ devices: [] }));
      knownDevices.current = new Set(existing.devices.map((d) => d.id));
      const made = await api.createRemotePairing({
        role,
        expires_in_days: days,
        public_url: url,
        ...(label.trim() ? { device_label: label.trim() } : {}),
      });
      setPairedName(null);
      setNow(Date.now());
      setPairing(made);
      onChanged();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("remoteAccess.pair.createFailed"));
    } finally {
      setBusy(false);
    }
  };

  const cancel = async () => {
    const current = pairing;
    setPairing(null);
    setPairedName(null);
    if (current) await api.cancelRemotePairing(current.id).catch(() => undefined);
    onChanged();
  };

  return (
    <Card className="w-full space-y-4 p-6">
      <div>
        <h2 className="flex items-center gap-2 text-heading font-semibold">
          <Smartphone className="h-4 w-4" />
          {t("remoteAccess.pair.title")}
        </h2>
        <p className="text-caption text-muted-foreground">{t("remoteAccess.pair.description")}</p>
      </div>

      {!url && <Notice variant="info" title={t("remoteAccess.pair.needsTunnel")} role={null} />}

      {url && !pairing && (
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-1 sm:col-span-2">
            <Label htmlFor="pair-label">{t("remoteAccess.pair.label")}</Label>
            <Input
              id="pair-label"
              value={label}
              onChange={(e) => setLabel(e.target.value)}
              placeholder={t("remoteAccess.pair.labelPlaceholder")}
              maxLength={80}
            />
          </div>
          <div className="space-y-1">
            <Label htmlFor="pair-role">{t("remoteAccess.pair.role")}</Label>
            <Select value={role} onValueChange={(v) => setRole(v as RemoteDeviceRole)}>
              <SelectTrigger id="pair-role">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="operator">{t("remoteAccess.pair.roles.operator")}</SelectItem>
                <SelectItem value="viewer">{t("remoteAccess.pair.roles.viewer")}</SelectItem>
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">{t(`remoteAccess.pair.roleHelp.${role}`)}</p>
          </div>
          <div className="space-y-1">
            <Label htmlFor="pair-expiry">{t("remoteAccess.pair.expiry")}</Label>
            <Select value={String(days)} onValueChange={(v) => setDays(Number(v))}>
              <SelectTrigger id="pair-expiry">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {PAIRING_EXPIRY_DAYS.map((d) => (
                  <SelectItem key={d} value={String(d)}>
                    {t(`remoteAccess.pair.expiries.${EXPIRY_KEYS[d]}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="sm:col-span-2">
            <Button onClick={() => void create()} disabled={busy}>
              {busy ? t("remoteAccess.pair.creating") : t("remoteAccess.pair.create")}
            </Button>
          </div>
        </div>
      )}

      {url && pairing && (
        <div className="space-y-4" data-testid="pairing">
          {pairedName ? (
            <Notice variant="info" title={t("remoteAccess.pair.paired", { name: pairedName })} role="status" />
          ) : expired ? (
            <Notice variant="warning" title={t("remoteAccess.pair.expired")} />
          ) : (
            <div className="flex flex-col items-center gap-4 sm:flex-row sm:items-start">
              {canDrawQr(pairing.uri) ? (
                <div className="shrink-0 rounded-lg border bg-white p-2">
                  <QrCode value={pairing.uri} label={t("remoteAccess.pair.scan")} />
                </div>
              ) : (
                <Notice variant="info" title={t("remoteAccess.pair.qrTooLong")} role={null} />
              )}
              <div className="min-w-0 space-y-2">
                <p className="text-sm font-medium">{t("remoteAccess.pair.scan")}</p>
                <p className="text-sm text-muted-foreground">{t("remoteAccess.pair.orType")}</p>
                <p className="select-all font-mono text-2xl font-semibold tracking-widest" data-testid="pairing-code">
                  {pairing.code_display}
                </p>
                <p className="break-all text-xs text-muted-foreground">{t("remoteAccess.pair.addressLine", { url })}</p>
                <p className="text-sm" aria-live="polite">
                  {t("remoteAccess.pair.expiresIn", { time: spanLabel(expiresAt - now, t) })}
                </p>
                <p className="text-sm text-muted-foreground">{t("remoteAccess.pair.waiting")}</p>
              </div>
            </div>
          )}
          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={() => void cancel()}>
              {pairedName || expired ? t("remoteAccess.pair.another") : t("remoteAccess.pair.cancel")}
            </Button>
          </div>
        </div>
      )}
    </Card>
  );
}
