import type { ReactNode } from "react";
import { Logo } from "@/assets/logo";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";

interface SetupShellProps {
  title: string;
  description?: string;
  children: ReactNode;
}

/** The full-page frame the guided first-run sequence renders inside. */
export function SetupShell({ title, description, children }: SetupShellProps) {
  return (
    <div className="flex min-h-screen items-center justify-center bg-background p-4">
      <div className="w-full max-w-3xl space-y-6">
        <div className="flex items-center justify-center gap-2">
          <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-primary text-primary-foreground">
            <Logo className="h-4 w-4" />
          </div>
          <p className="text-body font-semibold text-foreground">TaskTrooper</p>
        </div>

        <Card>
          <CardHeader className="text-center">
            <CardTitle className="text-title">{title}</CardTitle>
            {description && <CardDescription>{description}</CardDescription>}
          </CardHeader>
          <CardContent className="space-y-4">{children}</CardContent>
        </Card>
      </div>
    </div>
  );
}
