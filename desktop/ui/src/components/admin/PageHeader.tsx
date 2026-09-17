import { type ReactNode } from "react";

interface PageHeaderProps {
  title: string;
  description?: string;
  action?: ReactNode;
}

export function PageHeader({ title, description, action }: PageHeaderProps) {
  return (
    <div className="mb-6 flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
      <div>
        <h1 className="text-display font-semibold tracking-tight">{title}</h1>
        {description && <p className="mt-1 text-caption text-muted-foreground">{description}</p>}
      </div>
      {action}
    </div>
  );
}
