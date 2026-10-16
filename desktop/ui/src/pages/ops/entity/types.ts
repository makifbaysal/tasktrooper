import type { DeployEnvironment, EnvironmentSummary } from "@/api";
import type { OpsEntity } from "@/components/operations/shell/useOpsEntities";

/** What every entity sub-page receives: the entity, the environment picked in
 * the top bar, and that environment's binding when the entity has one there. */
export interface EntitySubPageProps {
  entity: OpsEntity;
  env: DeployEnvironment;
  environment?: EnvironmentSummary;
}
