import { Fragment, type ReactNode } from "react";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { ScrollArea } from "@/components/ui/scroll-area";
import { cn } from "@/lib/utils";

interface DetailPanelProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: ReactNode;
  description?: ReactNode;
  /** Status pills and chips under the title. */
  badges?: ReactNode;
  /** The panel's buttons, kept above the scrolling body. */
  actions?: ReactNode;
  children: ReactNode;
  className?: string;
}

/** The console's flyout: one record (a change, an issue) on the right edge,
 * over the list it was opened from, so the list keeps its place. */
export function DetailPanel({ open, onOpenChange, title, description, badges, actions, children, className }: DetailPanelProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        {...(description ? {} : { "aria-describedby": undefined })}
        className={cn(
          "top-0 right-0 bottom-0 left-auto flex h-full max-h-none w-full max-w-full translate-x-0 translate-y-0 flex-col gap-0 rounded-none border-y-0 border-r-0 p-0 sm:max-w-xl lg:max-w-2xl",
          className,
        )}
      >
        <DialogHeader className="space-y-2 border-b border-border px-5 py-4 text-left sm:text-left">
          <DialogTitle className="pr-8 text-left text-heading leading-snug break-words">{title}</DialogTitle>
          {description && <DialogDescription className="text-left">{description}</DialogDescription>}
          {badges && <div className="flex flex-wrap items-center gap-1.5">{badges}</div>}
        </DialogHeader>
        {actions && <div className="flex flex-wrap items-center gap-2 border-b border-border bg-muted/30 px-5 py-2.5">{actions}</div>}
        <ScrollArea className="min-h-0 flex-1">
          <div className="space-y-6 px-5 py-5">{children}</div>
        </ScrollArea>
      </DialogContent>
    </Dialog>
  );
}

interface DetailSectionProps {
  title: string;
  aside?: ReactNode;
  children: ReactNode;
  className?: string;
}

export function DetailSection({ title, aside, children, className }: DetailSectionProps) {
  return (
    <section className={cn("space-y-2", className)} aria-label={title}>
      <div className="flex items-center justify-between gap-2">
        <h3 className="text-micro font-medium tracking-wide text-muted-foreground uppercase">{title}</h3>
        {aside}
      </div>
      {children}
    </section>
  );
}

export interface DetailFact {
  label: string;
  value: ReactNode;
}

/** Label/value pairs in two columns: the dense fact sheet at the top of a record. */
export function DetailFacts({ items, className }: { items: DetailFact[]; className?: string }) {
  return (
    <dl className={cn("grid grid-cols-[minmax(7rem,max-content)_1fr] gap-x-4 gap-y-1.5 text-caption", className)}>
      {items.map((item) => (
        <Fragment key={item.label}>
          <dt className="text-muted-foreground">{item.label}</dt>
          <dd className="min-w-0 break-words">{item.value}</dd>
        </Fragment>
      ))}
    </dl>
  );
}
