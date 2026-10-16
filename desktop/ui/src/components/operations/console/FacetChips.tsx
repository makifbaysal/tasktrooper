import type { LucideIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

export interface FacetChip<T extends string> {
  value: T;
  label: string;
  count: number;
  icon?: LucideIcon;
  iconClassName?: string;
}

interface FacetChipsProps<T extends string> {
  label: string;
  items: FacetChip<T>[];
  active: T;
  onSelect: (value: T) => void;
}

/** A row of pressed-state facet buttons, each with its count. */
export function FacetChips<T extends string>({ label, items, active, onSelect }: FacetChipsProps<T>) {
  return (
    <div className="flex flex-wrap items-center gap-1.5" role="group" aria-label={label}>
      {items.map(({ value, label: text, count, icon: Icon, iconClassName }) => {
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
            {Icon && <Icon className={cn("h-3.5 w-3.5", iconClassName)} aria-hidden />}
            {text}
            <span className="font-mono text-micro tabular-nums text-muted-foreground">{count}</span>
          </Button>
        );
      })}
    </div>
  );
}
