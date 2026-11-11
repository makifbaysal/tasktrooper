import { HealthSquare } from "@/components/operations/console/HealthSquare";
import { telemetryHealthStatus } from "@/lib/telemetryHub";

/** The product's ok / warning / critical on the console's own health mark. */
export function HealthMark({ health, showLabel = false }: { health: string | undefined; showLabel?: boolean }) {
  return <HealthSquare status={telemetryHealthStatus(health)} showLabel={showLabel} />;
}
