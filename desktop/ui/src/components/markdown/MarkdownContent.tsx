import { memo, useMemo } from "react";
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

const REMARK_PLUGINS = [remarkGfm];

interface MarkdownContentProps {
  content: string;
  className?: string;
}

/**
 * The single markdown renderer for the app: task descriptions, comments,
 * task documents and chat messages all go through this so a fix here (or a
 * style tweak in `.prose-chat`) lands everywhere at once.
 *
 * Agent-authored markdown routinely contains wide GFM tables (a QA report's
 * #/Scenario/Result/Evidence columns, say). A bare `<table>` ignores its
 * container's width in auto table layout, so it — and with it the whole
 * comment card — spills past the page instead of staying put. Wrapping it in
 * its own `overflow-x: auto` box keeps the scrollbar local to the table.
 *
 * Memoized because parsing is the expensive part and its callers sit under
 * 2s polls: an agent's activity feed holds one block per narration message,
 * and re-parsing all of them on every tick pinned slower machines' CPU.
 */
export const MarkdownContent = memo(function MarkdownContent({ content, className }: MarkdownContentProps) {
  const { t } = useI18n();

  const components = useMemo<Components>(
    () => ({
      table: ({ node: _node, ...props }) => (
        <div
          className="prose-chat-table-wrap"
          tabIndex={0}
          role="group"
          aria-label={t("chatArea.markdown.table.scrollHint")}
        >
          <table {...props} />
        </div>
      ),
    }),
    [t],
  );

  if (!content.trim()) {
    return <p className="text-sm text-muted-foreground">{t("chatArea.markdown.empty")}</p>;
  }
  return (
    <div className={cn("prose-chat", className)}>
      <ReactMarkdown remarkPlugins={REMARK_PLUGINS} components={components}>
        {content}
      </ReactMarkdown>
    </div>
  );
});
