import type { ReactNode } from "react";
import { Navigate } from "react-router-dom";
import { ADVANCED_TEAM_CONFIG } from "@/lib/features";

interface AdvancedConfigRouteProps {
  children: ReactNode;
  redirectTo?: string;
}

export function AdvancedConfigRoute({ children, redirectTo = "/settings" }: AdvancedConfigRouteProps) {
  if (!ADVANCED_TEAM_CONFIG) return <Navigate to={redirectTo} replace />;
  return <>{children}</>;
}
