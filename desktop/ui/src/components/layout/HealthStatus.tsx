import { useCallback, useEffect, useState } from "react";
import { api, type HealthResponse, type LLMProviderHealthItem } from "@/api";
import { Badge } from "@/components/ui/badge";
import { useI18n } from "@/hooks/useI18n";
import { usePolling } from "@/hooks/usePolling";
import { useSetup } from "@/hooks/useSetup";
import { cn } from "@/lib/utils";

function providerDotClass(item: LLMProviderHealthItem) {
  if (!item.configured) return "bg-muted-foreground/40";
  if (item.status === "ok") return "bg-success";
  return "bg-destructive";
}

export function HealthStatus() {
  const { t } = useI18n();
  const providerShortLabel: Record<string, string> = {
    local: t("frame.layout.health.providerLocal"),
    openai: "OpenAI",
    groq: "Groq",
    gemini: "Gemini",
    anthropic: "Claude",
  };

  const providerStatusLabel = (item: LLMProviderHealthItem) => {
    if (!item.configured) return t("frame.layout.health.notConnected");
    if (item.status === "ok") return t("frame.layout.health.connected");
    return t("frame.layout.health.problem");
  };

  const { cliState } = useSetup();
  // Agent CLIs (Claude Code, Cursor, ...) are host-executed and can never be
  // `configured` on the chat-LLM providers below — they live in a separate
  // system (see api.ts's AgentCLIState). A connected one is what actually
  // does the work, so it outranks the chat-LLM badge when present.
  const connectedCliFlavors = (cliState?.flavors ?? []).filter((f) => f.connected);

  const [health, setHealth] = useState<HealthResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [ready, setReady] = useState(false);
  const [open, setOpen] = useState(false);

  const poll = useCallback(async () => {
    try {
      const data = await api.health();
      setHealth(data);
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : t("frame.layout.health.checkFailed"));
      setHealth(null);
    } finally {
      setReady(true);
    }
  }, [t]);

  // Visibility-gated by usePolling: a hidden tab stops polling /health
  // altogether and refreshes once on return. This effect used to only ever
  // poll *more* on visibilitychange, so a forgotten background tab produced a
  // request every 10s forever.
  usePolling(poll, 10000, true);

  useEffect(() => {
    const onFocus = () => void poll();
    window.addEventListener("focus", onFocus);
    return () => window.removeEventListener("focus", onFocus);
  }, [poll]);

  const bridgeReachable = !error && health !== null;
  const configuredProviders = (health?.providers ?? []).filter((p) => p.configured);
  const okProviders = configuredProviders.filter((p) => p.status === "ok");
  const llmOk = bridgeReachable && (configuredProviders.length === 0 ? health?.llm === "ok" : okProviders.length === configuredProviders.length);
  const isOk = bridgeReachable && llmOk;
  const isBusy = bridgeReachable && !llmOk && health?.status === "degraded";

  const dotClass = isOk ? "bg-success" : isBusy ? "bg-warning" : "bg-destructive";

  return (
    <div
      className="relative hidden sm:block"
      onMouseEnter={() => setOpen(true)}
      onMouseLeave={() => setOpen(false)}
    >
      <div className="flex cursor-default items-center gap-1.5">
        {!ready ? (
          <Badge variant="secondary" className="text-micro">...</Badge>
        ) : connectedCliFlavors.length > 0 ? (
          connectedCliFlavors.map((flavor) => (
            <Badge key={flavor.flavor} variant="secondary" className="gap-1 text-micro px-1.5 py-0">
              <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-success" />
              {flavor.label}
            </Badge>
          ))
        ) : configuredProviders.length > 0 ? (
          configuredProviders.map((item) => (
            <Badge
              key={item.provider_type}
              variant={item.status === "ok" ? "secondary" : "destructive"}
              className="gap-1 text-micro px-1.5 py-0"
            >
              <span className={cn("h-1.5 w-1.5 shrink-0 rounded-full", providerDotClass(item))} />
              {providerShortLabel[item.provider_type] ?? item.label}
            </Badge>
          ))
        ) : (
          <Badge variant={isOk ? "secondary" : "destructive"} className="gap-1 text-micro">
            <span className={cn("h-1.5 w-1.5 shrink-0 rounded-full", dotClass)} />
            {isOk ? "LLM" : t("frame.layout.health.badgeProblem")}
          </Badge>
        )}
      </div>

      {open && ready && (
        <div className="absolute right-0 top-full z-50 mt-2 w-80 rounded-lg border border-border bg-popover p-3 shadow-lg">
          <p className="mb-2 text-xs font-medium text-foreground">{t("frame.layout.health.providersTitle")}</p>
          {error ? (
            <p className="text-xs text-destructive">{t("frame.layout.health.serverUnreachable", { error })}</p>
          ) : connectedCliFlavors.length > 0 || configuredProviders.length > 0 ? (
            <ul className="space-y-2">
              {connectedCliFlavors.map((flavor) => (
                <li key={flavor.flavor} className="rounded-md border border-border/60 px-2.5 py-2">
                  <div className="flex items-center justify-between gap-2">
                    <div className="flex min-w-0 items-center gap-2">
                      <span className="h-2 w-2 shrink-0 rounded-full bg-success" />
                      <span className="truncate text-xs font-medium">{flavor.label}</span>
                    </div>
                    <span className="shrink-0 text-micro text-muted-foreground">
                      {t("frame.layout.health.connected")}
                    </span>
                  </div>
                </li>
              ))}
              {configuredProviders.map((item) => (
                <li key={item.provider_type} className="rounded-md border border-border/60 px-2.5 py-2">
                  <div className="flex items-center justify-between gap-2">
                    <div className="flex min-w-0 items-center gap-2">
                      <span className={cn("h-2 w-2 shrink-0 rounded-full", providerDotClass(item))} />
                      <span className="truncate text-xs font-medium">{item.label}</span>
                    </div>
                    <span className="shrink-0 text-micro text-muted-foreground">
                      {providerStatusLabel(item)}
                    </span>
                  </div>
                  {item.active && item.configured && (
                    <p className="mt-1 text-micro text-muted-foreground">{t("frame.layout.health.defaultProvider")}</p>
                  )}
                  {item.message && (
                    <p className="mt-1 line-clamp-2 text-micro text-destructive">{item.message}</p>
                  )}
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-xs text-muted-foreground">
              {health?.llm === "ok"
                ? t("frame.layout.health.llmWorking")
                : health?.llm ?? t("frame.layout.health.statusUnknown")}
            </p>
          )}
          {health && health.status !== "ok" && (
            <p className="mt-2 text-micro text-muted-foreground">
              {t("frame.layout.health.configureHint")}
            </p>
          )}
        </div>
      )}
    </div>
  );
}
