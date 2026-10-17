import { useState, type ReactNode } from "react";
import type { ComponentTable, DeployEnvironment } from "@/api";
import { SignalBindingEditor } from "@/components/operations/signals/SignalBindingEditor";
import { useOpsContext } from "@/components/operations/shell/OpsContext";
import { tableWindow, type TableWindow } from "@/components/operations/tables/componentTables";
import { useComponentSignals } from "@/hooks/useOpsSignals";

export interface TableBindings {
  /** Opens the binding editor on the table's `signal`; undefined when the answer names none. */
  edit: (table: ComponentTable) => (() => void) | undefined;
  /** The span of time the table covers, when it can be told. */
  windowOf: (table: ComponentTable) => TableWindow | undefined;
  /** The editor while it is open. */
  editor: ReactNode;
}

/** What a recipe-table page needs of the bindings behind its tables: the
 * editor a read error opens, and each table's window. A custom-query table's
 * window depends on its source kind and query, so the component's signals
 * are read only while one is listed. */
export function useTableBindings(componentId: string, env: DeployEnvironment, tables: ComponentTable[], onSaved: () => void): TableBindings {
  const { range } = useOpsContext();
  const [editing, setEditing] = useState<string | null>(null);
  const custom = tables.some((table) => !table.recipe_id);
  const signals = useComponentSignals(componentId, env, custom);

  const bindingOf = (table: ComponentTable) =>
    (signals.data?.signals ?? []).find(
      (s) => (table.signal_binding_id && s.binding?.id === table.signal_binding_id) || (table.signal && s.signal === table.signal),
    )?.binding;

  return {
    edit: (table) => (table.signal ? () => setEditing(table.signal!) : undefined),
    windowOf: (table) => tableWindow(table, table.recipe_id ? null : bindingOf(table)),
    editor: editing ? (
      <SignalBindingEditor
        open
        onOpenChange={(open) => !open && setEditing(null)}
        componentId={componentId}
        env={env}
        signal={editing}
        prefillCurrent
        previewWindow={range}
        onSaved={onSaved}
      />
    ) : null,
  };
}
