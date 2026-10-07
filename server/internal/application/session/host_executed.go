package session

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/proctree"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type hostTurn struct {
	provider     domain.LLMProviderType
	model        string
	policy       domain.ToolPolicy
	workspaceDir string
	history      []domain.Message
	prompt       string
	lang         string
}

func (s *Service) runHostExecutedTurn(
	ctx context.Context,
	sess domain.Session,
	turn hostTurn,
	out port.ChatStream,
) (domain.AgentResponse, error) {

	if s.chatExecutor == nil || !s.chatExecutor.Supports(turn.provider) {
		return domain.AgentResponse{}, domain.ErrHostExecutedProvider(turn.provider)
	}

	workDir := strings.TrimSpace(turn.workspaceDir)

	if workDir == "" {
		return domain.AgentResponse{}, errNoChatWorkspace(turn.lang, turn.provider)
	}

	questions := &domain.ClarificationSink{}
	result, err := s.chatExecutor.ExecuteChat(domain.WithClarificationSink(ctx, questions), domain.ChatExecution{
		History:         turn.history,
		Prompt:          turn.prompt,
		Model:           turn.model,
		Provider:        turn.provider,
		Policy:          turn.policy,
		WorkDir:         workDir,
		ResumeSessionID: sess.CLISessionID,
		SessionID:       sess.ID.String(),
	}, out)

	s.rememberCLISession(ctx, sess, result.CLISessionID)

	if err != nil {
		if block, ok := domain.QuotaBlockOf(err); ok {
			log.Warn().
				Str("session_id", sess.ID.String()).
				Str("cli_session_id", result.CLISessionID).
				Time("resume_at", block.ResumeAt).
				Msg("agent cli usage limit reached during a chat turn")
			return domain.AgentResponse{}, domain.NewQuotaNotice(block, turn.lang)
		}
		return domain.AgentResponse{}, err
	}
	if result.Response.Clarification == nil {
		result.Response.Clarification = questions.Request()
	}
	return result.Response, nil
}

func (s *Service) rememberCLISession(ctx context.Context, sess domain.Session, cliSessionID string) {
	id := strings.TrimSpace(cliSessionID)
	if id == "" || id == sess.CLISessionID {
		return
	}
	if err := s.store.UpdateCLISessionID(context.WithoutCancel(ctx), sess.ID, id); err != nil {
		log.Warn().Err(err).
			Str("session_id", sess.ID.String()).
			Str("cli_session_id", id).
			Msg("could not record the cli session for this chat; the next turn will start a fresh one")
	}
}

func errNoChatWorkspace(lang string, provider domain.LLMProviderType) error {
	label := domain.LLMProviderLabel(provider)
	if label == "" {
		switch lang {
		case "tr":
			return &chatSetupError{"Bu ajan yerel bir ajan CLI üzerinde çalışıyor ve bir çalışma dizini gerekiyor. " +
				"Sohbeti bir depoya bağlayın (ya da bir görev sohbeti açın), sonra tekrar deneyin."}
		default:
			return &chatSetupError{"This agent runs on a local agent CLI, which needs a workspace to run in. " +
				"Scope this chat to a repository (or open it from a task), then try again."}
		}
	}
	switch lang {
	case "tr":
		return &chatSetupError{"Bu ajan " + label + " üzerinde çalışıyor ve bir çalışma dizini gerekiyor. " +
			"Sohbeti bir depoya bağlayın (ya da bir görev sohbeti açın), sonra tekrar deneyin."}
	default:
		return &chatSetupError{"This agent runs on " + label + ", which needs a workspace to run in. " +
			"Scope this chat to a repository (or open it from a task), then try again."}
	}
}

type chatSetupError struct{ msg string }

func (e *chatSetupError) Error() string { return e.msg }

// withTurnScope scopes processes a turn's tools background (dev servers) to the
// turn, so they die when it returns instead of outliving it.
func withTurnScope(ctx context.Context, sessionID uuid.UUID) (context.Context, func()) {
	scope := "turn:" + sessionID.String() + ":" + uuid.NewString()
	return proctree.WithScope(ctx, scope), func() { proctree.Default.KillScope(scope, 3*time.Second) }
}
