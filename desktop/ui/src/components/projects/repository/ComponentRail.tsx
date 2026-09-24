import type { ReactNode } from "react";
import type { Component } from "@/api";
import { RoleBadge } from "@/components/projects/model/RoleBadge";
import { useI18n } from "@/hooks/useI18n";
import { effectiveRole } from "@/lib/project-model";
import { cn } from "@/lib/utils";

interface ComponentRailProps {
  components: Component[];
  /** null means "All" — only reachable when `allowAll` is set (the Links tab). */
  selectedId: string | null;
  onSelect: (id: string | null) => void;
  allowAll?: boolean;
  allLabel?: string;
  renderTrailing?: (component: Component) => ReactNode;
  footer?: ReactNode;
  className?: string;
}

/**
 * The component list shared by the Components/Checks/Links tabs — one
 * repository's components, each with its role and an optional per-tab
 * trailing count. Dismissed components stay listed, muted.
 */
export function ComponentRail({
  components,
  selectedId,
  onSelect,
  allowAll = false,
  allLabel,
  renderTrailing,
  footer,
  className,
}: ComponentRailProps) {
  const { t } = useI18n();
  return (
    <div className={cn("flex w-64 shrink-0 flex-col gap-1", className)}>
      {allowAll && (
        <button
          type="button"
          onClick={() => onSelect(null)}
          className={cn(
            "flex items-center justify-between rounded-md px-2.5 py-2 text-left text-body transition-colors hover:bg-muted/60",
            selectedId === null && "bg-muted font-medium",
          )}
        >
          <span>{allLabel ?? t("repositoryPage.links.all")}</span>
        </button>
      )}
      {components.map((component) => {
        const dismissed = component.status === "dismissed";
        const role = effectiveRole(component);
        return (
          <button
            key={component.id}
            type="button"
            onClick={() => onSelect(component.id)}
            className={cn(
              "flex flex-col gap-1 rounded-md px-2.5 py-2 text-left transition-colors hover:bg-muted/60",
              selectedId === component.id && "bg-muted",
              dismissed && "opacity-50",
            )}
          >
            <div className="flex items-center justify-between gap-2">
              <span className={cn("truncate font-mono text-caption", dismissed && "line-through")}>
                {component.path === "." ? "/" : component.path}
              </span>
              {role && <RoleBadge role={role} className="shrink-0" />}
            </div>
            <div className="flex items-center justify-between gap-2 text-micro text-muted-foreground">
              {dismissed && <span>{t("repositoryPage.components.dismissed")}</span>}
              {renderTrailing && <span className="ml-auto">{renderTrailing(component)}</span>}
            </div>
          </button>
        );
      })}
      {footer}
    </div>
  );
}
