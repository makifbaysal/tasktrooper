import { Check, Siren, X } from "lucide-react";
import { useMemo } from "react";
import { Link, useNavigate } from "react-router-dom";
import type { ErrorDefinition } from "@/api";
import { ActionLabel, CategoryPill, SourceBadge } from "@/components/operations/errorDefinitions/DefinitionBadges";
import { matchSummary, thresholdText } from "@/components/operations/errorDefinitions/errorDefinitions";
import { IssueSeverity } from "@/components/operations/issues/IssueSeverity";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { useI18n } from "@/hooks/useI18n";
import { intlLocale } from "@/lib/languages";
import { cn, formatDate, formatRelativeTime } from "@/lib/utils";

const TH = "px-3 py-2 text-left text-micro font-medium tracking-wide text-muted-foreground uppercase whitespace-nowrap";
const TD = "px-3 py-2 align-middle";

interface ErrorDefinitionsTableProps {
  definitions: ErrorDefinition[];
  /** "every component" or the component's name, with the repository when the list spans several. */
  scopeOf: (def: ErrorDefinition) => string;
  detailHref: (def: ErrorDefinition) => string;
  selectedId?: string | null;
  /** Suggestions: Accept and Dismiss on each row. */
  reviewing?: boolean;
  busy: ReadonlySet<string>;
  onAccept: (def: ErrorDefinition) => void;
  onDismiss: (def: ErrorDefinition) => void;
}

/** The dense list of definitions: name, where it applies, its match in
 * words, what it means and what it sets off. A row opens the side panel. */
export function ErrorDefinitionsTable({
  definitions,
  scopeOf,
  detailHref,
  selectedId,
  reviewing = false,
  busy,
  onAccept,
  onDismiss,
}: ErrorDefinitionsTableProps) {
  const { t } = useI18n();
  const c = (key: string) => t(`operations.errorDefinitions.columns.${key}`);
  return (
    <Card className="overflow-hidden">
      <div className="overflow-x-auto">
        <table className="w-full min-w-[1080px] border-collapse text-caption" aria-label={t("operations.errorDefinitions.listLabel")}>
          <thead className="bg-muted/40">
            <tr className="border-b border-border">
              <th scope="col" className={TH}>{c("name")}</th>
              <th scope="col" className={TH}>{c("appliesTo")}</th>
              <th scope="col" className={TH}>{c("match")}</th>
              <th scope="col" className={TH}>{c("category")}</th>
              <th scope="col" className={TH}>{c("severity")}</th>
              <th scope="col" className={TH}>{c("action")}</th>
              <th scope="col" className={TH}>{c("threshold")}</th>
              <th scope="col" className={TH}>{c("source")}</th>
              <th scope="col" className={TH}>{c("firing")}</th>
              {reviewing && (
                <th scope="col" className={cn(TH, "text-right")}>
                  {c("review")}
                </th>
              )}
            </tr>
          </thead>
          <tbody>
            {definitions.map((def) => (
              <DefinitionRow
                key={def.id}
                def={def}
                scope={scopeOf(def)}
                href={detailHref(def)}
                selected={def.id === selectedId}
                reviewing={reviewing}
                busy={busy.has(def.id)}
                onAccept={onAccept}
                onDismiss={onDismiss}
              />
            ))}
          </tbody>
        </table>
      </div>
    </Card>
  );
}

interface DefinitionRowProps {
  def: ErrorDefinition;
  scope: string;
  href: string;
  selected: boolean;
  reviewing: boolean;
  busy: boolean;
  onAccept: (def: ErrorDefinition) => void;
  onDismiss: (def: ErrorDefinition) => void;
}

function DefinitionRow({ def, scope, href, selected, reviewing, busy, onAccept, onDismiss }: DefinitionRowProps) {
  const { t, lang } = useI18n();
  const navigate = useNavigate();
  const d = (key: string, params?: Record<string, string | number>) => t(`operations.errorDefinitions.${key}`, params);
  const ctx = useMemo(() => ({ locale: intlLocale(lang), t }), [lang, t]);
  const match = matchSummary(def.match, ctx);
  const threshold = thresholdText(def, ctx);

  return (
    <tr
      data-selected={selected || undefined}
      className={cn("cursor-pointer border-b border-border/60 last:border-0 hover:bg-muted/40", selected && "bg-info/5 hover:bg-info/10")}
      onClick={(e) => {
        if ((e.target as HTMLElement).closest("a, button")) return;
        navigate(href);
      }}
    >
      <td className={cn(TD, "max-w-64")}>
        <Link to={href} className="block truncate font-medium text-foreground hover:underline focus-visible:underline focus-visible:outline-none" title={def.name}>
          {def.name}
        </Link>
      </td>
      <td className={cn(TD, "max-w-44")}>
        <span className="block truncate text-muted-foreground" title={scope}>
          {scope}
        </span>
      </td>
      <td className={cn(TD, "max-w-96")}>
        <span className="block truncate font-mono text-micro" title={match}>
          {match}
        </span>
      </td>
      <td className={TD}>
        <CategoryPill category={def.category} />
      </td>
      <td className={TD}>
        <IssueSeverity severity={def.severity} />
      </td>
      <td className={TD}>
        <ActionLabel action={def.action} />
      </td>
      <td className={cn(TD, "whitespace-nowrap tabular-nums")}>{threshold ?? <span className="text-muted-foreground">—</span>}</td>
      <td className={TD}>
        <SourceBadge source={def.source} />
      </td>
      <td className={cn(TD, "whitespace-nowrap")} title={def.firing_since ? formatDate(def.firing_since) : undefined}>
        {def.firing_since ? (
          <span className="inline-flex items-center gap-1 font-medium text-destructive">
            <Siren className="h-3.5 w-3.5" aria-hidden />
            {formatRelativeTime(def.firing_since, lang)}
          </span>
        ) : (
          <span className="text-muted-foreground">—</span>
        )}
      </td>
      {reviewing && (
        <td className={cn(TD, "text-right whitespace-nowrap")}>
          <div className="inline-flex items-center gap-1">
            <Button size="sm" variant="outline" className="h-7" disabled={busy} onClick={() => onAccept(def)} aria-label={d("acceptLabel", { name: def.name })}>
              <Check aria-hidden />
              {d("accept")}
            </Button>
            <Button size="sm" variant="ghost" className="h-7" disabled={busy} onClick={() => onDismiss(def)} aria-label={d("dismissLabel", { name: def.name })}>
              <X aria-hidden />
              {d("dismiss")}
            </Button>
          </div>
        </td>
      )}
    </tr>
  );
}
