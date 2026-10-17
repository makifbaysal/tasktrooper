import { Check, CircleCheck, CircleDashed, Link2, Pencil, Sparkles, Trash2, X, type LucideIcon } from "lucide-react";
import { Fragment } from "react";
import type { ComponentSignal, ComponentSignalStatus, MetricPoint, SignalBinding } from "@/api";
import { AIBadge } from "@/components/operations/ai/AIBadge";
import { Sparkline } from "@/components/operations/charts/Sparkline";
import { signalLabel } from "@/components/operations/console/signalFormat";
import { suggestionNeedsEditor } from "@/components/operations/signals/signalBinding";
import { Badge, type BadgeProps } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

const STATUS_META: Record<ComponentSignalStatus, { variant: NonNullable<BadgeProps["variant"]>; icon: LucideIcon }> = {
  bound: { variant: "success", icon: CircleCheck },
  suggested: { variant: "info", icon: Sparkles },
  unbound: { variant: "outline", icon: CircleDashed },
};

export function SignalStatusPill({ status }: { status: ComponentSignalStatus }) {
  const { t } = useI18n();
  const meta = STATUS_META[status] ?? STATUS_META.unbound;
  const Icon = meta.icon;
  return (
    <Badge variant={meta.variant} className="gap-1 whitespace-nowrap">
      <Icon className="h-3 w-3 shrink-0" aria-hidden />
      {t(`operations.signalsPage.status.${STATUS_META[status] ? status : "unbound"}`)}
    </Badge>
  );
}

function ParamChips({ params }: { params: Record<string, string> | undefined }) {
  const entries = Object.entries(params ?? {}).sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0));
  if (entries.length === 0) return <span className="text-muted-foreground">—</span>;
  return (
    <ul className="flex flex-wrap gap-1">
      {entries.map(([k, v]) => (
        <li key={k}>
          <Badge variant="outline" className="max-w-56 truncate font-mono font-normal" title={`${k}=${v}`}>
            {k}={v}
          </Badge>
        </li>
      ))}
    </ul>
  );
}

function RecipeOrQuery({ binding }: { binding: Pick<SignalBinding, "recipe_id" | "query"> }) {
  const { t } = useI18n();
  if (binding.recipe_id) return <span className="font-mono">{binding.recipe_id}</span>;
  return (
    <span className="block min-w-0">
      <span>{t("operations.signalsPage.customQuery")}</span>
      {binding.query && (
        <span className="block max-w-64 truncate font-mono text-micro text-muted-foreground" title={binding.query}>
          {binding.query}
        </span>
      )}
    </span>
  );
}

interface SignalsTableProps {
  signals: ComponentSignal[];
  sparks: Map<string, MetricPoint[]>;
  /** Signal ids and suggestion ids with a write in flight. */
  busy: ReadonlySet<string>;
  onBind: (signal: ComponentSignal) => void;
  onRemove: (signal: ComponentSignal) => void;
  onAccept: (signal: ComponentSignal, suggestion: SignalBinding) => void;
  onDismiss: (signal: ComponentSignal, suggestion: SignalBinding) => void;
}

const TH = "px-3 py-2 text-left text-micro font-medium tracking-wide text-muted-foreground uppercase whitespace-nowrap";
const TD = "px-3 py-2.5 align-top";

/** A component's signals in one environment: what feeds each one, its last
 * window as a sparkline, and the suggestions under it awaiting a yes or no. */
export function SignalsTable({ signals, sparks, busy, onBind, onRemove, onAccept, onDismiss }: SignalsTableProps) {
  const { t } = useI18n();
  const p = (key: string, params?: Record<string, string | number>) => t(`operations.signalsPage.${key}`, params);
  return (
    <Card className="overflow-hidden">
      <div className="overflow-x-auto">
        <table className="w-full min-w-[860px] border-collapse text-caption" aria-label={p("listLabel")}>
          <thead className="bg-muted/40">
            <tr className="border-b border-border">
              <th scope="col" className={TH}>{p("columns.signal")}</th>
              <th scope="col" className={TH}>{p("columns.status")}</th>
              <th scope="col" className={TH}>{p("columns.source")}</th>
              <th scope="col" className={TH}>{p("columns.recipe")}</th>
              <th scope="col" className={TH}>{p("columns.params")}</th>
              <th scope="col" className={TH}>{p("columns.preview")}</th>
              <th scope="col" className={cn(TH, "text-right")}>
                <span className="sr-only">{p("columns.actions")}</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {signals.map((s) => {
              const name = signalLabel(t, s.signal);
              const binding = s.binding;
              const points = sparks.get(s.signal) ?? [];
              const working = busy.has(s.signal);
              return (
                <Fragment key={s.signal}>
                  <tr className={cn("border-b border-border/60", (s.suggestions ?? []).length > 0 && "border-b-0")}>
                    <td className={cn(TD, "min-w-44")}>
                      <div className="flex flex-wrap items-center gap-1.5">
                        <span className="text-[13px] font-medium">{name}</span>
                        {s.golden && <Badge variant="secondary">{p("golden")}</Badge>}
                      </div>
                      <p className="font-mono text-micro text-muted-foreground">{s.signal}</p>
                    </td>
                    <td className={TD}>
                      <SignalStatusPill status={s.status} />
                    </td>
                    <td className={TD}>
                      {binding ? (
                        <>
                          <span className="font-medium">{binding.data_source_name}</span>
                          <p className="flex items-center gap-1.5 text-micro text-muted-foreground">
                            {p(`origin.${binding.source}`)}
                            {binding.source === "ai" && <AIBadge title={t("operations.aiJob.bindingTitle")} />}
                          </p>
                        </>
                      ) : (
                        <span className="text-muted-foreground">—</span>
                      )}
                    </td>
                    <td className={TD}>{binding ? <RecipeOrQuery binding={binding} /> : <span className="text-muted-foreground">—</span>}</td>
                    <td className={TD}>{binding ? <ParamChips params={binding.params} /> : <span className="text-muted-foreground">—</span>}</td>
                    <td className={TD}>
                      {binding && points.length > 0 ? (
                        <Sparkline points={points} ariaLabel={p("previewLabel", { signal: name })} />
                      ) : (
                        <span className="text-muted-foreground" title={binding ? p("noPreview") : t("operations.console.noMetricsSource")}>
                          —
                        </span>
                      )}
                    </td>
                    <td className={cn(TD, "whitespace-nowrap text-right")}>
                      {binding ? (
                        <div className="inline-flex gap-1">
                          <Button variant="ghost" size="sm" onClick={() => onBind(s)} disabled={working} aria-label={p("editLabel", { signal: name })}>
                            <Pencil aria-hidden />
                            {p("edit")}
                          </Button>
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => onRemove(s)}
                            disabled={working}
                            aria-label={p("removeLabel", { signal: name })}
                          >
                            <Trash2 aria-hidden />
                            {p("remove")}
                          </Button>
                        </div>
                      ) : (
                        <Button variant="outline" size="sm" onClick={() => onBind(s)} disabled={working} aria-label={p("bindLabel", { signal: name })}>
                          <Link2 aria-hidden />
                          {p("bind")}
                        </Button>
                      )}
                    </td>
                  </tr>
                  {(s.suggestions ?? []).map((sug, i, all) => (
                    <tr key={sug.id} className={cn("bg-info/5", i === all.length - 1 && "border-b border-border/60")}>
                      <td className={cn(TD, "pl-6")}>
                        {sug.source === "ai" ? (
                          <span className="inline-flex items-center gap-1.5 text-micro font-medium text-info">
                            <AIBadge title={t("operations.aiJob.bindingTitle")} />
                            {p("suggestion.fromAgent")}
                          </span>
                        ) : (
                          <span className="inline-flex items-center gap-1.5 text-micro font-medium text-info">
                            <Sparkles className="h-3 w-3" aria-hidden />
                            {p("suggestion.detected")}
                          </span>
                        )}
                      </td>
                      <td className={TD} />
                      <td className={TD}>
                        <span className="font-medium">{sug.data_source_name}</span>
                      </td>
                      <td className={TD}>
                        <RecipeOrQuery binding={sug} />
                      </td>
                      <td className={TD}>
                        <ParamChips params={sug.params} />
                        {(sug.needs_params ?? []).length > 0 && (
                          <p className="mt-1 text-micro text-warning">
                            {p("suggestion.needsParams", { params: sug.needs_params.join(", ") })}
                          </p>
                        )}
                      </td>
                      <td className={TD} />
                      <td className={cn(TD, "whitespace-nowrap text-right")}>
                        <div className="inline-flex gap-1">
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => onAccept(s, sug)}
                            disabled={busy.has(sug.id)}
                            aria-label={p(suggestionNeedsEditor(sug) ? "suggestion.reviewLabel" : "suggestion.acceptLabel", {
                              signal: name,
                              source: sug.data_source_name,
                            })}
                          >
                            <Check aria-hidden />
                            {p(suggestionNeedsEditor(sug) ? "suggestion.review" : "suggestion.accept")}
                          </Button>
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => onDismiss(s, sug)}
                            disabled={busy.has(sug.id)}
                            aria-label={p("suggestion.dismissLabel", { signal: name, source: sug.data_source_name })}
                          >
                            <X aria-hidden />
                            {p("suggestion.dismiss")}
                          </Button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </Fragment>
              );
            })}
          </tbody>
        </table>
      </div>
    </Card>
  );
}
