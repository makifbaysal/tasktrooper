import type { ReactNode } from "react";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

interface IntegrationChoiceTileProps {
  id: string;
  icon: ReactNode;
  name: string;
  description: string;
  recommended: boolean;
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
}

/**
 * One integration on the first run's "which ones?" screen: the whole tile is
 * the checkbox's label, and the checkbox is named by the integration alone
 * (the line under it describes it), so a screen reader hears "Jira, checkbox".
 */
export function IntegrationChoiceTile({
  id,
  icon,
  name,
  description,
  recommended,
  checked,
  onCheckedChange,
}: IntegrationChoiceTileProps) {
  const { t } = useI18n();
  const controlId = `integration-choice-${id}`;

  return (
    <Card className={cn("transition-colors", checked && "border-primary")} data-testid={controlId}>
      <Label
        htmlFor={controlId}
        className="flex h-full cursor-pointer items-start gap-3 p-4 font-normal leading-normal"
      >
        <Checkbox
          id={controlId}
          checked={checked}
          onCheckedChange={(value) => onCheckedChange(value === true)}
          aria-labelledby={`${controlId}-name`}
          aria-describedby={`${controlId}-description`}
          className="mt-1"
        />
        <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-border bg-muted/40 text-foreground">
          {icon}
        </span>
        <span className="min-w-0 flex-1 space-y-1">
          <span className="flex flex-wrap items-center gap-2">
            <span id={`${controlId}-name`} className="text-body font-medium">
              {name}
            </span>
            {recommended && (
              <Badge variant="info" className="text-micro">
                {t("setup.integrations.recommended")}
              </Badge>
            )}
          </span>
          <span id={`${controlId}-description`} className="block text-caption text-muted-foreground">
            {description}
          </span>
        </span>
      </Label>
    </Card>
  );
}
