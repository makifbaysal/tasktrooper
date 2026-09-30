package session

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type cancelSessionStore struct {
	port.SessionStore
	session domain.Session

	// rejectCancelledCtx makes AppendMessage fail the write when the ctx it
	// receives is already done — the shape a raw, un-detached ctx would have
	// if a dropped SSE connection cancelled it out from under a still-running
	// turn. It catches a caller that passes that ctx straight to the store
	// instead of a persistCtx-derived one.
	rejectCancelledCtx bool

	mu       sync.Mutex
	appended []domain.SessionMessage
}

func (s *cancelSessionStore) Get(_ context.Context, id uuid.UUID) (domain.Session, error) {
	if id != s.session.ID {
		return domain.Session{}, errors.New("no such session")
	}
	return s.session, nil
}

func (s *cancelSessionStore) ListMessages(context.Context, uuid.UUID) ([]domain.SessionMessage, error) {
	return s.messages(), nil
}

func (s *cancelSessionStore) AppendMessage(
	ctx context.Context,
	sessionID uuid.UUID,
	role domain.Role,
	content string,
	_ []byte,
	_ []byte,
) (domain.SessionMessage, error) {
	if s.rejectCancelledCtx && ctx.Err() != nil {
		return domain.SessionMessage{}, ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	msg := domain.SessionMessage{ID: uuid.New(), SessionID: sessionID, Role: role, Content: content}
	s.appended = append(s.appended, msg)
	return msg, nil
}

func (s *cancelSessionStore) messages() []domain.SessionMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.appended)
}

type contractActivityStore struct {
	port.ActivityStore
	mu     sync.Mutex
	status map[uuid.UUID]string

	bySession map[uuid.UUID][]uuid.UUID
}

func newContractActivityStore() *contractActivityStore {
	return &contractActivityStore{
		status:    make(map[uuid.UUID]string),
		bySession: make(map[uuid.UUID][]uuid.UUID),
	}
}

func (s *contractActivityStore) CreateRun(_ context.Context, sessionID *uuid.UUID, requestID, model string) (domain.SessionRun, error) {
	run := domain.SessionRun{
		ID: uuid.New(), SessionID: sessionID, RequestID: requestID, Model: model,
		Status: domain.TaskAgentRunStatusRunning,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status[run.ID] = run.Status
	if sessionID != nil {
		s.bySession[*sessionID] = append(s.bySession[*sessionID], run.ID)
	}
	return run, nil
}

func (s *contractActivityStore) CompleteRun(_ context.Context, runID uuid.UUID, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status[runID] == domain.TaskAgentRunStatusCancelled {
		return nil
	}
	s.status[runID] = status
	return nil
}

func (s *contractActivityStore) CancelRun(_ context.Context, runID uuid.UUID) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status[runID] != domain.TaskAgentRunStatusRunning {
		return false, nil
	}
	s.status[runID] = domain.TaskAgentRunStatusCancelled
	return true, nil
}

func (s *contractActivityStore) AppendStep(context.Context, uuid.UUID, string, []byte) error {
	return nil
}

func (s *contractActivityStore) RunStatus(_ context.Context, runID uuid.UUID) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status[runID], nil
}

func (s *contractActivityStore) ListRunsBySession(_ context.Context, sessionID uuid.UUID, _ int) ([]domain.SessionRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := s.bySession[sessionID]
	out := make([]domain.SessionRun, 0, len(ids))
	for _, id := range ids {
		sid := sessionID
		out = append(out, domain.SessionRun{ID: id, SessionID: &sid, Status: s.status[id]})
	}
	return out, nil
}

func (s *contractActivityStore) statuses() map[uuid.UUID]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[uuid.UUID]string, len(s.status))
	for id, status := range s.status {
		out[id] = status
	}
	return out
}

type blockingStreamLLM struct {
	port.LLMClient
	token     string
	entered   chan struct{}
	enterOnce sync.Once
	streams   atomic.Int32
}

func (l *blockingStreamLLM) Chat(context.Context, domain.AgentRequest) (domain.AgentResponse, error) {
	return domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant}}, nil
}

func (l *blockingStreamLLM) ChatStream(ctx context.Context, _ domain.AgentRequest, onToken func(string)) (domain.AgentResponse, error) {
	l.streams.Add(1)
	if onToken != nil {
		onToken(l.token)
	}
	l.enterOnce.Do(func() { close(l.entered) })
	<-ctx.Done()
	return domain.AgentResponse{}, ctx.Err()
}

type toollessRegistry struct{ port.ToolRegistry }

func (toollessRegistry) DefinitionsForPolicy(domain.ToolPolicy) []domain.ToolDefinition { return nil }

func newCancelTestService(t *testing.T) (*Service, *cancelSessionStore, *contractActivityStore, *blockingStreamLLM) {
	t.Helper()
	store := &cancelSessionStore{session: domain.Session{ID: uuid.New(), WorkspaceDir: t.TempDir()}}
	runs := newContractActivityStore()
	llm := &blockingStreamLLM{token: "half an answer", entered: make(chan struct{})}
	loop := agent.NewLoop(llm, toollessRegistry{}, 4, 4, 0)
	return NewService(store, runs, loop, nil, nil, 0, nil, nil), store, runs, llm
}

func TestCancelSessionStopsTheTurnAndKeepsWhatWasSaid(t *testing.T) {
	svc, store, runs, llm := newCancelTestService(t)
	sessionID := store.session.ID

	sent := make(chan error, 1)
	go func() {
		_, err := svc.SendMessageStream(context.Background(), sessionID, domain.SessionMessageRequest{
			Role: domain.RoleUser, Content: "do the thing",
		}, domain.ToolPolicy{}, func(string) {})
		sent <- err
	}()

	select {
	case <-llm.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the turn never reached the model")
	}

	cancelled, err := svc.CancelSession(context.Background(), sessionID, "user pressed stop")
	if err != nil {
		t.Fatalf("CancelSession: %v", err)
	}
	if !cancelled {
		t.Fatal("CancelSession did not find the turn this process was executing")
	}

	select {
	case err := <-sent:

		if !errors.Is(err, domain.ErrRunCancelled) {
			t.Fatalf("SendMessageStream returned %v, want domain.ErrRunCancelled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the turn kept going after it was cancelled")
	}

	statuses := runs.statuses()
	if len(statuses) != 1 {
		t.Fatalf("recorded %d runs, want 1", len(statuses))
	}
	for runID, status := range statuses {

		if status != domain.TaskAgentRunStatusCancelled {
			t.Fatalf("run %s ended as %q, want %q", runID, status, domain.TaskAgentRunStatusCancelled)
		}
	}

	msgs := store.messages()
	if len(msgs) != 2 {
		t.Fatalf("transcript holds %d messages, want the question and the partial answer: %+v", len(msgs), msgs)
	}
	if msgs[0].Role != domain.RoleUser || msgs[0].Content != "do the thing" {
		t.Fatalf("first message = %+v, want the user's question", msgs[0])
	}
	if msgs[1].Role != domain.RoleAssistant || msgs[1].Content != "half an answer" {
		t.Fatalf("second message = %+v, want the partial answer that had been streamed", msgs[1])
	}
	for _, m := range msgs {
		if strings.Contains(m.Content, "**Error:**") {
			t.Fatalf("a stopped turn wrote an error into the transcript: %q", m.Content)
		}
	}
	if got := llm.streams.Load(); got != 1 {
		t.Fatalf("the model was streamed from %d times, want 1 — a stopped turn must not buy another round-trip", got)
	}
}

func TestCancelSessionReportsFalseWhenNothingIsRunning(t *testing.T) {
	svc, store, runs, _ := newCancelTestService(t)

	cancelled, err := svc.CancelSession(context.Background(), store.session.ID, "")
	if err != nil {
		t.Fatalf("CancelSession on an idle session: %v", err)
	}
	if cancelled {
		t.Fatal("CancelSession claimed a turn the session never had")
	}
	if len(runs.statuses()) != 0 {
		t.Fatal("cancelling an idle session touched a row")
	}
}

func TestCancelSessionRejectsAnUnknownSession(t *testing.T) {
	svc, _, _, _ := newCancelTestService(t)

	if _, err := svc.CancelSession(context.Background(), uuid.New(), ""); !errors.Is(err, domain.ErrSessionNotFound) {
		t.Fatalf("CancelSession on an unknown session returned %v, want domain.ErrSessionNotFound", err)
	}
}

func TestCancelSessionRunIsScopedToItsSession(t *testing.T) {
	svc, store, _, llm := newCancelTestService(t)
	sessionID := store.session.ID

	sent := make(chan error, 1)
	go func() {
		_, err := svc.SendMessageStream(context.Background(), sessionID, domain.SessionMessageRequest{
			Role: domain.RoleUser, Content: "do the thing",
		}, domain.ToolPolicy{}, nil)
		sent <- err
	}()
	select {
	case <-llm.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the turn never reached the model")
	}

	cancelled, err := svc.CancelSessionRun(context.Background(), sessionID, uuid.New(), "")
	if err != nil {
		t.Fatalf("CancelSessionRun with a foreign run id: %v", err)
	}
	if cancelled {
		t.Fatal("CancelSessionRun stopped a turn it was not asked about")
	}

	runID := onlyRunID(t, svc, sessionID)
	cancelled, err = svc.CancelSessionRun(context.Background(), sessionID, runID, "stop")
	if err != nil {
		t.Fatalf("CancelSessionRun: %v", err)
	}
	if !cancelled {
		t.Fatal("CancelSessionRun did not stop the turn it named")
	}
	select {
	case err := <-sent:
		if !errors.Is(err, domain.ErrRunCancelled) {
			t.Fatalf("SendMessageStream returned %v, want domain.ErrRunCancelled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the turn kept going after it was cancelled")
	}
}

func onlyRunID(t *testing.T, svc *Service, sessionID uuid.UUID) uuid.UUID {
	t.Helper()
	handles := svc.liveRuns(sessionID)
	if len(handles) != 1 {
		t.Fatalf("session holds %d live turns, want 1", len(handles))
	}
	return handles[0].runID
}

func TestCompleteRunCannotOverwriteACancelledTurn(t *testing.T) {
	runs := newContractActivityStore()
	ctx := context.Background()

	run, err := runs.CreateRun(ctx, nil, "req-1", "model")
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	cancelled, err := runs.CancelRun(ctx, run.ID)
	if err != nil || !cancelled {
		t.Fatalf("CancelRun on a running turn = (%v, %v), want (true, nil)", cancelled, err)
	}

	if again, err := runs.CancelRun(ctx, run.ID); err != nil || again {
		t.Fatalf("second CancelRun = (%v, %v), want (false, nil)", again, err)
	}

	if err := runs.CompleteRun(ctx, run.ID, "failed"); err != nil {
		t.Fatalf("CompleteRun: %v", err)
	}
	if got := runs.statuses()[run.ID]; got != domain.TaskAgentRunStatusCancelled {
		t.Fatalf("run ended as %q, want %q — a late verdict overwrote the stop", got, domain.TaskAgentRunStatusCancelled)
	}
}

func TestStopFromAnotherReplicaStopsTheTurn(t *testing.T) {
	executing, store, runs, llm := newCancelTestService(t)
	executing.cancelPoll = 10 * time.Millisecond
	sessionID := store.session.ID

	other := &Service{store: store, activityStore: runs}

	sent := make(chan error, 1)
	go func() {
		_, err := executing.SendMessageStream(context.Background(), sessionID, domain.SessionMessageRequest{
			Role: domain.RoleUser, Content: "do the thing",
		}, domain.ToolPolicy{}, func(string) {})
		sent <- err
	}()

	select {
	case <-llm.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the turn never reached the model")
	}

	cancelled, err := other.CancelSession(context.Background(), sessionID, "user pressed stop")
	if err != nil {
		t.Fatalf("CancelSession on the non-executing replica: %v", err)
	}
	if !cancelled {
		t.Fatal("a stop served by another replica must still report that it stopped something")
	}

	select {
	case err := <-sent:
		if !errors.Is(err, domain.ErrRunCancelled) {
			t.Fatalf("SendMessageStream returned %v, want domain.ErrRunCancelled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the turn kept going after it was cancelled from another replica")
	}

	for id, status := range runs.statuses() {
		if status != domain.SessionRunStatusCancelled {
			t.Fatalf("run %s ended as %q, want cancelled", id, status)
		}
	}
}
