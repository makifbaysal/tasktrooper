import { memo, useLayoutEffect, useMemo, useRef, useState } from "react";
import { X } from "lucide-react";
import type { ClarificationRequest, SessionAction, SessionMessage } from "@/api";
import { AttachmentList } from "@/components/attachments/AttachmentList";
import { ClarificationCard } from "@/components/chat/ClarificationCard";
import { ClarificationSummary } from "@/components/chat/ClarificationSummary";
import { ReasoningBlock } from "@/components/chat/ReasoningBlock";
import { SessionActionCard } from "@/components/chat/SessionActionCard";
import { TypingIndicator } from "@/components/chat/TypingIndicator";
import { MarkdownContent } from "@/components/markdown/MarkdownContent";
import { useI18n } from "@/hooks/useI18n";
import { useStickToBottom } from "@/hooks/useStickToBottom";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/notice";
import {
  isAssistantErrorMessage,
  isQuotaQueuedMessage,
  isRateLimitMessage,
  quotaQueuedMessageBody,
  rateLimitMessageBody,
} from "@/lib/chat";
import { clarificationAnswers, parseClarificationAnswers } from "@/lib/clarification";
import type { QueuedMessage } from "@/lib/chatQueue";
import { groupActionsByMessage } from "@/lib/sessionActions";
import { cn, formatDate } from "@/lib/utils";

// A long session is hundreds of bubbles; rendering only the tail keeps every
// streamed chunk and poll cheap. Older ones are a click away.
const VISIBLE_TAIL = 120;
const NO_STRINGS: string[] = [];
const NO_ACTIONS: SessionAction[] = [];
const NO_QUEUED: QueuedMessage[] = [];
const NO_REASONING: Record<string, string[]> = {};

interface MessageListProps {
  messages: SessionMessage[];
  isAwaitingResponse?: boolean;
  streamingContent?: string | null;
  /** Reasoning the running turn has already closed, shown above the live bubble. */
  streamingReasoning?: string[];
  /** Reasoning kept per finished assistant message, folded shut by default. */
  reasoningByMessageId?: Record<string, string[]>;
  /** Board records the agents touched in this session, rendered inline. */
  actions?: SessionAction[];
  onOpenTask?: (action: SessionAction) => void;
  /** Answering the still-open question, if one is waiting. */
  onSubmitClarification?: (answer: string) => void;
  clarificationDisabled?: boolean;
  /** Messages typed while the agent was busy, sent when its turn ends. */
  queued?: QueuedMessage[];
  onRemoveQueued?: (id: string) => void;
}

// Memoized, like every row below it: the chat page re-renders on each
// keystroke in the composer and on each streamed chunk.
export const MessageList = memo(function MessageList({
  messages,
  isAwaitingResponse = false,
  streamingContent = null,
  streamingReasoning = NO_STRINGS,
  reasoningByMessageId = NO_REASONING,
  actions = NO_ACTIONS,
  onOpenTask,
  onSubmitClarification,
  clarificationDisabled = false,
  queued = NO_QUEUED,
  onRemoveQueued,
}: MessageListProps) {
  const { t } = useI18n();
  const [showAll, setShowAll] = useState(false);
  const showStreamingBubble = isAwaitingResponse || streamingContent !== null;

  const { byMessageId, trailing } = useMemo(
    () => groupActionsByMessage(messages, actions),
    [messages, actions],
  );
  const answers = useMemo(() => clarificationAnswers(messages), [messages]);

  const hiddenCount = showAll ? 0 : Math.max(0, messages.length - VISIBLE_TAIL);
  const visibleMessages = useMemo(
    () => (hiddenCount > 0 ? messages.slice(hiddenCount) : messages),
    [messages, hiddenCount],
  );

  const scrollRef = useRef<HTMLDivElement>(null);
  const pin = useStickToBottom(scrollRef, [messages, streamingContent, streamingReasoning, queued, showStreamingBubble]);
  const lastMessage = messages[messages.length - 1];
  const ownTail = `${lastMessage?.role === "user" ? lastMessage.id : ""}|${queued[queued.length - 1]?.id ?? ""}`;
  useLayoutEffect(() => {
    if (ownTail !== "|") pin();
  }, [ownTail, pin]);

  return (
    <div ref={scrollRef} className="flex-1 space-y-4 overflow-y-auto px-4 py-6 scrollbar-thin">
      {hiddenCount > 0 && (
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="h-7 w-full px-2 text-micro font-normal text-muted-foreground"
          onClick={() => setShowAll(true)}
        >
          {t("activityArea.feed.showEarlier", { count: hiddenCount })}
        </Button>
      )}

      {visibleMessages.map((message) => (
        <MessageRow
          key={message.id}
          message={message}
          reasoning={reasoningByMessageId[message.id] ?? NO_STRINGS}
          actions={byMessageId.get(message.id) ?? NO_ACTIONS}
          clarificationAnswer={answers.get(message.id) ?? null}
          onOpenTask={onOpenTask}
          onSubmitClarification={onSubmitClarification}
          clarificationDisabled={clarificationDisabled}
        />
      ))}

      {trailing.length > 0 && (
        <div className="flex justify-start">
          <div className="w-full max-w-[85%] space-y-2">
            {trailing.map((action) => (
              <SessionActionCard key={action.id} action={action} onOpenTask={onOpenTask} />
            ))}
          </div>
        </div>
      )}

      {showStreamingBubble && streamingReasoning.length > 0 && (
        // Open while the turn runs: this is the only sign of life a long
        // tool-calling run has, and folding it shut would leave the user
        // watching a typing dot for minutes.
        <div className="flex justify-start">
          <ReasoningBlock className="w-full max-w-[85%]" segments={streamingReasoning} defaultOpen />
        </div>
      )}

      {showStreamingBubble && <StreamingReply content={streamingContent} />}

      {queued.map((item, index) => (
        <div key={item.id} className="space-y-1">
          <div className="flex justify-end">
            <div
              data-testid="queued-message"
              className="min-w-0 max-w-[85%] rounded-2xl border border-dashed border-primary/50 bg-primary/10 px-4 py-3 text-foreground opacity-80"
            >
              <div className="mb-1 flex items-center gap-2">
                <span className="text-caption font-medium opacity-80">{t("chatArea.chat.message.you")}</span>
                <Badge variant="secondary">{t("chatArea.chat.message.queuedBadge")}</Badge>
                {onRemoveQueued && (
                  <Button
                    variant="ghost"
                    size="icon"
                    className="ml-auto h-6 w-6"
                    title={t("chatArea.chat.message.queuedRemove")}
                    aria-label={t("chatArea.chat.message.queuedRemove")}
                    onClick={() => onRemoveQueued(item.id)}
                  >
                    <X className="h-3.5 w-3.5" />
                  </Button>
                )}
              </div>
              <p className="whitespace-pre-wrap text-sm [overflow-wrap:anywhere]">{item.content}</p>
              {item.attachments.length > 0 && (
                <AttachmentList compact className="mt-2" attachments={item.attachments} />
              )}
            </div>
          </div>
          {index === queued.length - 1 && (
            <p className="text-right text-caption text-muted-foreground">
              {t("chatArea.chat.message.queuedHint")}
            </p>
          )}
        </div>
      ))}
    </div>
  );
});

interface MessageRowProps {
  message: SessionMessage;
  reasoning: string[];
  actions: SessionAction[];
  /** The user's reply to this message's clarification; null while it is open. */
  clarificationAnswer: string | null;
  onOpenTask?: (action: SessionAction) => void;
  onSubmitClarification?: (answer: string) => void;
  clarificationDisabled: boolean;
}

const MessageRow = memo(function MessageRow({
  message,
  reasoning,
  actions,
  clarificationAnswer,
  onOpenTask,
  onSubmitClarification,
  clarificationDisabled,
}: MessageRowProps) {
  const { t } = useI18n();
  const isError = message.role === "assistant" && isAssistantErrorMessage(message.content);
  const isRateLimit = message.role === "assistant" && isRateLimitMessage(message.content);
  const isQuotaQueued = message.role === "assistant" && isQuotaQueuedMessage(message.content);

  const renderClarification = (clarification: ClarificationRequest) => {
    if (clarificationAnswer === null) {
      // Still open. Without an answer handler the page owns the interactive
      // card, so render nothing here rather than a dead second copy.
      if (!onSubmitClarification) return null;
      return (
        <ClarificationCard
          clarification={clarification}
          disabled={clarificationDisabled}
          onSubmit={onSubmitClarification}
        />
      );
    }
    return (
      <ClarificationSummary
        clarification={clarification}
        answers={parseClarificationAnswers(clarification.questions, clarificationAnswer)}
      />
    );
  };

  // A rate limit is a condition of the account, not a crash: it gets the
  // shared warning callout with the server's own sentence, never the red
  // error bubble that used to show the provider's raw JSON.
  if (isRateLimit) {
    return (
      <div className="flex justify-start">
        <Notice className="max-w-[85%]" variant="warning" title={t("chatArea.chat.message.rateLimitTitle")}>
          <p className="whitespace-pre-wrap">{rateLimitMessageBody(message.content)}</p>
          <p className="mt-1 text-xs opacity-75">{t("chatArea.chat.message.rateLimitHint")}</p>
        </Notice>
      </div>
    );
  }
  // A queued turn is not a warning either — nothing is wrong with the
  // account, there is just nothing to show yet. `info`, not `warning`,
  // and no hint asking the user to do anything: the sweeper reruns this
  // turn on its own.
  if (isQuotaQueued) {
    return (
      <div className="flex justify-start">
        <Notice className="max-w-[85%]" variant="info" title={t("chatArea.chat.message.quotaQueuedTitle")}>
          <p className="whitespace-pre-wrap">{quotaQueuedMessageBody(message.content)}</p>
        </Notice>
      </div>
    );
  }
  return (
    <div className="space-y-2">
      {reasoning.length > 0 && (
        // Above the reply and outside its bubble: it came first in time,
        // and it is not the answer.
        <div className="flex justify-start">
          <ReasoningBlock className="w-full max-w-[85%]" segments={reasoning} />
        </div>
      )}
      <div className={cn("flex", message.role === "user" ? "justify-end" : "justify-start")}>
        <div
          className={cn(
            // min-w-0: a flex item's default min-width is its content
            // size, so a wide table or an unbroken long token in the
            // message would otherwise force this bubble past
            // max-w-[85%] instead of letting the content's own
            // wrap/scroll handling (prose-chat, whitespace-pre-wrap)
            // contain it.
            "min-w-0 max-w-[85%] rounded-2xl px-4 py-3 shadow-[var(--shadow-raised)]",
            message.role === "user"
              ? "bg-primary text-primary-foreground"
              : isError
                ? "border border-destructive/40 bg-destructive/10 text-destructive"
                : "border border-border bg-card text-card-foreground",
          )}
        >
          <div className="mb-1 flex items-center gap-2">
            <span className="text-caption font-medium opacity-80">
              {message.role === "user"
                ? t("chatArea.chat.message.you")
                : isError
                  ? t("chatArea.chat.message.error")
                  : t("chatArea.chat.message.assistant")}
            </span>
            <span className="text-micro opacity-60">{formatDate(message.created_at)}</span>
          </div>
          {message.role === "assistant" ? (
            <MarkdownContent content={message.content} className={cn(isError && "text-destructive")} />
          ) : (
            <p className="whitespace-pre-wrap text-sm [overflow-wrap:anywhere]">{message.content}</p>
          )}
          {(message.attachments ?? []).length > 0 && (
            <AttachmentList compact className="mt-2" attachments={message.attachments ?? []} />
          )}
        </div>
      </div>

      {actions.length > 0 && (
        <div className="flex justify-start">
          <div className="w-full max-w-[85%] space-y-2">
            {actions.map((action) => (
              <SessionActionCard key={action.id} action={action} onOpenTask={onOpenTask} />
            ))}
          </div>
        </div>
      )}

      {message.clarification && (
        <div className="flex justify-start">
          <div className="w-full max-w-[85%]">{renderClarification(message.clarification)}</div>
        </div>
      )}
    </div>
  );
});

// Not useDeferredValue: AgentChatPage already batches chunks per frame, and a
// deferred copy lagged the stream and outlived it under the finished message.
const StreamingReply = memo(function StreamingReply({ content }: { content: string | null }) {
  const { t } = useI18n();
  return (
    <div className="flex justify-start">
      <div className="min-w-0 max-w-[85%] rounded-2xl border border-border bg-card px-4 py-3 text-card-foreground shadow-[var(--shadow-raised)]">
        <div className="mb-1 flex items-center gap-2">
          <span className="text-caption font-medium opacity-80">{t("chatArea.chat.message.assistant")}</span>
        </div>
        {content ? <MarkdownContent content={content} /> : <TypingIndicator />}
      </div>
    </div>
  );
});
