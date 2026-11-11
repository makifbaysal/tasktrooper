import { useCallback, useEffect, useRef, useState } from "react";
import { api, type TelemetryStatus } from "@/api";

export type TelemetryConnection = "checking" | "notConfigured" | "unreachable" | "ready" | "failed";

export interface TelemetryConnectionState {
  connection: TelemetryConnection;
  error?: string;
  recheck(): void;
}

export function connectionOf(status: TelemetryStatus): TelemetryConnection {
  if (!status.configured) return "notConfigured";
  return status.reachable ? "ready" : "unreachable";
}

/** Whether the app server has a telemetry address and key, and can reach the
 * product with them; asked once on mount and again on `recheck`. */
export function useTelemetryStatus(): TelemetryConnectionState {
  const [connection, setConnection] = useState<TelemetryConnection>("checking");
  const [error, setError] = useState<string | undefined>(undefined);
  const ticket = useRef(0);

  const check = useCallback(async () => {
    const mine = ++ticket.current;
    try {
      const status = await api.telemetryStatus();
      if (mine !== ticket.current) return;
      setConnection(connectionOf(status));
      setError(undefined);
    } catch (e) {
      if (mine !== ticket.current) return;
      setConnection("failed");
      setError(e instanceof Error ? e.message : String(e));
    }
  }, []);

  useEffect(() => {
    void check();
    return () => {
      ticket.current++;
    };
  }, [check]);

  const recheck = useCallback(() => {
    setConnection("checking");
    void check();
  }, [check]);

  return { connection, error, recheck };
}
