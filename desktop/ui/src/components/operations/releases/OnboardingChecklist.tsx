import { CircleCheck, CircleDashed, ListChecks, RefreshCw } from "lucide-react";
import { api, type StoreAppView } from "@/api";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import { useI18n } from "@/hooks/useI18n";
import { useStoreAction } from "@/hooks/useMobileReleases";
import { cn, formatDate, formatRelativeTime } from "@/lib/utils";

/** The first publish's manual steps, each re-checked against the store on
 * Verify rather than ticked by hand. */
export function OnboardingChecklist({ app, onVerified }: { app: StoreAppView; onVerified: () => void }) {
  const { t, lang } = useI18n();
  const o = (key: string, params?: Record<string, string | number>) => t(`operations.mobileReleases.onboarding.${key}`, params);
  const { busy, run } = useStoreAction();
  const items = app.checklist ?? [];
  const done = items.filter((i) => i.done).length;

  const verify = async () => {
    if (await run("verify", () => api.verifyStoreOnboarding(app.repository_id, app.platform), o("verified"))) onVerified();
  };

  return (
    <Card className="space-y-3 p-3">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="flex min-w-0 items-start gap-2">
          <ListChecks className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" aria-hidden />
          <div className="min-w-0">
            <h3 className="text-body font-semibold">{o("title")}</h3>
            <p className="text-caption text-muted-foreground">{o("description")}</p>
          </div>
        </div>
        <Button size="sm" variant="outline" disabled={busy !== null} onClick={() => void verify()}>
          <RefreshCw className={cn(busy !== null && "animate-spin")} aria-hidden />
          {busy !== null ? o("verifying") : o("verify")}
        </Button>
      </div>
      {items.length === 0 ? (
        <p className="text-caption text-muted-foreground">{o("empty")}</p>
      ) : (
        <>
          <div className="flex items-center gap-3">
            <Progress value={(done / items.length) * 100} tone={done === items.length ? "success" : "warning"} label={o("progress", { done, total: items.length })} className="max-w-48" />
            <span className="font-mono text-caption tabular-nums text-muted-foreground">{o("progress", { done, total: items.length })}</span>
          </div>
          <ul className="divide-y divide-border rounded-lg border border-border">
            {items.map((item) => (
              <li key={item.key} className="flex items-start gap-2 px-3 py-2 text-caption">
                {item.done ? (
                  <CircleCheck className="mt-0.5 h-3.5 w-3.5 shrink-0 text-success" aria-hidden />
                ) : (
                  <CircleDashed className="mt-0.5 h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden />
                )}
                <span className="min-w-0 flex-1">
                  <span className="sr-only">{item.done ? o("doneItem") : o("todoItem")}: </span>
                  {item.title}
                </span>
                {item.verified_at && (
                  <span className="shrink-0 text-micro text-muted-foreground" title={formatDate(item.verified_at)}>
                    {o("checkedAt", { time: formatRelativeTime(item.verified_at, lang) })}
                  </span>
                )}
              </li>
            ))}
          </ul>
        </>
      )}
    </Card>
  );
}
