import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/notice";
import { StatTile } from "@/components/ui/stat-tile";
import { useI18n } from "@/hooks/useI18n";
import { CHUNK_KINDS, type ChunkKind, type KindShare } from "@/lib/embeddingMapTopics";
import { formatCompact } from "@/lib/usage";
import { formatRelativeTime } from "@/lib/utils";

const GENERATED_WARNING_SHARE = 0.15;

export interface EmbeddingMapSummaryProps {
  source: "files" | "code";
  totalChunks: number;
  fileCount: number | null;
  topicCount: number;
  /** 0..1, share of sampled points inside a topic. */
  clusteredShare: number;
  indexedAt?: string;
  branch?: string;
  embeddingModel?: string;
  /** Null for the files source. */
  composition: KindShare[] | null;
  kindColors: Record<ChunkKind, string>;
  stale: boolean;
  /** Server text (English), shown small under the localized body. */
  staleDetail?: string;
  onColorByKind?: () => void;
  onReindex?: () => void;
  reindexing?: boolean;
}

export function EmbeddingMapSummary({
  source,
  totalChunks,
  fileCount,
  topicCount,
  clusteredShare,
  indexedAt,
  branch,
  embeddingModel,
  composition,
  kindColors,
  stale,
  staleDetail,
  onColorByKind,
  onReindex,
  reindexing = false,
}: EmbeddingMapSummaryProps) {
  const { t, lang } = useI18n();
  const s = (key: string, params?: Record<string, string | number>) =>
    t(`content.embeddingMap.summary.${key}`, params);
  const code = source === "code";
  const locale = lang === "tr" ? "tr-TR" : "en-US";

  const visibleKinds = composition ? composition.filter((entry) => entry.count > 0) : [];
  const compositionTotal = visibleKinds.reduce((sum, entry) => sum + entry.count, 0);
  const shareOf = (kind: ChunkKind) => composition?.find((entry) => entry.kind === kind)?.share ?? 0;
  const generatedShare = shareOf("mock") + shareOf("generated");
  const indexedFoot = [branch, embeddingModel].filter(Boolean).join(" · ");
  const kindLabel = (kind: ChunkKind) => t(`content.embeddingMap.kinds.${kind}`);
  const percent = (share: number) => Math.round(share * 100);

  return (
    <div className="space-y-4">
      {stale && (
        <Notice variant="warning" title={s("staleTitle")}>
          {s("staleBody")}
          {staleDetail && <span className="mt-1 block text-caption opacity-80">{staleDetail}</span>}
        </Notice>
      )}

      <div className="grid gap-4 sm:grid-cols-3">
        <StatTile
          label={s("statChunks")}
          value={formatCompact(totalChunks)}
          title={totalChunks.toLocaleString(locale)}
          foot={
            fileCount === null
              ? undefined
              : code
                ? s("statChunksFoot", { count: fileCount })
                : s("statDocumentsFoot", { count: fileCount })
          }
        />
        <StatTile
          label={s("statTopics")}
          value={topicCount}
          foot={s("statTopicsFoot", { pct: percent(clusteredShare) })}
        />
        {code && (
          <StatTile
            label={s("statIndexed")}
            value={indexedAt ? formatRelativeTime(indexedAt, lang) : "—"}
            foot={indexedFoot || undefined}
          />
        )}
      </div>

      {code && composition && compositionTotal > 0 && (
        <div className="space-y-2">
          <div className="flex flex-wrap items-baseline justify-between gap-2">
            <h3 className="text-sm font-medium">{s("compositionTitle")}</h3>
            <p className="text-xs text-muted-foreground">{s("compositionHint")}</p>
          </div>
          <div className="flex h-2.5 w-full gap-0.5 overflow-hidden rounded-[4px]">
            {CHUNK_KINDS.map((kind) => {
              const entry = visibleKinds.find((item) => item.kind === kind);
              if (!entry) return null;
              return (
                <div
                  key={kind}
                  title={`${kindLabel(kind)}: ${entry.count}`}
                  style={{
                    flexGrow: entry.count,
                    flexBasis: 0,
                    minWidth: 2,
                    backgroundColor: kindColors[kind],
                  }}
                />
              );
            })}
          </div>
          <ul className="flex flex-wrap gap-x-4 gap-y-1 text-caption text-muted-foreground">
            {CHUNK_KINDS.map((kind) => {
              const entry = visibleKinds.find((item) => item.kind === kind);
              if (!entry) return null;
              return (
                <li key={kind} className="flex items-center gap-1.5">
                  <span
                    aria-hidden
                    className="h-2 w-2 shrink-0 rounded-full"
                    style={{ backgroundColor: kindColors[kind] }}
                  />
                  {s("compositionItem", { label: kindLabel(kind), pct: percent(entry.share) })}
                </li>
              );
            })}
          </ul>
        </div>
      )}

      {code && generatedShare >= GENERATED_WARNING_SHARE && (
        <Notice variant="warning" title={s("generatedWarningTitle", { pct: percent(generatedShare) })}>
          {s("generatedWarningBody")}
          {(onColorByKind || onReindex) && (
            <div className="mt-2 flex flex-wrap gap-2">
              {onReindex && (
                <Button size="sm" variant="outline" onClick={onReindex} disabled={reindexing}>
                  {s("reindex")}
                </Button>
              )}
              {onColorByKind && (
                <Button size="sm" variant="outline" onClick={onColorByKind}>
                  {s("showKinds")}
                </Button>
              )}
            </div>
          )}
        </Notice>
      )}
    </div>
  );
}
