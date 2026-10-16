import { useId, type ReactNode } from "react";
import { cn } from "@/lib/utils";

interface ReleaseSectionProps {
  title: string;
  /** Right of the title: meta text and the section's own buttons. */
  actions?: ReactNode;
  children: ReactNode;
  className?: string;
}

/** One titled block of the Releases page, headed the way the entity Summary heads its own. */
export function ReleaseSection({ title, actions, children, className }: ReleaseSectionProps) {
  const id = useId();
  return (
    <section aria-labelledby={id} className={cn("min-w-0 space-y-2", className)}>
      <div className="flex min-h-8 flex-wrap items-center justify-between gap-2">
        <h2 id={id} className="text-micro font-medium tracking-wide text-muted-foreground uppercase">
          {title}
        </h2>
        {actions && <div className="flex flex-wrap items-center gap-2">{actions}</div>}
      </div>
      {children}
    </section>
  );
}
