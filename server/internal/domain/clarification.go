package domain

import (
	"context"
	"sync"
)

const AskUserToolName = "ask_user"

type ClarificationOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type ClarificationQuestion struct {
	ID            string                `json:"id"`
	Prompt        string                `json:"prompt"`
	AllowMultiple bool                  `json:"allow_multiple,omitempty"`
	Options       []ClarificationOption `json:"options"`
}

type ClarificationRequest struct {
	Context   string                  `json:"context,omitempty"`
	Questions []ClarificationQuestion `json:"questions"`
}

func (r ClarificationRequest) Valid() bool {
	if len(r.Questions) == 0 {
		return false
	}
	for _, q := range r.Questions {
		if q.ID == "" || q.Prompt == "" || len(q.Options) < 2 {
			return false
		}
		for _, o := range q.Options {
			if o.ID == "" || o.Label == "" {
				return false
			}
		}
	}
	return true
}

func ClarificationStepPayload(req ClarificationRequest, source string) map[string]any {
	return map[string]any{
		"source":         source,
		"question_count": len(req.Questions),
		"context":        req.Context,
		"questions":      req.Questions,
	}
}

// ClarificationSink carries an agent CLI's ask_user call back to the run that
// started the CLI. The call arrives on the MCP endpoint, a different request
// from the run, so the question cannot come back in the tool result the way an
// in-process agent loop's does; the run reads it here once the CLI exits.
type ClarificationSink struct {
	mu  sync.Mutex
	req *ClarificationRequest
}

// Record keeps the latest question: the tool tells the CLI to end its turn on
// the first one, so a second call is the agent rewording, not adding.
func (s *ClarificationSink) Record(req ClarificationRequest) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.req = &req
}

func (s *ClarificationSink) Request() *ClarificationRequest {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.req
}

type clarificationSinkKey struct{}

// WithClarificationSink marks ctx as a run that can wait for a human answer,
// which is what makes the MCP endpoint serve ask_user to it at all.
func WithClarificationSink(ctx context.Context, sink *ClarificationSink) context.Context {
	return context.WithValue(ctx, clarificationSinkKey{}, sink)
}

func ClarificationSinkFrom(ctx context.Context) *ClarificationSink {
	if ctx == nil {
		return nil
	}
	sink, _ := ctx.Value(clarificationSinkKey{}).(*ClarificationSink)
	return sink
}
