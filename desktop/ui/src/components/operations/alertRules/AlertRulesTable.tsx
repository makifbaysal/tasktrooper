import { TriangleAlert } from "lucide-react";
import { useMemo } from "react";
import { Link, useNavigate } from "react-router-dom";
import type { AlertRule } from "@/api";
import { AIBadge } from "@/components/operations/ai/AIBadge";
import { conditionText, formatThreshold, readFailures } from "@/components/operations/alertRules/alertRules";
import { AlertStatePill } from "@/components/operations/alertRules/AlertStatePill";
import { entityPath } from "@/components/operations/console/opsEntities";
import { IssueSeverity } from "@/components/operations/issues/IssueSeverity";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import type { OpsEntity } from "@/components/operations/shell/useOpsEntities";
import { Card } from "@/components/ui/card";
import { Switch } from "@/components/ui/switch";
import { useI18n } from "@/hooks/useI18n";
import { intlLocale } from "@/lib/languages";
import { cn, formatDate, formatRelativeTime } from "@/lib/utils";

const TH = "px-3 py-2 text-left text-micro font-medium tracking-wide text-muted-foreground uppercase whitespace-nowrap";
const TD = "px-3 py-2 align-middle";

interface AlertRulesTableProps {
  rules: AlertRule[];
  entityOf: (rule: AlertRule) => OpsEntity | undefined;
  unitOf: (rule: AlertRule) => string | undefined;
  detailHref: (rule: AlertRule) => string;
  onToggle: (rule: AlertRule, enabled: boolean) => void;
  /** Rules with a write in flight. */
  busy: ReadonlySet<string>;
  selectedId?: string | null;
  showEntity?: boolean;
}

/** New Relic's alert conditions: one row per rule, its state first, the
 * condition read back in words. A row opens the rule in the side panel. */
export function AlertRulesTable({ rules, entityOf, unitOf, detailHref, onToggle, busy, selectedId, showEntity = true }: AlertRulesTableProps) {
  const { t } = useI18n();
  const a = (key: string) => t(`operations.alertRules.${key}`);
  return (
    <Card className="overflow-hidden">
      <div className="overflow-x-auto">
        <table className="w-full min-w-[980px] border-collapse text-caption" aria-label={a("listLabel")}>
          <thead className="bg-muted/40">
            <tr className="border-b border-border">
              <th scope="col" className={TH}>{a("columns.state")}</th>
              <th scope="col" className={TH}>{a("columns.name")}</th>
              {showEntity && <th scope="col" className={TH}>{a("columns.entity")}</th>}
              <th scope="col" className={TH}>{a("columns.environment")}</th>
              <th scope="col" className={TH}>{a("columns.condition")}</th>
              <th scope="col" className={TH}>{a("columns.severity")}</th>
              <th scope="col" className={cn(TH, "text-right")}>{a("columns.lastValue")}</th>
              <th scope="col" className={cn(TH, "text-right")}>{a("columns.lastEvaluated")}</th>
              <th scope="col" className={cn(TH, "text-right")}>{a("columns.enabled")}</th>
            </tr>
          </thead>
          <tbody>
            {rules.map((rule) => (
              <RuleRow
                key={rule.id}
                rule={rule}
                entity={entityOf(rule)}
                unit={unitOf(rule)}
                href={detailHref(rule)}
                selected={rule.id === selectedId}
                showEntity={showEntity}
                busy={busy.has(rule.id)}
                onToggle={onToggle}
              />
            ))}
          </tbody>
        </table>
      </div>
    </Card>
  );
}

interface RuleRowProps {
  rule: AlertRule;
  entity?: OpsEntity;
  unit?: string;
  href: string;
  selected: boolean;
  showEntity: boolean;
  busy: boolean;
  onToggle: (rule: AlertRule, enabled: boolean) => void;
}

function RuleRow({ rule, entity, unit, href, selected, showEntity, busy, onToggle }: RuleRowProps) {
  const { t, lang } = useI18n();
  const navigate = useNavigate();
  const opsHref = useOpsHref();
  const a = (key: string, params?: Record<string, string | number>) => t(`operations.alertRules.${key}`, params);
  const ctx = useMemo(() => ({ locale: intlLocale(lang), t }), [lang, t]);
  const condition = conditionText(rule, unit, ctx);
  const value = rule.last_value;
  const evaluated = rule.last_evaluated_at;
  const failing = readFailures(rule);
  const failingText = failing ? a("readFailing", { count: failing.count, error: failing.error || "—" }) : "";

  return (
    <tr
      data-selected={selected || undefined}
      className={cn(
        "cursor-pointer border-b border-border/60 last:border-0 hover:bg-muted/40",
        selected && "bg-info/5 hover:bg-info/10",
        !rule.enabled && "text-muted-foreground",
      )}
      onClick={(e) => {
        if ((e.target as HTMLElement).closest("a, button, [role='switch']")) return;
        navigate(href);
      }}
    >
      <td className={TD}>
        <span className="inline-flex items-center gap-1.5">
          <AlertStatePill state={rule.state} disabled={!rule.enabled} />
          {failing && (
            <span title={failingText}>
              <TriangleAlert className="h-3.5 w-3.5 text-warning" aria-hidden />
              <span className="sr-only">{failingText}</span>
            </span>
          )}
        </span>
      </td>
      <td className={cn(TD, "max-w-64")}>
        <div className="flex min-w-0 items-center gap-1.5">
          <Link to={href} className="block min-w-0 truncate font-medium text-foreground hover:underline focus-visible:underline focus-visible:outline-none" title={rule.name}>
            {rule.name}
          </Link>
          {rule.created_by === "ai" && <AIBadge title={t("operations.aiJob.ruleTitle")} className="shrink-0" />}
        </div>
      </td>
      {showEntity && (
        <td className={cn(TD, "max-w-48")}>
          {entity ? (
            <Link to={opsHref(entityPath(entity.id, "alerts"))} className="block truncate hover:underline">
              {entity.name}
            </Link>
          ) : (
            <span className="block truncate font-mono text-muted-foreground" title={rule.component_id}>
              {rule.component_id.slice(0, 8)}
            </span>
          )}
        </td>
      )}
      <td className={cn(TD, "whitespace-nowrap")}>{t(`cloud.environments.${rule.environment}`)}</td>
      <td className={cn(TD, "max-w-80")}>
        <span className="block truncate" title={condition}>
          {condition}
        </span>
      </td>
      <td className={TD}>
        <IssueSeverity severity={rule.severity} />
      </td>
      <td className={cn(TD, "text-right font-mono whitespace-nowrap tabular-nums")}>
        {value == null ? <span className="text-muted-foreground">—</span> : formatThreshold(rule.condition.op, value, unit, ctx)}
      </td>
      <td className={cn(TD, "text-right whitespace-nowrap")} title={evaluated ? formatDate(evaluated) : undefined}>
        {evaluated ? formatRelativeTime(evaluated, lang) : <span className="text-muted-foreground">{a("neverEvaluated")}</span>}
      </td>
      <td className={cn(TD, "text-right")}>
        <Switch
          checked={rule.enabled}
          disabled={busy}
          onCheckedChange={(checked) => onToggle(rule, checked)}
          aria-label={a("enabledLabel", { name: rule.name })}
        />
      </td>
    </tr>
  );
}
