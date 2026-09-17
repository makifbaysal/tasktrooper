import { FileText, Trash2 } from "lucide-react";
import { useState } from "react";
import type { TaskDocument } from "@/api";
import { MarkdownContent } from "@/components/markdown/MarkdownContent";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { useI18n } from "@/hooks/useI18n";
import { formatRelativeDate } from "@/lib/utils";

interface TaskDocumentListProps {
  documents: TaskDocument[];
  agentNameMap: Record<string, string>;
  onDelete: (docId: string) => void;
}

/**
 * Task documents as files, not as inline walls of markdown.
 *
 * An agent-written spec or plan routinely runs to hundreds of lines, and
 * rendering every one of them inline pushed the rest of the drawer — runs,
 * pipeline, comments — off the bottom of the scroll. Each document is a card
 * with a one-line preview here; the full content opens in a reader dialog.
 */
export function TaskDocumentList({ documents, agentNameMap, onDelete }: TaskDocumentListProps) {
  const { t } = useI18n();
  const [openDocId, setOpenDocId] = useState<string | null>(null);
  // Read off the live list rather than a copy: the drawer polls every 2s, so a
  // document the agent rewrites while it is open updates in place.
  const openDoc = documents.find((doc) => doc.id === openDocId) ?? null;

  const authorOf = (doc: TaskDocument) =>
    doc.created_by_type === "agent"
      ? (agentNameMap[doc.created_by_id] ?? t("boardArea.components.taskDetail.agentFallback"))
      : t("boardArea.components.taskDetail.userAuthor");

  if (documents.length === 0) {
    return <p className="text-xs text-muted-foreground">{t("boardArea.components.taskDetail.docsEmpty")}</p>;
  }

  return (
    <>
      <div className="space-y-2">
        {documents.map((doc) => (
          <div
            key={doc.id}
            className="flex items-start gap-1 rounded-lg border border-border transition-colors hover:bg-muted/30"
          >
            <button
              type="button"
              className="flex min-w-0 flex-1 items-start gap-3 px-default py-compact text-left"
              onClick={() => setOpenDocId(doc.id)}
            >
              <span className="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-muted">
                <FileText className="h-4 w-4 text-muted-foreground" />
              </span>
              <span className="min-w-0 flex-1">
                <span className="block truncate text-sm font-medium">{doc.title}</span>
                <span className="mt-0.5 block text-[11px] text-muted-foreground">
                  {authorOf(doc)}
                  {" · "}
                  {t("boardArea.components.taskDetail.docWords", { count: wordCount(doc.content) })}
                  {" · "}
                  {t("boardArea.components.taskDetail.docUpdated", {
                    relative: formatRelativeDate(doc.updated_at),
                  })}
                </span>
                {doc.content.trim() && (
                  <span className="mt-1 line-clamp-1 block text-xs text-muted-foreground">
                    {previewOf(doc.content)}
                  </span>
                )}
              </span>
            </button>
            <Button
              variant="ghost"
              size="icon"
              className="mt-1.5 mr-1.5 h-7 w-7 shrink-0"
              onClick={() => onDelete(doc.id)}
            >
              <Trash2 className="h-3.5 w-3.5" />
            </Button>
          </div>
        ))}
      </div>

      <Dialog open={!!openDoc} onOpenChange={(next) => !next && setOpenDocId(null)}>
        <DialogContent className="flex max-h-[85vh] max-w-3xl flex-col">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <FileText className="h-4 w-4 shrink-0 text-muted-foreground" />
              <span className="min-w-0 truncate">{openDoc?.title}</span>
            </DialogTitle>
            {openDoc && (
              <p className="text-[11px] text-muted-foreground">
                {authorOf(openDoc)}
                {" · "}
                {t("boardArea.components.taskDetail.docUpdated", {
                  relative: formatRelativeDate(openDoc.updated_at),
                })}
              </p>
            )}
          </DialogHeader>
          {/* A plain overflow box, NOT ScrollArea. Radix positions its viewport
              absolutely, so the component contributes no intrinsic height —
              and inside a dialog that is only max-height (auto height, so there
              is no free space to distribute) `flex-1` resolved to 0. The reader
              then opened as a header with nothing under it: the document was
              fetched, rendered, and invisible. This box sizes to its content and
              starts scrolling at 70vh. */}
          <div className="min-h-0 max-h-[70vh] overflow-y-auto pr-3">
            {openDoc?.content.trim() ? (
              <MarkdownContent content={openDoc.content} />
            ) : (
              <p className="text-xs text-muted-foreground">
                {t("boardArea.components.taskDetail.docEmptyContent")}
              </p>
            )}
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}

// Markdown syntax carries no meaning in a one-line card preview, so it is
// stripped rather than rendered: fenced code, links, emphasis and list markers
// all collapse to their text.
function previewOf(content: string): string {
  const flat = content
    .replace(/```[\s\S]*?```/g, " ")
    .replace(/`([^`]*)`/g, "$1")
    .replace(/!?\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/^\s{0,3}#{1,6}\s+/gm, "")
    .replace(/^\s*[-*+]\s+/gm, "")
    .replace(/^\s*>\s?/gm, "")
    .replace(/[*_|]+/g, "")
    .replace(/\s+/g, " ")
    .trim();
  return flat.length > 160 ? `${flat.slice(0, 160)}…` : flat;
}

function wordCount(content: string): number {
  const trimmed = content.trim();
  return trimmed ? trimmed.split(/\s+/).length : 0;
}
