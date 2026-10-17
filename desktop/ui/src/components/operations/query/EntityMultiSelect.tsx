import { Boxes, ChevronDown, Search } from "lucide-react";
import { useMemo, useState } from "react";
import type { OpsEntity } from "@/components/operations/shell/useOpsEntities";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

const SEARCH_FROM = 8;

interface EntityMultiSelectProps {
  entities: OpsEntity[];
  selected: string[];
  onChange: (ids: string[]) => void;
  /** Read at most this many at once; past it the menu says so. */
  limit?: number;
  /** Nothing picked means "pick some", not "all of them". */
  required?: boolean;
  className?: string;
}

/** The query bar's entity filter: a menu of checkable entities. */
export function EntityMultiSelect({ entities, selected, onChange, limit, required = false, className }: EntityMultiSelectProps) {
  const { t } = useI18n();
  const p = (key: string, params?: Record<string, string | number>) => t(`operations.opsQuery.entityPicker.${key}`, params);
  const [query, setQuery] = useState("");
  const byId = useMemo(() => new Map(entities.map((e) => [e.id, e])), [entities]);
  const shown = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return entities;
    return entities.filter((e) => e.name.toLowerCase().includes(q) || e.repositoryName.toLowerCase().includes(q));
  }, [entities, query]);

  const toggle = (id: string, on: boolean) =>
    onChange(on ? [...selected.filter((s) => s !== id), id] : selected.filter((s) => s !== id));

  const label =
    selected.length === 0
      ? required
        ? p("pick")
        : p("all")
      : selected.length === 1
        ? (byId.get(selected[0])?.name ?? p("count", { count: 1 }))
        : p("count", { count: selected.length });

  return (
    <DropdownMenu onOpenChange={(open) => !open && setQuery("")}>
      <DropdownMenuTrigger asChild>
        <Button
          variant="outline"
          size="sm"
          className={cn("h-8 max-w-56 justify-between gap-1.5", required && selected.length === 0 && "border-dashed", className)}
          aria-label={p("label", { value: label })}
        >
          <Boxes aria-hidden className="text-muted-foreground" />
          <span className="truncate">{label}</span>
          <ChevronDown aria-hidden className="text-muted-foreground" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-72">
        <DropdownMenuLabel>{p("title")}</DropdownMenuLabel>
        {entities.length >= SEARCH_FROM && (
          <div className="relative px-1 pb-1">
            <Search aria-hidden className="pointer-events-none absolute top-1/2 left-3.5 h-3.5 w-3.5 -translate-y-[calc(50%+2px)] text-muted-foreground" />
            <Input
              type="search"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              // The menu's typeahead would take every letter typed here and
              // move focus to the item it matches.
              onKeyDown={(e) => {
                if (e.key !== "Escape" && e.key !== "ArrowDown" && e.key !== "ArrowUp") e.stopPropagation();
              }}
              placeholder={p("search")}
              aria-label={p("search")}
              className="h-8 pl-8 text-caption"
            />
          </div>
        )}
        <div className="max-h-64 overflow-y-auto">
          {shown.length === 0 ? (
            <p className="px-2 py-4 text-center text-caption text-muted-foreground">{p("none")}</p>
          ) : (
            shown.map((e) => (
              <DropdownMenuCheckboxItem
                key={e.id}
                checked={selected.includes(e.id)}
                onCheckedChange={(on) => toggle(e.id, on === true)}
                onSelect={(event) => event.preventDefault()}
              >
                <span className="min-w-0 flex-1 truncate">{e.name}</span>
                <span className="max-w-[40%] truncate text-micro text-muted-foreground">{e.repositoryName}</span>
              </DropdownMenuCheckboxItem>
            ))
          )}
        </div>
        {limit !== undefined && selected.length > limit && (
          <p className="px-2 py-1.5 text-micro text-warning">{p("overLimit", { max: limit })}</p>
        )}
        {selected.length > 0 && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem onSelect={() => onChange([])}>{p("clear")}</DropdownMenuItem>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
