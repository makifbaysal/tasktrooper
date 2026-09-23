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

func AttachImageAttachmentsForTest(
	ctx context.Context,
	store AttachmentLinker,
	rows []domain.SessionMessage,
	history []domain.Message,
) {
	attachImageAttachments(ctx, store, rows, history)
}

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

func NewHostExecutedServiceForTest(store port.SessionStore, executor port.ChatExecutor) *Service {
	return &Service{store: store, chatExecutor: executor}
}

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

func AppendAssistantErrorForTest(ctx context.Context, store port.SessionStore, sessionID uuid.UUID, err error) {
	(&Service{store: store}).appendAssistantError(ctx, sessionID, err)
}

func MaybeAutoTitleForTest(
	ctx context.Context,
	store port.SessionStore,
	titleGen TitleGenerator,
	sess domain.Session,
	sessionID uuid.UUID,
	userContent, assistantContent, model string,
	provider domain.LLMProviderType,
) {
	svc := &Service{store: store, titleGen: titleGen}
	svc.maybeAutoTitle(ctx, sess, sessionID, userContent, assistantContent, model, provider)
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
