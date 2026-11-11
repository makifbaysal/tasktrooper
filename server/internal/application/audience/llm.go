package audience

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// A name no tool carries: an empty AllowTools means "everything", and an
// invented person must not be able to touch the board, a repository or the web.
var noToolsPolicy = domain.ToolPolicy{AllowTools: []string{"__audience_no_tools__"}}

type askRequest struct {
	Messages []domain.Message
	Label    string
	Title    string
	MaxTurns int
}

// ask is one prompt to one final answer, on whichever engine the person's
// agent lives on: a host-executed CLI answers as a short session in an empty
// scratch directory with no TaskTrooper tools, an API provider answers
// directly. Calls share the service's concurrency limit.
func (s *Service) ask(ctx context.Context, a domain.Agent, req askRequest) (string, error) {
	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	callCtx, cancel := context.WithTimeout(ctx, s.d.CallTimeout)
	defer cancel()

	if domain.RequiresHostExecutor(a.ProviderType) {
		if s.d.Runner == nil {
			return "", domain.ErrHostExecutedProvider(a.ProviderType)
		}
		maxTurns := req.MaxTurns
		if maxTurns <= 0 {
			maxTurns = 6
		}
		resp, err := s.d.Runner.Run(callCtx, req.Messages, a.Model, a.ProviderType, noToolsPolicy,
			agent.WithSessionLimits(maxTurns, a.Effort),
			agent.WithCLILabel(req.Label, req.Title),
			agent.WithScratchWorkspace())
		if err != nil {
			return "", callError(ctx, callCtx, err)
		}
		return resp.Message.Content, nil
	}
	if s.d.LLM == nil {
		return "", ErrNoRuntime
	}
	resp, err := s.d.LLM.Chat(callCtx, domain.AgentRequest{Messages: req.Messages, ProviderType: a.ProviderType, Model: a.Model})
	if err != nil {
		return "", callError(ctx, callCtx, err)
	}
	return resp.Message.Content, nil
}

func callError(parent, call context.Context, err error) error {
	if parent.Err() != nil {
		return parent.Err()
	}
	if errors.Is(call.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%w: the model did not answer in time", ErrCallFailed)
	}
	if errors.Is(err, ErrNoRuntime) {
		return err
	}
	return fmt.Errorf("%w: %v", ErrCallFailed, err)
}

// askJSON asks, parses the object out of the answer and, when the answer was
// not usable, asks once more naming what was wrong. parse returns the problem.
func (s *Service) askJSON(ctx context.Context, a domain.Agent, req askRequest, parse func(raw string) error) error {
	raw, err := s.ask(ctx, a, req)
	if err != nil {
		return err
	}
	perr := parse(raw)
	if perr == nil {
		return nil
	}
	retry := req
	retry.Messages = append(append([]domain.Message{}, req.Messages...),
		domain.Message{Role: domain.RoleAssistant, Content: raw},
		domain.Message{Role: domain.RoleUser, Content: fmt.Sprintf("That was not usable: %v. Reply again with only the JSON object described above.", perr)})
	raw, err = s.ask(ctx, a, retry)
	if err != nil {
		return err
	}
	if perr := parse(raw); perr != nil {
		return fmt.Errorf("the model's answer was not valid: %w", perr)
	}
	return nil
}

// extractJSON returns the outermost JSON object in a model's answer, which may
// wrap it in prose or a code fence.
func extractJSON(raw string) (string, error) {
	text := strings.TrimSpace(raw)
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end < start {
		return "", errors.New("no JSON object in the answer")
	}
	return text[start : end+1], nil
}

func decodeObject(raw string, into any) error {
	obj, err := extractJSON(raw)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(obj), into); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}
