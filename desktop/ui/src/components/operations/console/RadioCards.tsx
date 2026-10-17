import type { LucideIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

export interface RadioCardOption<T extends string> {
  value: T;
  label: string;
  description?: string;
  icon?: LucideIcon;
}

interface RadioCardsProps<T extends string> {
  /** The id of the element that names the group. */
  labelledBy: string;
  options: RadioCardOption<T>[];
  value: T;
  onChange: (value: T) => void;
  disabled?: boolean;
  className?: string;
}

/** Exclusive choices as outlined cards, each a word and a line: the console's
 * dialogs' "which one" pickers (a data source kind, a mode, a target). */
export function RadioCards<T extends string>({ labelledBy, options, value, onChange, disabled, className }: RadioCardsProps<T>) {
  return (
    <div role="radiogroup" aria-labelledby={labelledBy} className={cn("grid gap-2", className)}>
      {options.map((o) => {
        const checked = o.value === value;
        const Icon = o.icon;
        return (
          <Button
            key={o.value}
            role="radio"
            aria-checked={checked}
            variant="outline"
            disabled={disabled}
            onClick={() => onChange(o.value)}
            className={cn(
              "h-auto items-start justify-start gap-3 whitespace-normal px-4 py-3 text-left",
              checked && "border-primary bg-primary/5 ring-2 ring-primary/20",
            )}
          >
            {Icon && <Icon className={cn("mt-0.5 h-4 w-4 shrink-0", checked ? "text-primary" : "text-muted-foreground")} aria-hidden />}
            <span className="flex min-w-0 flex-col gap-0.5">
              <span className="font-medium">{o.label}</span>
              {o.description && <span className="text-caption font-normal text-muted-foreground">{o.description}</span>}
            </span>
          </Button>
        );
      })}
    </div>
  );
}
