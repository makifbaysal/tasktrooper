import type { TaskDocument } from "@/api";
import { AnalysisFrame } from "@/components/board/analysis/AnalysisFrame";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { useI18n } from "@/hooks/useI18n";
import { useTheme } from "@/hooks/useTheme";
import { designDocumentLabel } from "@/lib/design-system";

interface DesignDocumentDialogProps {
  /** The document shown; null closes the dialog. */
  document: TaskDocument | null;
  onOpenChange: (open: boolean) => void;
}

/**
 * Molecule: one design document (a mockup, a design review) in a large dialog,
 * read-only. Agent-written HTML goes through the same sandboxed frame as the
 * analysis review page — never into this page's DOM.
 */
export function DesignDocumentDialog({ document, onOpenChange }: DesignDocumentDialogProps) {
  return (
    <Dialog open={document !== null} onOpenChange={onOpenChange}>
      <DialogContent
        className="flex h-[90vh] w-[95vw] max-w-6xl flex-col gap-3 p-4 sm:p-6"
        aria-describedby={undefined}
      >
        <DialogHeader className="pr-8">
          <DialogTitle className="truncate">{document ? designDocumentLabel(document) : ""}</DialogTitle>
        </DialogHeader>
        {document && <DesignDocumentFrame document={document} />}
      </DialogContent>
    </Dialog>
  );
}

function DesignDocumentFrame({ document }: { document: TaskDocument }) {
  const { t } = useI18n();
  const { theme } = useTheme();
  return (
    <AnalysisFrame
      className="min-h-0 flex-1 overflow-hidden rounded-lg border border-border"
      document={document}
      theme={theme}
      title={t("designSystem.task.frameTitle", { title: designDocumentLabel(document) })}
    />
  );
}
