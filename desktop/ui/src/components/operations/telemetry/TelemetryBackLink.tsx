import { ArrowLeft } from "lucide-react";
import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import { Button } from "@/components/ui/button";

/** The row above a telemetry detail page: back to its list, then the page's own links. */
export function TelemetryBackLink({ to, label, children }: { to: string; label: string; children?: ReactNode }) {
  const href = useOpsHref();
  return (
    <div className="flex flex-wrap items-center gap-2">
      <Button asChild variant="ghost" size="sm">
        <Link to={href(to)}>
          <ArrowLeft aria-hidden />
          {label}
        </Link>
      </Button>
      {children && <div className="ml-auto flex flex-wrap items-center gap-2">{children}</div>}
    </div>
  );
}
