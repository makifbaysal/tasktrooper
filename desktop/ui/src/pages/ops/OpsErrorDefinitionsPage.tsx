import { ErrorDefinitionsView } from "@/components/operations/errorDefinitions/ErrorDefinitionsView";
import { ErrorsAreaTabs } from "@/components/operations/errorDefinitions/ErrorsAreaTabs";
import { useOpsPageScope } from "@/components/operations/shell/OpsContext";
import { useI18n } from "@/hooks/useI18n";

/** `/operations/errors/definitions`: what each kind of runtime error means
 * and what it sets off. Definitions have no environment or window; the
 * environment picked is the one the AI reads recent errors from. */
export default function OpsErrorDefinitionsPage() {
  const { t } = useI18n();
  useOpsPageScope({ time: false, compare: false, env: true, note: t("operations.errorDefinitions.scopeNote") });
  return (
    <div className="space-y-4">
      <ErrorsAreaTabs active="definitions" />
      <ErrorDefinitionsView />
    </div>
  );
}
