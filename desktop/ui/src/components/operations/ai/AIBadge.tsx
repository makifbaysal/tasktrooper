import { Sparkles } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

/** Something the release engineer proposed (an alert rule, a signal
 * binding): "AI", with what that means as its title. */
export function AIBadge({ title, className }: { title?: string; className?: string }) {
  const { t } = useI18n();
  return (
    <Badge variant="info" className={cn("gap-1 px-2 py-0 text-micro whitespace-nowrap", className)} title={title ?? t("operations.aiJob.badgeTitle")}>
      <Sparkles className="h-3 w-3" aria-hidden />
      {t("operations.aiJob.badge")}
    </Badge>
  );
}
