package session

import (
	"context"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func PrependWorkspacePromptForTest(history []domain.Message, workspaceDir, lang string) []domain.Message {
	return prependWorkspacePrompt(history, workspaceDir, lang)
}

func ResolveChatModelForTest(reqModel, sessionModel, agentModel string) string {
	return resolveChatModel(reqModel, sessionModel, agentModel)
}

// AttachImageAttachmentsForTest exercises the image-enrichment helper directly
// against a fake attachment store; rows and history must be index-parallel.
func AttachImageAttachmentsForTest(
	ctx context.Context,
	store AttachmentLinker,
	rows []domain.SessionMessage,
	history []domain.Message,
) {
	attachImageAttachments(ctx, store, rows, history)
}

// ResolveRunWorkspaceForTest exercises the workspace + context + prompt decisions
// one turn makes, which is where a task-bound chat differs from every other chat:
// it must land in the task's own branch checkout rather than the shared mirror
// clone. It returns the workspace dir, the run context (carrying the branch and
// task id) and the history the model would see.
func ResolveRunWorkspaceForTest(
	ctx context.Context,
	store port.SessionStore,
	repositories RepositoryResolver,
	tasks TaskWorkspaceResolver,
	sess domain.Session,
	settings domain.AppSettings,
	history []domain.Message,
) (string, context.Context, []domain.Message, error) {
	svc := &Service{store: store, repositories: repositories, taskWorkspaces: tasks}
	dir, binding, err := svc.ensureSessionWorkspace(ctx, sess, settings)
	if err != nil {
		return "", ctx, nil, err
	}
	runCtx, out, err := svc.prepareRunContext(ctx, sess, sess.ID, dir, settings.DefaultLanguage, history, binding)
	return dir, runCtx, out, err
}

// NewHostExecutedServiceForTest builds the minimum Service the host-executor
// chat path needs: a store to record the CLI session on, and the executor that
// answers the turn. Everything else that path touches is a parameter.
func NewHostExecutedServiceForTest(store port.SessionStore, executor port.ChatExecutor) *Service {
	return &Service{store: store, chatExecutor: executor}
}

// ParkTurnOnQuotaForTest exercises the quota-park path directly — the store
// write and the transcript notice it leaves behind — without paying for the
// rest of SendMessage's setup (workspace, agent context, the run itself).
func ParkTurnOnQuotaForTest(
	ctx context.Context,
	store port.SessionStore,
	sessionID uuid.UUID,
	req domain.SessionMessageRequest,
	policy domain.ToolPolicy,
	block *domain.QuotaBlock,
	lang string,
) error {
	svc := &Service{store: store}
	return svc.parkTurnOnQuota(ctx, sessionID, req, policy, block, lang)
}

// RunHostExecutedTurnForTest exercises the branch a claude_code chat takes
// instead of the agent loop — the branch whose absence made such an agent
// chattable only in theory.
//
// The arguments are spelled out rather than hidden behind a fixture because they
// ARE the contract with the executor: which provider decided the route, which
// workspace the CLI is started in, which policy its tools are served under, and
// whether the session row carried a CLI session to resume.
func (s *Service) RunHostExecutedTurnForTest(
	ctx context.Context,
	sess domain.Session,
	provider domain.LLMProviderType,
	model string,
	policy domain.ToolPolicy,
	workspaceDir string,
	history []domain.Message,
	prompt string,
	lang string,
	out port.ChatStream,
) (domain.AgentResponse, error) {
	return s.runHostExecutedTurn(ctx, sess, hostTurn{
		provider:     provider,
		model:        model,
		policy:       policy,
		workspaceDir: workspaceDir,
		history:      history,
		prompt:       prompt,
		lang:         lang,
	}, out)
}

// AppendAssistantErrorForTest exercises how a failed turn is written into the
// transcript, which is where a user actually meets an error.
func AppendAssistantErrorForTest(ctx context.Context, store port.SessionStore, sessionID uuid.UUID, err error) {
	(&Service{store: store}).appendAssistantError(ctx, sessionID, err)
}

func BuildMessageHistoryForTest(
	ctx context.Context,
	store port.SessionStore,
	actions port.SessionActionStore,
	sessionID uuid.UUID,
) ([]domain.Message, error) {
	svc := &Service{store: store, actions: actions}
	return svc.buildMessageHistory(ctx, sessionID)
}
