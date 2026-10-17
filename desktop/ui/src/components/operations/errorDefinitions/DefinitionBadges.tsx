import { BellRing, Code2, EyeOff, ListTodo, Minus, Siren, Sparkles, User, type LucideIcon } from "lucide-react";
import type { ErrorAction, ErrorCategory, ErrorDefinitionRef, ErrorDefinitionSource } from "@/api";
import { Badge, type BadgeProps } from "@/components/ui/badge";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

const PILL = "px-2 py-0 text-micro whitespace-nowrap";

const CATEGORY: Record<ErrorCategory, { variant: NonNullable<BadgeProps["variant"]>; className?: string }> = {
  bug: { variant: "outline", className: "border-transparent bg-destructive/15 text-destructive" },
  expected: { variant: "secondary" },
  dependency: { variant: "warning" },
  infrastructure: { variant: "info" },
  user_error: { variant: "outline", className: "text-muted-foreground" },
};

/** What kind of failure a definition names. */
export function CategoryPill({ category, className }: { category: ErrorCategory; className?: string }) {
  const { t } = useI18n();
  const meta = CATEGORY[category] ?? CATEGORY.bug;
  return (
    <Badge variant={meta.variant} className={cn(PILL, meta.className, className)}>
      {t(`operations.errorDefinitions.categories.${category}`)}
    </Badge>
  );
}

const SOURCE: Record<ErrorDefinitionSource, { icon: LucideIcon; variant: NonNullable<BadgeProps["variant"]> }> = {
  ai: { icon: Sparkles, variant: "info" },
  code: { icon: Code2, variant: "secondary" },
  human: { icon: User, variant: "outline" },
};

/** Who wrote a definition: a person, the release engineer from recent errors (AI), or from the code's own error types. */
export function SourceBadge({ source, className }: { source: ErrorDefinitionSource; className?: string }) {
  const { t } = useI18n();
  const meta = SOURCE[source] ?? SOURCE.human;
  const Icon = meta.icon;
  return (
    <Badge variant={meta.variant} className={cn(PILL, "gap-1", className)} title={t(`operations.errorDefinitions.sourceTitles.${source}`)}>
      <Icon className="h-3 w-3" aria-hidden />
      {t(`operations.errorDefinitions.sources.${source}`)}
    </Badge>
  );
}

const ACTION_ICON: Record<ErrorAction, LucideIcon> = {
  none: Minus,
  ignore: EyeOff,
  alert: BellRing,
  incident: Siren,
  task: ListTodo,
};

/** What a matching group sets off, as an icon and a word. */
export function ActionLabel({ action, className }: { action: ErrorAction; className?: string }) {
  const { t } = useI18n();
  const Icon = ACTION_ICON[action] ?? Minus;
  return (
    <span className={cn("inline-flex items-center gap-1.5 whitespace-nowrap", action === "none" && "text-muted-foreground", className)}>
      <Icon className="h-3.5 w-3.5 shrink-0" aria-hidden />
      {t(`operations.errorDefinitions.actions.${action}`)}
    </span>
  );
}

/** The definition an error group matches, for the inbox: its name and category. */
export function DefinitionChip({ definition, className }: { definition: ErrorDefinitionRef; className?: string }) {
  const { t } = useI18n();
  return (
    <span
      className={cn("inline-flex min-w-0 max-w-full items-center gap-1.5", className)}
      title={t("operations.errorDefinitions.chipTitle", { name: definition.name })}
    >
      {definition.action === "ignore" && (
        <>
          <EyeOff className="h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden />
          <span className="sr-only">{t("operations.errorDefinitions.inbox.ignored")}</span>
        </>
      )}
      <CategoryPill category={definition.category} />
      <span className="min-w-0 truncate text-caption text-muted-foreground">{definition.name}</span>
    </span>
  );
}
