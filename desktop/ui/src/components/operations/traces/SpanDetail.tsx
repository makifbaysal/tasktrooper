import { Copy, X } from "lucide-react";
import { Fragment } from "react";
import { toast } from "sonner";
import { DetailFacts, DetailSection } from "@/components/operations/console/DetailPanel";
import { formatSpanDuration, spanAttributes, type WaterfallRow } from "@/components/operations/traces/waterfallLayout";
import { serviceBg } from "@/components/operations/traces/TraceWaterfall";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { useI18n } from "@/hooks/useI18n";
import { copyText } from "@/lib/clipboard";
import { intlLocale } from "@/lib/languages";
import { cn, formatDate } from "@/lib/utils";

interface SpanDetailProps {
  row: WaterfallRow;
  onClose: () => void;
  className?: string;
}

function Attributes({ items, label }: { items: Array<[string, string]>; label: string }) {
  return (
    <dl className="grid grid-cols-[minmax(6rem,max-content)_minmax(0,1fr)] gap-x-3 gap-y-1 font-mono text-micro" aria-label={label}>
      {items.map(([k, v]) => (
        <Fragment key={k}>
          <dt className="break-all text-muted-foreground">{k}</dt>
          <dd className="min-w-0 break-all">{v}</dd>
        </Fragment>
      ))}
    </dl>
  );
}

/** The side panel of one span: what it is, when it ran, how it ended, and
 * its own and its resource's attributes. */
export function SpanDetail({ row, onClose, className }: SpanDetailProps) {
  const { t, lang } = useI18n();
  const s = (key: string, params?: Record<string, string | number>) => t(`operations.telemetry.traces.span.${key}`, params);
  const locale = intlLocale(lang);
  const { span } = row;
  const attrs = spanAttributes(span);
  const kindKey = `operations.telemetry.traces.span.kinds.${span.kind}`;
  const kindText = span.kind ? (t(kindKey) === kindKey ? span.kind : t(kindKey)) : "";
  const copyId = async () => {
    if (await copyText(span.id)) toast.success(s("copied"));
    else toast.error(t("common.actionFailed"));
  };

  return (
    <Card className={cn("flex min-w-0 flex-col gap-4 p-4", className)} role="region" aria-labelledby="span-detail-title">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0 space-y-1">
          <h3 id="span-detail-title" className="font-mono text-body font-semibold break-all">
            {span.name || t("operations.telemetry.traces.waterfall.noName")}
          </h3>
          <p className="flex flex-wrap items-center gap-1.5 text-caption text-muted-foreground">
            <span className={cn("h-2 w-2 rounded-full", serviceBg(row.colorIndex))} aria-hidden />
            {span.service || t("operations.telemetry.traces.waterfall.noService")}
            {kindText && <Badge variant="secondary" className="px-1.5 py-0 text-micro">{kindText}</Badge>}
            {row.error ? (
              <Badge variant="destructive" className="px-1.5 py-0 text-micro">{s("statuses.error")}</Badge>
            ) : span.status === "ok" ? (
              <Badge variant="success" className="px-1.5 py-0 text-micro">{s("statuses.ok")}</Badge>
            ) : null}
          </p>
        </div>
        <Button variant="ghost" size="icon" className="h-7 w-7 shrink-0" onClick={onClose} aria-label={s("close")} title={s("close")}>
          <X aria-hidden />
        </Button>
      </div>

      {row.error && span.status_message && (
        <p className="rounded-md bg-destructive/10 px-2 py-1.5 font-mono text-micro break-words text-destructive">{span.status_message}</p>
      )}

      <DetailFacts
        items={[
          { label: s("duration"), value: <span className="font-mono tabular-nums">{formatSpanDuration(row.durationMs, locale)}</span> },
          { label: s("offset"), value: <span className="font-mono tabular-nums">+{formatSpanDuration(row.offsetMs, locale)}</span> },
          { label: s("started"), value: <span className="font-mono tabular-nums">{formatDate(span.start)}</span> },
          {
            label: s("id"),
            value: (
              <span className="inline-flex max-w-full items-center gap-1">
                <span className="font-mono break-all">{span.id}</span>
                <Button variant="ghost" size="icon" className="h-6 w-6 shrink-0 [&_svg]:size-3.5" onClick={() => void copyId()} aria-label={s("copy")} title={s("copy")}>
                  <Copy aria-hidden />
                </Button>
              </span>
            ),
          },
          ...(span.parent_id ? [{ label: s("parent"), value: <span className="font-mono break-all">{span.parent_id}</span> }] : []),
        ]}
      />

      <DetailSection title={s("attributes")}>
        {attrs.span.length > 0 ? <Attributes items={attrs.span} label={s("attributes")} /> : <p className="text-caption text-muted-foreground">{s("noAttributes")}</p>}
      </DetailSection>
      {attrs.resource.length > 0 && (
        <DetailSection title={s("resource")}>
          <Attributes items={attrs.resource} label={s("resource")} />
        </DetailSection>
      )}
    </Card>
  );
}
