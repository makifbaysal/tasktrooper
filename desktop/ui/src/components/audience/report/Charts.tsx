import { cn } from "@/lib/utils";
import { formatMoney, formatPct, intentTone } from "@/lib/audienceHub";
import type { PriceTest, ReportBucket, ReportTheme } from "@/api";

const TONE_BAR = {
  destructive: "bg-destructive/70",
  warning: "bg-warning/70",
  success: "bg-success/70",
  primary: "bg-primary/70",
} as const;

/** A column chart in plain divs: no chart library to load for eleven bars. */
function Columns({
  items,
  label,
}: {
  items: { key: string; value: number; caption: string; tone: keyof typeof TONE_BAR; title: string }[];
  label: string;
}) {
  const max = Math.max(1, ...items.map((i) => i.value));
  return (
    <div role="img" aria-label={label} className="flex h-40 items-end gap-1.5">
      {items.map((item) => (
        <div key={item.key} className="flex h-full min-w-0 flex-1 flex-col items-center justify-end gap-1" title={item.title}>
          <span className="text-caption tabular-nums text-muted-foreground">{item.value > 0 ? item.value : ""}</span>
          <div
            className={cn("w-full rounded-t-sm", TONE_BAR[item.tone], item.value === 0 && "opacity-30")}
            style={{ height: `${Math.max(item.value === 0 ? 2 : 6, (item.value / max) * 100)}%` }}
          />
          <span className="w-full truncate text-center text-caption text-muted-foreground">{item.caption}</span>
        </div>
      ))}
    </div>
  );
}

/** How many people gave each purchase-intent score, 0 to 10. */
export function IntentChart({ distribution, label, peopleLabel }: { distribution: number[]; label: string; peopleLabel: (n: number) => string }) {
  return (
    <Columns
      label={label}
      items={distribution.map((count, score) => ({
        key: String(score),
        value: count,
        caption: String(score),
        tone: intentTone(score),
        title: `${score}/10: ${peopleLabel(count)}`,
      }))}
    />
  );
}

/** The most each person said they would pay, bucketed. */
export function PriceHistogram({
  buckets,
  currency,
  lang,
  label,
  peopleLabel,
}: {
  buckets: ReportBucket[];
  currency: string;
  lang: string;
  label: string;
  peopleLabel: (n: number) => string;
}) {
  return (
    <Columns
      label={label}
      items={buckets.map((b, i) => ({
        key: String(i),
        value: b.count,
        caption: formatMoney(b.from, currency, lang),
        tone: "primary" as const,
        title: `${formatMoney(b.from, currency, lang)} - ${formatMoney(b.to, currency, lang)}: ${peopleLabel(b.count)}`,
      }))}
    />
  );
}

/** Horizontal share bars: the share who would buy at each tested price. */
export function PriceTests({ tests, currency, lang, label }: { tests: PriceTest[]; currency: string; lang: string; label: (t: PriceTest) => string }) {
  return (
    <ul className="space-y-2">
      {tests.map((test) => (
        <li key={test.price} className="space-y-1">
          <div className="flex items-baseline justify-between gap-3 text-caption">
            <span className="font-medium">{formatMoney(test.price, currency, lang)}</span>
            <span className="text-muted-foreground">{label(test)}</span>
          </div>
          <div className="h-2 overflow-hidden rounded-full bg-surface-sunken" role="img" aria-label={label(test)}>
            <div className="h-full rounded-full bg-primary/70" style={{ width: `${Math.min(100, test.asked === 0 ? 0 : test.buy_pct)}%` }} />
          </div>
        </li>
      ))}
    </ul>
  );
}

/** Themes people raised, each with how many of them said it. */
export function ThemeBars({ themes, total, peopleLabel, tone = "primary" }: { themes: ReportTheme[]; total: number; peopleLabel: (count: number, total: number) => string; tone?: keyof typeof TONE_BAR }) {
  return (
    <ul className="space-y-3">
      {themes.map((theme) => (
        <li key={theme.label} className="space-y-1">
          <div className="flex items-baseline justify-between gap-3">
            <span className="text-body font-medium">{theme.label}</span>
            <span className="shrink-0 text-caption tabular-nums text-muted-foreground">
              {peopleLabel(theme.count, total)} · {formatPct(theme.pct)}
            </span>
          </div>
          <div className="h-1.5 overflow-hidden rounded-full bg-surface-sunken">
            <div className={cn("h-full rounded-full", TONE_BAR[tone])} style={{ width: `${Math.min(100, theme.pct)}%` }} />
          </div>
          {theme.personas && theme.personas.length > 0 && (
            <p className="text-caption text-muted-foreground">{theme.personas.join(", ")}</p>
          )}
        </li>
      ))}
    </ul>
  );
}

/** The purchase-intent index on a 0-100 track with the verdict thresholds marked. */
export function IndexTrack({ score, tone }: { score: number; tone: "success" | "info" | "warning" | "destructive" }) {
  const fill = { success: "bg-success", info: "bg-info", warning: "bg-warning", destructive: "bg-destructive" }[tone];
  return (
    <div className="relative mt-3 h-3 rounded-full bg-surface-sunken" role="img" aria-label={`${Math.round(score)}/100`}>
      <div className={cn("h-full rounded-full", fill)} style={{ width: `${Math.min(100, Math.max(0, score))}%` }} />
      {[30, 50, 70].map((mark) => (
        <span key={mark} className="absolute top-0 h-3 w-px bg-border" style={{ left: `${mark}%` }} aria-hidden />
      ))}
    </div>
  );
}
