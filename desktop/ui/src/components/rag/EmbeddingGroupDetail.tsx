import { X } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { shortenPath } from "@/lib/embeddingMapTopics";

const SNIPPET_MAX = 220;
const PATH_MAX = 48;

export interface EmbeddingGroupDetailProps {
  title: string;
  subtitle?: string;
  color: string;
  count: number;
  share: string;
  files: { path: string; count: number }[];
  samples: { path: string; symbol?: string; snippet: string }[];
  onClose: () => void;
  labels: {
    files: string;
    samples: string;
    close: string;
    chunks: (count: number) => string;
  };
}

function truncate(snippet: string): string {
  const trimmed = snippet.trim();
  return trimmed.length <= SNIPPET_MAX ? trimmed : `${trimmed.slice(0, SNIPPET_MAX)}…`;
}

export function EmbeddingGroupDetail({
  title,
  subtitle,
  color,
  count,
  share,
  files,
  samples,
  onClose,
  labels,
}: EmbeddingGroupDetailProps) {
  return (
    <Card className="space-y-4 p-4 shadow-none">
      <div className="space-y-1">
        <div className="flex items-center gap-2">
          <span aria-hidden className="h-2.5 w-2.5 shrink-0 rounded-full" style={{ backgroundColor: color }} />
          <h3 className="min-w-0 flex-1 break-words text-sm font-medium">{title}</h3>
          <Badge variant="secondary">{share}</Badge>
          <Button
            type="button"
            variant="ghost"
            size="icon"
            className="h-7 w-7"
            aria-label={labels.close}
            title={labels.close}
            onClick={onClose}
          >
            <X className="h-4 w-4" />
          </Button>
        </div>
        {subtitle && <p className="break-all font-mono text-caption text-muted-foreground">{subtitle}</p>}
        <p className="text-caption text-muted-foreground">{labels.chunks(count)}</p>
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        <section className="min-w-0 space-y-2">
          <h4 className="text-xs font-medium text-muted-foreground">{labels.files}</h4>
          <ul className="space-y-1">
            {files.map((file) => (
              <li key={file.path} className="flex items-baseline justify-between gap-3 text-xs">
                <span className="min-w-0 truncate font-mono" title={file.path}>
                  {shortenPath(file.path, PATH_MAX)}
                </span>
                <span className="shrink-0 tabular-nums text-muted-foreground">{file.count}</span>
              </li>
            ))}
          </ul>
        </section>
        <section className="min-w-0 space-y-2">
          <h4 className="text-xs font-medium text-muted-foreground">{labels.samples}</h4>
          <ul className="space-y-3">
            {samples.map((sample, i) => (
              <li key={`${sample.path}:${i}`} className="space-y-1">
                <p className="break-all font-mono text-xs font-medium">{sample.path}</p>
                {sample.symbol && <p className="text-micro text-muted-foreground">{sample.symbol}</p>}
                <pre className="whitespace-pre-wrap break-words font-mono text-micro leading-snug text-foreground/80">
                  {truncate(sample.snippet)}
                </pre>
              </li>
            ))}
          </ul>
        </section>
      </div>
    </Card>
  );
}
