package session

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// dropConnectionLLM finishes the turn on its own schedule, independent of
// whatever context it was handed — the shape a still-running LLM call has
// once the run is properly detached from a caller's ctx.
type dropConnectionLLM struct {
	port.LLMClient
	token   string
	entered chan struct{}
}

func (l *dropConnectionLLM) Chat(context.Context, domain.AgentRequest) (domain.AgentResponse, error) {
	return domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant}}, nil
}

func (l *dropConnectionLLM) ChatStream(_ context.Context, _ domain.AgentRequest, onToken func(string)) (domain.AgentResponse, error) {
	if onToken != nil {
		onToken(l.token)
	}
	close(l.entered)
	// Gives the test a window to cancel the caller's ctx before this call
	// returns, proving the answer is not tied to it.
	time.Sleep(50 * time.Millisecond)
	return domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: l.token}}, nil
}

func TestSendMessageStreamSurvivesADroppedConnection(t *testing.T) {
	store := &cancelSessionStore{
		session:            domain.Session{ID: uuid.New(), WorkspaceDir: t.TempDir()},
		rejectCancelledCtx: true,
	}
	runs := newContractActivityStore()
	llm := &dropConnectionLLM{token: "the whole answer", entered: make(chan struct{})}
	loop := agent.NewLoop(llm, toollessRegistry{}, 4, 4, 0)
	svc := NewService(store, runs, loop, nil, nil, 0, nil, nil)
	sessionID := store.session.ID

	// requestCtx stands in for what the handler passes down: it is what a
	// dropped SSE connection (a write failure that used to call
	// streamCancel()) would cancel mid-turn.
	requestCtx, cancelRequest := context.WithCancel(context.Background())

	type result struct {
		resp domain.AgentResponse
		err  error
	}
	sent := make(chan result, 1)
	go func() {
		resp, err := svc.SendMessageStream(requestCtx, sessionID, domain.SessionMessageRequest{
			Role: domain.RoleUser, Content: "do the thing",
		}, domain.ToolPolicy{}, func(string) {})
		sent <- result{resp, err}
	}()

	select {
	case <-llm.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the turn never reached the model")
	}

	cancelRequest()

	var got result
	select {
	case got = <-sent:
	case <-time.After(5 * time.Second):
		t.Fatal("the turn never finished after the caller's context was cancelled")
	}
	if got.err != nil {
		t.Fatalf("SendMessageStream returned %v, want nil — a dropped connection must not fail the turn", got.err)
	}
	if got.resp.Message.Content != "the whole answer" {
		t.Fatalf("response content = %q, want the full answer", got.resp.Message.Content)
	}

	msgs := store.messages()
	if len(msgs) != 2 {
		t.Fatalf("transcript holds %d messages, want the question and the answer: %+v", len(msgs), msgs)
	}
	if msgs[0].Role != domain.RoleUser || msgs[0].Content != "do the thing" {
		t.Fatalf("first message = %+v, want the user's question", msgs[0])
	}
	if msgs[1].Role != domain.RoleAssistant || msgs[1].Content != "the whole answer" {
		t.Fatalf("second message = %+v, want the persisted answer", msgs[1])
	}
}

// dropConnectionErrorLLM fails the turn (not stopped by the user) after the
// caller's ctx has already been cancelled, exercising appendAssistantError's
// own write.
type dropConnectionErrorLLM struct {
	port.LLMClient
	entered chan struct{}
}

func (l *dropConnectionErrorLLM) Chat(context.Context, domain.AgentRequest) (domain.AgentResponse, error) {
	return domain.AgentResponse{}, nil
}

func (l *dropConnectionErrorLLM) ChatStream(_ context.Context, _ domain.AgentRequest, onToken func(string)) (domain.AgentResponse, error) {
	close(l.entered)
	time.Sleep(50 * time.Millisecond)
	// A 400 is not retryable (domain.LLMHTTPError.Retryable), so the loop
	// gives up on the first attempt instead of retrying with backoff.
	return domain.AgentResponse{}, domain.NewLLMHTTPError(400, []byte("provider unreachable"))
}

func TestAppendAssistantErrorSurvivesADroppedConnection(t *testing.T) {
	store := &cancelSessionStore{
		session:            domain.Session{ID: uuid.New(), WorkspaceDir: t.TempDir()},
		rejectCancelledCtx: true,
	}
	runs := newContractActivityStore()
	llm := &dropConnectionErrorLLM{entered: make(chan struct{})}
	loop := agent.NewLoop(llm, toollessRegistry{}, 4, 4, 0)
	svc := NewService(store, runs, loop, nil, nil, 0, nil, nil)
	sessionID := store.session.ID

	requestCtx, cancelRequest := context.WithCancel(context.Background())

	sent := make(chan error, 1)
	go func() {
		_, err := svc.SendMessageStream(requestCtx, sessionID, domain.SessionMessageRequest{
			Role: domain.RoleUser, Content: "do the thing",
		}, domain.ToolPolicy{}, func(string) {})
		sent <- err
	}()

	select {
	case <-llm.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the turn never reached the model")
	}

	cancelRequest()

	select {
	case err := <-sent:
		if err == nil {
			t.Fatal("SendMessageStream returned nil, want the provider error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the turn never finished after the caller's context was cancelled")
	}

	msgs := store.messages()
	if len(msgs) != 2 {
		t.Fatalf("transcript holds %d messages, want the question and the error note: %+v", len(msgs), msgs)
	}
	if !strings.Contains(msgs[1].Content, "**Error:**") {
		t.Fatalf("second message = %+v, want an error note", msgs[1])
	}
}
