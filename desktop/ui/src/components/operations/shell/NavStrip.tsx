import type { LucideIcon } from "lucide-react";
import { Fragment } from "react";
import { Link } from "react-router-dom";
import { cn } from "@/lib/utils";

export interface NavStripItem {
  key: string;
  to: string;
  label: string;
  icon?: LucideIcon;
  active: boolean;
}

interface NavStripProps {
  label: string;
  groups: NavStripItem[][];
  className?: string;
}

/** A side navigation folded into one scrolling row, for widths where a
 * column would not fit: groups are kept apart by a hairline. */
export function NavStrip({ label, groups, className }: NavStripProps) {
  return (
    <nav aria-label={label} className={className}>
      <ul className="flex items-center gap-1 overflow-x-auto py-2 scrollbar-thin">
        {groups.map((items, index) => (
          <Fragment key={items[0]?.key ?? index}>
            {index > 0 && <li aria-hidden className="mx-1 h-5 w-px shrink-0 bg-border" />}
            {items.map((item) => {
              const Icon = item.icon;
              return (
                <li key={item.key} className="shrink-0">
                  <Link
                    to={item.to}
                    aria-current={item.active ? "page" : undefined}
                    className={cn(
                      "flex items-center gap-1.5 whitespace-nowrap rounded-md px-2.5 py-1.5 text-caption font-medium transition-colors",
                      item.active
                        ? "bg-sidebar-accent text-sidebar-accent-foreground"
                        : "text-muted-foreground hover:bg-sidebar-accent/60 hover:text-sidebar-accent-foreground",
                    )}
                  >
                    {Icon && <Icon className="h-3.5 w-3.5 shrink-0" aria-hidden />}
                    {item.label}
                  </Link>
                </li>
              );
            })}
          </Fragment>
        ))}
      </ul>
    </nav>
  );
}
