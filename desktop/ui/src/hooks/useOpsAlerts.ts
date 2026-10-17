import { useState } from "react";
import {
  api,
  type AlertBacktest,
  type AlertRule,
  type AlertRuleEvent,
  type MetricQueryResult,
  type NotificationChannelInfo,
  type ReleaseImpact,
} from "@/api";
import { ALERT_RULES_POLL_MS, replayDays, replayRequest } from "@/components/operations/alertRules/alertRules";
import { impactPollMs } from "@/components/operations/changes/releaseImpact";
import { autoStepSeconds } from "@/components/operations/charts/signalChart";
import type { OpsTimeRange } from "@/components/operations/shell/OpsContext";
import { windowKey } from "@/components/operations/query/opsQuery";
import { useKeyedRead, type KeyedRead } from "@/hooks/useOpsSignals";

// A replay reads up to 14 days at the rule's window; it is re-read far less
// often than a plain series.
const RULE_REPLAY_POLL_MS = 120_000;
// What delivers changes only with the server's own build.
const NOTIFICATION_CHANNELS_POLL_MS = 10 * 60_000;

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

/** What the evaluator judges a rule on, replayed by the server back to the
 * window's start (1–14 days): a web vital at p75 and a change rule's percent
 * change, neither of which a plain series read answers. */
export function useRuleReplay(rule: AlertRule, range: OpsTimeRange, enabled: boolean): KeyedRead<AlertBacktest> {
  const days = replayDays(range.from, Date.now());
  const key = `operations.alertRules.replay.${rule.id}.${rule.updated_at}.${days}`;
  return useKeyedRead(key, () => api.backtestAlertRule(replayRequest(rule, days)), enabled, RULE_REPLAY_POLL_MS);
}

/** Which channels deliver here; null until the server has answered. */
export function useNotificationChannels(): KeyedRead<NotificationChannelInfo[]> {
  return useKeyedRead(
    "operations.notificationChannels",
    async () => (await api.listNotificationChannels())?.channels ?? [],
    true,
    NOTIFICATION_CHANNELS_POLL_MS,
  );
}

/** A release's before/after, re-read every minute while its window after the
 * deploy is open: by the release's own state, or by the impact's `pending`. */
export function useReleaseImpact(releaseId: string | null, enabled: boolean, settling: boolean): KeyedRead<ReleaseImpact> {
  const key = `operations.releaseImpact.${releaseId ?? ""}`;
  const [pending, setPending] = useState(false);
  const read = useKeyedRead(key, () => api.getReleaseImpact(releaseId ?? ""), enabled && releaseId !== null, impactPollMs(settling, pending));
  const answered = read.data?.pending === true;
  if (answered !== pending) setPending(answered);
  return read;
}
