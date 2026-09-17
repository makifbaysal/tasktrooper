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

// SessionQuotaTaker is the one method SessionQuotaSweeper needs from
// port.SessionStore — named locally, board.QuotaResumeTaker style, so the
// dependency is obvious without following it back to the full store
// interface.
type SessionQuotaTaker interface {
	TakePendingSessionTurn(ctx context.Context, now time.Time) (domain.PendingSessionTurn, bool, error)
}

// QuotaSweeperInterval mirrors board.QuotaSweeperInterval: the two sweep
// independent state (board_tasks vs sessions) but answer the same question at
// the same cadence, so a task and a chat parked on the same usage-limit reset
// come back within a minute of each other rather than one waiting far longer
// than the other for no reason.
const QuotaSweeperInterval = time.Minute

// quotaSweepBatchCap mirrors board.quotaSweepBatchCap — see that constant's
// comment for why a pass is capped rather than draining every due row.
const quotaSweepBatchCap = 20

// SessionQuotaSweeper is board.QuotaSweeper's twin for chat sessions: it
// releases turns parked on the local Claude Code subscription's usage limit
// (see Service.parkTurnOnQuota), same claim-and-rerun shape, same
// one-pass-drains-everything-due pacing, for the same reason — a quota reset
// has no per-resume contention to pace around, unlike a held device.
type SessionQuotaSweeper struct {
	taker SessionQuotaTaker
	svc   *Service
	now   func() time.Time
}

func NewSessionQuotaSweeper(taker SessionQuotaTaker, svc *Service) *SessionQuotaSweeper {
	return &SessionQuotaSweeper{taker: taker, svc: svc, now: time.Now}
}

// Start runs the sweep on interval until ctx ends, sweeping once immediately
// on boot — a parked chat's resume time is a fact already on the row, so a
// restart changes nothing about whether it is already due.
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
		log.Info().Str("session_id", pending.SessionID.String()).Msg("parked chat turn resumed: the claude code usage limit has reset")
	}
	log.Info().Int("cap", quotaSweepBatchCap).
		Msg("session quota sweeper: per-pass cap reached, any remaining parked turns resume on the next pass")
}

// resumeParkedTurn reruns a turn a sweep claimed. It is SendMessage's body
// from "build history" on, minus the two steps that only make sense for a
// brand new message: appending it (already in the transcript — that is what
// made this a resumable turn rather than a lost one) and checking whether it
// answers a blocked task (resumeBlockedTask never runs the agent loop at all,
// so it could not have been the thing that hit the quota in the first place).
//
// A QuotaBlock hit here just re-parks: the CLI's own reset estimate corrects
// itself on every hit, so a still-closed window is not a bug, it is the same
// wait continuing under a fresher ResumeAt.
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
	ctx, history, err = s.prepareRunContext(ctx, sess, sessionID, workspaceDir, settings.DefaultLanguage, history, taskBinding)
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
