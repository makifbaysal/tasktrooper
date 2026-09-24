import type { RepoShape } from "@/api";
import { Badge, type BadgeProps } from "@/components/ui/badge";
import { useI18n } from "@/hooks/useI18n";

const REPO_SHAPE_VARIANT: Record<RepoShape, NonNullable<BadgeProps["variant"]>> = {
  single: "outline",
  monorepo: "info",
};

interface RepoShapeBadgeProps {
  shape: RepoShape;
  className?: string;
}

export function RepoShapeBadge({ shape, className }: RepoShapeBadgeProps) {
  const { t } = useI18n();
  return (
    <Badge variant={REPO_SHAPE_VARIANT[shape]} className={className}>
      {t(`projectModel.repoShapes.${shape}`)}
    </Badge>
  );
}
