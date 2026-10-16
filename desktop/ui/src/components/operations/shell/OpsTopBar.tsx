import { ChevronRight, Info, RefreshCw } from "lucide-react";
import { useId } from "react";
import { Link } from "react-router-dom";
import { OPS_ENVIRONMENTS, useOpsContext, useOpsShellState } from "@/components/operations/shell/OpsContext";
import { OpsTimePicker } from "@/components/operations/shell/OpsTimePicker";
import { Button } from "@/components/ui/button";
import { Elapsed } from "@/components/ui/elapsed";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { useI18n } from "@/hooks/useI18n";
import { formatRelativeTime } from "@/lib/utils";

export interface OpsCrumb {
  label: string;
  to?: string;
}

interface OpsTopBarProps {
  crumbs: OpsCrumb[];
  title?: string;
}

/** The console's global bar: where you are on the left; the environment, the
 * time window, compare and refresh on the right, each shown only where the
 * page shown reads it. */
export function OpsTopBar({ crumbs, title }: OpsTopBarProps) {
  const { t, lang } = useI18n();
  const { env, setEnv, compare, setCompare, refresh } = useOpsContext();
  const { refreshedAt, scope } = useOpsShellState();
  const compareId = useId();

  return (
    <header className="flex shrink-0 flex-wrap items-center gap-x-4 gap-y-2 border-b border-border bg-card/60 px-4 py-2.5 lg:px-6">
      <div className="min-w-0 flex-[1_1_14rem]">
        {crumbs.length > 0 && (
          <nav aria-label={t("operations.shell.breadcrumb")}>
            <ol className="flex min-w-0 items-center gap-1 text-caption text-muted-foreground">
              {crumbs.map((crumb, i) => (
                <li key={`${i}:${crumb.label}`} className="flex min-w-0 items-center gap-1">
                  {i > 0 && <ChevronRight className="h-3 w-3 shrink-0" aria-hidden />}
                  {crumb.to ? (
                    <Link to={crumb.to} className="truncate hover:text-foreground hover:underline">
                      {crumb.label}
                    </Link>
                  ) : (
                    <span className="truncate text-foreground" aria-current={title ? undefined : "page"}>
                      {crumb.label}
                    </span>
                  )}
                </li>
              ))}
            </ol>
          </nav>
        )}
        {title && <h1 className="truncate text-heading font-semibold tracking-tight">{title}</h1>}
      </div>

      <div className="ml-auto flex flex-wrap items-center justify-end gap-2">
        {scope.env && (
          <Select value={env} onValueChange={(v) => setEnv(v as typeof env)}>
            <SelectTrigger className="h-8 w-40 text-caption" aria-label={t("operations.shell.environment")}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {OPS_ENVIRONMENTS.map((e) => (
                <SelectItem key={e} value={e}>
                  {t(`cloud.environments.${e}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
        {scope.time ? (
          <OpsTimePicker />
        ) : (
          scope.note && (
            <span className="inline-flex items-center gap-1.5 rounded-md border border-dashed border-border px-2.5 py-1 text-caption text-muted-foreground">
              <Info className="h-3.5 w-3.5 shrink-0" aria-hidden />
              {scope.note}
            </span>
          )
        )}
        {scope.time && scope.compare && (
          <div className="flex items-center gap-2 px-1.5">
            <Switch id={compareId} checked={compare} onCheckedChange={setCompare} />
            <Label htmlFor={compareId} className="cursor-pointer text-caption font-normal text-muted-foreground">
              {t("operations.shell.compare")}
            </Label>
          </div>
        )}
        <div className="flex items-center gap-1">
          <Button variant="ghost" size="sm" onClick={refresh} className="gap-1.5 px-2">
            <RefreshCw aria-hidden />
            {t("operations.shell.refresh")}
          </Button>
          <Elapsed
            since={refreshedAt}
            className="hidden whitespace-nowrap text-micro text-muted-foreground tabular-nums sm:inline"
            format={(ms) => t("operations.shell.updated", { time: formatRelativeTime(new Date(Date.now() - ms).toISOString(), lang) })}
          />
        </div>
      </div>
    </header>
  );
}
