import { BellRing, Plus, Search, SearchX, Sparkles } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useLocation, useSearchParams } from "react-router-dom";
import { toast } from "sonner";
import { api, type AlertRule } from "@/api";
import {
  filterRules,
  isAlertStateFilter,
  removeRule,
  sortRules,
  stateCounts,
  upsertRule,
  type AlertStateFilter,
} from "@/components/operations/alertRules/alertRules";
import { AlertRuleDetailPanel } from "@/components/operations/alertRules/AlertRuleDetailPanel";
import { AlertRuleEditor } from "@/components/operations/alertRules/AlertRuleEditor";
import { AlertRulesTable } from "@/components/operations/alertRules/AlertRulesTable";
import { AlertStateStrip } from "@/components/operations/alertRules/AlertStateStrip";
import { useOpsContext } from "@/components/operations/shell/OpsContext";
import { useOpsEntities, type OpsEntity } from "@/components/operations/shell/useOpsEntities";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { Input } from "@/components/ui/input";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useRecipeCatalog } from "@/hooks/useDataSources";
import { useI18n } from "@/hooks/useI18n";
import { useAlertRule, useAlertRules } from "@/hooks/useOpsAlerts";

interface AlertsViewProps {
  /** Scopes the rules to one entity (its Alerts sub-page, or the page's filter). */
  entity?: OpsEntity;
  showEntity?: boolean;
  /** Filters the page puts first in the toolbar (the entity picker). */
  toolbarStart?: ReactNode;
}

type EditorState = { kind: "new" } | { kind: "suggest" } | { kind: "edit"; rule: AlertRule };

/** New Relic's alert conditions: the rules in the console's environment, their
 * states as filters, the rule in a side panel (`?rule=`), and the editor. */
export function AlertsView({ entity, showEntity = true, toolbarStart }: AlertsViewProps) {
  const { t } = useI18n();
  const a = (key: string, params?: Record<string, string | number>) => t(`operations.alertRules.${key}`, params);
  const { env, setEnv } = useOpsContext();
  const [params, setParams] = useSearchParams();
  const { pathname } = useLocation();
  const { entities } = useOpsEntities();
  const catalog = useRecipeCatalog();
  const rules = useAlertRules(entity?.id);
  const [editor, setEditor] = useState<EditorState | null>(null);
  const [busy, setBusy] = useState<ReadonlySet<string>>(() => new Set());

  const stateParam = params.get("state");
  const state: AlertStateFilter = isAlertStateFilter(stateParam) ? stateParam : "all";
  const query = params.get("q") ?? "";
  const selectedId = params.get("rule");
  const envLabel = t(`cloud.environments.${env}`);

  const setParam = useCallback(
    (key: string, value: string | null) =>
      setParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (value === null || value === "" || value === "all") next.delete(key);
          else next.set(key, value);
          return next;
        },
        { replace: true },
      ),
    [setParams],
  );

  const entitiesById = useMemo(() => {
    const map = new Map(entities.map((e) => [e.id, e]));
    if (entity) map.set(entity.id, entity);
    return map;
  }, [entities, entity]);
  const entityOf = useCallback((r: AlertRule) => entitiesById.get(r.component_id), [entitiesById]);
  const units = useMemo(() => new Map(catalog.signals.map((s) => [s.id, s.unit])), [catalog.signals]);
  const unitOf = useCallback((r: AlertRule) => (r.signal ? units.get(r.signal) : undefined), [units]);

  const all = useMemo(() => rules.data ?? [], [rules.data]);
  const inEnv = useMemo(() => all.filter((r) => r.environment === env), [all, env]);
  const visible = useMemo(
    () => sortRules(filterRules(inEnv, state, query, (r) => [entityOf(r)?.name, entityOf(r)?.repositoryName])),
    [inEnv, state, query, entityOf],
  );
  const counts = useMemo(() => stateCounts(inEnv), [inEnv]);
  const listed = selectedId ? all.find((r) => r.id === selectedId) : undefined;
  const listRead = rules.data !== null;
  const single = useAlertRule(selectedId, listRead && !listed);
  const selected = listed ?? (single.data?.id === selectedId ? single.data : undefined);

  // A notification's `?rule=` link names no environment: the console follows
  // the rule there once, so it is listed behind its panel.
  const followed = useRef<string | null>(null);
  useEffect(() => {
    if (!selected || followed.current === selected.id) return;
    followed.current = selected.id;
    if (selected.environment !== env) setEnv(selected.environment);
  }, [selected, env, setEnv]);

  const detailHref = useCallback(
    (r: AlertRule) => {
      const next = new URLSearchParams(params);
      next.set("rule", r.id);
      return `${pathname}?${next.toString()}`;
    },
    [params, pathname],
  );

  const mark = (id: string, on: boolean) =>
    setBusy((prev) => {
      const next = new Set(prev);
      if (on) next.add(id);
      else next.delete(id);
      return next;
    });

  const toggle = async (rule: AlertRule, enabled: boolean) => {
    mark(rule.id, true);
    rules.update((prev) => (prev ? upsertRule(prev, { ...rule, enabled }) : prev));
    try {
      const saved = await api.updateAlertRule(rule.id, { enabled });
      rules.update((prev) => (prev ? upsertRule(prev, saved) : prev));
    } catch (err) {
      rules.update((prev) => (prev ? upsertRule(prev, rule) : prev));
      toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
    } finally {
      mark(rule.id, false);
    }
  };

  // The list shows the console's environment only: a rule saved into another
  // one takes the console there, so it is not saved out of sight.
  const saved = (rule: AlertRule) => {
    rules.update((prev) => upsertRule(prev ?? [], rule));
    if (rule.environment !== env) {
      setEnv(rule.environment);
      toast.success(a("savedSwitched", { name: rule.name, env: t(`cloud.environments.${rule.environment}`) }));
      return;
    }
    toast.success(a("saved", { name: rule.name }));
  };

  const deleted = (id: string) => {
    rules.update((prev) => (prev ? removeRule(prev, id) : prev));
    setParam("rule", null);
  };

  const clearFilters = () =>
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        next.delete("state");
        next.delete("q");
        return next;
      },
      { replace: true },
    );

  const newRule = (
    <Button size="sm" onClick={() => setEditor({ kind: "new" })}>
      <Plus aria-hidden />
      {a("newRule")}
    </Button>
  );
  const suggestRules = entity ? (
    <Button size="sm" variant="outline" onClick={() => setEditor({ kind: "suggest" })}>
      <Sparkles aria-hidden />
      {a("suggestRules")}
    </Button>
  ) : null;

  let body: ReactNode;
  if (rules.loading && all.length === 0) {
    body = <Skeleton className="h-72 w-full" />;
  } else if (rules.error && !rules.data) {
    body = (
      <Card>
        <EmptyState
          icon={BellRing}
          variant="critical"
          title={a("loadFailed")}
          description={rules.error}
          action={
            <Button variant="outline" size="sm" onClick={() => void rules.reload()}>
              {t("operations.chart.retry")}
            </Button>
          }
        />
      </Card>
    );
  } else if (inEnv.length === 0) {
    body = (
      <Card>
        <EmptyState
          icon={BellRing}
          title={a("empty.title", { env: envLabel })}
          description={entity ? a("empty.entityDescription", { name: entity.name, env: envLabel }) : a("empty.description", { env: envLabel })}
          action={
            <div className="flex flex-wrap justify-center gap-2">
              {suggestRules}
              {newRule}
            </div>
          }
        />
      </Card>
    );
  } else if (visible.length === 0) {
    body = (
      <Card>
        <EmptyState
          icon={SearchX}
          variant="search"
          title={a("noMatch.title")}
          action={
            <Button variant="outline" size="sm" onClick={clearFilters}>
              {a("noMatch.action")}
            </Button>
          }
        />
      </Card>
    );
  } else {
    body = (
      <AlertRulesTable
        rules={visible}
        entityOf={entityOf}
        unitOf={unitOf}
        detailHref={detailHref}
        onToggle={(r, on) => void toggle(r, on)}
        busy={busy}
        selectedId={selectedId}
        showEntity={showEntity}
      />
    );
  }

  return (
    <div className="@container space-y-4">
      {rules.error && rules.data && (
        <Notice variant="warning" title={a("refreshFailed")}>
          {rules.error}
        </Notice>
      )}

      <AlertStateStrip counts={counts} active={state} onSelect={(s) => setParam("state", s)} />

      <div className="flex flex-wrap items-center gap-2">
        {toolbarStart}
        <div className="relative w-full max-w-xs">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" aria-hidden />
          <Input
            type="search"
            value={query}
            onChange={(e) => setParam("q", e.target.value)}
            placeholder={a("searchPlaceholder")}
            aria-label={a("searchLabel")}
            className="h-8 pl-8 text-caption"
          />
        </div>
        <span className="font-mono text-caption text-muted-foreground tabular-nums">{a("count", { count: visible.length })}</span>
        <div className="ml-auto flex flex-wrap items-center gap-2">
          {suggestRules}
          {newRule}
        </div>
      </div>

      {body}

      <AlertRuleDetailPanel
        ruleId={selectedId}
        rule={selected}
        loading={rules.loading || (!selected && (single.loading || single.fetching))}
        entityOf={entityOf}
        unitOf={unitOf}
        onOpenChange={(open) => {
          if (!open) setParam("rule", null);
        }}
        onEdit={(rule) => setEditor({ kind: "edit", rule })}
        onDeleted={deleted}
      />

      {editor && (
        <AlertRuleEditor
          open
          onOpenChange={(open) => !open && setEditor(null)}
          entities={entities}
          lockedEntity={entity}
          defaultEnv={env}
          rule={editor.kind === "edit" ? editor.rule : null}
          suggest={editor.kind === "suggest"}
          catalogUnits={units}
          onSaved={saved}
        />
      )}
    </div>
  );
}
