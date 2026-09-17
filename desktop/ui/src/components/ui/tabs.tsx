import {
  createContext,
  forwardRef,
  useContext,
  useId,
  useRef,
  type ButtonHTMLAttributes,
  type HTMLAttributes,
  type KeyboardEvent,
  type ReactNode,
} from "react";
import { cn } from "@/lib/utils";

type TabsVariant = "underline" | "pill";

interface TabsContextValue {
  value: string;
  onValueChange: (value: string) => void;
  idPrefix: string;
  variant: TabsVariant;
}

const TabsContext = createContext<TabsContextValue | null>(null);

function useTabsContext(component: string): TabsContextValue {
  const ctx = useContext(TabsContext);
  if (!ctx) throw new Error(`<${component} /> must be used inside <Tabs>`);
  return ctx;
}

export interface TabsProps {
  value: string;
  onValueChange: (value: string) => void;
  /** "underline" (default) matches the app's sub-nav look; "pill" is a
   * segmented-control style for tighter/secondary contexts. */
  variant?: TabsVariant;
  className?: string;
  children: ReactNode;
}

function Tabs({ value, onValueChange, variant = "underline", className, children }: TabsProps) {
  const idPrefix = useId();
  return (
    <TabsContext.Provider value={{ value, onValueChange, idPrefix, variant }}>
      <div className={cn("w-full", className)}>{children}</div>
    </TabsContext.Provider>
  );
}

const TabsList = forwardRef<HTMLDivElement, HTMLAttributes<HTMLDivElement>>(
  ({ className, onKeyDown, ...props }, forwardedRef) => {
    const { variant } = useTabsContext("TabsList");
    const innerRef = useRef<HTMLDivElement | null>(null);

    const setRefs = (node: HTMLDivElement | null) => {
      innerRef.current = node;
      if (typeof forwardedRef === "function") forwardedRef(node);
      else if (forwardedRef) forwardedRef.current = node;
    };

    function handleKeyDown(e: KeyboardEvent<HTMLDivElement>) {
      onKeyDown?.(e);
      if (e.defaultPrevented) return;
      if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(e.key)) return;

      const tabs = Array.from(
        innerRef.current?.querySelectorAll<HTMLButtonElement>('[role="tab"]:not(:disabled)') ?? [],
      );
      if (tabs.length === 0) return;
      const currentIndex = tabs.findIndex((t) => t === document.activeElement);
      if (currentIndex === -1) return;

      let nextIndex = currentIndex;
      if (e.key === "ArrowRight") nextIndex = (currentIndex + 1) % tabs.length;
      else if (e.key === "ArrowLeft") nextIndex = (currentIndex - 1 + tabs.length) % tabs.length;
      else if (e.key === "Home") nextIndex = 0;
      else if (e.key === "End") nextIndex = tabs.length - 1;

      e.preventDefault();
      const next = tabs[nextIndex];
      next.focus();
      next.click();
    }

    return (
      <div
        ref={setRefs}
        role="tablist"
        onKeyDown={handleKeyDown}
        className={cn(
          variant === "pill"
            ? "inline-flex items-center gap-1 rounded-md bg-muted p-1"
            : "flex flex-wrap gap-1 border-b border-border",
          className,
        )}
        {...props}
      />
    );
  },
);
TabsList.displayName = "TabsList";

export interface TabsTriggerProps extends Omit<ButtonHTMLAttributes<HTMLButtonElement>, "value"> {
  value: string;
}

const TabsTrigger = forwardRef<HTMLButtonElement, TabsTriggerProps>(
  ({ value, className, children, disabled, onClick, ...props }, ref) => {
    const { value: activeValue, onValueChange, idPrefix, variant } = useTabsContext("TabsTrigger");
    const isActive = value === activeValue;

    return (
      <button
        ref={ref}
        type="button"
        role="tab"
        id={`${idPrefix}-tab-${value}`}
        aria-selected={isActive}
        aria-controls={`${idPrefix}-panel-${value}`}
        data-state={isActive ? "active" : "inactive"}
        data-value={value}
        tabIndex={isActive ? 0 : -1}
        disabled={disabled}
        onClick={(e) => {
          onClick?.(e);
          if (!disabled) onValueChange(value);
        }}
        className={cn(
          "inline-flex items-center whitespace-nowrap text-body font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-50",
          variant === "pill"
            ? cn(
                "rounded-md px-3 py-1.5",
                isActive ? "bg-background text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground",
              )
            : cn(
                "-mb-px border-b-2 px-4 py-2.5",
                isActive
                  ? "border-primary text-primary"
                  : "border-transparent text-muted-foreground hover:border-border hover:text-foreground",
              ),
          className,
        )}
        {...props}
      >
        <span className="flex w-full items-center justify-between gap-2">{children}</span>
      </button>
    );
  },
);
TabsTrigger.displayName = "TabsTrigger";

export interface TabsContentProps extends HTMLAttributes<HTMLDivElement> {
  value: string;
}

const TabsContent = forwardRef<HTMLDivElement, TabsContentProps>(
  ({ value, className, children, ...props }, ref) => {
    const { value: activeValue, idPrefix } = useTabsContext("TabsContent");
    const isActive = value === activeValue;
    if (!isActive) return null;

    return (
      <div
        ref={ref}
        role="tabpanel"
        id={`${idPrefix}-panel-${value}`}
        aria-labelledby={`${idPrefix}-tab-${value}`}
        tabIndex={0}
        className={cn("mt-4 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring", className)}
        {...props}
      >
        {children}
      </div>
    );
  },
);
TabsContent.displayName = "TabsContent";

export { Tabs, TabsList, TabsTrigger, TabsContent };
