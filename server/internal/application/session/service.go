package session

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/activity"
	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	appcontext "github.com/makifbaysal/tasktrooper/server/internal/application/context"
	"github.com/makifbaysal/tasktrooper/server/internal/application/memory"
	"github.com/makifbaysal/tasktrooper/server/internal/application/orchestrator"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	appSettings "github.com/makifbaysal/tasktrooper/server/internal/application/settings"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/rs/zerolog/log"
)

type Service struct {
	store          port.SessionStore
	activityStore  port.ActivityStore
	cancelPoll     time.Duration
	agentLoop      *agent.Loop
	orchestrator   *orchestrator.Service
	settings       *appSettings.Service
	repositories   RepositoryResolver
	board          port.BoardConfigStore
	catalog        port.CatalogStore
	ttl            time.Duration
	rag            RAGInjector
	indexInjector  IndexInjector
	budget         appcontext.Budget
	summarizer     appcontext.Summarizer
	contextCfg     domain.ContextConfig
	indexerCfg     domain.IndexerConfig
	mappingCfg     domain.MappingConfig
	memories       port.AgentMemoryStore
	kpis           port.AgentKPIStore
	actions        port.SessionActionStore
	answerResumer  AnswerResumer
	workspace      WorkspaceLister
	attachments    AttachmentLinker
	taskWorkspaces TaskWorkspaceResolver
	chatExecutor   port.ChatExecutor
	runsMu         sync.Mutex
	runs           map[uuid.UUID]*runHandle
	sessionRuns    map[uuid.UUID]map[uuid.UUID]struct{}
}

type AttachmentLinker interface {
	LinkMessage(ctx context.Context, messageID, attachmentID uuid.UUID) error
	ListMetaByMessageIDs(ctx context.Context, messageIDs []uuid.UUID) (map[uuid.UUID][]domain.AttachmentMeta, error)
	Get(ctx context.Context, id uuid.UUID) (domain.Attachment, error)
}

func (s *Service) SetAttachments(a AttachmentLinker) {
	s.attachments = a
}

type TaskBinding struct {
	Task         domain.BoardTask
	Criteria     []domain.AcceptanceCriterion
	WorkspaceDir string
	Branch       string
}

type TaskWorkspaceResolver interface {
	ResolveTaskWorkspace(ctx context.Context, repositoryID, taskID uuid.UUID) (TaskBinding, error)
}

func (s *Service) SetTaskWorkspaces(r TaskWorkspaceResolver) {
	s.taskWorkspaces = r
}

func (s *Service) SetChatExecutor(e port.ChatExecutor) {
	s.chatExecutor = e
}

type AnswerResumer interface {
	ResumeOnAnswer(ctx context.Context, sessionID uuid.UUID, answer string) bool
}

func (s *Service) SetAnswerResumer(r AnswerResumer) {
	s.answerResumer = r
}

type RepositoryResolver interface {
	ResolveRootPath(ctx context.Context, repositoryID uuid.UUID) (string, error)
	ResolveDescription(ctx context.Context, repositoryID uuid.UUID) (string, error)
	ResolveRepository(ctx context.Context, repositoryID uuid.UUID) (domain.Repository, error)
}

type IndexInjector interface {
	InjectContext(ctx context.Context, sessionID uuid.UUID, messages []domain.Message, opts domain.InjectOptions) ([]domain.Message, error)
}

type RAGInjector interface {
	InjectContext(ctx context.Context, messages []domain.Message, fileIDs []string) ([]domain.Message, error)
}

type Deps struct {
	IndexInjector      IndexInjector
	RepositoryResolver RepositoryResolver
	Board              port.BoardConfigStore
	Catalog            port.CatalogStore
	Budget             appcontext.Budget
	Summarizer         appcontext.Summarizer
	ContextCfg         domain.ContextConfig
	IndexerCfg         domain.IndexerConfig
	MappingCfg         domain.MappingConfig
	Memories           port.AgentMemoryStore
	KPIs               port.AgentKPIStore
	Actions            port.SessionActionStore
	Workspace          WorkspaceLister
}

func NewService(store port.SessionStore, activityStore port.ActivityStore, agentLoop *agent.Loop, orch *orchestrator.Service, settings *appSettings.Service, ttl time.Duration, rag RAGInjector, deps *Deps) *Service {
	svc := &Service{
		store:         store,
		activityStore: activityStore,
		agentLoop:     agentLoop,
		orchestrator:  orch,
		settings:      settings,
		ttl:           ttl,
		rag:           rag,
	}
	if deps != nil {
		svc.indexInjector = deps.IndexInjector
		svc.repositories = deps.RepositoryResolver
		svc.board = deps.Board
		svc.catalog = deps.Catalog
		svc.budget = deps.Budget
		svc.summarizer = deps.Summarizer
		svc.contextCfg = deps.ContextCfg
		svc.indexerCfg = deps.IndexerCfg
		svc.mappingCfg = deps.MappingCfg
		svc.memories = deps.Memories
		svc.kpis = deps.KPIs
		svc.actions = deps.Actions
		svc.workspace = deps.Workspace
	}
	return svc
}

func (s *Service) Create(ctx context.Context, req domain.CreateSessionRequest) (domain.Session, error) {
	if err := s.validateCreateRequest(ctx, req); err != nil {
		return domain.Session{}, err
	}

	var expiresAt *time.Time
	if s.ttl > 0 {
		t := time.Now().Add(s.ttl)
		expiresAt = &t
	}

	settings, err := s.loadSettings(ctx)
	if err != nil {
		return domain.Session{}, err
	}

	model := req.Model
	if model == "" && req.AgentID != nil && s.catalog != nil {
		agentRec, err := s.catalog.GetAgent(ctx, *req.AgentID)
		if err == nil && agentRec.Model != "" {
			model = agentRec.Model
		}
	}

	sess, err := s.store.Create(ctx, req.Title, model, "", req.ProjectID, req.AgentID, expiresAt)
	if err != nil {
		return domain.Session{}, err
	}

	if req.TaskID != nil {
		if err := s.store.BindTask(ctx, sess.ID, *req.TaskID); err != nil {
			_ = s.store.Delete(ctx, sess.ID)
			return domain.Session{}, err
		}
		sess.TaskID = req.TaskID
	}

	if req.ProjectID != nil && s.repositories != nil {
		rootPath, err := s.repositories.ResolveRootPath(ctx, *req.ProjectID)
		if err != nil {
			_ = s.store.Delete(ctx, sess.ID)
			return domain.Session{}, err
		}
		sess.WorkspaceDir = rootPath
		sess.ProjectID = req.ProjectID
		if err := s.store.UpdateWorkspaceDir(ctx, sess.ID, rootPath); err != nil {
			_ = s.store.Delete(ctx, sess.ID)
			return domain.Session{}, err
		}
		return sess, nil
	}

	workspaceDir, err := workspaceDirFor(settings.WorkspaceRoot, sess)
	if err != nil {
		_ = s.store.Delete(ctx, sess.ID)
		return domain.Session{}, err
	}
	if err := workspace.EnsureDir(workspaceDir); err != nil {
		_ = s.store.Delete(ctx, sess.ID)
		return domain.Session{}, fmt.Errorf("create session workspace: %w", err)
	}
	if err := s.store.UpdateWorkspaceDir(ctx, sess.ID, workspaceDir); err != nil {
		_ = workspace.RemoveDir(workspaceDir)
		_ = s.store.Delete(ctx, sess.ID)
		return domain.Session{}, err
	}

	sess.WorkspaceDir = workspaceDir
	return sess, nil
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (domain.Session, []domain.SessionMessage, error) {
	sess, err := s.store.Get(ctx, id)
	if err != nil {
		return domain.Session{}, nil, err
	}
	msgs, err := s.store.ListMessages(ctx, id)
	if err != nil {
		return domain.Session{}, nil, err
	}
	s.enrichMessageAttachments(ctx, msgs)
	return sess, msgs, nil
}

func (s *Service) enrichMessageAttachments(ctx context.Context, msgs []domain.SessionMessage) {
	if s.attachments == nil || len(msgs) == 0 {
		return
	}
	ids := make([]uuid.UUID, 0, len(msgs))
	for _, m := range msgs {
		ids = append(ids, m.ID)
	}
	metas, err := s.attachments.ListMetaByMessageIDs(ctx, ids)
	if err != nil || len(metas) == 0 {
		return
	}
	for i := range msgs {
		if a, ok := metas[msgs[i].ID]; ok {
			msgs[i].Attachments = a
		}
	}
}

func (s *Service) linkMessageAttachments(ctx context.Context, messageID uuid.UUID, attachmentIDs []string) {
	if s.attachments == nil || len(attachmentIDs) == 0 {
		return
	}
	for _, raw := range attachmentIDs {
		id, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil {
			continue
		}
		_ = s.attachments.LinkMessage(ctx, messageID, id)
	}
}

func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	sess, err := s.store.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := s.store.Delete(ctx, id); err != nil {
		return err
	}

	if filepath.Base(sess.WorkspaceDir) == sess.ID.String() {
		_ = workspace.RemoveDir(sess.WorkspaceDir)
	}
	return nil
}

func workspaceDirFor(root string, sess domain.Session) (string, error) {
	if sess.AgentID != nil {
		return workspace.AgentDir(root, *sess.AgentID)
	}
	return workspace.SessionDir(root, sess.ID)
}

func (s *Service) List(ctx context.Context, limit, offset int) ([]domain.Session, error) {
	return s.store.List(ctx, limit, offset)
}

func (s *Service) ListByProject(ctx context.Context, projectID uuid.UUID, limit, offset int) ([]domain.Session, error) {
	return s.store.ListByProject(ctx, projectID, limit, offset)
}

func (s *Service) ListByAgent(ctx context.Context, agentID uuid.UUID, limit, offset int) ([]domain.Session, error) {
	return s.store.ListByAgent(ctx, agentID, limit, offset)
}

func (s *Service) SendMessage(ctx context.Context, sessionID uuid.UUID, req domain.SessionMessageRequest, policy domain.ToolPolicy) (resp domain.AgentResponse, err error) {
	sess, err := s.store.Get(ctx, sessionID)
	if err != nil {
		return domain.AgentResponse{}, err
	}

	if sess.ExpiresAt != nil && time.Now().After(*sess.ExpiresAt) {
		return domain.AgentResponse{}, fmt.Errorf("session expired")
	}

	userMsg, err := s.store.AppendMessage(ctx, sessionID, req.Role, req.Content, nil, nil)
	if err != nil {
		return domain.AgentResponse{}, err
	}
	s.linkMessageAttachments(ctx, userMsg.ID, req.AttachmentIDs)
	if s.resumeBlockedTask(ctx, sessionID, req) {
		return s.ackResumedAnswer(ctx, sessionID), nil
	}

	history, err := s.buildMessageHistory(ctx, sessionID)
	if err != nil {
		return domain.AgentResponse{}, err
	}

	if req.Role == domain.RoleUser {
		history = s.injectMentionContext(ctx, history, req)
	}

	if s.rag != nil && len(req.FileIDs) > 0 {
		history, err = s.rag.InjectContext(ctx, history, req.FileIDs)
		if err != nil {
			return domain.AgentResponse{}, err
		}
	}

	settings, err := s.loadSettings(ctx)
	if err != nil {
		return domain.AgentResponse{}, err
	}

	workspaceDir, taskBinding, err := s.ensureSessionWorkspace(ctx, sess, settings)
	if err != nil {
		return domain.AgentResponse{}, err
	}
	ctx, history, err = s.prepareRunContext(ctx, sess, sessionID, workspaceDir, settings.DefaultLanguage, history, taskBinding)
	if err != nil {
		return domain.AgentResponse{}, err
	}

	ctx, history, agentPolicy, agentModel, agentProvider, err := s.applyAgentContext(ctx, sess, history, settings.DefaultLanguage)
	if err != nil {
		return domain.AgentResponse{}, err
	}
	model := resolveChatModel(req.Model, sess.Model, agentModel)
	effectivePolicy := domain.UpliftWorkspaceTools(domain.MergeToolPolicy(policy, agentPolicy))

	history, err = s.applyContextPipeline(ctx, sessionID, history, model, agentProvider)
	if err != nil {
		return domain.AgentResponse{}, err
	}

	requestID := registry.RequestIDFromContext(ctx)
	runCtx, rec, err := activity.StartRun(ctx, s.activityStore, &sessionID, requestID, model)
	if err != nil {
		return domain.AgentResponse{}, err
	}
	runCtx, cancelRun := context.WithCancel(runCtx)
	defer cancelRun()
	handle, deregister := s.registerRun(sessionID, rec.RunID(), cancelRun)
	defer deregister()
	if rec != nil {
		rec.Step("user_message", map[string]string{"content": req.Content})
		defer func() {
			switch {
			case handle.userStopped():
				rec.Complete(domain.TaskAgentRunStatusCancelled)
			case err != nil:
				rec.Complete("failed")
			default:
				rec.Complete("completed")
			}
		}()
	}

	forceOrchestrate := req.Orchestrate != nil && *req.Orchestrate
	useOrchestrator := s.orchestrator != nil && s.orchestrator.ShouldOrchestrate(req.Content, forceOrchestrate)

	workspacePolicy := domain.EnsureAskUserTool(effectivePolicy)

	switch {
	case domain.RequiresHostExecutor(agentProvider):
		resp, err = s.runHostExecutedTurn(runCtx, sess, hostTurn{
			provider:     agentProvider,
			model:        model,
			policy:       workspacePolicy,
			workspaceDir: workspaceDir,
			history:      history,
			prompt:       req.Content,
			lang:         settings.DefaultLanguage,
		}, port.ChatStream{})
	case useOrchestrator && sess.AgentID != nil:
		resp, err = s.orchestrator.RunSolo(runCtx, req.Content, history, model, workspacePolicy, settings.DefaultLanguage, *sess.AgentID)
	case useOrchestrator:
		resp, err = s.orchestrator.RunMulti(runCtx, req.Content, history, model, workspacePolicy, settings.DefaultLanguage)
	default:
		resp, err = s.agentLoop.Run(runCtx, history, model, agentProvider, workspacePolicy, agent.WithLightModel(agentModel))
	}
	if err != nil {
		if handle.userStopped() {
			return domain.AgentResponse{}, domain.ErrRunCancelled
		}
		if block, ok := domain.QuotaBlockOf(err); ok {
			return domain.AgentResponse{}, s.parkTurnOnQuota(ctx, sessionID, req, policy, block, settings.DefaultLanguage)
		}
		s.appendAssistantError(ctx, sessionID, err)
		return domain.AgentResponse{}, err
	}
	if resp.Clarification != nil {
		resp = prompt.BuildClarificationResponse(*resp.Clarification)
	}

	s.persistAssistantResponse(ctx, sessionID, resp)

	return resp, nil
}

func (s *Service) resumeBlockedTask(ctx context.Context, sessionID uuid.UUID, req domain.SessionMessageRequest) bool {
	if s.answerResumer == nil || req.Role != domain.RoleUser {
		return false
	}
	answer := strings.TrimSpace(req.Content)
	if answer == "" {
		return false
	}
	return s.answerResumer.ResumeOnAnswer(context.WithoutCancel(ctx), sessionID, answer)
}

func (s *Service) ackResumedAnswer(ctx context.Context, sessionID uuid.UUID) domain.AgentResponse {
	lang := ""
	if settings, err := s.loadSettings(ctx); err == nil {
		lang = settings.DefaultLanguage
	}
	resp := domain.AgentResponse{Message: domain.Message{
		Role:    domain.RoleAssistant,
		Content: prompt.ClarificationAckMessage(lang),
	}}
	s.persistAssistantResponse(ctx, sessionID, resp)
	return resp
}

func (s *Service) persistAssistantResponse(ctx context.Context, sessionID uuid.UUID, resp domain.AgentResponse) {
	if strings.TrimSpace(resp.Message.Content) == "" && len(resp.Message.ToolCalls) == 0 && resp.Clarification == nil {
		log.Warn().Str("session_id", sessionID.String()).Msg("assistant produced an empty turn; not persisting it")
		return
	}
	toolCallsJSON, _ := json.Marshal(resp.Message.ToolCalls)
	var clarificationJSON []byte
	if resp.Clarification != nil {
		clarificationJSON, _ = json.Marshal(resp.Clarification)
	}
	_, _ = s.store.AppendMessage(ctx, sessionID, resp.Message.Role, resp.Message.Content, toolCallsJSON, clarificationJSON)
}

func (s *Service) SendMessageStream(ctx context.Context, sessionID uuid.UUID, req domain.SessionMessageRequest, policy domain.ToolPolicy, onToken func(string)) (resp domain.AgentResponse, err error) {
	sess, err := s.store.Get(ctx, sessionID)
	if err != nil {
		return domain.AgentResponse{}, err
	}

	if sess.ExpiresAt != nil && time.Now().After(*sess.ExpiresAt) {
		return domain.AgentResponse{}, fmt.Errorf("session expired")
	}

	userMsg, err := s.store.AppendMessage(ctx, sessionID, req.Role, req.Content, nil, nil)
	if err != nil {
		return domain.AgentResponse{}, err
	}
	s.linkMessageAttachments(ctx, userMsg.ID, req.AttachmentIDs)
	if s.resumeBlockedTask(ctx, sessionID, req) {
		resp := s.ackResumedAnswer(ctx, sessionID)
		if onToken != nil {
			onToken(resp.Message.Content)
		}
		return resp, nil
	}

	history, err := s.buildMessageHistory(ctx, sessionID)
	if err != nil {
		return domain.AgentResponse{}, err
	}

	if req.Role == domain.RoleUser {
		history = s.injectMentionContext(ctx, history, req)
	}

	if s.rag != nil && len(req.FileIDs) > 0 {
		history, err = s.rag.InjectContext(ctx, history, req.FileIDs)
		if err != nil {
			return domain.AgentResponse{}, err
		}
	}

	settings, err := s.loadSettings(ctx)
	if err != nil {
		return domain.AgentResponse{}, err
	}

	workspaceDir, taskBinding, err := s.ensureSessionWorkspace(ctx, sess, settings)
	if err != nil {
		return domain.AgentResponse{}, err
	}
	ctx, history, err = s.prepareRunContext(ctx, sess, sessionID, workspaceDir, settings.DefaultLanguage, history, taskBinding)
	if err != nil {
		return domain.AgentResponse{}, err
	}

	ctx, history, agentPolicy, agentModel, agentProvider, err := s.applyAgentContext(ctx, sess, history, settings.DefaultLanguage)
	if err != nil {
		return domain.AgentResponse{}, err
	}
	model := resolveChatModel(req.Model, sess.Model, agentModel)
	effectivePolicy := domain.UpliftWorkspaceTools(domain.MergeToolPolicy(policy, agentPolicy))

	history, err = s.applyContextPipeline(ctx, sessionID, history, model, agentProvider)
	if err != nil {
		return domain.AgentResponse{}, err
	}

	requestID := registry.RequestIDFromContext(ctx)
	runCtx, rec, err := activity.StartRun(ctx, s.activityStore, &sessionID, requestID, model)
	if err != nil {
		return domain.AgentResponse{}, err
	}
	runCtx, cancelRun := context.WithCancel(runCtx)
	defer cancelRun()
	handle, deregister := s.registerRun(sessionID, rec.RunID(), cancelRun)
	defer deregister()
	if rec != nil {
		rec.Step("user_message", map[string]string{"content": req.Content})
		defer func() {
			switch {
			case handle.userStopped():
				rec.Complete(domain.TaskAgentRunStatusCancelled)
			case err != nil:
				rec.Complete("failed")
			default:
				rec.Complete("completed")
			}
		}()
	}

	forceOrchestrate := req.Orchestrate != nil && *req.Orchestrate
	if !domain.RequiresHostExecutor(agentProvider) &&
		s.orchestrator != nil && s.orchestrator.ShouldOrchestrate(req.Content, forceOrchestrate) {
		return domain.AgentResponse{}, fmt.Errorf("orchestration does not support streaming")
	}

	workspacePolicy := domain.EnsureAskUserTool(effectivePolicy)
	var streamed strings.Builder
	capture := func(token string) {
		streamed.WriteString(token)
		if onToken != nil {
			onToken(token)
		}
	}
	if domain.RequiresHostExecutor(agentProvider) {
		resp, err = s.runHostExecutedTurn(runCtx, sess, hostTurn{
			provider:     agentProvider,
			model:        model,
			policy:       workspacePolicy,
			workspaceDir: workspaceDir,
			history:      history,
			prompt:       req.Content,
			lang:         settings.DefaultLanguage,
		}, port.ChatStream{
			OnText:         capture,
			OnSegmentBreak: func() { agent.SegmentBreak(runCtx) },
		})
	} else {
		resp, err = s.agentLoop.RunStream(runCtx, history, model, agentProvider, workspacePolicy, capture, agent.WithLightModel(agentModel))
	}
	if err != nil {
		if handle.userStopped() {
			s.persistCancelledTurn(ctx, sessionID, streamed.String())
			return domain.AgentResponse{}, domain.ErrRunCancelled
		}
		if block, ok := domain.QuotaBlockOf(err); ok {
			return domain.AgentResponse{}, s.parkTurnOnQuota(ctx, sessionID, req, policy, block, settings.DefaultLanguage)
		}
		s.appendAssistantError(ctx, sessionID, err)
		return domain.AgentResponse{}, err
	}

	s.persistAssistantResponse(ctx, sessionID, resp)

	return resp, nil
}

func (s *Service) ListRuns(ctx context.Context, sessionID uuid.UUID, limit int) ([]domain.SessionRun, error) {
	if s.activityStore == nil {
		return nil, fmt.Errorf("activity tracking not enabled")
	}
	return s.activityStore.ListRunsBySession(ctx, sessionID, limit)
}

func (s *Service) ListRunSteps(ctx context.Context, runID uuid.UUID) ([]domain.SessionStep, error) {
	if s.activityStore == nil {
		return nil, fmt.Errorf("activity tracking not enabled")
	}
	return s.activityStore.ListStepsByRun(ctx, runID)
}

func (s *Service) ListActiveRuns(ctx context.Context) ([]domain.SessionRun, error) {
	if s.activityStore == nil {
		return nil, fmt.Errorf("activity tracking not enabled")
	}
	return s.activityStore.ListActiveRuns(ctx)
}

func (s *Service) SetProjectRoot(ctx context.Context, sessionID uuid.UUID, projectRoot string, allowedRoots []string) (domain.Session, error) {
	settings, err := s.loadSettings(ctx)
	if err != nil {
		return domain.Session{}, err
	}
	abs, err := workspace.ValidateProjectRoot(projectRoot, settings.WorkspaceRoot, allowedRoots)
	if err != nil {
		return domain.Session{}, err
	}
	if err := s.store.UpdateProjectRoot(ctx, sessionID, abs); err != nil {
		return domain.Session{}, err
	}
	return s.store.Get(ctx, sessionID)
}

func (s *Service) applyContextPipeline(ctx context.Context, sessionID uuid.UUID, history []domain.Message, model string, provider domain.LLMProviderType) ([]domain.Message, error) {
	if s.indexInjector != nil && s.indexerCfg.Enabled {
		topK := s.indexerCfg.TopK
		if topK <= 0 {
			topK = 5
		}
		opts := domain.InjectOptions{
			TopK:            topK,
			IncludeTree:     s.mappingCfg.Enabled,
			IncludeSkeleton: s.mappingCfg.Enabled && s.mappingCfg.InjectOnSessionStart,
			MaxChunkTokens:  6000,
			RewriteQuery:    true,
		}
		injected, err := s.indexInjector.InjectContext(ctx, sessionID, history, opts)
		if err != nil {
			return nil, err
		}
		if len(injected) == len(history)+1 {
			history = insertBeforeLastUser(history, injected[0])
		} else {
			history = injected
		}
	}
	if s.summarizer != nil && s.contextCfg.SummarizeThreshold > 0 {
		var err error
		history, err = appcontext.SummarizeRollingFor(ctx, s.budget, s.summarizer, history, model, provider)
		if err != nil {
			return nil, err
		}
	}
	if s.budget.MaxTokens > 0 {
		history = s.budget.Apply(history)
	}
	return history, nil
}

func (s *Service) buildMessageHistory(ctx context.Context, sessionID uuid.UUID) ([]domain.Message, error) {
	msgs, err := s.store.ListMessages(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	history := make([]domain.Message, 0, len(msgs)+1)
	for _, m := range msgs {
		dm := domain.Message{Role: m.Role, Content: m.Content}
		if len(m.ToolCalls) > 0 {
			_ = json.Unmarshal(m.ToolCalls, &dm.ToolCalls)
		}
		if m.Clarification != nil {
			if note := prompt.ClarificationHistoryNote(*m.Clarification); note != "" {
				dm.Content = strings.TrimSpace(dm.Content + "\n\n" + note)
			}
		}
		history = append(history, dm)
	}
	// history is index-parallel to msgs here — the only point where a domain
	// message can still be traced back to the row whose attachments it owns.
	attachImageAttachments(ctx, s.attachments, msgs, history)
	return s.withActionDigest(ctx, sessionID, history), nil
}

func (s *Service) withActionDigest(ctx context.Context, sessionID uuid.UUID, history []domain.Message) []domain.Message {
	if s.actions == nil {
		return history
	}
	actions, err := s.actions.ListActions(ctx, sessionID)
	if err != nil || len(actions) == 0 {
		return history
	}
	digest := domain.SessionActionDigest(actions)
	if digest == "" {
		return history
	}
	return insertBeforeLastUser(history, domain.Message{Role: domain.RoleSystem, Content: digest})
}

func insertBeforeLastUser(history []domain.Message, msg domain.Message) []domain.Message {
	at := len(history)
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == domain.RoleUser {
			at = i
			break
		}
	}
	out := make([]domain.Message, 0, len(history)+1)
	out = append(out, history[:at]...)
	out = append(out, msg)
	out = append(out, history[at:]...)
	return out
}

func (s *Service) ListActions(ctx context.Context, sessionID uuid.UUID) ([]domain.SessionAction, error) {
	if s.actions == nil {
		return []domain.SessionAction{}, nil
	}
	return s.actions.ListActions(ctx, sessionID)
}

func (s *Service) LoadHistory(ctx context.Context, sessionID uuid.UUID) ([]domain.Message, error) {
	return s.buildMessageHistory(ctx, sessionID)
}

func (s *Service) loadSettings(ctx context.Context) (domain.AppSettings, error) {
	if s.settings == nil {
		return domain.AppSettings{WorkspaceRoot: "./data/workspaces", DefaultLanguage: "en"}, nil
	}
	return s.settings.Get(ctx)
}

func (s *Service) ensureSessionWorkspace(ctx context.Context, sess domain.Session, settings domain.AppSettings) (string, *TaskBinding, error) {
	if sess.TaskID != nil && sess.ProjectID != nil && s.taskWorkspaces != nil {
		binding, err := s.taskWorkspaces.ResolveTaskWorkspace(ctx, *sess.ProjectID, *sess.TaskID)
		if err != nil {
			return "", nil, fmt.Errorf("prepare the task's working copy: %w", err)
		}
		if binding.WorkspaceDir != "" && sess.WorkspaceDir != binding.WorkspaceDir {
			if err := s.store.UpdateWorkspaceDir(ctx, sess.ID, binding.WorkspaceDir); err != nil {
				return "", nil, err
			}
		}
		return binding.WorkspaceDir, &binding, nil
	}

	if sess.ProjectID != nil && s.repositories != nil {
		rootPath, err := s.repositories.ResolveRootPath(ctx, *sess.ProjectID)
		if err != nil {
			return "", nil, err
		}
		if err := workspace.EnsureDir(rootPath); err != nil {
			return "", nil, fmt.Errorf("ensure project workspace: %w", err)
		}
		if sess.WorkspaceDir != rootPath {
			if err := s.store.UpdateWorkspaceDir(ctx, sess.ID, rootPath); err != nil {
				return "", nil, err
			}
		}
		return rootPath, nil, nil
	}

	if sess.WorkspaceDir != "" {
		if err := workspace.EnsureDir(sess.WorkspaceDir); err != nil {
			return "", nil, fmt.Errorf("ensure session workspace: %w", err)
		}
		return sess.WorkspaceDir, nil, nil
	}

	workspaceDir, err := workspaceDirFor(settings.WorkspaceRoot, sess)
	if err != nil {
		return "", nil, err
	}
	if err := workspace.EnsureDir(workspaceDir); err != nil {
		return "", nil, fmt.Errorf("create session workspace: %w", err)
	}
	if err := s.store.UpdateWorkspaceDir(ctx, sess.ID, workspaceDir); err != nil {
		return "", nil, err
	}
	return workspaceDir, nil, nil
}

func (s *Service) prepareRunContext(
	ctx context.Context,
	sess domain.Session,
	sessionID uuid.UUID,
	workspaceDir string,
	lang string,
	history []domain.Message,
	task *TaskBinding,
) (context.Context, []domain.Message, error) {
	ctx = registry.ContextWithSessionID(registry.ContextWithWorkspaceDir(ctx, workspaceDir), sessionID)
	if task != nil {
		ctx = registry.ContextWithTaskID(ctx, task.Task.ID)
		ctx = registry.ContextWithBranch(ctx, task.Branch)
	}
	if sess.ProjectID != nil {
		ctx = registry.ContextWithProjectID(ctx, *sess.ProjectID)
		if s.repositories != nil {
			if repo, err := s.repositories.ResolveRepository(ctx, *sess.ProjectID); err == nil {
				if repo.Description != "" {
					history = prependProjectPrompt(history, repo.Description)
				}
				if repo.ProfileMD != "" {
					history = prependProjectProfilePrompt(history, repo.ProfileMD)
				}
			}
		}
	}
	history = prependWorkspacePrompt(history, workspaceDir, lang)
	if task != nil {
		history = prependTaskChatPrompt(history, *task)
	}
	return ctx, history, nil
}

// parkTurnOnQuota is the chat equivalent of the board runner parking a task on
// domain.QuotaBlock: instead of failing the turn, it records enough to rerun
// it once the usage limit lifts (see SessionQuotaSweeper) and tells the user
// their message is queued rather than that it failed.
//
// Best-effort by design, like appendAssistantError: this already runs on an
// error path, so a second failure here (the park itself could not be written)
// falls back to the plain notice rather than losing the turn's outcome
// entirely.
// The returned error is always a *domain.QuotaNotice (queued wording on
// success, or the block itself when the park could not be written) — never
// nil — so a caller that returns it straight through hands its own caller
// (the SSE handler, in particular) the sentence a reader should see, not the
// board-card-style Error() a bare *domain.QuotaBlock would give them.
func (s *Service) parkTurnOnQuota(ctx context.Context, sessionID uuid.UUID, req domain.SessionMessageRequest, policy domain.ToolPolicy, block *domain.QuotaBlock, lang string) error {
	if err := s.store.ParkPendingTurn(context.WithoutCancel(ctx), sessionID, req, policy, block.ResumeAt); err != nil {
		log.Warn().Err(err).Str("session_id", sessionID.String()).Msg("could not park a quota-blocked chat turn; the user will have to resend it")
		notice := domain.NewQuotaNotice(block, lang)
		s.appendAssistantError(ctx, sessionID, notice)
		return notice
	}
	notice := domain.NewQuotaQueuedNotice(block, lang)
	_, _ = s.store.AppendMessage(ctx, sessionID, domain.RoleAssistant, domain.QuotaQueuedNoticePrefix+" "+notice.Error(), nil, nil)
	return notice
}

func (s *Service) appendAssistantError(ctx context.Context, sessionID uuid.UUID, err error) {
	if err == nil {
		return
	}

	if rl, ok := domain.RateLimitOf(err); ok {
		_, _ = s.store.AppendMessage(ctx, sessionID, domain.RoleAssistant, domain.RateLimitNoticePrefix+" "+rl.UserMessage(), nil, nil)
		return
	}

	if _, ok := domain.QuotaBlockOf(err); ok {
		_, _ = s.store.AppendMessage(ctx, sessionID, domain.RoleAssistant, domain.RateLimitNoticePrefix+" "+err.Error(), nil, nil)
		return
	}
	_, _ = s.store.AppendMessage(ctx, sessionID, domain.RoleAssistant, fmt.Sprintf("**Error:** %s", err.Error()), nil, nil)
}

func (s *Service) validateCreateRequest(ctx context.Context, req domain.CreateSessionRequest) error {
	if req.TaskID != nil && req.ProjectID == nil {
		return fmt.Errorf("task_id requires project_id: a task is resolved through its repository")
	}
	if req.AgentID == nil {
		return nil
	}
	if s.catalog == nil {
		return fmt.Errorf("agent sessions are not available")
	}
	agent, err := s.catalog.GetAgent(ctx, *req.AgentID)
	if err != nil {
		return fmt.Errorf("agent not found")
	}
	if !agent.Enabled {
		return fmt.Errorf("agent is disabled")
	}
	if req.ProjectID != nil && s.repositories != nil {
		if _, err := s.repositories.ResolveRootPath(ctx, *req.ProjectID); err != nil {
			return fmt.Errorf("project not found")
		}
	}
	return nil
}

func resolveChatModel(reqModel, sessionModel, agentModel string) string {
	if reqModel != "" {
		return reqModel
	}
	if agentModel != "" {
		return agentModel
	}
	return sessionModel
}

func (s *Service) applyAgentContext(
	ctx context.Context,
	sess domain.Session,
	history []domain.Message,
	lang string,
) (context.Context, []domain.Message, domain.ToolPolicy, string, domain.LLMProviderType, error) {
	if sess.AgentID == nil || s.catalog == nil {
		return ctx, history, domain.ToolPolicy{}, "", "", nil
	}
	agentID := *sess.AgentID
	agentRec, err := s.catalog.GetAgent(ctx, agentID)
	if err != nil {
		return ctx, history, domain.ToolPolicy{}, "", "", fmt.Errorf("agent not found")
	}
	skills, err := s.catalog.ListSkillsByAgent(ctx, agentID)
	if err != nil {
		return ctx, history, domain.ToolPolicy{}, "", "", err
	}
	enabledSkills := make([]domain.Skill, 0, len(skills))
	for _, sk := range skills {
		if sk.Enabled {
			enabledSkills = append(enabledSkills, sk)
		}
	}
	stacks, err := s.catalog.ListTechStacksByAgent(ctx, agentID)
	if err != nil {
		return ctx, history, domain.ToolPolicy{}, "", "", err
	}
	rules, err := s.catalog.ListEnabledRulesByAgent(ctx, agentID)
	if err != nil {
		return ctx, history, domain.ToolPolicy{}, "", "", err
	}
	ruleTexts := make([]string, 0, len(rules))
	for _, rule := range rules {
		ruleTexts = append(ruleTexts, rule.Content)
	}
	ctx = registry.ContextWithAgentID(ctx, agentID)
	systemPrompt := prompt.BuildSystemPrompt(agentRec, enabledSkills, stacks, ruleTexts, lang)
	head := []domain.Message{{Role: domain.RoleSystem, Content: systemPrompt}}
	if s.kpis != nil {
		if kpiDefs, err := s.kpis.ListByAgent(ctx, agentID); err == nil && len(kpiDefs) > 0 {
			latest, _ := s.kpis.LatestResults(ctx, agentID)
			if kpiMsg := prompt.KPIContextMessage(kpiDefs, latest); kpiMsg != "" {
				head = append(head, domain.Message{Role: domain.RoleSystem, Content: kpiMsg})
			}
		}
	}
	if s.memories != nil {
		repoName := ""
		if sess.ProjectID != nil && s.repositories != nil {
			if repo, repoErr := s.repositories.ResolveRepository(ctx, *sess.ProjectID); repoErr == nil {
				repoName = repo.Name
			}
		}
		if mems := memory.Recall(ctx, s.memories, agentID, sess.ProjectID, 8); len(mems) > 0 {
			head = append(head, domain.Message{Role: domain.RoleSystem, Content: prompt.MemoryContextMessage(mems, repoName)})
		}
	}
	history = append(head, history...)
	return ctx, history, agentRec.ToolPolicy, agentRec.Model, agentRec.ProviderType, nil
}
