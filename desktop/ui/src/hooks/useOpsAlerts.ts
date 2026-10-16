import { api, type AlertRule, type AlertRuleEvent, type MetricQueryResult, type ReleaseImpact } from "@/api";
import { ALERT_RULES_POLL_MS } from "@/components/operations/alertRules/alertRules";
import { autoStepSeconds } from "@/components/operations/charts/signalChart";
import type { OpsTimeRange } from "@/components/operations/shell/OpsContext";
import { windowKey } from "@/components/operations/query/opsQuery";
import { useKeyedRead, type KeyedRead } from "@/hooks/useOpsSignals";

const RELEASE_IMPACT_PENDING_POLL_MS = 60_000;
// A settled impact does not change; the poll only guards a page left open.
const RELEASE_IMPACT_SETTLED_POLL_MS = 10 * 60_000;

/** Every environment's alert rules, of one component or of all: a `?rule=`
 * link names no environment, so the page filters by the console's itself. */
export function useAlertRules(componentId?: string): KeyedRead<AlertRule[]> {
  const key = `operations.alertRules.${componentId ?? "all"}`;
  return useKeyedRead(
    key,
    async () => (await api.listAlertRules({ componentId })).rules ?? [],
    true,
    ALERT_RULES_POLL_MS,
  );
}

/** A rule's state changes, newest first. */
export function useAlertRuleEvents(ruleId: string | null): KeyedRead<AlertRuleEvent[]> {
  const key = `operations.alertRules.events.${ruleId ?? ""}`;
  return useKeyedRead(key, async () => (await api.listAlertRuleEvents(ruleId ?? "", 100)).events ?? [], ruleId !== null, ALERT_RULES_POLL_MS);
}

/** One rule by id: a `?rule=` link to a rule the list behind it does not hold. */
export function useAlertRule(ruleId: string | null, enabled: boolean): KeyedRead<AlertRule> {
  return useKeyedRead(`operations.alertRules.rule.${ruleId ?? ""}`, () => api.getAlertRule(ruleId ?? ""), enabled && ruleId !== null, ALERT_RULES_POLL_MS);
}

/** A custom-query rule's own query over the window, for its chart. */
export function useRuleQuerySeries(
  dataSourceId: string | undefined,
  query: string | undefined,
  range: OpsTimeRange,
  enabled: boolean,
): KeyedRead<MetricQueryResult> {
  const key = `operations.alertRules.query.${dataSourceId ?? ""}.${windowKey(range)}.${query ?? ""}`;
  return useKeyedRead(
    key,
    () =>
      api.queryDataSource(dataSourceId ?? "", {
        query: query ?? "",
        from: new Date(range.from).toISOString(),
        to: new Date(range.to).toISOString(),
        step_seconds: autoStepSeconds(range.to - range.from),
      }),
    enabled && Boolean(dataSourceId) && Boolean(query),
  );
}

/** A release's before/after, re-read while its window after the deploy is open. */
export function useReleaseImpact(releaseId: string | null, enabled: boolean, pending: boolean): KeyedRead<ReleaseImpact> {
  const key = `operations.releaseImpact.${releaseId ?? ""}`;
  return useKeyedRead(
    key,
    () => api.getReleaseImpact(releaseId ?? ""),
    enabled && releaseId !== null,
    pending ? RELEASE_IMPACT_PENDING_POLL_MS : RELEASE_IMPACT_SETTLED_POLL_MS,
  );
}
