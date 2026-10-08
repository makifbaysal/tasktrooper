import { useState } from "react";
import { FileText, X } from "lucide-react";
import { toast } from "sonner";
import type { AttachmentMeta } from "@/api";
import { ImageLightbox } from "@/components/attachments/ImageLightbox";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { downloadAttachmentBlob, useAttachmentBlob } from "@/components/attachments/useAttachmentBlob";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

interface AttachmentListProps {
  attachments: AttachmentMeta[];
  /** Present = each item gets a remove button. */
  onRemove?: (attachment: AttachmentMeta) => void;
  /** Chip-sized items for tight spots (composer, chat bubbles). */
  compact?: boolean;
  className?: string;
}

function isImage(meta: AttachmentMeta): boolean {
  return meta.content_type.startsWith("image/");
}

export function formatAttachmentSize(bytes: number): string {
  if (bytes >= 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  if (bytes >= 1024) return `${Math.round(bytes / 1024)} KB`;
  return `${bytes} B`;
}

/**
 * Molecule: grid of binary attachments. Images render an authenticated blob
 * thumbnail (an <img src> to the API would arrive without the Authorization
 * header) that opens full size in a lightbox; other types render a file chip
 * that downloads the file.
 */
export function AttachmentList({ attachments, onRemove, compact = false, className }: AttachmentListProps) {
  const { t } = useI18n();
  if (attachments.length === 0) return null;

  const download = async (meta: AttachmentMeta) => {
    try {
      await downloadAttachmentBlob(meta.id, meta.filename);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("frame.ui.attachments.openFailed"));
    }
  };

  return (
    <div className={cn("flex flex-wrap gap-2", className)}>
      {attachments.map((meta) => (
        <AttachmentItem
          key={meta.id}
          meta={meta}
          compact={compact}
          onDownload={() => download(meta)}
          onRemove={onRemove ? () => onRemove(meta) : undefined}
        />
      ))}
    </div>
  );
}

function AttachmentItem({
  meta,
  compact,
  onDownload,
  onRemove,
}: {
  meta: AttachmentMeta;
  compact: boolean;
  onDownload: () => void;
  onRemove?: () => void;
}) {
  const { t } = useI18n();
  const image = isImage(meta);
  // Only images fetch bytes eagerly (for the thumbnail); other types fetch on click.
  const { url, loading } = useAttachmentBlob(image ? meta.id : null);
  const [viewing, setViewing] = useState(false);
  const onOpen = image ? () => setViewing(true) : onDownload;

  return (
    <div
      className={cn(
        "group relative flex items-center gap-2 overflow-hidden rounded-lg border border-border bg-card",
        image && !compact ? "h-24 w-32 justify-center p-0" : compact ? "px-2 py-1" : "px-3 py-2",
      )}
    >
      <button
        type="button"
        onClick={onOpen}
        title={meta.filename}
        className={cn("flex min-w-0 items-center gap-2 text-left", image && !compact && "h-full w-full justify-center")}
      >
        {image ? (
          loading ? (
            <Spinner size="sm" />
          ) : url ? (
            <img
              src={url}
              alt={meta.filename}
              className={cn("object-cover", compact ? "h-8 w-8 rounded" : "h-full w-full")}
            />
          ) : (
            <FileText className={cn("shrink-0 text-muted-foreground", compact ? "h-3.5 w-3.5" : "h-4 w-4")} />
          )
        ) : (
          <FileText className={cn("shrink-0 text-muted-foreground", compact ? "h-3.5 w-3.5" : "h-4 w-4")} />
        )}
        {(!image || compact) && (
          <span className="min-w-0">
            <span className={cn("block truncate font-medium", compact ? "max-w-32 text-xs" : "max-w-48 text-sm")}>
              {meta.filename}
            </span>
            {!compact && (
              <span className="block text-xs text-muted-foreground">{formatAttachmentSize(meta.size_bytes)}</span>
            )}
          </span>
        )}
      </button>
      {onRemove && (
        <Button
          type="button"
          variant="ghost"
          size="icon"
          className={cn(
            "shrink-0",
            compact ? "h-5 w-5" : "h-6 w-6",
            image && !compact && "absolute right-1 top-1 bg-background/80 opacity-0 transition-opacity group-hover:opacity-100",
          )}
          title={t("frame.ui.attachments.remove")}
          onClick={onRemove}
        >
          <X className="h-3 w-3" />
        </Button>
      )}
      {image && (
        <ImageLightbox src={url} title={meta.filename} open={viewing} onOpenChange={setViewing} />
      )}
    </div>
  );
}
