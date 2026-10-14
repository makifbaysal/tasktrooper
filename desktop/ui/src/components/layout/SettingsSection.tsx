import type { ReactNode } from "react";

interface SettingsSectionProps {
  title: string;
  description?: string;
  action?: ReactNode;
  children: ReactNode;
}

export function SettingsSection({ title, description, action, children }: SettingsSectionProps) {
  return (
    <section className="space-y-3">
      <div className="flex min-h-8 items-end justify-between gap-2">
        <div className="min-w-0">
          <h2 className="text-heading font-semibold">{title}</h2>
          {description && <p className="text-caption text-muted-foreground">{description}</p>}
        </div>
        {action}
      </div>
      <div className="space-y-4">{children}</div>
    </section>
  );
}
