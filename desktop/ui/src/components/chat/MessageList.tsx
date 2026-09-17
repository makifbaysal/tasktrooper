import { useMemo } from "react";
import type { ClarificationRequest, SessionAction, SessionMessage } from "@/api";
import { AttachmentList } from "@/components/attachments/AttachmentList";
import { ClarificationCard } from "@/components/chat/ClarificationCard";
import { ClarificationSummary } from "@/components/chat/ClarificationSummary";
import { ReasoningBlock } from "@/components/chat/ReasoningBlock";
import { SessionActionCard } from "@/components/chat/SessionActionCard";
import { TypingIndicator } from "@/components/chat/TypingIndicator";
import { MarkdownContent } from "@/components/markdown/MarkdownContent";
import { useI18n } from "@/hooks/useI18n";
import { Notice } from "@/components/ui/notice";
import { isAssistantErrorMessage, isRateLimitMessage, rateLimitMessageBody } from "@/lib/chat";
import { findClarificationAnswer, parseClarificationAnswers } from "@/lib/clarification";
import { groupActionsByMessage } from "@/lib/sessionActions";
import { cn, formatDate } from "@/lib/utils";

interface MessageListProps {
  messages: SessionMessage[];
  endRef?: React.RefObject<HTMLDivElement | null>;
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
}

export function MessageList({
  messages,
  endRef,
  isAwaitingResponse = false,
  streamingContent = null,
  streamingReasoning = [],
  reasoningByMessageId = {},
  actions = [],
  onOpenTask,
  onSubmitClarification,
  clarificationDisabled = false,
}: MessageListProps) {
  const { t } = useI18n();
  const showStreamingBubble = isAwaitingResponse || streamingContent !== null;
  const hasStreamingText = Boolean(streamingContent && streamingContent.length > 0);

  const { byMessageId, trailing } = useMemo(
    () => groupActionsByMessage(messages, actions),
    [messages, actions],
  );

  const renderClarification = (message: SessionMessage, clarification: ClarificationRequest) => {
    const answer = findClarificationAnswer(messages, message.id);
    if (answer === null) {
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
        answers={parseClarificationAnswers(clarification.questions, answer)}
      />
    );
  };

  return (
    <div className="flex-1 space-y-4 overflow-y-auto px-4 py-6 scrollbar-thin">
      {messages.map((message) => {
        const isError = message.role === "assistant" && isAssistantErrorMessage(message.content);
        const isRateLimit = message.role === "assistant" && isRateLimitMessage(message.content);
        const messageActions = byMessageId.get(message.id) ?? [];
        // A rate limit is a condition of the account, not a crash: it gets the
        // shared warning callout with the server's own sentence, never the red
        // error bubble that used to show the provider's raw JSON.
        if (isRateLimit) {
          return (
            <div key={message.id} className="flex justify-start">
              <Notice
                className="max-w-[85%]"
                variant="warning"
                title={t("chatArea.chat.message.rateLimitTitle")}
              >
                <p className="whitespace-pre-wrap">{rateLimitMessageBody(message.content)}</p>
                <p className="mt-1 text-xs opacity-75">{t("chatArea.chat.message.rateLimitHint")}</p>
              </Notice>
            </div>
          );
        }
        const reasoning = reasoningByMessageId[message.id] ?? [];
        return (
          <div key={message.id} className="space-y-2">
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
                  "min-w-0 max-w-[85%] rounded-2xl px-4 py-3 shadow-sm",
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

            {messageActions.length > 0 && (
              <div className="flex justify-start">
                <div className="w-full max-w-[85%] space-y-2">
                  {messageActions.map((action) => (
                    <SessionActionCard key={action.id} action={action} onOpenTask={onOpenTask} />
                  ))}
                </div>
              </div>
            )}

            {message.clarification && (
              <div className="flex justify-start">
                <div className="w-full max-w-[85%]">
                  {renderClarification(message, message.clarification)}
                </div>
              </div>
            )}
          </div>
        );
      })}

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

      {showStreamingBubble && (
        <div className="flex justify-start">
          <div className="min-w-0 max-w-[85%] rounded-2xl border border-border bg-card px-4 py-3 text-card-foreground shadow-sm">
            <div className="mb-1 flex items-center gap-2">
              <span className="text-caption font-medium opacity-80">
                {t("chatArea.chat.message.assistant")}
              </span>
            </div>
            {hasStreamingText ? (
              <MarkdownContent content={streamingContent ?? ""} />
            ) : (
              <TypingIndicator />
            )}
          </div>
        </div>
      )}

      <div ref={endRef} />
    </div>
  );
}
