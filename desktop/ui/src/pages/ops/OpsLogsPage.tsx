import { LogsExplorer } from "@/components/operations/logs/LogsExplorer";
import { useOpsPageScope } from "@/components/operations/shell/OpsContext";

/** `/operations/logs`: log entries across connected components in the
 * environment and window picked. */
export default function OpsLogsPage() {
  useOpsPageScope({ time: true, compare: false, env: true });
  return <LogsExplorer />;
}
