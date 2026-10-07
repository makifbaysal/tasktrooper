import { Loader2, MonitorSmartphone, Play } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { api, type MobileStorePlatform, type SimulatorDevice, type SimulatorRun } from "@/api";
import { LogTail, StorePlatformIcon } from "@/components/operations/StoreTestBuildParts";
import {
  isSimulatorRunActive,
  simulatorRunStatusBadgeVariant,
  simulatorRunStatusLabelKey,
} from "@/components/operations/storeTestBuilds";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useI18n } from "@/hooks/useI18n";
import { usePolling } from "@/hooks/usePolling";
import { keepEqual } from "@/lib/stableState";

const POLL_MS = 3_000;
const STARTED_STATES = /^(booted|device|online|running)$/i;

interface SimulatorRunPanelProps {
  repositoryId: string;
  taskId: string;
  /** Platforms of the repository's linked store apps; other devices are not offered. */
  platforms: MobileStorePlatform[];
}

function preferredDevice(devices: SimulatorDevice[], lastDeviceId?: string): string {
  return (
    devices.find((d) => d.id === lastDeviceId)?.id ??
    devices.find((d) => STARTED_STATES.test(d.state))?.id ??
    devices[0]?.id ??
    ""
  );
}

/**
 * "Run on simulator" beside "Run locally": builds the task's branch and opens
 * it on an iOS simulator or Android emulator of this machine. Renders nothing
 * when the machine has none (the server's 409) for the app's platforms.
 */
export function SimulatorRunPanel({ repositoryId, taskId, platforms }: SimulatorRunPanelProps) {
  const { t } = useI18n();
  const [devices, setDevices] = useState<SimulatorDevice[] | null>(null);
  const [run, setRun] = useState<SimulatorRun | null>(null);
  const [deviceId, setDeviceId] = useState("");
  const [busy, setBusy] = useState(false);
  const runSeq = useRef(0);
  const platformKey = platforms.join(",");

  useEffect(() => {
    let cancelled = false;
    const wanted = platformKey.split(",");
    api
      .listSimulatorDevices()
      .then((list) => {
        if (!cancelled) setDevices(list.filter((d) => wanted.includes(d.platform)));
      })
      .catch(() => {
        if (!cancelled) setDevices([]);
      });
    return () => {
      cancelled = true;
    };
  }, [platformKey]);

  const refresh = useCallback(async () => {
    const ticket = ++runSeq.current;
    try {
      const next = await api.getSimulatorRun(repositoryId, taskId);
      if (ticket === runSeq.current) setRun((prev) => keepEqual(prev, next));
    } catch {
      /* keep the last known run: a failed poll is not "never ran" */
    }
  }, [repositoryId, taskId]);

  useEffect(() => {
    runSeq.current += 1;
    setRun(null);
    void refresh();
  }, [refresh]);

  const active = run !== null && isSimulatorRunActive(run.status);
  usePolling(refresh, POLL_MS, active, { leading: false });

  const lastDeviceId = run?.device_id;
  useEffect(() => {
    if (!devices || devices.length === 0) return;
    setDeviceId((prev) => (devices.some((d) => d.id === prev) ? prev : preferredDevice(devices, lastDeviceId)));
  }, [devices, lastDeviceId]);

  if (!devices || devices.length === 0) return null;

  const start = async () => {
    const device = devices.find((d) => d.id === deviceId);
    if (!device) return;
    setBusy(true);
    runSeq.current += 1;
    try {
      setRun(await api.startSimulatorRun(repositoryId, taskId, { platform: device.platform, device_id: device.id }));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("operations.storeTest.simulator.startFailed"));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="space-y-2 rounded-lg border border-border bg-background/60 p-3">
      <div className="flex items-center gap-2">
        <MonitorSmartphone className="h-4 w-4 text-muted-foreground" aria-hidden />
        <span className="text-sm font-medium">{t("operations.storeTest.simulator.title")}</span>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <Select value={deviceId} onValueChange={setDeviceId} disabled={busy || active}>
          <SelectTrigger className="h-8 w-64 max-w-full" aria-label={t("operations.storeTest.simulator.device")}>
            <SelectValue placeholder={t("operations.storeTest.simulator.devicePlaceholder")} />
          </SelectTrigger>
          <SelectContent>
            {devices.map((device) => (
              <SelectItem key={device.id} value={device.id}>
                <span className="flex items-center gap-2">
                  <StorePlatformIcon platform={device.platform} className="h-3.5 w-3.5" />
                  {device.runtime ? `${device.name} · ${device.runtime}` : device.name}
                </span>
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button size="sm" variant="outline" onClick={() => void start()} disabled={busy || active || !deviceId}>
          {busy ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Play className="h-3.5 w-3.5" />}
          {t("operations.storeTest.simulator.run")}
        </Button>
        {run && (
          <>
            <Badge variant={simulatorRunStatusBadgeVariant(run.status)} className="gap-1">
              {active && <Loader2 className="h-3 w-3 animate-spin" aria-hidden />}
              {t(simulatorRunStatusLabelKey(run.status))}
            </Badge>
            <span className="text-xs text-muted-foreground">{run.device_name || run.device_id}</span>
          </>
        )}
      </div>
      {run?.status === "failed" && run.failure && (
        <p className="whitespace-pre-line text-xs text-destructive">{run.failure}</p>
      )}
      {run && <LogTail key={`${run.id}-${run.status === "failed"}`} text={run.log_tail} defaultOpen={run.status === "failed"} />}
    </div>
  );
}
