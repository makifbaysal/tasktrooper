import { AlertTriangle } from "lucide-react";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

interface GitWarningIconProps {
  warning: string;
  className?: string;
}

/** Hover-only warning icon for a repository row — the native `title` carries
 * the server's own sentence, mirroring ui/help-tooltip's pattern but in the
 * warning color since this is a problem, not an explanation. */
export function GitWarningIcon({ warning, className }: GitWarningIconProps) {
  const { t } = useI18n();
  return (
    <span
      title={warning}
      tabIndex={0}
      className={cn("inline-flex shrink-0 cursor-help text-warning outline-none", className)}
    >
      <AlertTriangle className="h-3.5 w-3.5" aria-hidden />
      <span className="sr-only">{t("projectsHub.card.gitWarning", { warning })}</span>
    </span>
  );
}
