import type { ComponentRole } from "@/api";
import { Badge } from "@/components/ui/badge";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

// Nine roles, nine theme tokens — an arbitrary but exhaustive 1:1 assignment
// so every role reads as a distinct color without a raw hex value.
const ROLE_COLOR_CLASSES: Record<ComponentRole, string> = {
  frontend: "border-transparent bg-info/15 text-info",
  backend: "border-transparent bg-primary/15 text-primary",
  mobile: "border-transparent bg-success/15 text-success",
  desktop: "border-transparent bg-accent-warm/15 text-accent-warm",
  worker: "border-transparent bg-warning/15 text-warning",
  library: "border-transparent bg-secondary text-secondary-foreground",
  infra: "border-transparent bg-accent text-accent-foreground",
  cli: "border-transparent bg-muted text-muted-foreground",
  other: "border-transparent bg-destructive/15 text-destructive",
};

interface RoleBadgeProps {
  role: ComponentRole;
  className?: string;
}

export function RoleBadge({ role, className }: RoleBadgeProps) {
  const { t } = useI18n();
  return (
    <Badge className={cn(ROLE_COLOR_CLASSES[role], className)}>{t(`projectModel.roles.${role}`)}</Badge>
  );
}
