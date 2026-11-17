import { Badge, type BadgeProps } from "@/components/ui/badge";
import { useI18n } from "@/hooks/useI18n";

const VARIANT: Record<string, BadgeProps["variant"]> = {
  queued: "secondary",
  running: "info",
  passed: "success",
  failed: "destructive",
  aborted: "warning",
  error: "destructive",
};

export function LoadRunBadge({ state }: { state: string }) {
  const { t } = useI18n();
  return <Badge variant={VARIANT[state] ?? "outline"}>{state in VARIANT ? t(`telemetryViews.loadTests.states.${state}`) : state}</Badge>;
}
