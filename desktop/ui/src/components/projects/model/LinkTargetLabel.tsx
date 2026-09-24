import type { ComponentLink, RepositoryModel } from "@/api";
import { useI18n } from "@/hooks/useI18n";
import { linkTargetLabel } from "@/lib/project-model";
import { cn } from "@/lib/utils";

interface LinkTargetLabelProps {
  link: ComponentLink;
  model: RepositoryModel;
  className?: string;
}

export function LinkTargetLabel({ link, model, className }: LinkTargetLabelProps) {
  const { t } = useI18n();
  const label = linkTargetLabel(link, model);
  if (!label) {
    return <span className={cn("italic text-muted-foreground", className)}>{t("projectModel.linkTargetUnknown")}</span>;
  }
  return <span className={cn("font-medium", className)}>{label}</span>;
}
