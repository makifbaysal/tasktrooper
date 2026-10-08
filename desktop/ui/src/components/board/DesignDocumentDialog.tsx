import { useEffect, useRef, useState } from "react";
import type { TaskDocument } from "@/api";
import { AnalysisFrame, type AnalysisFrameHandle } from "@/components/board/analysis/AnalysisFrame";
import { DesignPagesList } from "@/components/board/analysis/DesignPagesList";
import type { CanvasPage } from "@/components/board/analysis/srcdoc";
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
 * Molecule: one design document in a near-full-screen dialog, read-only, on
 * the same canvas as the review page with its page list. Agent-written HTML
 * goes through the same sandboxed frame — never into this page's DOM.
 */
export function DesignDocumentDialog({ document, onOpenChange }: DesignDocumentDialogProps) {
  return (
    <Dialog open={document !== null} onOpenChange={onOpenChange}>
      <DialogContent
        className="flex h-[92vh] w-[96vw] max-w-none flex-col gap-3 p-4 sm:p-6"
        aria-describedby={undefined}
      >
        <DialogHeader className="pr-8">
          <DialogTitle className="truncate">{document ? designDocumentLabel(document) : ""}</DialogTitle>
        </DialogHeader>
        {document && <DesignDocumentCanvas document={document} />}
      </DialogContent>
    </Dialog>
  );
}

function DesignDocumentCanvas({ document }: { document: TaskDocument }) {
  const { t } = useI18n();
  const { theme } = useTheme();
  const frameRef = useRef<AnalysisFrameHandle>(null);
  const [pages, setPages] = useState<CanvasPage[]>([]);
  const [page, setPage] = useState<string | null>(null);

  useEffect(() => {
    setPages([]);
    setPage(null);
  }, [document.id]);

  return (
    <div className="flex min-h-0 flex-1 overflow-hidden rounded-lg border border-border">
      <DesignPagesList
        className="hidden w-48 shrink-0 border-r border-border md:flex"
        pages={pages}
        activeId={page}
        onSelect={(id) => frameRef.current?.goToPage(id)}
      />
      <AnalysisFrame
        ref={frameRef}
        className="min-h-0 min-w-0 flex-1"
        document={document}
        theme={theme}
        title={t("designSystem.task.frameTitle", { title: designDocumentLabel(document) })}
        canvas
        onOutline={setPages}
        onPageChange={(id) => setPage(id)}
      />
    </div>
  );
}
