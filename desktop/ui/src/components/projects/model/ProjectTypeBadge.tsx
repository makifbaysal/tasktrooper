import type { ProjectType } from "@/api";
import { Badge, type BadgeProps } from "@/components/ui/badge";
import { useI18n } from "@/hooks/useI18n";

const PROJECT_TYPE_VARIANT: Record<ProjectType, NonNullable<BadgeProps["variant"]>> = {
  empty: "outline",
  single_repo: "secondary",
  monorepo: "info",
  multi_repo: "default",
};

interface ProjectTypeBadgeProps {
  type: ProjectType;
  className?: string;
}

export function ProjectTypeBadge({ type, className }: ProjectTypeBadgeProps) {
  const { t } = useI18n();
  return (
    <Badge variant={PROJECT_TYPE_VARIANT[type]} className={className}>
      {t(`projectModel.projectTypes.${type}`)}
    </Badge>
  );
}
