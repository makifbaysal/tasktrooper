import { ChevronDown } from "lucide-react";
import {
  createContext,
  forwardRef,
  useContext,
  useId,
  useState,
  type ButtonHTMLAttributes,
  type HTMLAttributes,
  type ReactNode,
} from "react";
import { cn } from "@/lib/utils";

interface AccordionContextValue {
  openValues: string[];
  toggle: (value: string) => void;
}

const AccordionContext = createContext<AccordionContextValue | null>(null);

function useAccordionContext(component: string): AccordionContextValue {
  const ctx = useContext(AccordionContext);
  if (!ctx) throw new Error(`<${component} /> must be used inside <Accordion>`);
  return ctx;
}

interface AccordionBaseProps {
  className?: string;
  children: ReactNode;
}

export interface AccordionSingleProps extends AccordionBaseProps {
  type?: "single";
  /** Controlled open item. Omit (with defaultValue) for uncontrolled use. */
  value?: string;
  defaultValue?: string;
  onValueChange?: (value: string) => void;
  /** Whether the open item can be collapsed back to none. Default true. */
  collapsible?: boolean;
}

export interface AccordionMultipleProps extends AccordionBaseProps {
  type: "multiple";
  value?: string[];
  defaultValue?: string[];
  onValueChange?: (value: string[]) => void;
}

export type AccordionProps = AccordionSingleProps | AccordionMultipleProps;

function Accordion(props: AccordionProps) {
  const { className, children } = props;
  const isMultiple = props.type === "multiple";

  const [uncontrolledSingle, setUncontrolledSingle] = useState(
    !isMultiple ? ((props as AccordionSingleProps).defaultValue ?? "") : "",
  );
  const [uncontrolledMultiple, setUncontrolledMultiple] = useState<string[]>(
    isMultiple ? ((props as AccordionMultipleProps).defaultValue ?? []) : [],
  );

  const openValues: string[] = isMultiple
    ? ((props as AccordionMultipleProps).value ?? uncontrolledMultiple)
    : (() => {
        const current = (props as AccordionSingleProps).value ?? uncontrolledSingle;
        return current ? [current] : [];
      })();

  function toggle(itemValue: string) {
    if (isMultiple) {
      const multi = props as AccordionMultipleProps;
      const current = multi.value ?? uncontrolledMultiple;
      const next = current.includes(itemValue)
        ? current.filter((v) => v !== itemValue)
        : [...current, itemValue];
      if (multi.value === undefined) setUncontrolledMultiple(next);
      multi.onValueChange?.(next);
    } else {
      const single = props as AccordionSingleProps;
      const collapsible = single.collapsible ?? true;
      const current = single.value ?? uncontrolledSingle;
      const next = current === itemValue ? (collapsible ? "" : current) : itemValue;
      if (single.value === undefined) setUncontrolledSingle(next);
      single.onValueChange?.(next);
    }
  }

  return (
    <AccordionContext.Provider value={{ openValues, toggle }}>
      <div className={cn("flex flex-col divide-y divide-border overflow-hidden rounded-lg border border-border", className)}>
        {children}
      </div>
    </AccordionContext.Provider>
  );
}

interface AccordionItemContextValue {
  value: string;
  isOpen: boolean;
  idPrefix: string;
  disabled?: boolean;
}

const AccordionItemContext = createContext<AccordionItemContextValue | null>(null);

function useAccordionItemContext(component: string): AccordionItemContextValue {
  const ctx = useContext(AccordionItemContext);
  if (!ctx) throw new Error(`<${component} /> must be used inside <AccordionItem>`);
  return ctx;
}

export interface AccordionItemProps extends HTMLAttributes<HTMLDivElement> {
  value: string;
  disabled?: boolean;
}

const AccordionItem = forwardRef<HTMLDivElement, AccordionItemProps>(
  ({ value, disabled, className, children, ...props }, ref) => {
    const { openValues } = useAccordionContext("AccordionItem");
    const idPrefix = useId();
    const isOpen = openValues.includes(value);

    return (
      <AccordionItemContext.Provider value={{ value, isOpen, idPrefix, disabled }}>
        <div ref={ref} data-state={isOpen ? "open" : "closed"} className={cn("bg-card", className)} {...props}>
          {children}
        </div>
      </AccordionItemContext.Provider>
    );
  },
);
AccordionItem.displayName = "AccordionItem";

const AccordionTrigger = forwardRef<HTMLButtonElement, ButtonHTMLAttributes<HTMLButtonElement>>(
  ({ className, children, onClick, disabled: disabledProp, ...props }, ref) => {
    const { toggle } = useAccordionContext("AccordionTrigger");
    const { value, isOpen, idPrefix, disabled: itemDisabled } = useAccordionItemContext("AccordionTrigger");
    const disabled = disabledProp ?? itemDisabled;

    return (
      <h3 className="flex">
        <button
          ref={ref}
          type="button"
          id={`${idPrefix}-trigger`}
          aria-expanded={isOpen}
          aria-controls={`${idPrefix}-content`}
          disabled={disabled}
          onClick={(e) => {
            onClick?.(e);
            if (!disabled) toggle(value);
          }}
          className={cn(
            "flex flex-1 items-center justify-between gap-2 px-4 py-3 text-left text-body font-medium transition-colors hover:bg-accent/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset disabled:pointer-events-none disabled:opacity-50",
            className,
          )}
          {...props}
        >
          <span className="flex-1">{children}</span>
          <ChevronDown
            className={cn("h-4 w-4 shrink-0 text-muted-foreground transition-transform duration-200", isOpen && "rotate-180")}
            aria-hidden
          />
        </button>
      </h3>
    );
  },
);
AccordionTrigger.displayName = "AccordionTrigger";

const AccordionContent = forwardRef<HTMLDivElement, HTMLAttributes<HTMLDivElement>>(
  ({ className, children, ...props }, ref) => {
    const { isOpen, idPrefix } = useAccordionItemContext("AccordionContent");

    return (
      <div
        id={`${idPrefix}-content`}
        role="region"
        aria-labelledby={`${idPrefix}-trigger`}
        className={cn("grid transition-[grid-template-rows] duration-200 ease-in-out", isOpen ? "grid-rows-[1fr]" : "grid-rows-[0fr]")}
      >
        <div ref={ref} className="overflow-hidden">
          <div className={cn("px-4 pb-3 pt-0 text-body text-muted-foreground", className)} {...props}>
            {children}
          </div>
        </div>
      </div>
    );
  },
);
AccordionContent.displayName = "AccordionContent";

export { Accordion, AccordionItem, AccordionTrigger, AccordionContent };
