import { AlertTriangle, Rocket, Smartphone } from "lucide-react";
import { NavLink, Outlet } from "react-router-dom";
import { PageContent } from "@/components/layout/PageContent";
import { PageSuspense } from "@/components/layout/PageSuspense";
import { PageHeader } from "@/components/admin/PageHeader";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

export function OperationsLayout() {
  const { t } = useI18n();
  const tabs = [
    { to: "/operations/deployments", label: t("operations.tabs.deployments"), icon: Rocket, end: true },
    { to: "/operations/apps", label: t("operations.tabs.apps"), icon: Smartphone, end: true },
    { to: "/operations/incidents", label: t("operations.tabs.incidents"), icon: AlertTriangle, end: true },
  ];

  return (
    <PageContent>
      <PageHeader title={t("operations.title")} description={t("operations.description")} />
      <nav className="mb-6 flex flex-wrap gap-1 border-b border-border">
        {tabs.map(({ to, label, icon: Icon, end }) => (
          <NavLink
            key={to}
            to={to}
            end={end}
            className={({ isActive }) =>
              cn(
                "flex items-center gap-2 border-b-2 px-4 py-2 text-sm font-medium transition-colors -mb-px",
                isActive
                  ? "border-primary text-primary"
                  : "border-transparent text-muted-foreground hover:border-border hover:text-foreground",
              )
            }
          >
            <Icon className="h-4 w-4 shrink-0" />
            {label}
          </NavLink>
        ))}
      </nav>
      <PageSuspense>
        <Outlet />
      </PageSuspense>
    </PageContent>
  );
}
