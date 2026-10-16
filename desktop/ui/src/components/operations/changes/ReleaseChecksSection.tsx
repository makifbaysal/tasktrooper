import { ExternalLink } from "lucide-react";
import type { ReleaseChecks, ReleaseStatus } from "@/api";
import { ImpactChips } from "@/components/operations/changes/ImpactChips";
import { DetailSection } from "@/components/operations/console/DetailPanel";
import { Notice } from "@/components/ui/notice";
import { useI18n } from "@/hooks/useI18n";
import { intlLocale } from "@/lib/languages";
import { cn, formatDate, formatRelativeTime } from "@/lib/utils";

const TH = "px-2 py-1.5 text-left text-micro font-medium text-muted-foreground whitespace-nowrap";
const TD = "px-2 py-1.5 align-top";

function Result({ ok, error }: { ok: boolean; error?: string }) {
  const { t } = useI18n();
  return (
    <span className={cn("font-medium", ok ? "text-success" : "text-destructive")} title={error}>
      {ok ? t("operations.changes.detail.checks.ok") : t("operations.changes.detail.checks.failed")}
      {error && <span className="block max-w-56 truncate font-normal text-muted-foreground">{error}</span>}
    </span>
  );
}

/** Everything the soak recorded: health samples, smoke checks, new error
 * groups, and what it could not check. */
export function ReleaseChecksSection({ checks, status }: { checks?: ReleaseChecks; status: ReleaseStatus }) {
  const { t, lang } = useI18n();
  const k = (key: string, params?: Record<string, string | number>) => t(`operations.changes.detail.checks.${key}`, params);
  const clock = new Intl.DateTimeFormat(intlLocale(lang), { hour: "2-digit", minute: "2-digit", second: "2-digit" });
  const health = checks?.health ?? [];
  const smoke = checks?.smoke ?? [];
  const newErrors = checks?.new_errors ?? [];
  const notes = checks?.notes ?? [];
  const nothing = health.length === 0 && smoke.length === 0 && newErrors.length === 0 && notes.length === 0 && !checks?.early_stop;
  const okCount = health.filter((h) => h.ok).length;

  return (
    <DetailSection title={k("title")} aside={<ImpactChips checks={checks} status={status} />}>
      {nothing && <p className="text-caption text-muted-foreground">{k("empty")}</p>}

      {checks?.early_stop && (
        <Notice variant="error" title={k("earlyStop")}>
          <p className="break-words text-caption">{checks.early_stop}</p>
        </Notice>
      )}

      {health.length > 0 && (
        <div className="space-y-1">
          <h4 className="text-caption font-medium">
            {k("health")}{" "}
            <span className="font-mono font-normal text-muted-foreground tabular-nums">
              {k("healthSummary", { ok: okCount, failed: health.length - okCount })}
            </span>
          </h4>
          <div className="max-h-56 overflow-auto rounded-md border border-border">
            <table className="w-full border-collapse text-caption">
              <thead className="sticky top-0 bg-muted">
                <tr>
                  <th scope="col" className={TH}>{k("time")}</th>
                  <th scope="col" className={cn(TH, "text-right")}>{k("status")}</th>
                  <th scope="col" className={cn(TH, "text-right")}>{k("latency")}</th>
                  <th scope="col" className={TH}>{k("result")}</th>
                </tr>
              </thead>
              <tbody>
                {health.map((h, i) => (
                  <tr key={`${h.at}:${i}`} className="border-t border-border/60">
                    <td className={cn(TD, "font-mono tabular-nums")} title={formatDate(h.at)}>
                      {Number.isFinite(Date.parse(h.at)) ? clock.format(Date.parse(h.at)) : h.at}
                    </td>
                    <td className={cn(TD, "text-right font-mono tabular-nums")}>{h.status ?? "—"}</td>
                    <td className={cn(TD, "text-right font-mono tabular-nums")}>{h.latency_ms !== undefined ? `${h.latency_ms} ms` : "—"}</td>
                    <td className={TD}>
                      <Result ok={h.ok} error={h.error} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {smoke.length > 0 && (
        <div className="space-y-1">
          <h4 className="text-caption font-medium">{k("smoke")}</h4>
          <div className="overflow-auto rounded-md border border-border">
            <table className="w-full border-collapse text-caption">
              <thead className="bg-muted">
                <tr>
                  <th scope="col" className={TH}>{k("check")}</th>
                  <th scope="col" className={cn(TH, "text-right")}>{k("status")}</th>
                  <th scope="col" className={cn(TH, "text-right")}>{k("latency")}</th>
                  <th scope="col" className={TH}>{k("result")}</th>
                </tr>
              </thead>
              <tbody>
                {smoke.map((s, i) => (
                  <tr key={`${s.url}:${i}`} className="border-t border-border/60">
                    <td className={cn(TD, "max-w-60")}>
                      <span className="block truncate font-medium" title={s.url}>
                        {s.check.name || `${s.check.method ?? "GET"} ${s.check.path}`}
                      </span>
                      <span className="block truncate font-mono text-micro text-muted-foreground" title={s.url}>{s.url}</span>
                    </td>
                    <td className={cn(TD, "text-right font-mono tabular-nums")}>{s.status ?? "—"}</td>
                    <td className={cn(TD, "text-right font-mono tabular-nums")}>{s.latency_ms !== undefined ? `${s.latency_ms} ms` : "—"}</td>
                    <td className={TD}>
                      <Result ok={s.ok} error={s.error} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {newErrors.length > 0 && (
        <div className="space-y-1">
          <h4 className="text-caption font-medium">{k("newErrors")}</h4>
          <ul className="divide-y divide-border rounded-md border border-border">
            {newErrors.map((g) => (
              <li key={g.fingerprint} className="flex items-start gap-3 px-3 py-2">
                <div className="min-w-0 flex-1">
                  <p className="line-clamp-2 font-mono text-caption break-words">{g.message}</p>
                  <p className="text-micro text-muted-foreground" title={formatDate(g.first_seen)}>
                    {k("firstSeen", { time: formatRelativeTime(g.first_seen, lang) })}
                    {g.source && <span> · {g.source}</span>}
                  </p>
                </div>
                <span className="shrink-0 font-mono text-caption tabular-nums">{k("occurrences", { count: g.count })}</span>
                {g.external_url && (
                  <a
                    href={g.external_url}
                    target="_blank"
                    rel="noreferrer"
                    className="shrink-0 text-info hover:underline"
                    aria-label={k("openExternal")}
                    title={k("openExternal")}
                  >
                    <ExternalLink className="h-3.5 w-3.5" aria-hidden />
                  </a>
                )}
              </li>
            ))}
          </ul>
        </div>
      )}

      {notes.length > 0 && (
        <div className="space-y-1">
          <h4 className="text-caption font-medium">{k("notes")}</h4>
          <ul className="list-disc space-y-0.5 pl-5 text-caption text-muted-foreground">
            {notes.map((note) => (
              <li key={note}>{note}</li>
            ))}
          </ul>
        </div>
      )}
    </DetailSection>
  );
}
