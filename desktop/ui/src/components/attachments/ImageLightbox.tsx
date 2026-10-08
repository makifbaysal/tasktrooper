import { Download } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { useI18n } from "@/hooks/useI18n";

interface ImageLightboxProps {
  /** An object URL from useAttachmentBlob; the lightbox never fetches. */
  src: string | null;
  title: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** The name a download saves under; defaults to the title. */
  filename?: string;
}

/**
 * Molecule: an attachment image at full size, inside the app. Opening it in a
 * new tab cannot work in the desktop shell — every popup is handed to the OS,
 * which has no idea what a blob: URL is — so the picture is shown here instead.
 */
export function ImageLightbox({ src, title, open, onOpenChange, filename }: ImageLightboxProps) {
  const { t } = useI18n();
  return (
    <Dialog open={open && !!src} onOpenChange={onOpenChange}>
      <DialogContent className="w-auto max-w-[min(92vw,1400px)] gap-3 p-3 sm:p-4">
        <div className="flex min-w-0 items-center justify-between gap-3 pr-8">
          <DialogTitle className="truncate text-caption font-medium">{title}</DialogTitle>
          {src && (
            <Button variant="outline" size="sm" asChild>
              <a href={src} download={filename ?? title}>
                <Download />
                {t("frame.ui.attachments.download")}
              </a>
            </Button>
          )}
        </div>
        <DialogDescription className="sr-only">{t("frame.ui.attachments.lightboxDescription")}</DialogDescription>
        {src && (
          <div className="flex max-h-[80vh] min-h-32 items-center justify-center overflow-auto rounded-lg bg-surface-sunken">
            <img src={src} alt={title} className="max-h-[80vh] max-w-full object-contain" />
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
