import { useMemo } from "react";
import { ALL } from "@/components/operations/console/opsEntities";
import type { OpsEntity } from "@/components/operations/shell/useOpsEntities";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";

interface EntityPickerProps {
  entities: OpsEntity[];
  /** An entity id, or `ALL`. */
  value: string;
  /** An entity picked by link that the list does not carry (a library, say). */
  current?: OpsEntity;
  label: string;
  allLabel: string;
  onChange: (value: string) => void;
}

/** The console's entity filter: every listed entity, its repository beside a
 * name another repository shares. */
export function EntityPicker({ entities, value, current, label, allLabel, onChange }: EntityPickerProps) {
  const options = useMemo(() => {
    const list = current && !entities.some((e) => e.id === current.id) ? [...entities, current] : entities;
    const names = new Map<string, number>();
    for (const e of list) names.set(e.name, (names.get(e.name) ?? 0) + 1);
    return list
      .map((e) => ({ id: e.id, text: (names.get(e.name) ?? 0) > 1 ? `${e.name} · ${e.repositoryName}` : e.name }))
      .sort((a, b) => a.text.localeCompare(b.text));
  }, [entities, current]);

  return (
    <Select value={value} onValueChange={onChange}>
      <SelectTrigger className="h-8 w-52 text-caption" aria-label={label}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value={ALL}>{allLabel}</SelectItem>
        {options.map((o) => (
          <SelectItem key={o.id} value={o.id}>
            {o.text}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
