import { ErrorsAreaTabs } from "@/components/operations/errorDefinitions/ErrorsAreaTabs";
import { ErrorsInbox } from "@/components/operations/errors/ErrorsInbox";
import { useOpsPageScope } from "@/components/operations/shell/OpsContext";

/** `/operations/errors`: every connected component's error groups in the
 * environment picked, since the start of the range. */
export default function OpsErrorsInboxPage() {
  useOpsPageScope({ time: true, compare: false, env: true });
  return (
    <div className="space-y-4">
      <ErrorsAreaTabs active="inbox" />
      <ErrorsInbox />
    </div>
  );
}
