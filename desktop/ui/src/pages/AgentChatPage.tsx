import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useLocation, useNavigate, useParams } from "react-router-dom";
import { toast } from "sonner";
import {
  api,
  getStoredOrchestrate,
  type Agent,
  type AttachmentMeta,
  type FileRecord,
  type MessageMention,
  type SessionAction,
  type SessionMessage,
  type SessionRun,
} from "@/api";
import { Button } from "@/components/ui/button";
import { ChatTaskDrawer } from "@/components/chat/ChatTaskDrawer";
import { Composer } from "@/components/chat/Composer";
import type { MentionOption } from "@/components/chat/MentionMenu";
import { MessageList } from "@/components/chat/MessageList";
import { SessionActivityPanel } from "@/components/chat/SessionActivityPanel";
import { SessionSidebar } from "@/components/chat/SessionSidebar";
import { LeadChatHeader } from "@/components/chat/LeadChatHeader";
import { PageHeader } from "@/components/admin/PageHeader";
import { useWorkspaceOutlet } from "@/hooks/useWorkspaceOutlet";
import { useI18n } from "@/hooks/useI18n";
import { useMentionOptions } from "@/hooks/useMentionOptions";
import { useAgentSessions } from "@/hooks/useAgentSessions";
import { usePolling } from "@/hooks/usePolling";
import { isAbortError } from "@/lib/errors";
import { loadQueue, mergeQueued, saveQueue, type QueuedMessage } from "@/lib/chatQueue";
import { QUOTA_QUEUED_ERROR_TYPE, RATE_LIMIT_ERROR_TYPE } from "@/lib/chat";
import { AgentStreamError, countAssistantMessages, sendSessionMessageWithRecovery } from "@/lib/sessionSend";

function isActiveRunStatus(status: string): boolean {
  const normalized = status.toLowerCase();
  return normalized === "running" || normalized === "pending";
}

interface SendOptions {
  content?: string;
  mentions?: MessageMention[];
  attachments?: AttachmentMeta[];
  fileIds?: string[];
  fromQueue?: boolean;
}

function mergeServerMessages(
  serverMessages: SessionMessage[],
  currentMessages: SessionMessage[],
): SessionMessage[] {
  const pendingOptimistic = currentMessages.filter(
    (message) =>
      message.id.startsWith("temp-") &&
      !serverMessages.some(
        (serverMessage) =>
          serverMessage.role === message.role && serverMessage.content === message.content,
      ),
  );
  if (pendingOptimistic.length === 0) return serverMessages;
  return [...serverMessages, ...pendingOptimistic];
}

export function AgentChatPage() {
  const { t } = useI18n();
  const { agentId, sessionId } = useParams();
  const navigate = useNavigate();
  const location = useLocation();
  // The message a caller wants sent into this session, passed as router state
  // because the chat URL is the only place the page can be entered from.
  const autoSend = typeof location.state?.autoSend === "string" ? location.state.autoSend : "";
  const autoSendMentions: MessageMention[] = Array.isArray(location.state?.autoSendMentions)
    ? location.state.autoSendMentions
    : [];
  const workspace = useWorkspaceOutlet();
  const isLead = !!agentId && workspace?.leadAgent?.id === agentId;
  const team = useMemo(
    () => (workspace?.agents ?? []).filter((a) => a.id !== workspace?.leadAgent?.id),
    [workspace?.agents, workspace?.leadAgent?.id],
  );
  const endRef = useRef<HTMLDivElement>(null);
  const {
    sessions,
    loading: sessionsLoading,
    failed: sessionsFailed,
    refresh: refreshSessions,
    createSession,
    deleteSession,
  } = useAgentSessions(agentId);
  const [agent, setAgent] = useState<Agent | null>(null);
  const [messages, setMessages] = useState<SessionMessage[]>([]);
  const [actions, setActions] = useState<SessionAction[]>([]);
  const [openTaskId, setOpenTaskId] = useState<string | null>(null);
  const [activeSessionId, setActiveSessionId] = useState<string | null>(sessionId ?? null);
  // Mirrors activeSessionId so an in-flight send can tell whether the user has
  // since switched sessions (the closure's copy is stale by then).
  const activeSessionIdRef = useRef(activeSessionId);
  activeSessionIdRef.current = activeSessionId;
  const [loadingMessages, setLoadingMessages] = useState(false);
  // Which session's transcript is on screen. `loadingMessages` alone cannot
  // answer "are this session's messages in yet?" — it reads false on the first
  // render, before the load effect has even started.
  const [loadedSessionId, setLoadedSessionId] = useState<string | null>(null);
  const [input, setInput] = useState("");
  const [sending, setSending] = useState(false);
  const [stopping, setStopping] = useState(false);
  const [streamingContent, setStreamingContent] = useState<string | null>(null);
  // The reasoning stretches this turn has closed so far and — once it commits —
  // the same list filed under the assistant message it belonged to. Kept on the
  // page rather than on the message: the transcript the server returns holds the
  // answer alone, so this is the only place both halves of the turn are known.
  const [streamingReasoning, setStreamingReasoning] = useState<string[]>([]);
  const [reasoningByMessageId, setReasoningByMessageId] = useState<Record<string, string[]>>({});
  // Cancels the SSE request of the send that is currently in flight.
  const streamAbortRef = useRef<AbortController | null>(null);
  // Set while a user-requested stop is unwinding, so the send's own cleanup
  // leaves the partial answer on screen for handleStop to replace with the
  // persisted copy instead of blanking it the moment the request is aborted.
  const stopRequestedRef = useRef(false);
  const [files, setFiles] = useState<FileRecord[]>([]);
  const [selectedFileIds, setSelectedFileIds] = useState<string[]>([]);
  // Binary attachments queued in the composer for the next message.
  const [pendingAttachments, setPendingAttachments] = useState<AttachmentMeta[]>([]);
  const [sessionRuns, setSessionRuns] = useState<SessionRun[]>([]);
  // Which session's run list is in. Until it is, `hasActiveRun` reads false
  // for a session whose run is still going, and the queue must not flush into it.
  const [runsLoadedFor, setRunsLoadedFor] = useState<string | null>(null);
  // Tagged with its session so a queue is never saved under the session that
  // replaced it between the switch and the load effect.
  const [queueState, setQueueState] = useState<{ sessionId: string | null; items: QueuedMessage[] }>({
    sessionId: null,
    items: [],
  });
  const flushingRef = useRef(false);

  useEffect(() => {
    if (!agentId) return;
    api.getAgent(agentId).then(setAgent).catch(() => setAgent(null));
    api.listFiles().then((data) => setFiles(data.files ?? [])).catch(() => setFiles([]));
  }, [agentId]);

  const mentionOptions = useMentionOptions();
  // Entities picked from the autocomplete for the message being composed. A
  // ref, not state: only send-time reads it, no render depends on it.
  const selectedMentionsRef = useRef<MentionOption[]>([]);

  useEffect(() => {
    setActiveSessionId(sessionId ?? null);
    setMessages([]);
    setActions([]);
    setOpenTaskId(null);
    setStreamingContent(null);
    setStreamingReasoning([]);
    setReasoningByMessageId({});
    setInput("");
    setPendingAttachments([]);
    selectedMentionsRef.current = [];
    setSessionRuns([]);
    setRunsLoadedFor(null);
    // Without this the composer stays disabled in the new session while the old
    // session's send is still in flight.
    setSending(false);
    setStopping(false);
    stopRequestedRef.current = false;
    // The tokens of the session we just left have nowhere to go, and the
    // backend reads the closed stream as "nobody is listening" and cancels the
    // agent loop instead of billing the rest of the turn.
    streamAbortRef.current?.abort();
    streamAbortRef.current = null;
  }, [agentId, sessionId]);

  useEffect(() => {
    setQueueState({ sessionId: activeSessionId, items: activeSessionId ? loadQueue(activeSessionId) : [] });
  }, [activeSessionId]);

  useEffect(() => {
    if (queueState.sessionId) saveQueue(queueState.sessionId, queueState.items);
  }, [queueState]);

  const queue = queueState.sessionId === activeSessionId ? queueState.items : [];
  const updateQueue = (fn: (items: QueuedMessage[]) => QueuedMessage[]) =>
    setQueueState((s) => ({ ...s, items: fn(s.items) }));

  // Same reasoning for leaving the chat entirely — including a full page
  // navigation, where nothing else would ever close the request.
  useEffect(() => () => streamAbortRef.current?.abort(), []);

  // Navigating away is only correct when the session really is gone. A failed
  // list load must not be read as "deleted" — that used to eject the user from
  // a live conversation on any backend blip.
  useEffect(() => {
    if (sessionsLoading || sessionsFailed || !activeSessionId) return;
    if (!sessions.some((s) => s.id === activeSessionId)) {
      setActiveSessionId(null);
      setMessages([]);
      setActions([]);
      if (agentId) {
        navigate(`/agents/${agentId}/chat`, { replace: true });
      }
    }
  }, [sessions, sessionsLoading, sessionsFailed, activeSessionId, agentId, navigate]);

  const loadMessages = useCallback(
    async (id: string) => {
      if (!agentId) return;
      setLoadingMessages(true);
      try {
        const data = await api.getSession(id);
        if (data.session.agent_id !== agentId) {
          setMessages([]);
          setActions([]);
          setActiveSessionId(null);
          navigate(`/agents/${agentId}/chat`, { replace: true });
          return;
        }
        setMessages(data.messages ?? []);
        setActions(data.actions ?? []);
        setLoadedSessionId(id);
      } catch (e) {
        toast.error(e instanceof Error ? e.message : t("agentArea.chat.toast.loadFailed"));
        setMessages([]);
        setActions([]);
      } finally {
        setLoadingMessages(false);
      }
    },
    [agentId, navigate, t],
  );

  const loadSessionActivity = useCallback(async () => {
    if (!activeSessionId) return;
    try {
      const activity = await api.sessionActivity(activeSessionId);
      setSessionRuns(activity.runs ?? []);
      setRunsLoadedFor(activeSessionId);
    } catch {
      // Keep the last known runs: blanking them on a blip would read as "no run
      // is going" and let the message queue flush into a turn still running.
    }
  }, [activeSessionId]);

  useEffect(() => {
    if (activeSessionId) {
      void loadMessages(activeSessionId);
      void loadSessionActivity();
    }
  }, [activeSessionId, loadMessages, loadSessionActivity]);

  // Regaining focus is the moment a run that finished while this window was
  // backgrounded (or a whole relaunch, see the desktop shell's close-hides-not-
  // destroys fix) needs to surface: the local `sending` flag never survived
  // that gap, so only the server's own view of the session can be trusted.
  useEffect(() => {
    if (!activeSessionId) return;
    const refresh = () => {
      void loadMessages(activeSessionId);
      void loadSessionActivity();
    };
    const onVisibilityChange = () => {
      if (document.visibilityState === "visible") refresh();
    };
    window.addEventListener("focus", refresh);
    document.addEventListener("visibilitychange", onVisibilityChange);
    return () => {
      window.removeEventListener("focus", refresh);
      document.removeEventListener("visibilitychange", onVisibilityChange);
    };
  }, [activeSessionId, loadMessages, loadSessionActivity]);

  // The renderer can vanish (window close, App Nap) without this chat screen
  // ever unmounting cleanly, so the shell needs an explicit signal for which
  // session is open rather than inferring it from window focus alone.
  useEffect(() => {
    const runner = window.__tasktrooperDesktop?.runner;
    if (!runner) return;
    if (agentId && activeSessionId) {
      void runner.reportChatFocus({ agentId, sessionId: activeSessionId });
    }
    return () => {
      void runner.reportChatFocus(null);
    };
  }, [agentId, activeSessionId]);

  const hasActiveRun = sessionRuns.some((run) => isActiveRunStatus(run.status));
  const busy = sending || hasActiveRun;
  const shouldPollSession = !!activeSessionId && (sending || hasActiveRun);

  const pollSession = useCallback(async () => {
    if (!activeSessionId) return;
    try {
      const [sessionData, activity] = await Promise.all([
        api.getSession(activeSessionId),
        api.sessionActivity(activeSessionId),
      ]);
      setMessages((current) => mergeServerMessages(sessionData.messages ?? [], current));
      setActions(sessionData.actions ?? []);
      setSessionRuns(activity.runs ?? []);
      setRunsLoadedFor(activeSessionId);
    } catch {
      /* retry on next tick */
    }
  }, [activeSessionId]);

  usePolling(pollSession, 2000, shouldPollSession);
  usePolling(loadSessionActivity, 2000, !!activeSessionId && !shouldPollSession);

  useEffect(() => {
    endRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages, streamingContent, queue]);

  const selectSession = (id: string) => {
    setActiveSessionId(id);
    if (agentId) {
      navigate(`/agents/${agentId}/chat/${id}`, { replace: true });
    }
  };

  const handleCreateSession = async () => {
    if (!agentId) return;
    try {
      const title = agent ? t("agentArea.chat.newSessionTitle", { name: agent.name }) : t("agentArea.chat.newSession");
      const session = await createSession(title);
      selectSession(session.id);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("agentArea.chat.toast.sessionCreateFailed"));
    }
  };

  const handleSend = async (options: SendOptions = {}) => {
    const { fromQueue = false } = options;
    const isOverride = options.content !== undefined;
    const ownsComposer = !isOverride && !fromQueue;
    const content = (options.content ?? input).trim();
    const sessionAtSend = activeSessionId;
    if (!sessionAtSend || !content) return;
    // Only tags whose @Name still appears in the outgoing text count — the
    // user may have deleted a tag after picking it from the autocomplete.
    const contentFold = content.toLocaleLowerCase("tr");
    const mentions: MessageMention[] =
      options.mentions ??
      selectedMentionsRef.current
        .filter((m) => contentFold.includes(`@${m.name.toLocaleLowerCase("tr")}`))
        .map((m) => ({ kind: m.kind, id: m.id, name: m.name }));
    if (ownsComposer && busy) {
      updateQueue((items) => [
        ...items,
        {
          id: `queued-${Date.now()}-${items.length}`,
          content,
          mentions,
          attachments: pendingAttachments,
          fileIds: selectedFileIds,
          createdAt: new Date().toISOString(),
        },
      ]);
      setInput("");
      setPendingAttachments([]);
      selectedMentionsRef.current = [];
      return;
    }
    if (sending) return;
    if (ownsComposer) setInput("");
    setSending(true);
    setStreamingContent(null);
    setStreamingReasoning([]);
    const baselineAssistantCount = countAssistantMessages(messages);
    const optimistic: SessionMessage = {
      id: `temp-${Date.now()}`,
      role: "user",
      content,
      created_at: new Date().toISOString(),
    };
    setMessages((prev) => [...prev, optimistic]);
    // Every write below is gated on the user still being in the session this
    // send belongs to — otherwise switching sessions mid-send paints session
    // A's reply into session B.
    const stillHere = () => activeSessionIdRef.current === sessionAtSend;
    // Snapshot + clear like the input text: a failed send restores them below
    // so nothing has to be re-attached from scratch.
    const attachmentsAtSend = fromQueue ? (options.attachments ?? []) : pendingAttachments;
    const attachmentIds = attachmentsAtSend.map((a) => a.id);
    if (ownsComposer) setPendingAttachments([]);
    const abort = new AbortController();
    streamAbortRef.current = abort;
    let reasoningSoFar: string[] = [];
    try {
      const result = await sendSessionMessageWithRecovery(sessionAtSend, content, {
        fileIds: fromQueue ? (options.fileIds ?? []) : selectedFileIds,
        attachmentIds: isOverride && !fromQueue ? undefined : attachmentIds,
        // Off by default, which is what makes the reply stream: the backend
        // refuses to stream an orchestrated turn, so forcing this on meant the
        // web client always waited for one whole answer. iOS has always sent
        // the fast path, and `fast_path: true` in the backend config says chat
        // should skip orchestration unless the caller asks for it. The stored
        // preference stays as the escape hatch for turns that want a plan.
        orchestrate: getStoredOrchestrate(),
        baselineAssistantCount,
        mentions,
        signal: abort.signal,
        onProgress: ({ answer, reasoning }) => {
          if (!stillHere()) return;
          // Mirrored into a plain local as well: the commit below runs in this
          // same closure and needs the final list, which the state variable
          // captured at render time cannot give it.
          reasoningSoFar = reasoning;
          setStreamingContent(answer);
          setStreamingReasoning(reasoning);
        },
      });
      if (!stillHere()) return;
      if (ownsComposer) selectedMentionsRef.current = [];
      setMessages(result.messages);
      setActions(result.actions);
      // File this turn's reasoning under the message it produced, so it stays
      // reachable — folded — instead of disappearing with the streaming bubble.
      // The last assistant row is that message: the transcript was just reloaded
      // and this reply is what was appended to it.
      const answered = [...result.messages].reverse().find((m) => m.role === "assistant");
      if (answered && reasoningSoFar.length > 0) {
        const segments = reasoningSoFar;
        setReasoningByMessageId((prev) => ({ ...prev, [answered.id]: segments }));
      }
      // Cleared in the same batch that commits the reply: leaving it for the
      // finally below would show the streaming bubble and the finished message
      // side by side across the await that follows.
      setStreamingContent(null);
      if (result.status === "recovered") {
        toast.info(t("agentArea.chat.toast.recovered"));
      }
      // backend may have just set this chat's auto-generated title
      void refreshSessions();
      await loadSessionActivity();
    } catch (e) {
      // We cancelled this ourselves (session switch, or the user left the
      // chat); there is nothing to report and nothing left to render into.
      if (isAbortError(e)) return;
      // A run the backend failed arrives as an error frame, not as reply text.
      // Surfacing it here — and dropping the partial content instead of
      // committing it — is what keeps it out of the transcript as if the agent
      // had said it; the reload below picks up the backend's own "**Error:**"
      // line, which renders as an error bubble.
      // A provider rate limit is the exception: it is a condition of the
      // account with an action attached, so it is a warning carrying the
      // server's own sentence, not a red failure.
      const isQuotaQueued = e instanceof AgentStreamError && e.type === QUOTA_QUEUED_ERROR_TYPE;
      if (isQuotaQueued) {
        // Not a failure at all — session.Service already queued this exact
        // turn (see SessionQuotaSweeper) and will answer it on its own once
        // the usage limit lifts. A calm info toast, and nothing handed back
        // to the composer: there is nothing here for the user to retry.
        toast.info(e.message || t("chatArea.chat.message.quotaQueuedTitle"));
      } else if (e instanceof AgentStreamError && e.type === RATE_LIMIT_ERROR_TYPE) {
        toast.warning(e.message || t("chatArea.chat.message.rateLimitTitle"));
      } else {
        toast.error(e instanceof Error ? e.message : t("agentArea.chat.toast.sendFailed"));
      }
      if (!stillHere()) return;
      if (!isQuotaQueued) {
        // Hand the text back so the send can be retried — reloading the server
        // transcript drops the optimistic bubble, and the user would otherwise
        // have to retype from memory.
        if (ownsComposer) setInput((prev) => prev || content);
        // Same for the queued attachments: they are already uploaded, only the
        // message that was to carry them failed.
        if (ownsComposer) setPendingAttachments((prev) => (prev.length > 0 ? prev : attachmentsAtSend));
        if (fromQueue) {
          // The composer may hold something newer by now, so this appends
          // instead of the keep-what-is-there rule above.
          setInput((prev) => (prev.trim() ? `${content}\n\n${prev}` : content));
          setPendingAttachments((prev) => [
            ...attachmentsAtSend,
            ...prev.filter((a) => !attachmentsAtSend.some((b) => b.id === a.id)),
          ]);
          selectedMentionsRef.current = [
            ...selectedMentionsRef.current,
            ...mentions.filter(
              (m) => !selectedMentionsRef.current.some((x) => x.kind === m.kind && x.id === m.id),
            ),
          ];
        }
      }
      await loadMessages(sessionAtSend);
    } finally {
      if (streamAbortRef.current === abort) streamAbortRef.current = null;
      if (stillHere()) {
        // A user-requested stop owns this cleanup: it keeps the half-written
        // answer visible until the reload has the server's persisted copy of it,
        // so the text does not blink out and back in.
        if (!stopRequestedRef.current) setStreamingContent(null);
        setSending(false);
      }
    }
  };

  // Stop the turn in flight. Two halves, in this order: the server ends the run
  // (writes the cancelled status and keeps the partial answer), then the SSE
  // request is aborted locally. Aborting alone would only stop us listening —
  // the run would finish server-side and land in the transcript afterwards.
  const handleStop = async () => {
    const sessionAtStop = activeSessionId;
    if (!sessionAtStop || !busy || stopping) return;
    setStopping(true);
    stopRequestedRef.current = true;
    // The user stopped to change course, so nothing queued goes out on its own.
    if (queue.length > 0) {
      const returned = mergeQueued(queue);
      updateQueue(() => []);
      setInput((prev) => (prev.trim() ? `${prev}\n\n${returned.content}` : returned.content));
      setPendingAttachments((prev) => [
        ...prev,
        ...returned.attachments.filter((a) => !prev.some((b) => b.id === a.id)),
      ]);
      selectedMentionsRef.current = [
        ...selectedMentionsRef.current,
        ...returned.mentions.filter(
          (m) => !selectedMentionsRef.current.some((x) => x.kind === m.kind && x.id === m.id),
        ),
      ];
      toast.info(t("chatArea.chat.message.queuedReturned"));
    }
    try {
      // `cancelled: false` means the answer beat the button — a race the user
      // cannot win from the UI and has nothing to fix, so it gets no error toast.
      const { cancelled } = await api.cancelSessionRun(sessionAtStop);
      if (cancelled) toast.success(t("agentArea.chat.toast.runStopped"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("agentArea.chat.toast.runStopFailed"));
    } finally {
      streamAbortRef.current?.abort();
      streamAbortRef.current = null;
      if (activeSessionIdRef.current === sessionAtStop) {
        setSending(false);
        // Reload before clearing the streaming bubble: the server persisted the
        // partial answer as a real message, and this is what swaps one for the
        // other without the transcript flashing empty.
        await loadMessages(sessionAtStop);
        await loadSessionActivity();
        setStreamingContent(null);
      }
      stopRequestedRef.current = false;
      setStopping(false);
    }
  };

  // A caller hands its opening message over as router state, because the chat
  // URL is all it knows about the session it wants opened. The ref covers
  // StrictMode's double effect; the state is dropped from the history entry
  // itself (React Router keeps it under `usr`) so a reload or a back-navigation
  // does not send it again. Not navigate(): a navigation gives `navigate` a new
  // identity, which reloads the transcript mid-send.
  const autoSendRef = useRef("");
  useEffect(() => {
    if (!autoSend || !activeSessionId || loadedSessionId !== activeSessionId) return;
    if (sending || stopping) return;
    const key = `${activeSessionId}\u0000${autoSend}`;
    if (autoSendRef.current === key) return;
    autoSendRef.current = key;
    // handleSend reads the picked entities from this ref and keeps those whose
    // @Name is still in the text, exactly as for a message typed here.
    selectedMentionsRef.current = autoSendMentions;
    void handleSend({ content: autoSend });
    window.history.replaceState({ ...(window.history.state ?? {}), usr: null }, "");
    // eslint-disable-next-line react-hooks/exhaustive-deps -- handleSend is recreated every render; the key ref guards re-sends
  }, [autoSend, activeSessionId, loadedSessionId, sending, stopping]);

  // Runs after the effects above on purpose: autoSend takes a free session
  // before a stored queue does.
  useEffect(() => {
    if (busy || stopping || queue.length === 0 || flushingRef.current) return;
    if (!activeSessionId || loadedSessionId !== activeSessionId || runsLoadedFor !== activeSessionId) return;
    flushingRef.current = true;
    const merged = mergeQueued(queue);
    updateQueue(() => []);
    void handleSend({
      content: merged.content,
      mentions: merged.mentions,
      attachments: merged.attachments,
      fileIds: merged.fileIds,
      fromQueue: true,
    }).finally(() => {
      flushingRef.current = false;
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps -- handleSend is recreated every render; flushingRef guards re-entry
  }, [busy, stopping, queue, activeSessionId, loadedSessionId, runsLoadedFor]);

  if (!agentId) return null;

  const leadAgent = agent ?? workspace?.leadAgent ?? null;
  const headerActions = (
    <div className="flex items-center gap-2">
      <Button variant="outline" asChild>
        <Link to={`/agents/${agentId}/settings`}>{t("agentArea.chat.settingsLink")}</Link>
      </Button>
      <Button variant="outline" asChild>
        <Link to={`/agents/${agentId}/memory`}>{t("agentArea.chat.memoryLink")}</Link>
      </Button>
      <Button variant="outline" asChild>
        <Link to={`/agents/${agentId}/performance`}>{t("agentArea.chat.performanceLink")}</Link>
      </Button>
    </div>
  );

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="shrink-0 border-b border-border px-6 py-4">
        {isLead && leadAgent ? (
          <LeadChatHeader agent={leadAgent} team={team} actions={headerActions} />
        ) : (
          <PageHeader title={agent?.name ?? t("agentArea.chat.header.title")} action={headerActions} />
        )}
      </div>

      <div className="relative flex min-h-0 flex-1 overflow-hidden">
        <SessionSidebar
          sessions={sessions}
          activeSessionId={activeSessionId}
          loading={sessionsLoading}
          onSelect={selectSession}
          onCreate={handleCreateSession}
          onDelete={async (id) => {
            await deleteSession(id);
            if (activeSessionId === id) {
              setActiveSessionId(null);
              navigate(`/agents/${agentId}/chat`, { replace: true });
            }
          }}
        />

        <div className="flex min-h-0 min-w-0 flex-1 flex-col">
          {activeSessionId ? (
            <>
              <MessageList
                messages={messages}
                endRef={endRef}
                isAwaitingResponse={sending || hasActiveRun}
                streamingContent={streamingContent}
                streamingReasoning={streamingReasoning}
                reasoningByMessageId={reasoningByMessageId}
                actions={actions}
                onOpenTask={(action) => setOpenTaskId(action.entity_id ?? null)}
                onSubmitClarification={(answer) => handleSend({ content: answer })}
                clarificationDisabled={sending}
                queued={queue}
                onRemoveQueued={(id) => updateQueue((items) => items.filter((i) => i.id !== id))}
              />
              <Composer
                value={input}
                onChange={setInput}
                onSend={() => handleSend()}
                sending={busy}
                queueing
                onStop={() => void handleStop()}
                stopping={stopping}
                files={files}
                selectedFileIds={selectedFileIds}
                onToggleFile={(id, checked) =>
                  setSelectedFileIds((prev) =>
                    checked ? [...prev, id] : prev.filter((fid) => fid !== id),
                  )
                }
                pendingAttachments={pendingAttachments}
                onPendingAttachmentsChange={setPendingAttachments}
                mentionOptions={mentionOptions}
                onMentionSelect={(option) => {
                  const cur = selectedMentionsRef.current;
                  if (!cur.some((m) => m.kind === option.kind && m.id === option.id)) {
                    selectedMentionsRef.current = [...cur, option];
                  }
                }}
              />
            </>
          ) : (
            <div className="flex flex-1 items-center justify-center p-8 text-center text-muted-foreground">
              {loadingMessages ? t("agentArea.chat.loading") : t("agentArea.chat.emptyState")}
            </div>
          )}
        </div>

        {activeSessionId && (
          <SessionActivityPanel
            key={activeSessionId}
            sessionId={activeSessionId}
            agentName={agent?.name ?? leadAgent?.name ?? ""}
            lead={isLead}
            runs={sessionRuns}
            sending={sending}
            onStop={() => void handleStop()}
            stopping={stopping}
          />
        )}
      </div>

      <ChatTaskDrawer taskId={openTaskId} onClose={() => setOpenTaskId(null)} />
    </div>
  );
}
