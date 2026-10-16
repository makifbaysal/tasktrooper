import { ChevronDown, ChevronRight, Copy, ExternalLink, ListPlus } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router-dom";
import type { OpsErrorGroup } from "@/api";
import { groupKey } from "@/components/operations/errors/errorGroups";
import { SeenLane } from "@/components/operations/errors/SeenLane";
import type { EnvironmentRef } from "@/components/operations/query/opsQuery";
import type { OpsEntity } from "@/components/operations/shell/useOpsEntities";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Spinner } from "@/components/ui/spinner";
import { useI18n } from "@/hooks/useI18n";
import { cn, formatDate, formatRelativeTime } from "@/lib/utils";

const TH = "px-3 py-2 text-left text-micro font-medium tracking-wide text-muted-foreground uppercase whitespace-nowrap";
const TD = "px-3 py-2 align-top";

interface ErrorGroupTableProps {
  groups: OpsErrorGroup[];
  environments: Map<string, EnvironmentRef>;
  entities: Map<string, OpsEntity>;
  window: { from: number; to: number };
  selectedKey?: string;
  selectHref: (group: OpsErrorGroup) => string;
  entityHref?: (entity: OpsEntity) => string;
  showEntity: boolean;
  creatingKey?: string;
  onCreateTask: (group: OpsErrorGroup) => void;
  onCopyFingerprint: (group: OpsErrorGroup) => void;
}

/** The inbox's dense list: one row per error group, newest problems first. */
export function ErrorGroupTable({
  groups,
  environments,
  entities,
  window,
  selectedKey,
  selectHref,
  entityHref,
  showEntity,
  creatingKey,
  onCreateTask,
  onCopyFingerprint,
}: ErrorGroupTableProps) {
  const { t, lang } = useI18n();
  const e = (key: string, params?: Record<string, string | number>) => t(`operations.errorsInbox.${key}`, params);
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set());
  const number = new Intl.NumberFormat(lang);

  const toggle = (key: string) =>
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });

  return (
    <Card className="overflow-hidden">
      <div className="overflow-x-auto">
        <table className="w-full min-w-[760px] border-collapse text-caption" aria-label={e("listLabel")}>
          <thead className="bg-muted/40">
            <tr className="border-b border-border">
              <th scope="col" className={cn(TH, "w-16")}>
                <span className="sr-only">{e("columns.status")}</span>
              </th>
              <th scope="col" className={TH}>{e("columns.error")}</th>
              {showEntity && <th scope="col" className={TH}>{e("columns.entity")}</th>}
              <th scope="col" className={cn(TH, "text-right")}>{e("columns.occurrences")}</th>
              <th scope="col" className={TH}>{e("columns.lastSeen")}</th>
              <th scope="col" className={TH}>{e("columns.firstSeen")}</th>
              <th scope="col" className={cn(TH, "w-28")}>
                <span className="sr-only">{e("columns.actions")}</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {groups.map((g) => {
              const key = groupKey(g);
              const open = expanded.has(key);
              const selected = key === selectedKey;
              const ref = environments.get(g.environment_id);
              const entity = ref?.entity ?? entities.get(g.component_id);
              const provider = t(`cloud.providers.${g.provider}`);
              const message = g.message || e("noMessage");
              return (
                <tr
                  key={key}
                  data-state={selected ? "selected" : undefined}
                  className={cn("border-b border-border/60 last:border-0 hover:bg-muted/30", selected && "bg-info/5 hover:bg-info/10")}
                >
                  <td className={TD}>
                    {g.new && (
                      <Badge variant="warning" className="rounded-[4px] px-1.5 py-0 text-micro" title={e("newTitle")}>
                        {e("newBadge")}
                        <span className="sr-only">{`: ${e("newTitle")}`}</span>
                      </Badge>
                    )}
                  </td>
                  <td className={cn(TD, "max-w-0 w-full")}>
                    <div className="flex min-w-0 items-start gap-1">
                      <Button
                        variant="ghost"
                        size="icon"
                        className="-ml-1 h-5 w-5 shrink-0 [&_svg]:size-3.5"
                        aria-expanded={open}
                        aria-label={open ? e("collapse") : e("expand")}
                        onClick={() => toggle(key)}
                      >
                        {open ? <ChevronDown aria-hidden /> : <ChevronRight aria-hidden />}
                      </Button>
                      <div className="min-w-0 flex-1">
                        <Link
                          to={selectHref(g)}
                          aria-current={selected ? "true" : undefined}
                          className={cn(
                            "block font-mono text-caption text-foreground hover:underline focus-visible:underline",
                            open ? "whitespace-pre-wrap break-words" : "truncate",
                          )}
                          title={open ? undefined : g.message}
                        >
                          {message}
                        </Link>
                        {g.source && <p className="truncate font-mono text-micro text-muted-foreground">{g.source}</p>}
                      </div>
                    </div>
                  </td>
                  {showEntity && (
                    <td className={cn(TD, "max-w-56")}>
                      {entity ? (
                        entityHref ? (
                          <Link to={entityHref(entity)} className="block truncate font-medium hover:underline">
                            {entity.name}
                          </Link>
                        ) : (
                          <span className="block truncate font-medium">{entity.name}</span>
                        )
                      ) : (
                        <span className="text-muted-foreground">{t("operations.opsQuery.unavailable.unknownEntity")}</span>
                      )}
                      <span className="block truncate font-mono text-micro text-muted-foreground" title={ref?.resource}>
                        {ref?.resource ?? provider}
                      </span>
                    </td>
                  )}
                  <td className={cn(TD, "whitespace-nowrap")}>
                    <div className="flex items-center justify-end gap-2">
                      <SeenLane
                        from={window.from}
                        to={window.to}
                        firstSeen={g.first_seen}
                        lastSeen={g.last_seen}
                        label={e("seenLane", { first: formatDate(g.first_seen), last: formatDate(g.last_seen) })}
                      />
                      <span className="min-w-10 text-right font-mono font-semibold tabular-nums">{number.format(g.count)}</span>
                    </div>
                  </td>
                  <td className={cn(TD, "whitespace-nowrap font-mono tabular-nums")} title={formatDate(g.last_seen)}>
                    {formatRelativeTime(g.last_seen, lang)}
                  </td>
                  <td className={cn(TD, "whitespace-nowrap font-mono tabular-nums text-muted-foreground")} title={formatDate(g.first_seen)}>
                    {formatRelativeTime(g.first_seen, lang)}
                  </td>
                  <td className={cn(TD, "py-1.5")}>
                    <div className="flex items-center justify-end gap-0.5">
                      {g.external_url && (
                        <Button variant="ghost" size="icon" className="h-7 w-7" asChild>
                          <a
                            href={g.external_url}
                            target="_blank"
                            rel="noreferrer"
                            aria-label={e("openInProvider", { provider })}
                            title={e("openInProvider", { provider })}
                          >
                            <ExternalLink aria-hidden />
                          </a>
                        </Button>
                      )}
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-7 w-7"
                        aria-label={e("copyFingerprint")}
                        title={e("copyFingerprint")}
                        onClick={() => onCopyFingerprint(g)}
                      >
                        <Copy aria-hidden />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-7 w-7"
                        aria-label={e("createTask")}
                        title={e("createTask")}
                        disabled={creatingKey !== undefined}
                        onClick={() => onCreateTask(g)}
                      >
                        {creatingKey === key ? <Spinner size="sm" /> : <ListPlus aria-hidden />}
                      </Button>
                    </div>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </Card>
  );
}
