import { useState } from "react";
import { durationParts, type DurationParts } from "@/components/operations/alertRules/alertRules";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useI18n } from "@/hooks/useI18n";

const FACTOR: Record<DurationParts["unit"], number> = { seconds: 1, minutes: 60, hours: 3600 };
const UNITS: DurationParts["unit"][] = ["seconds", "minutes", "hours"];

interface DurationInputProps {
  id: string;
  seconds: number;
  onChange: (seconds: number) => void;
  /** The field's name for the unit picker's label. */
  label: string;
  invalid?: boolean;
  disabled?: boolean;
  describedBy?: string;
}

/** A span typed as a number and a unit, handed back in seconds. It starts
 * from `seconds` once; remount it (a `key`) to load another value. */
export function DurationInput({ id, seconds, onChange, label, invalid, disabled, describedBy }: DurationInputProps) {
  const { t } = useI18n();
  const [unit, setUnit] = useState<DurationParts["unit"]>(() => durationParts(seconds).unit);
  const [text, setText] = useState(() => String(durationParts(seconds).value));

  const emit = (nextText: string, nextUnit: DurationParts["unit"]) => {
    const n = Number(nextText.replace(",", "."));
    onChange(nextText.trim() === "" || !Number.isFinite(n) ? NaN : n * FACTOR[nextUnit]);
  };

  return (
    <div className="flex items-center gap-2">
      <Input
        id={id}
        type="number"
        min={0}
        inputMode="decimal"
        value={text}
        onChange={(e) => {
          setText(e.target.value);
          emit(e.target.value, unit);
        }}
        aria-invalid={invalid ? true : undefined}
        aria-describedby={describedBy}
        disabled={disabled}
        className="w-24 font-mono tabular-nums"
      />
      <Select
        value={unit}
        onValueChange={(v) => {
          const next = v as DurationParts["unit"];
          setUnit(next);
          emit(text, next);
        }}
        disabled={disabled}
      >
        <SelectTrigger className="w-28" aria-label={t("operations.alertRules.editor.durationUnit", { field: label })}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {UNITS.map((u) => (
            <SelectItem key={u} value={u}>
              {t(`operations.alertRules.editor.units.${u}`)}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
