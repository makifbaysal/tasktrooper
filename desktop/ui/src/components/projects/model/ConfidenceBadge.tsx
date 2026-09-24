import type { Confidence } from "@/api";
import { Badge, type BadgeProps } from "@/components/ui/badge";
import { useI18n } from "@/hooks/useI18n";

const CONFIDENCE_VARIANT: Record<Confidence, NonNullable<BadgeProps["variant"]>> = {
  exact: "success",
  high: "info",
  medium: "warning",
  low: "secondary",
};

interface ConfidenceBadgeProps {
  confidence?: Confidence;
  className?: string;
}

/** Absent confidence (a user-entered value with no scan behind it) renders nothing. */
export function ConfidenceBadge({ confidence, className }: ConfidenceBadgeProps) {
  const { t } = useI18n();
  if (!confidence) return null;
  return (
    <Badge variant={CONFIDENCE_VARIANT[confidence]} className={className}>
      {t(`projectModel.confidence.${confidence}`)}
    </Badge>
  );
}
