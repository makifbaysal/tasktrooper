import { Suspense, type ReactNode } from "react";
import { Spinner } from "@/components/ui/spinner";

// Pages are code-split (App.tsx); each layout wraps its <Outlet /> in one of
// these so a page's chunk loading never blanks the layout around it. A chunk
// that fails to load throws to the top-level ErrorBoundary like any render error.
export function PageSuspense({ children }: { children: ReactNode }) {
  return (
    <Suspense
      fallback={
        <div className="flex min-h-[12rem] flex-1 items-center justify-center">
          <Spinner size="lg" />
        </div>
      }
    >
      {children}
    </Suspense>
  );
}
