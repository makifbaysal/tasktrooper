import { MessageSquarePlus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import type { Session } from "@/api";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { EmptyState } from "@/components/ui/empty-state";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { cn, formatRelativeDate } from "@/lib/utils";
import { useState } from "react";

interface SessionSidebarProps {
  sessions: Session[];
  activeSessionId: string | null;
  loading: boolean;
  onSelect: (id: string) => void;
  onCreate: () => void;
  onDelete: (id: string) => Promise<void>;
}

export function SessionSidebar({
  sessions,
  activeSessionId,
  loading,
  onSelect,
  onCreate,
  onDelete,
}: SessionSidebarProps) {
  const { t } = useI18n();
  const [deleteId, setDeleteId] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);

  return (
    <div
      data-testid="session-sidebar"
      className="flex h-full w-72 shrink-0 flex-col border-r border-border bg-muted/20 shadow-[var(--shadow-raised)]"
    >
      <div className="flex items-center justify-between border-b border-border p-3">
        <h2 className="text-heading font-semibold">{t("chatArea.chat.sidebar.title")}</h2>
        <Button size="sm" variant="outline" onClick={onCreate} className="gap-1.5">
          <MessageSquarePlus className="h-3.5 w-3.5" />
          {t("chatArea.chat.sidebar.new")}
        </Button>
      </div>

      <ScrollArea className="flex-1">
        {loading ? (
          <div className="space-y-2 p-3">
            {Array.from({ length: 5 }).map((_, i) => (
              <Skeleton key={i} className="h-14 w-full" />
            ))}
          </div>
        ) : sessions.length === 0 ? (
          <EmptyState
            icon={MessageSquarePlus}
            title={t("chatArea.chat.sidebar.emptyTitle")}
            description={t("chatArea.chat.sidebar.emptyDescription")}
            action={
              <Button size="sm" onClick={onCreate}>
                {t("chatArea.chat.sidebar.startChat")}
              </Button>
            }
            className="py-8"
          />
        ) : (
          <ul className="space-y-1 p-2">
            {sessions.map((session) => (
              <li key={session.id}>
                <div
                  className={cn(
                    "group flex items-start gap-1 rounded-lg transition-colors",
                    activeSessionId === session.id ? "bg-accent" : "hover:bg-accent/50",
                  )}
                >
                  <button
                    type="button"
                    onClick={() => onSelect(session.id)}
                    className="min-w-0 flex-1 px-3 py-2.5 text-left"
                  >
                    <div className="truncate text-sm font-medium">{session.title || t("chatArea.chat.sidebar.untitled")}</div>
                    <div
                      data-testid={`session-sidebar-date-${session.id}`}
                      className="text-caption text-muted-foreground"
                    >
                      {formatRelativeDate(session.updated_at)}
                    </div>
                  </button>
                  <Button
                    variant="ghost"
                    size="icon"
                    className="mt-1 mr-1 h-7 w-7 shrink-0 opacity-0 group-hover:opacity-100"
                    onClick={() => setDeleteId(session.id)}
                  >
                    <Trash2 className="h-3.5 w-3.5 text-destructive" />
                  </Button>
                </div>
              </li>
            ))}
          </ul>
        )}
      </ScrollArea>

      <ConfirmDialog
        open={deleteId !== null}
        onOpenChange={(open) => !open && setDeleteId(null)}
        title={t("chatArea.chat.sidebar.deleteTitle")}
        description={t("chatArea.chat.sidebar.deleteDescription")}
        confirmLabel={t("chatArea.chat.sidebar.deleteConfirm")}
        loading={deleting}
        onConfirm={async () => {
          if (!deleteId) return;
          setDeleting(true);
          try {
            await onDelete(deleteId);
          } catch (e) {
            // Without this the dialog closed as if the delete had worked: no
            // toast, the session still in the list, and an unhandled rejection.
            toast.error(e instanceof Error ? e.message : t("chatArea.chat.sidebar.deleteFailed"));
          } finally {
            setDeleting(false);
            setDeleteId(null);
          }
        }}
      />
    </div>
  );
}
