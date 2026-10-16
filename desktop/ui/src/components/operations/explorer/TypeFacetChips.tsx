import { ALL, type ExplorerRowType } from "@/components/operations/console/opsEntities";
import type { OpsEntityType } from "@/components/operations/console/entityTypes";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

const ORDER: ExplorerRowType[] = ["services", "web", "mobile", "workers", "other", "repository"];

interface TypeFacetChipsProps {
  counts: Map<ExplorerRowType, number>;
  active: OpsEntityType | typeof ALL;
  onSelect: (type: OpsEntityType | typeof ALL) => void;
}

/** "All entities"' type facets: every type present, with its count. */
export function TypeFacetChips({ counts, active, onSelect }: TypeFacetChipsProps) {
  const { t } = useI18n();
  const total = [...counts.values()].reduce((a, b) => a + b, 0);
  const chip = (value: OpsEntityType | typeof ALL, label: string, count: number) => {
    const pressed = active === value;
    return (
      <Button
        key={value}
        variant={pressed ? "secondary" : "ghost"}
        size="sm"
        aria-pressed={pressed}
        onClick={() => onSelect(value)}
        className={cn("h-7 gap-1.5 rounded-full border px-3", pressed ? "border-ring" : "border-border")}
      >
        {label}
        <span className="font-mono text-micro tabular-nums text-muted-foreground">{count}</span>
      </Button>
    );
  };
  return (
    <div className="flex flex-wrap items-center gap-1.5" role="group" aria-label={t("operations.explorer.typeFilter")}>
      {chip(ALL, t("operations.explorer.allTypes"), total)}
      {ORDER.filter((type) => type !== "repository" && ((counts.get(type) ?? 0) > 0 || active === type)).map((type) =>
        chip(type as OpsEntityType, t(`operations.explorer.types.${type}`), counts.get(type) ?? 0),
      )}
    </div>
  );
}
