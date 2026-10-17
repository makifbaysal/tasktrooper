import { Eye, EyeOff, Plus, Trash2 } from "lucide-react";
import { useState } from "react";
import {
  headerPlaceholder,
  headerRowValid,
  newHeaderRow,
  type HeaderRow,
} from "@/components/operations/sources/dataSourceSchema";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useI18n } from "@/hooks/useI18n";

interface HeadersEditorProps {
  rows: HeaderRow[];
  onChange: (rows: HeaderRow[]) => void;
  placeholder?: string;
  disabled?: boolean;
  invalid?: boolean;
  labelledBy?: string;
  describedBy?: string;
}

/** Extra HTTP headers as name/value rows, sent as one "Name: value" per
 * line. Write-only: a saved set is never read back, so on edit the rows
 * start empty and anything entered replaces all of it. */
export function HeadersEditor({
  rows,
  onChange,
  placeholder = "",
  disabled = false,
  invalid = false,
  labelledBy,
  describedBy,
}: HeadersEditorProps) {
  const { t } = useI18n();
  const f = (key: string, params?: Record<string, string | number>) => t(`operations.sources.form.${key}`, params);
  const hint = headerPlaceholder(placeholder);
  const [revealed, setRevealed] = useState<number[]>([]);

  const update = (id: number, patch: Partial<HeaderRow>) =>
    onChange(rows.map((row) => (row.id === id ? { ...row, ...patch } : row)));

  return (
    <div role="group" aria-labelledby={labelledBy} aria-describedby={describedBy} className="space-y-2">
      {rows.length === 0 ? (
        <p className="text-caption text-muted-foreground">{f("noHeaders")}</p>
      ) : (
        rows.map((row, i) => {
          const rowInvalid = invalid && !headerRowValid(row);
          const shown = revealed.includes(row.id);
          return (
            <div key={row.id} className="flex gap-2">
              <Input
                aria-label={f("headerName", { n: i + 1 })}
                aria-invalid={rowInvalid || undefined}
                value={row.name}
                onChange={(e) => update(row.id, { name: e.target.value })}
                placeholder={hint.name}
                className="w-2/5 font-mono text-caption"
                autoComplete="off"
                spellCheck={false}
                disabled={disabled}
              />
              <Input
                type={shown ? "text" : "password"}
                aria-label={f("headerValue", { n: i + 1 })}
                aria-invalid={rowInvalid || undefined}
                value={row.value}
                onChange={(e) => update(row.id, { value: e.target.value })}
                placeholder={hint.value}
                className="flex-1 font-mono text-caption"
                autoComplete="new-password"
                spellCheck={false}
                disabled={disabled}
              />
              <Button
                type="button"
                variant="ghost"
                size="icon"
                aria-label={f("revealHeader", { n: i + 1 })}
                aria-pressed={shown}
                title={f("revealHeader", { n: i + 1 })}
                onClick={() =>
                  setRevealed((prev) => (shown ? prev.filter((id) => id !== row.id) : [...prev, row.id]))
                }
                disabled={disabled}
              >
                {shown ? <EyeOff aria-hidden /> : <Eye aria-hidden />}
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                aria-label={f("removeHeader", { n: i + 1 })}
                title={f("removeHeader", { n: i + 1 })}
                onClick={() => onChange(rows.filter((r) => r.id !== row.id))}
                disabled={disabled}
              >
                <Trash2 className="text-destructive" />
              </Button>
            </div>
          );
        })
      )}
      <Button
        type="button"
        variant="outline"
        size="sm"
        className="gap-1"
        onClick={() => onChange([...rows, newHeaderRow()])}
        disabled={disabled}
      >
        <Plus className="h-3 w-3" />
        {f("addHeader")}
      </Button>
    </div>
  );
}
