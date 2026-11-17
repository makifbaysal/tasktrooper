import { HealthSquare } from "@/components/operations/console/HealthSquare";
import { telemetryHealthStatus } from "@/lib/telemetryHub";
import { infraOpsStatus } from "@/lib/telemetryViews";

/** The product's ok / warning / critical on the console's own health mark. */
export function HealthMark({ health, showLabel = false }: { health: string | undefined; showLabel?: boolean }) {
  return <HealthSquare status={telemetryHealthStatus(health)} showLabel={showLabel} />;
}

/** An infrastructure status: anything but ok / warning / critical (stale, idle, missing) reads "not reporting". */
export function InfraHealthMark({ status, showLabel = false }: { status: string | undefined; showLabel?: boolean }) {
  return <HealthSquare status={infraOpsStatus(status)} showLabel={showLabel} />;
}
