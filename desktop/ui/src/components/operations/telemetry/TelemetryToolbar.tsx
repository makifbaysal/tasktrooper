import type { ReactNode } from "react";
import { TelemetryEnvSelect } from "@/components/operations/telemetry/TelemetryEnvSelect";

/** The row above a telemetry list: its own filters, then the environment. */
export function TelemetryToolbar({ children, env = true }: { children?: ReactNode; env?: boolean }) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      {children}
      {env && (
        <div className="ml-auto">
          <TelemetryEnvSelect />
        </div>
      )}
    </div>
  );
}
