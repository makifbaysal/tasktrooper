import type { ReactNode } from "react";
import { Card } from "@/components/ui/card";
import { cn } from "@/lib/utils";

const TH = "px-3 py-2 text-left text-micro font-medium tracking-wide text-muted-foreground uppercase whitespace-nowrap";
const TD = "px-3 py-2 align-middle";

export interface TelemetryColumn {
  key: string;
  label: ReactNode;
  className?: string;
  numeric?: boolean;
}

interface TelemetryTableProps {
  label: string;
  columns: TelemetryColumn[];
  children: ReactNode;
  className?: string;
}

/** The console's dense list: sticky-free, scrolls sideways below its minimum width. */
export function TelemetryTable({ label, columns, children, className }: TelemetryTableProps) {
  return (
    <Card className={cn("overflow-hidden", className)}>
      <div className="overflow-x-auto">
        <table className="w-full min-w-[640px] border-collapse text-caption" aria-label={label}>
          <thead className="bg-muted/40">
            <tr className="border-b border-border">
              {columns.map((c) => (
                <th key={c.key} scope="col" className={cn(TH, c.numeric && "text-right", c.className)}>
                  {c.label}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>{children}</tbody>
        </table>
      </div>
    </Card>
  );
}

interface TelemetryRowProps {
  children: ReactNode;
  onOpen?: () => void;
  selected?: boolean;
}

export function TelemetryRow({ children, onOpen, selected }: TelemetryRowProps) {
  return (
    <tr
      className={cn("border-b border-border/50 last:border-0 hover:bg-muted/30", onOpen && "cursor-pointer", selected && "bg-muted/30")}
      onClick={(ev) => {
        if (!onOpen) return;
        if ((ev.target as HTMLElement).closest("a,button")) return;
        onOpen();
      }}
    >
      {children}
    </tr>
  );
}

interface TelemetryCellProps {
  children?: ReactNode;
  numeric?: boolean;
  className?: string;
}

export function TelemetryCell({ children, numeric, className }: TelemetryCellProps) {
  return <td className={cn(TD, numeric && "text-right font-mono tabular-nums", className)}>{children}</td>;
}
