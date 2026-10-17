import type { ComponentType } from "react";
import { Navigate, useLocation, useOutletContext } from "react-router-dom";
import { entityPath, hasTab, type EntityTab } from "@/components/operations/console/roleViews";
import type { EntitySubPageProps } from "@/pages/ops/entity/types";

interface EntitySubRouteProps {
  tab: EntityTab;
  page: ComponentType<EntitySubPageProps>;
}

/** One entity sub-page route: hands the page its props from the entity page,
 * and sends a section this entity's role does not have to its Summary with
 * the query kept (a notification's `?rule=` survives, like the index's). */
export function EntitySubRoute({ tab, page: Page }: EntitySubRouteProps) {
  const { entity, env, environment } = useOutletContext<EntitySubPageProps>();
  const { search } = useLocation();
  if (!hasTab(entity.role, tab)) return <Navigate to={{ pathname: entityPath(entity.id), search }} replace />;
  return <Page entity={entity} env={env} environment={environment} />;
}
