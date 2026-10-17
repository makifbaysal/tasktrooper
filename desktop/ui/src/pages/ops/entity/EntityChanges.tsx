import { ChangesView } from "@/components/operations/changes/ChangesView";
import { useOpsPageScope } from "@/components/operations/shell/OpsContext";
import type { EntitySubPageProps } from "@/pages/ops/entity/types";

/** One entity's changes: the console's change feed, scoped to it. */
export default function EntityChanges({ entity }: EntitySubPageProps) {
  useOpsPageScope({ time: true, compare: false });
  return <ChangesView entity={entity} showEntity={false} />;
}
