package evolution

import (
	"context"

	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type askRequest struct {
	Model    string
	Provider domain.LLMProviderType
	Messages []domain.Message
	// Schema constrains an HTTP provider's answer; a CLI has no such knob, so callers parse leniently either way.
	Schema   *prompt.SchemaKey
	Label    string
	Title    string
	MaxTurns int
}

// One prompt to one final answer, on whichever engine the model lives on. The golden suite, the probes, the judge and the lesson used to be HTTP-only utility calls, so for an agent on Claude Code, OpenCode or another CLI every one of them was refused and the gate silently measured nothing; a CLI answers the same prompt as a short session in an empty scratch directory with no TaskTrooper tools.
func (s *Service) ask(ctx context.Context, req askRequest) (string, error) {
	if domain.RequiresHostExecutor(req.Provider) {
		if s.agentLoop == nil {
			return "", domain.ErrHostExecutedProvider(req.Provider)
		}
		maxTurns := req.MaxTurns
		if maxTurns <= 0 {
			maxTurns = s.cfg.ProbeMaxTurns
		}
		resp, err := s.agentLoop.Run(ctx, req.Messages, req.Model, req.Provider, noToolsPolicy,
			agent.WithSessionLimits(maxTurns, ""),
			agent.WithCLILabel(req.Label, req.Title),
			agent.WithScratchWorkspace())
		if err != nil {
			return "", err
		}
		return resp.Message.Content, nil
	}
	if s.llm == nil {
		return "", errNoLLM
	}
	chat := domain.AgentRequest{Messages: req.Messages, ProviderType: req.Provider, Model: req.Model}
	if req.Schema != nil {
		chat.ResponseFormat = domain.JSONSchemaResponseFormat(req.Schema.Name(), req.Schema.Map())
	}
	resp, err := s.llm.Chat(ctx, chat)
	if err != nil {
		return "", err
	}
	return resp.Message.Content, nil
}

// A name no tool carries: an empty AllowTools means "everything", and a rehearsal or a verdict must not be able to touch the board, a repository or the web.
var noToolsPolicy = domain.ToolPolicy{AllowTools: []string{"__evolution_no_tools__"}}

type evolutionError string

func (e evolutionError) Error() string { return string(e) }

const errNoLLM = evolutionError("no LLM client is configured")
