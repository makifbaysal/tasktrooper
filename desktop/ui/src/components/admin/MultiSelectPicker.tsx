import { ChevronDown, Search, X } from "lucide-react";
import { useId, useMemo, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

export interface MultiSelectOption {
  value: string;
  label?: string;
  description?: string;
  locked?: boolean;
}

interface MultiSelectPickerProps {
  label: string;
  options: MultiSelectOption[];
  selected: string[];
  onChange: (values: string[]) => void;
  exclude?: string[];
  loading?: boolean;
  emptyText?: string;
}

function normalize(value: string) {
  return value.toLocaleLowerCase("tr");
}

export function MultiSelectPicker({
  label,
  options,
  selected: chosen,
  onChange: emit,
  exclude = [],
  loading = false,
  emptyText,
}: MultiSelectPickerProps) {
  const { t } = useI18n();
  const resolvedEmptyText = emptyText ?? t("frame.admin.multiSelect.emptyDefault");
  const baseId = useId();
  const lockedValues = useMemo(
    () => options.filter((option) => option.locked).map((option) => option.value),
    [options],
  );
  const selected = useMemo(
    () => [...lockedValues, ...chosen.filter((value) => !lockedValues.includes(value))],
    [lockedValues, chosen],
  );
  const onChange = (values: string[]) => emit(values.filter((value) => !lockedValues.includes(value)));
  const [expanded, setExpanded] = useState(false);
  const [query, setQuery] = useState("");

  const visibleOptions = useMemo(() => {
    const byValue = new Map<string, MultiSelectOption>();
    for (const option of options) {
      byValue.set(option.value, option);
    }
    for (const value of selected) {
      if (!byValue.has(value)) {
        byValue.set(value, { value, label: value });
      }
    }
    return [...byValue.values()].filter(
      (option) => selected.includes(option.value) || !exclude.includes(option.value),
    );
  }, [options, selected, exclude]);

  const filteredOptions = useMemo(() => {
    const q = normalize(query.trim());
    if (!q) return visibleOptions;
    return visibleOptions.filter((option) => {
      const labelText = option.label ?? option.value;
      return (
        normalize(labelText).includes(q) ||
        normalize(option.value).includes(q) ||
        (option.description ? normalize(option.description).includes(q) : false)
      );
    });
  }, [visibleOptions, query]);

  const toggle = (value: string, checked: boolean) => {
    if (checked) {
      if (!selected.includes(value)) {
        onChange([...selected, value]);
      }
      return;
    }
    onChange(selected.filter((item) => item !== value));
  };

  const remove = (value: string) => {
    onChange(selected.filter((item) => item !== value));
  };

  const labelByValue = useMemo(() => {
    const map = new Map<string, string>();
    for (const option of visibleOptions) {
      map.set(option.value, option.label ?? option.value);
    }
    return map;
  }, [visibleOptions]);

  if (loading) {
    return (
      <div className="rounded-lg border border-border/60 bg-muted/10 p-3">
        <Skeleton className="mb-2 h-3 w-24" />
        <Skeleton className="h-8 w-full" />
      </div>
    );
  }

  return (
    <div className="rounded-lg border border-border/60 bg-muted/10 p-3">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0 flex-1 space-y-1.5">
          <p className="text-xs font-medium text-muted-foreground">{label}</p>
          {selected.length === 0 ? (
            <p className="text-xs text-muted-foreground/80">{t("frame.admin.multiSelect.noSelection")}</p>
          ) : (
            <div className="flex flex-wrap gap-1">
              {selected.map((value) => {
                const displayLabel = labelByValue.get(value) ?? value;
                return (
                <Badge key={value} variant="secondary" className="max-w-full gap-1 pr-1 text-micro">
                  <span className="truncate">{displayLabel}</span>
                  {lockedValues.includes(value) ? null : (
                    <button
                      type="button"
                      onClick={() => remove(value)}
                      className="rounded-full p-0.5 hover:bg-muted"
                      aria-label={t("frame.admin.multiSelect.remove", { label: displayLabel })}
                    >
                      <X className="h-3 w-3" />
                    </button>
                  )}
                </Badge>
                );
              })}
            </div>
          )}
        </div>
        <Button
          type="button"
          size="sm"
          variant={expanded ? "secondary" : "outline"}
          className="shrink-0 gap-1"
          disabled={visibleOptions.length === 0 && selected.length === 0}
          onClick={() => setExpanded((open) => !open)}
        >
          {expanded ? t("frame.admin.multiSelect.close") : t("frame.admin.multiSelect.select")}
          <ChevronDown className={cn("h-3.5 w-3.5 transition-transform", expanded && "rotate-180")} />
        </Button>
      </div>

      {expanded && (
        <div className="mt-3 space-y-2 border-t border-border/60 pt-3">
          {visibleOptions.length === 0 ? (
            <p className="py-4 text-center text-xs text-muted-foreground">{resolvedEmptyText}</p>
          ) : (
            <>
              <div className="relative">
                <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
                <Input
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                  placeholder={t("frame.admin.multiSelect.search")}
                  className="h-8 pl-8 text-sm"
                />
              </div>
              <ScrollArea className="h-44 rounded-md border border-border/60 bg-background">
                <div className="p-1">
                  {filteredOptions.length === 0 ? (
                    <p className="px-2 py-6 text-center text-xs text-muted-foreground">{t("frame.admin.multiSelect.noResults")}</p>
                  ) : (
                    filteredOptions.map((option) => {
                      const checked = selected.includes(option.value);
                      const id = `${baseId}-${option.value}`;
                      const locked = option.locked === true;
                      const title = option.description ?? option.label ?? option.value;
                      return (
                        <label
                          key={option.value}
                          htmlFor={id}
                          title={title}
                          className={cn(
                            "flex items-center gap-2 rounded-md px-2 py-1.5",
                            locked ? "cursor-default" : "cursor-pointer hover:bg-muted/50",
                            checked && "bg-muted/40",
                          )}
                        >
                          <Checkbox
                            id={id}
                            checked={checked}
                            disabled={locked}
                            onCheckedChange={(value) => toggle(option.value, value === true)}
                          />
                          <span className="truncate text-xs">{option.label ?? option.value}</span>
                        </label>
                      );
                    })
                  )}
                </div>
              </ScrollArea>
              <p className="text-right text-micro text-muted-foreground">
                {t("frame.admin.multiSelect.summary", {
                  selected: selected.length,
                  shown: filteredOptions.length,
                })}
              </p>
            </>
          )}
        </div>
      )}
    </div>
  );
}
