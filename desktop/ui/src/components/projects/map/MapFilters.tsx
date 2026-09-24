import type { ReactNode } from "react";
import { Badge } from "@/components/ui/badge";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

function FilterChip({ active, onClick, children }: { active: boolean; onClick: () => void; children: ReactNode }) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className="rounded-full focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
    >
      <Badge variant={active ? "default" : "outline"} className={cn(!active && "text-muted-foreground")}>
        {children}
      </Badge>
    </button>
  );
}

interface MapFiltersProps {
  showLibraries: boolean;
  onShowLibrariesChange: (value: boolean) => void;
  showSuggestions: boolean;
  onShowSuggestionsChange: (value: boolean) => void;
  showForeign: boolean;
  onShowForeignChange: (value: boolean) => void;
  className?: string;
}

/** The architecture map's filter row: library components, unconfirmed
 * suggestions and other projects' components/edges are each opt-in toggles
 * so the default view stays this project's confirmed picture. */
export function MapFilters({
  showLibraries,
  onShowLibrariesChange,
  showSuggestions,
  onShowSuggestionsChange,
  showForeign,
  onShowForeignChange,
  className,
}: MapFiltersProps) {
  const { t } = useI18n();
  return (
    <div className={cn("flex flex-wrap items-center gap-1.5", className)}>
      <FilterChip active={showLibraries} onClick={() => onShowLibrariesChange(!showLibraries)}>
        {t("projectModel.map.filters.libraries")}
      </FilterChip>
      <FilterChip active={showSuggestions} onClick={() => onShowSuggestionsChange(!showSuggestions)}>
        {t("projectModel.map.filters.suggestions")}
      </FilterChip>
      <FilterChip active={showForeign} onClick={() => onShowForeignChange(!showForeign)}>
        {t("projectModel.map.filters.otherProjects")}
      </FilterChip>
    </div>
  );
}
