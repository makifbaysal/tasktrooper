package session

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/activity"
	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// multiRunLLM lets a test hold several concurrent runs open at once and
// release (or leave to their own ctx cancellation) each one independently,
// keyed by the run id the activity recorder assigns it.
type multiRunLLM struct {
	port.LLMClient

	mu      sync.Mutex
	gates   map[uuid.UUID]chan struct{}
	entered chan uuid.UUID
}

func newMultiRunLLM() *multiRunLLM {
	return &multiRunLLM{gates: make(map[uuid.UUID]chan struct{}), entered: make(chan uuid.UUID, 4)}
}

func (l *multiRunLLM) gateFor(runID uuid.UUID) chan struct{} {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.gates[runID] == nil {
		l.gates[runID] = make(chan struct{})
	}
	return l.gates[runID]
}

func (l *multiRunLLM) release(runID uuid.UUID) {
	close(l.gateFor(runID))
}

func (l *multiRunLLM) Chat(context.Context, domain.AgentRequest) (domain.AgentResponse, error) {
	return domain.AgentResponse{}, nil
}

func (l *multiRunLLM) ChatStream(ctx context.Context, _ domain.AgentRequest, onToken func(string)) (domain.AgentResponse, error) {
	runID := activity.FromContext(ctx).RunID()
	gate := l.gateFor(runID)
	l.entered <- runID
	select {
	case <-gate:
		content := "answer for " + runID.String()
		if onToken != nil {
			onToken(content)
		}
		return domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: content}}, nil
	case <-ctx.Done():
		return domain.AgentResponse{}, ctx.Err()
	}
}

// TestConcurrentRunsOnTheSameSessionAreTrackedIndependently covers the
// scenario a dropped connection makes possible now that a run no longer
// aborts with it: a second turn on the same session can be started, or
// cancelled, while the first is still finishing in the background, and
// sessionRuns must keep the two apart.
func TestConcurrentRunsOnTheSameSessionAreTrackedIndependently(t *testing.T) {
	store := &cancelSessionStore{session: domain.Session{ID: uuid.New(), WorkspaceDir: t.TempDir()}}
	runs := newContractActivityStore()
	llm := newMultiRunLLM()
	loop := agent.NewLoop(llm, toollessRegistry{}, 4, 4, 0)
	svc := NewService(store, runs, loop, nil, nil, 0, nil, nil)
	sessionID := store.session.ID

	type result struct {
		resp domain.AgentResponse
		err  error
	}

	// Run 1 starts on a request ctx that then gets cancelled — the dropped
	// connection this whole fix is about — but must keep running.
	req1Ctx, cancelReq1 := context.WithCancel(context.Background())
	sent1 := make(chan result, 1)
	go func() {
		resp, err := svc.SendMessageStream(req1Ctx, sessionID, domain.SessionMessageRequest{
			Role: domain.RoleUser, Content: "first turn",
		}, domain.ToolPolicy{}, func(string) {})
		sent1 <- result{resp, err}
	}()

	var run1ID uuid.UUID
	select {
	case run1ID = <-llm.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the first turn never reached the model")
	}
	cancelReq1()

	// Run 2 starts on the very same session while run 1 is still in flight.
	sent2 := make(chan result, 1)
	go func() {
		resp, err := svc.SendMessageStream(context.Background(), sessionID, domain.SessionMessageRequest{
			Role: domain.RoleUser, Content: "second turn",
		}, domain.ToolPolicy{}, func(string) {})
		sent2 <- result{resp, err}
	}()

	var run2ID uuid.UUID
	select {
	case run2ID = <-llm.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the second turn never reached the model")
	}
	if run1ID == run2ID {
		t.Fatalf("both turns were assigned the same run id %s", run1ID)
	}

	live := svc.liveRuns(sessionID)
	if len(live) != 2 {
		t.Fatalf("session holds %d live turns while both are in flight, want 2", len(live))
	}
	seen := map[uuid.UUID]bool{}
	for _, h := range live {
		seen[h.runID] = true
	}
	if !seen[run1ID] || !seen[run2ID] {
		t.Fatalf("liveRuns = %v, want both %s and %s", seen, run1ID, run2ID)
	}

	// Stopping run 2 by its own id must not touch run 1.
	stopped, err := svc.CancelSessionRun(context.Background(), sessionID, run2ID, "user pressed stop")
	if err != nil {
		t.Fatalf("CancelSessionRun: %v", err)
	}
	if !stopped {
		t.Fatal("CancelSessionRun did not stop the run it named")
	}

	select {
	case got := <-sent2:
		if got.err == nil {
			t.Fatal("the cancelled turn returned nil, want domain.ErrRunCancelled")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the cancelled turn never returned")
	}

	// Run 1 is still alive and unaffected by run 2's cancellation.
	live = svc.liveRuns(sessionID)
	if len(live) != 1 || live[0].runID != run1ID {
		t.Fatalf("liveRuns after cancelling run 2 = %v, want only run 1 (%s)", live, run1ID)
	}

	llm.release(run1ID)
	select {
	case got := <-sent1:
		if got.err != nil {
			t.Fatalf("the first (dropped-connection) turn returned %v, want nil", got.err)
		}
		want := "answer for " + run1ID.String()
		if got.resp.Message.Content != want {
			t.Fatalf("first turn content = %q, want %q", got.resp.Message.Content, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the first turn never finished")
	}

	msgs := store.messages()
	var assistantMsgs []domain.SessionMessage
	for _, m := range msgs {
		if m.Role == domain.RoleAssistant {
			assistantMsgs = append(assistantMsgs, m)
		}
	}
	if len(assistantMsgs) != 1 || assistantMsgs[0].Content != "answer for "+run1ID.String() {
		t.Fatalf("assistant messages = %+v, want only run 1's answer", assistantMsgs)
	}
}
