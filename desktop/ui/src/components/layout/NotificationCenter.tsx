import { AlertTriangle, Bell, CheckCircle2, HelpCircle, MessageSquare } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { api, type BoardTask } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { EmptyState } from "@/components/ui/empty-state";
import { useActivity } from "@/hooks/useActivity";
import { useI18n } from "@/hooks/useI18n";
import { usePolling } from "@/hooks/usePolling";
import {
  laterCursor,
  partitionByCursor,
  toNotificationItems,
  type NotificationItem,
  type ReadCursor,
} from "@/lib/notifications";
import { cn, formatRelativeDate } from "@/lib/utils";

const READ_CURSOR_KEY = "tt.notificationCenter.readCursor";

function readCursor(): ReadCursor | null {
  try {
    const raw = window.localStorage.getItem(READ_CURSOR_KEY);
    return raw ? (JSON.parse(raw) as ReadCursor) : null;
  } catch {
    return null;
  }
}

function writeCursor(cursor: ReadCursor): void {
  try {
    window.localStorage.setItem(READ_CURSOR_KEY, JSON.stringify(cursor));
  } catch {
    /* private-mode storage or a quota error — the badge just outlives this tab */
  }
}

const KIND_ICON: Record<NotificationItem["kind"], typeof MessageSquare> = {
  comment: MessageSquare,
  review: CheckCircle2,
  uat: CheckCircle2,
  decision: AlertTriangle,
  question: HelpCircle,
};

interface NotificationCenterProps {
  onAgentSeen: (agentId: string, iso: string) => void;
}

export function NotificationCenter({ onAgentSeen }: NotificationCenterProps) {
  const { t } = useI18n();
  const navigate = useNavigate();
  const { items: activity } = useActivity(100, 5000);
  const [tasks, setTasks] = useState<BoardTask[]>([]);
  const [cursor, setCursor] = useState<ReadCursor | null>(() => readCursor());

  usePolling(
    async () => {
      const { tasks: fetched } = await api.listAllTasks();
      setTasks(fetched ?? []);
    },
    5000,
    true,
  );

  const items = toNotificationItems(activity, tasks);

  // First install (or first run after this shipped): nothing in the existing
  // history is "unread" — only what arrives after this seed is.
  useEffect(() => {
    if (cursor !== null || items.length === 0) return;
    const seeded = laterCursor(null, { lastEventId: items[0].id, lastEventAt: items[0].createdAt });
    setCursor(seeded);
    if (seeded) writeCursor(seeded);
  }, [cursor, items]);

  const { unread } = partitionByCursor(items, cursor);
  const unreadCount = unread.length;

  const advanceCursor = useCallback((next: ReadCursor) => {
    setCursor((prev) => {
      const merged = laterCursor(prev, next);
      if (merged) writeCursor(merged);
      return merged;
    });
  }, []);

  const handleItemClick = useCallback(
    (item: NotificationItem) => {
      navigate(`/board?task=${encodeURIComponent(item.taskId)}`);
      if (item.agentId) onAgentSeen(item.agentId, item.createdAt);
      advanceCursor({ lastEventId: item.id, lastEventAt: item.createdAt });
    },
    [navigate, onAgentSeen, advanceCursor],
  );

  const handleMarkAllRead = useCallback(() => {
    if (items.length === 0) return;
    advanceCursor({ lastEventId: items[0].id, lastEventAt: items[0].createdAt });
  }, [items, advanceCursor]);

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" className="relative" title={t("frame.layout.header.notificationCenter.title")}>
          <Bell className="h-4 w-4" />
          {unreadCount > 0 && (
            <Badge
              variant="destructive"
              aria-label={t("frame.layout.header.notificationCenter.unreadBadge", { count: unreadCount })}
              className="absolute -right-1 -top-1 flex h-4 min-w-4 items-center justify-center rounded-full px-1 text-[10px] leading-none"
            >
              {unreadCount > 9 ? t("frame.layout.header.notificationCenter.unreadBadgeOverflow") : unreadCount}
            </Badge>
          )}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" sideOffset={8} className="w-96 p-0">
        <div className="flex items-center justify-between px-3 py-2">
          <DropdownMenuLabel className="p-0">
            {t("frame.layout.header.notificationCenter.title")}
          </DropdownMenuLabel>
          <Button
            variant="ghost"
            size="sm"
            disabled={unreadCount === 0}
            onClick={handleMarkAllRead}
            className="h-auto px-2 py-1 text-xs"
          >
            {t("frame.layout.header.notificationCenter.markAllRead")}
          </Button>
        </div>
        <DropdownMenuSeparator className="m-0" />
        {/* Plain overflow-y-auto, not Radix ScrollArea: nesting ScrollArea's own
            pointer-capture/viewport primitives inside a Menu's roving-focus
            content intermittently swallowed clicks on the items underneath it. */}
        <div className="max-h-96 overflow-y-auto">
          <div className="p-1">
            {items.length === 0 ? (
              <EmptyState
                icon={Bell}
                title={t("frame.layout.header.notificationCenter.empty")}
                className="py-8"
              />
            ) : (
              items.map((item) => {
                const isUnread = unread.some((u) => u.id === item.id);
                const Icon = KIND_ICON[item.kind];
                return (
                  <DropdownMenuItem
                    key={item.id}
                    onSelect={() => handleItemClick(item)}
                    className={cn("flex items-start gap-2.5 whitespace-normal py-2", isUnread && "bg-muted/40")}
                  >
                    <Icon className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
                    <div className="min-w-0 flex-1">
                      <p className={cn("truncate text-sm", isUnread ? "font-semibold" : "font-normal")}>
                        {item.title}
                      </p>
                      <p className="line-clamp-2 text-xs text-muted-foreground">{item.body}</p>
                      <p className="mt-0.5 text-micro text-muted-foreground">{formatRelativeDate(item.createdAt)}</p>
                    </div>
                    {isUnread && <span className="mt-1.5 h-1.5 w-1.5 shrink-0 rounded-full bg-primary" />}
                  </DropdownMenuItem>
                );
              })
            )}
          </div>
        </div>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
