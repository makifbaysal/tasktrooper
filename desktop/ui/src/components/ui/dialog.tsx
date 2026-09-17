import * as DialogPrimitive from "@radix-ui/react-dialog";
import { X } from "lucide-react";
import {
  createContext,
  forwardRef,
  useCallback,
  useEffect,
  useRef,
  useState,
  type ComponentPropsWithoutRef,
  type ElementRef,
} from "react";
import { cn } from "@/lib/utils";

const Dialog = DialogPrimitive.Root;
const DialogTrigger = DialogPrimitive.Trigger;
const DialogPortal = DialogPrimitive.Portal;
const DialogClose = DialogPrimitive.Close;

export const DialogPortalContainerContext = createContext<HTMLElement | null>(null);

function isNestedOverlayOpen() {
  return document.querySelector('[role="listbox"][data-state="open"]') !== null;
}

function isNestedOverlayPortalTarget(target: EventTarget | null) {
  if (!(target instanceof Element)) return false;
  return Boolean(
    target.closest('[role="listbox"]') ||
    target.closest("[data-radix-popper-content-wrapper]") ||
    target.closest("[data-radix-select-viewport]"),
  );
}

function useNestedOverlayDismissGuard() {
  const wasNestedOverlayOpenRef = useRef(false);

  useEffect(() => {
    const onPointerDownCapture = () => {
      wasNestedOverlayOpenRef.current = isNestedOverlayOpen();
    };
    document.addEventListener("pointerdown", onPointerDownCapture, true);
    return () => document.removeEventListener("pointerdown", onPointerDownCapture, true);
  }, []);

  return useCallback((event: { preventDefault: () => void }, target?: EventTarget | null) => {
    if (
      wasNestedOverlayOpenRef.current ||
      isNestedOverlayOpen() ||
      isNestedOverlayPortalTarget(target ?? null)
    ) {
      event.preventDefault();
      wasNestedOverlayOpenRef.current = false;
    }
  }, []);
}

const DialogOverlay = forwardRef<
  ElementRef<typeof DialogPrimitive.Overlay>,
  ComponentPropsWithoutRef<typeof DialogPrimitive.Overlay>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Overlay
    ref={ref}
    className={cn(
      "fixed inset-0 z-50 bg-black/60 backdrop-blur-sm data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0",
      className,
    )}
    {...props}
  />
));
DialogOverlay.displayName = DialogPrimitive.Overlay.displayName;

interface DialogContentOwnProps {
  /**
   * Hides the top-right "X". Additive and off by default — every existing
   * dialog keeps its close button. Used for a dialog that must be finished
   * rather than dismissed (see InitialSetupDialog's `dismissable` prop on
   * FormDialog, which is what actually sets this).
   */
  hideCloseButton?: boolean;
}

const DialogContent = forwardRef<
  ElementRef<typeof DialogPrimitive.Content>,
  ComponentPropsWithoutRef<typeof DialogPrimitive.Content> & DialogContentOwnProps
>(({ className, children, onPointerDownOutside, onInteractOutside, onFocusOutside, onEscapeKeyDown, hideCloseButton, ...props }, ref) => {
  const [portalContainer, setPortalContainer] = useState<HTMLElement | null>(null);
  const preventDismissWhenNestedOverlayActive = useNestedOverlayDismissGuard();

  const contentRef = useCallback(
    (node: HTMLDivElement | null) => {
      setPortalContainer(node);
      if (typeof ref === "function") {
        ref(node);
      } else if (ref) {
        ref.current = node;
      }
    },
    [ref],
  );

  return (
    <DialogPortal>
      <DialogOverlay />
      <DialogPrimitive.Content
        ref={contentRef}
        onPointerDownOutside={(event) => {
          preventDismissWhenNestedOverlayActive(event, event.target);
          onPointerDownOutside?.(event);
        }}
        onInteractOutside={(event) => {
          preventDismissWhenNestedOverlayActive(event, event.target);
          onInteractOutside?.(event);
        }}
        onFocusOutside={(event) => {
          preventDismissWhenNestedOverlayActive(event, event.target);
          onFocusOutside?.(event);
        }}
        onEscapeKeyDown={(event) => {
          if (isNestedOverlayOpen()) {
            event.preventDefault();
          }
          onEscapeKeyDown?.(event);
        }}
        className={cn(
          "fixed left-[50%] top-[50%] z-50 grid w-full max-w-lg translate-x-[-50%] translate-y-[-50%] gap-4 border border-border bg-surface-raised p-6 shadow-[var(--shadow-overlay)] duration-200 data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95 data-[state=closed]:slide-out-to-left-1/2 data-[state=closed]:slide-out-to-top-[48%] data-[state=open]:slide-in-from-left-1/2 data-[state=open]:slide-in-from-top-[48%] rounded-xl",
          className,
        )}
        {...props}
      >
        <DialogPortalContainerContext.Provider value={portalContainer}>
          {children}
        </DialogPortalContainerContext.Provider>
        {!hideCloseButton && (
          <DialogPrimitive.Close className="absolute right-4 top-4 rounded-sm opacity-70 ring-offset-background transition-opacity hover:opacity-100 focus:outline-none focus:ring-2 focus:ring-ring">
            <X className="h-4 w-4" />
            <span className="sr-only">Kapat</span>
          </DialogPrimitive.Close>
        )}
      </DialogPrimitive.Content>
    </DialogPortal>
  );
});
DialogContent.displayName = DialogPrimitive.Content.displayName;

function DialogHeader({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("flex flex-col space-y-1.5 text-center sm:text-left", className)} {...props} />;
}

function DialogFooter({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("flex flex-col-reverse sm:flex-row sm:justify-end sm:space-x-2", className)} {...props} />;
}

const DialogTitle = forwardRef<
  ElementRef<typeof DialogPrimitive.Title>,
  ComponentPropsWithoutRef<typeof DialogPrimitive.Title>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Title ref={ref} className={cn("text-title font-semibold leading-none tracking-tight", className)} {...props} />
));
DialogTitle.displayName = DialogPrimitive.Title.displayName;

const DialogDescription = forwardRef<
  ElementRef<typeof DialogPrimitive.Description>,
  ComponentPropsWithoutRef<typeof DialogPrimitive.Description>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Description ref={ref} className={cn("text-caption text-muted-foreground", className)} {...props} />
));
DialogDescription.displayName = DialogPrimitive.Description.displayName;

export {
  Dialog,
  DialogPortal,
  DialogOverlay,
  DialogClose,
  DialogTrigger,
  DialogContent,
  DialogHeader,
  DialogFooter,
  DialogTitle,
  DialogDescription,
};
