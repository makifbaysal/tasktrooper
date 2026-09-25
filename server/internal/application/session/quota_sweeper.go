package session

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/activity"
	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type SessionQuotaTaker interface {
	TakePendingSessionTurn(ctx context.Context, now time.Time) (domain.PendingSessionTurn, bool, error)
}

const QuotaSweeperInterval = time.Minute

const quotaSweepBatchCap = 20

type SessionQuotaSweeper struct {
	taker SessionQuotaTaker
	svc   *Service
	now   func() time.Time
}

func NewSessionQuotaSweeper(taker SessionQuotaTaker, svc *Service) *SessionQuotaSweeper {
	return &SessionQuotaSweeper{taker: taker, svc: svc, now: time.Now}
}

func (s *SessionQuotaSweeper) Start(ctx context.Context, interval time.Duration) {
	if s == nil || s.taker == nil || s.svc == nil {
		return
	}
	if interval <= 0 {
		interval = QuotaSweeperInterval
	}
	go func() {
		s.sweep(ctx)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.sweep(ctx)
			}
		}
	}()
}

func (s *SessionQuotaSweeper) sweep(ctx context.Context) {
	now := s.now()
	for range quotaSweepBatchCap {
		pending, ok, err := s.taker.TakePendingSessionTurn(ctx, now)
		if err != nil {
			log.Warn().Err(err).Msg("session quota sweeper: claiming a parked chat turn failed")
			return
		}
		if !ok {
			return
		}
		if err := s.svc.resumeParkedTurn(ctx, pending); err != nil {
			log.Warn().Err(err).Str("session_id", pending.SessionID.String()).Msg("session quota sweeper: resuming a parked chat turn failed")
			continue
		}
		log.Info().Str("session_id", pending.SessionID.String()).Msg("parked chat turn resumed: the agent cli usage limit has reset")
	}
	log.Info().Int("cap", quotaSweepBatchCap).
		Msg("session quota sweeper: per-pass cap reached, any remaining parked turns resume on the next pass")
}

func (s *Service) resumeParkedTurn(ctx context.Context, pending domain.PendingSessionTurn) (err error) {
	sessionID := pending.SessionID
	req := pending.Request

	sess, err := s.store.Get(ctx, sessionID)
	if err != nil {
		return err
	}
	if sess.ExpiresAt != nil && time.Now().After(*sess.ExpiresAt) {
		return nil
	}

	history, err := s.buildMessageHistory(ctx, sessionID)
	if err != nil {
		return err
	}
	if req.Role == domain.RoleUser {
		history = s.injectMentionContext(ctx, history, req)
	}
	if s.rag != nil && len(req.FileIDs) > 0 {
		history, err = s.rag.InjectContext(ctx, history, req.FileIDs)
		if err != nil {
			return err
		}
	}

	settings, err := s.loadSettings(ctx)
	if err != nil {
		return err
	}

	workspaceDir, taskBinding, err := s.ensureSessionWorkspace(ctx, sess, settings)
	if err != nil {
		return err
	}
	ctx, history, err = s.prepareRunContext(ctx, sess, sessionID, workspaceDir, settings.DefaultLanguage, history, taskBinding, s.effectiveToolPolicy(ctx, sess, pending.Policy))
	if err != nil {
		return err
	}

	ctx, history, agentPolicy, agentModel, agentProvider, err := s.applyAgentContext(ctx, sess, history, settings.DefaultLanguage)
	if err != nil {
		return err
	}
	model := resolveChatModel(req.Model, sess.Model, agentModel)
	effectivePolicy := domain.UpliftWorkspaceTools(domain.MergeToolPolicy(pending.Policy, agentPolicy))

	history, err = s.applyContextPipeline(ctx, sessionID, history, model, agentProvider)
	if err != nil {
		return err
	}

	requestID := registry.RequestIDFromContext(ctx)
	runCtx, rec, err := activity.StartRun(ctx, s.activityStore, &sessionID, requestID, model)
	if err != nil {
		return err
	}
	runCtx, cancelRun := context.WithCancel(runCtx)
	defer cancelRun()
	handle, deregister := s.registerRun(sessionID, rec.RunID(), cancelRun)
	defer deregister()
	if rec != nil {
		rec.Step("quota_resume", map[string]string{"resumed": "quota_reset"})
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

	var resp domain.AgentResponse
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
			return domain.ErrRunCancelled
		}
		if block, ok := domain.QuotaBlockOf(err); ok {
			s.parkTurnOnQuota(ctx, sessionID, req, pending.Policy, block, settings.DefaultLanguage)
			return nil
		}
		s.appendAssistantError(ctx, sessionID, err)
		return err
	}
	if resp.Clarification != nil {
		resp = prompt.BuildClarificationResponse(*resp.Clarification)
	}
	s.persistAssistantResponse(ctx, sessionID, resp)
	return nil
}
