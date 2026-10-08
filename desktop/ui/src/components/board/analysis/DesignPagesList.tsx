import { FileStack } from "lucide-react";
import type { ReactNode } from "react";
import type { CanvasPage } from "@/components/board/analysis/srcdoc";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

interface DesignPagesListProps {
  pages: CanvasPage[];
  /** The page in view, highlighted; null when none is. */
  activeId: string | null;
  onSelect: (id: string) => void;
  /** Rendered under the heading — the compare view's "keep pages in step" switch. */
  toolbar?: ReactNode;
  className?: string;
}

/**
 * Molecule: a design canvas's pages, Figma's left-hand list — one per `<h2>`
 * section of the document, the one in view highlighted, a click frames it.
 */
export function DesignPagesList({ pages, activeId, onSelect, toolbar, className }: DesignPagesListProps) {
  const { t } = useI18n();

  return (
    <nav aria-label={t("analysisReview.design.pages.title")} className={cn("flex min-h-0 flex-col bg-background", className)}>
      <div className="space-y-2 border-b border-border px-3 py-3">
        <h2 className="flex items-center gap-2 text-body font-semibold">
          <FileStack className="h-4 w-4 text-muted-foreground" aria-hidden />
          {t("analysisReview.design.pages.title")}
        </h2>
        {toolbar}
      </div>
      <div className="min-h-0 flex-1 space-y-0.5 overflow-y-auto p-2 scrollbar-thin">
        {pages.length === 0 ? (
          <p className="px-2 py-3 text-caption text-muted-foreground">{t("analysisReview.design.pages.empty")}</p>
        ) : (
          pages.map((page) => {
            const active = page.id === activeId;
            return (
              <Button
                key={page.id}
                variant="ghost"
                size="sm"
                aria-current={active ? "page" : undefined}
                onClick={() => onSelect(page.id)}
                title={page.title}
                className={cn(
                  "h-auto w-full justify-start whitespace-normal px-2 py-1.5 text-left font-normal",
                  active && "bg-accent font-medium text-accent-foreground",
                )}
              >
                <span className="line-clamp-2">{page.title}</span>
              </Button>
            );
          })
        )}
      </div>
    </nav>
  );
}
