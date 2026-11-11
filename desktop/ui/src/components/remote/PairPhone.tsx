import { Check, Copy, Smartphone, Unplug } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { api, type RemoteDevice, type RemoteDeviceRole, type RemotePairing } from "@/api";
import { QrCode, canDrawQr } from "@/components/remote/QrCode";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Spinner } from "@/components/ui/spinner";
import { useI18n } from "@/hooks/useI18n";
import { copyText } from "@/lib/clipboard";
import { connectedDevice, EXPIRY_KEYS, PAIRING_EXPIRY_DAYS, spanLabel } from "@/lib/remoteAccess";

const DEVICE_REFRESH_MS = 8_000;
const PAIRED_POLL_MS = 2_000;

interface PairPhoneProps {
  /** The tunnel's https address, or null while the tunnel is not connected. */
  url: string | null;
  /** The computer's name as the phone will show it (the setting; the server fills in its hostname when empty). */
  serverName: string;
  /** A phone was paired or disconnected: refresh whatever shows the activity. */
  onChanged: () => void;
  /**
   * inline: the Pair phone button and, beside it, the connected phone (the full form).
   * summary: one phone line for the compact running card: "Not paired" with a
   * Pair phone button, or "Paired: <name>" with Stop pairing.
   */
  variant?: "inline" | "summary";
}

/**
 * "Pair phone" next to Start tunnel, and the one-line state of the phone that
 * is connected. One phone at a time, so there is no list: pairing a new phone
 * replaces the old one (the server enforces it), and "Disconnect" ends it.
 */
export function PairPhone({ url, serverName, onChanged, variant = "inline" }: PairPhoneProps) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [device, setDevice] = useState<RemoteDevice | null>(null);
  const [confirming, setConfirming] = useState(false);

  const load = useCallback(async () => {
    try {
      setDevice(connectedDevice((await api.listRemoteDevices()).devices, Date.now()));
    } catch {
      // The status line is a convenience; the next refresh tries again.
    }
  }, []);

  useEffect(() => {
    void load();
    const timer = window.setInterval(() => void load(), DEVICE_REFRESH_MS);
    return () => window.clearInterval(timer);
  }, [load]);

  // A stopped tunnel invalidates the address a code was made for.
  useEffect(() => {
    if (!url) setOpen(false);
  }, [url]);

  const disconnect = async (d: RemoteDevice): Promise<boolean> => {
    try {
      await api.revokeRemoteDevice(d.id);
      toast.success(t("remoteAccess.pair.disconnected"));
      setDevice(null);
      onChanged();
      return true;
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("remoteAccess.pair.disconnectFailed"));
      return false;
    }
  };

  const summary = variant === "summary";

  return (
    <div className={summary ? "flex flex-wrap items-center justify-between gap-3 rounded-lg border p-3" : "flex flex-wrap items-center gap-x-3 gap-y-1"} data-testid={summary ? "phone-summary" : undefined}>
      {summary ? (
        <>
          <span className="flex items-center gap-2 text-sm font-medium" role="status">
            <Smartphone className="h-4 w-4 text-muted-foreground" />
            {device ? t("remoteAccess.pair.pairedSummary", { name: device.name }) : t("remoteAccess.pair.notPaired")}
          </span>
          {device ? (
            <Button variant="outline" size="sm" onClick={() => setConfirming(true)}>
              <Unplug className="mr-2 h-4 w-4" />
              {t("remoteAccess.pair.stopPairing")}
            </Button>
          ) : (
            <Button onClick={() => setOpen(true)} disabled={!url} data-testid="pair-phone-button">
              <Smartphone className="mr-2 h-4 w-4" />
              {t("remoteAccess.pair.button")}
            </Button>
          )}
        </>
      ) : (
        <>
          <Button variant="outline" onClick={() => setOpen(true)} disabled={!url} data-testid="pair-phone-button">
            <Smartphone className="mr-2 h-4 w-4" />
            {t("remoteAccess.pair.button")}
          </Button>
          {!url && <span className="text-xs text-muted-foreground">{t("remoteAccess.pair.needsTunnel")}</span>}
          {device && (
            <span className="flex items-center gap-2 text-sm" data-testid="phone-connected">
              <Smartphone className="h-4 w-4 text-muted-foreground" />
              {t("remoteAccess.pair.connected", { name: device.name })}
              <Button variant="outline" size="sm" onClick={() => setConfirming(true)}>
                <Unplug className="mr-2 h-4 w-4" />
                {t("remoteAccess.pair.disconnect")}
              </Button>
            </span>
          )}
        </>
      )}

      {url && (
        <PairPhoneDialog
          open={open}
          onOpenChange={setOpen}
          url={url}
          serverName={serverName}
          onPaired={() => {
            void load();
            onChanged();
          }}
        />
      )}

      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title={t("remoteAccess.pair.disconnectTitle", { name: device?.name ?? "" })}
        description={t("remoteAccess.pair.disconnectBody")}
        confirmLabel={summary ? t("remoteAccess.pair.stopPairing") : t("remoteAccess.pair.disconnect")}
        onConfirm={async () => (device ? disconnect(device) : false)}
      />
    </div>
  );
}

interface PairPhoneDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  url: string;
  serverName: string;
  onPaired: () => void;
}

/**
 * Makes a pairing code when it opens and shows it twice: as the QR code the
 * phone scans and as text to type, with copy buttons. Closing with an unused
 * code cancels it.
 */
function PairPhoneDialog({ open, onOpenChange, url, serverName, onPaired }: PairPhoneDialogProps) {
  const { t } = useI18n();
  const [role, setRole] = useState<RemoteDeviceRole>("operator");
  const [days, setDays] = useState<number>(30);
  const [pairing, setPairing] = useState<RemotePairing | null>(null);
  const [creating, setCreating] = useState(false);
  const [failed, setFailed] = useState<string | null>(null);
  const [pairedName, setPairedName] = useState<string | null>(null);
  const [computer, setComputer] = useState(serverName);
  const [now, setNow] = useState(() => Date.now());
  const known = useRef<Set<string>>(new Set());
  const current = useRef<RemotePairing | null>(null);
  const used = useRef(false);
  const request = useRef(0);
  const paired = useRef(onPaired);
  paired.current = onPaired;

  const expiresAt = pairing ? Date.parse(pairing.expires_at) : 0;
  const expired = pairing !== null && expiresAt <= now;

  const cancelCurrent = useCallback(() => {
    const pending = current.current;
    current.current = null;
    if (pending && !used.current) void api.cancelRemotePairing(pending.id).catch(() => undefined);
  }, []);

  const create = useCallback(async () => {
    const mine = ++request.current;
    cancelCurrent();
    setCreating(true);
    setFailed(null);
    setPairing(null);
    setPairedName(null);
    used.current = false;
    try {
      const existing = await api.listRemoteDevices().catch(() => ({ devices: [] }));
      known.current = new Set(existing.devices.map((d) => d.id));
      const made = await api.createRemotePairing({ role, expires_in_days: days, public_url: url });
      if (mine !== request.current) {
        void api.cancelRemotePairing(made.id).catch(() => undefined);
        return;
      }
      current.current = made;
      setNow(Date.now());
      setPairing(made);
    } catch (e) {
      if (mine === request.current) setFailed(e instanceof Error ? e.message : t("remoteAccess.pair.createFailed"));
    } finally {
      if (mine === request.current) setCreating(false);
    }
  }, [cancelCurrent, days, role, t, url]);

  // A code when the dialog opens, and a new one when the access choice changes.
  useEffect(() => {
    if (!open) return;
    void create();
    return () => {
      request.current += 1;
      cancelCurrent();
    };
  }, [open, create, cancelCurrent]);

  useEffect(() => {
    if (!open) return;
    api
      .remoteAccessStatus()
      .then((status) => setComputer(status.server_name || serverName))
      .catch(() => undefined);
  }, [open, serverName]);

  useEffect(() => {
    if (!open || !pairing || pairedName) return;
    const tick = window.setInterval(() => setNow(Date.now()), 1_000);
    return () => window.clearInterval(tick);
  }, [open, pairing, pairedName]);

  // The phone shows up as a device that was not there when the code was made.
  useEffect(() => {
    if (!open || !pairing || pairedName || expired) return;
    let alive = true;
    const poll = window.setInterval(() => {
      void api.listRemoteDevices().then(
        ({ devices }) => {
          if (!alive) return;
          const fresh = devices.find((d) => !d.revoked_at && !known.current.has(d.id));
          if (fresh) {
            used.current = true;
            setPairedName(fresh.name);
            paired.current();
          }
        },
        () => undefined,
      );
    }, PAIRED_POLL_MS);
    return () => {
      alive = false;
      window.clearInterval(poll);
    };
  }, [open, pairing, pairedName, expired]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-xl" data-testid="pair-dialog">
        <DialogHeader>
          <DialogTitle>{t("remoteAccess.pair.title")}</DialogTitle>
          <DialogDescription>{t("remoteAccess.pair.description")}</DialogDescription>
        </DialogHeader>

        {failed && (
          <Notice variant="error" title={t("remoteAccess.pair.createFailed")}>
            {failed}
          </Notice>
        )}

        {creating && !pairing && (
          <p className="flex items-center gap-2 text-sm text-muted-foreground" role="status">
            <Spinner size="sm" />
            {t("remoteAccess.pair.creating")}
          </p>
        )}

        {pairing && pairedName && <Notice variant="info" title={t("remoteAccess.pair.paired", { name: pairedName })} role="status" />}

        {pairing && !pairedName && (
          <div className="flex flex-col items-center gap-4 sm:flex-row sm:items-start" data-testid="pairing">
            {expired ? null : canDrawQr(pairing.uri) ? (
              <div className="shrink-0 rounded-lg border bg-white p-2">
                <QrCode value={pairing.uri} label={t("remoteAccess.pair.scan")} />
              </div>
            ) : (
              <Notice variant="info" title={t("remoteAccess.pair.qrTooLong")} role={null} />
            )}
            <div className="min-w-0 flex-1 space-y-3">
              <Field label={t("remoteAccess.pair.computer")} value={computer || "-"} />
              <Field label={t("remoteAccess.pair.address")} value={url} copyable mono testId="pairing-address" />
              {!expired && <Field label={t("remoteAccess.pair.code")} value={pairing.code_display} copyable mono big testId="pairing-code" />}
              {expired ? (
                <Notice variant="warning" title={t("remoteAccess.pair.expired")} />
              ) : (
                <p className="text-sm" aria-live="polite" data-testid="pairing-expiry">
                  {t("remoteAccess.pair.expiresIn", { time: spanLabel(expiresAt - now, t) })}
                </p>
              )}
              {!expired && (
                <p className="flex items-center gap-2 text-sm text-muted-foreground">
                  <Spinner size="sm" />
                  {t("remoteAccess.pair.waiting")}
                </p>
              )}
              {expired && (
                <Button onClick={() => void create()} disabled={creating}>
                  {t("remoteAccess.pair.newCode")}
                </Button>
              )}
            </div>
          </div>
        )}

        {!pairedName && (
          <div className="grid gap-3 sm:grid-cols-2">
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
          </div>
        )}

        {!pairedName && <p className="text-xs text-muted-foreground">{t("remoteAccess.pair.onePhone")}</p>}

        <DialogFooter>
          <Button variant={pairedName ? "default" : "outline"} onClick={() => onOpenChange(false)}>
            {t("remoteAccess.pair.done")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function Field({ label, value, copyable, mono, big, testId }: { label: string; value: string; copyable?: boolean; mono?: boolean; big?: boolean; testId?: string }) {
  const { t } = useI18n();
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    if (await copyText(value)) {
      setCopied(true);
      toast.success(t("remoteAccess.pair.copied"));
      window.setTimeout(() => setCopied(false), 1500);
    } else toast.error(t("remoteAccess.pair.copyFailed"));
  };
  return (
    <div className="space-y-1">
      <p className="text-xs text-muted-foreground">{label}</p>
      <div className="flex items-center gap-2">
        <code
          className={`min-w-0 flex-1 select-all break-all rounded bg-muted px-2 py-1 ${mono ? "font-mono" : ""} ${big ? "text-lg font-semibold tracking-widest" : "text-sm"}`}
          data-testid={testId}
        >
          {value}
        </code>
        {copyable && (
          <Button variant="outline" size="sm" onClick={() => void copy()} aria-label={`${t("remoteAccess.pair.copy")}: ${label}`}>
            {copied ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
          </Button>
        )}
      </div>
    </div>
  );
}
