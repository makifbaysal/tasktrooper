import { AlertTriangle } from "lucide-react";
import { Card, CardContent } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { tStatic } from "@/hooks/useI18n";

/**
 * Shown instead of the app when this build has no bearer token for the local
 * server. In the desktop shell the token always arrives over the bridge, so
 * this is the browser-development case: `VITE_API_KEY` must be set and must
 * match the server's `SERVER_API_KEY`.
 */
export function ConfigErrorPage({ missing }: { missing: string[] }) {
  return (
    <div className="flex min-h-screen items-center justify-center bg-background p-4">
      <Card className="w-full max-w-md">
        <CardContent className="pt-6">
          <EmptyState
            variant="critical"
            icon={AlertTriangle}
            title={tStatic("common.configError.title")}
            description={tStatic("common.configError.body")}
          />
          <p className="text-center text-caption text-muted-foreground">{tStatic("common.configError.missing")}</p>
          <p className="mt-1 break-words text-center font-mono text-caption text-destructive">{missing.join(", ")}</p>
        </CardContent>
      </Card>
    </div>
  );
}
