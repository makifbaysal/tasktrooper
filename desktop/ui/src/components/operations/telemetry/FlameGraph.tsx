import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { useI18n } from "@/hooks/useI18n";
import { intlLocale } from "@/lib/languages";
import { flameCells, flameNodeAt, formatProfileValue, type TelemetryFlameNode } from "@/lib/telemetryViews";
import { cn } from "@/lib/utils";

const ROW = 20;
const HUES = ["bg-chart-1/70", "bg-chart-2/70", "bg-chart-3/70", "bg-chart-4/70", "bg-chart-5/70"];

function hue(name: string): string {
  let h = 0;
  for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) >>> 0;
  return HUES[h % HUES.length];
}

/** An icicle of the merged profile, root on top. Clicking a frame zooms to it. */
export function FlameGraph({ root, unit }: { root: TelemetryFlameNode; unit: string }) {
  const { t, lang } = useI18n();
  const locale = intlLocale(lang);
  const [focus, setFocus] = useState<number[]>([]);
  const focused = flameNodeAt(root, focus);
  const cells = flameCells(root, focused.path);
  const depth = Math.max(0, ...cells.map((c) => c.depth)) + 1;
  const total = root.value || 1;
  const describe = (name: string, value: number) =>
    t("telemetryViews.profiling.frameTitle", { name, value: formatProfileValue(value, unit, locale), share: `${Math.round((value / total) * 1000) / 10}%` });

  return (
    <Card className="p-4">
      <div className="mb-2 flex flex-wrap items-center gap-2">
        <h3 className="text-body font-medium">{t("telemetryViews.profiling.flame")}</h3>
        {focused.path.length > 0 && (
          <Button variant="outline" size="sm" className="ml-auto" onClick={() => setFocus([])}>
            {t("telemetryViews.profiling.resetZoom")}
          </Button>
        )}
      </div>
      <div className="relative w-full overflow-hidden" style={{ height: depth * ROW }} role="group" aria-label={t("telemetryViews.profiling.flame")}>
        {cells.map((c) => (
          <button
            key={c.key}
            type="button"
            title={describe(c.name, c.value)}
            aria-label={describe(c.name, c.value)}
            onClick={() => setFocus(c.path)}
            className={cn(
              "absolute truncate border-r border-b border-background px-1 text-left font-mono text-[11px] leading-[19px] text-foreground hover:brightness-110",
              c.depth === 0 ? "bg-muted" : hue(c.name),
            )}
            style={{ left: `${c.x * 100}%`, width: `${c.w * 100}%`, top: c.depth * ROW, height: ROW }}
          >
            {c.w > 0.03 ? c.name : ""}
          </button>
        ))}
      </div>
    </Card>
  );
}
