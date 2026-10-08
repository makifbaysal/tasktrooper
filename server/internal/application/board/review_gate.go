package board

import (
	"context"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/rs/zerolog/log"
)

type VerdictStore interface {
	SetReviewVerdict(ctx context.Context, taskID uuid.UUID, column, verdict string) error
	OpenSpan(ctx context.Context, taskID uuid.UUID) (domain.TaskColumnSpan, bool, error)
}

type EscapeCharger interface {
	ApplyReviewEscape(ctx context.Context, task domain.BoardTask, agentID uuid.UUID)
}

type ReviewGate struct {
	spans     VerdictStore
	escapes   EscapeCharger
	workflows port.WorkflowReader
	roles     port.RoleResolver
	quorum    *ReviewQuorum
}

func NewReviewGate(spans VerdictStore, escapes EscapeCharger) *ReviewGate {
	return &ReviewGate{spans: spans, escapes: escapes}
}

func (g *ReviewGate) SetWorkflows(w port.WorkflowReader)  { g.workflows = w }
func (g *ReviewGate) SetRoleResolver(r port.RoleResolver) { g.roles = r }
func (g *ReviewGate) SetQuorum(q *ReviewQuorum)           { g.quorum = q }

// Approval becomes a verdict and is held; rejection is allowed through - human gates let work through, not send it back.
func (g *ReviewGate) InterceptAgentMove(ctx context.Context, task domain.BoardTask, from, to domain.TaskColumn, actor domain.TaskActor, repo domain.Repository) bool {
	if g == nil || g.spans == nil || actor != domain.TaskActorAgent || !repo.RequireHumanReview {
		return true
	}
	if !holdsForHumanApproval(from) {
		return true
	}
	verdict := domain.ReviewVerdictApprove
	if to == domain.TaskColumnNeedRevision {
		verdict = domain.ReviewVerdictReject
	}
	if err := g.spans.SetReviewVerdict(ctx, task.ID, string(from), verdict); err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("set review verdict failed")
	}
	return verdict == domain.ReviewVerdictReject
}

// code_review is the one column an agent can approve its way out of that a
// human has not already seen, so it is the only one held when the repository
// requires human review. Not a per-stage setting: pm_uat and human_uat are
// human-facing by definition and holding them made humans approve twice, and
// no other column has an agent-issued approval to hold in the first place.
func holdsForHumanApproval(from domain.TaskColumn) bool {
	return from == domain.TaskColumnCodeReview
}

func (g *ReviewGate) OnHumanRejection(ctx context.Context, task domain.BoardTask, from domain.TaskColumn) {
	if g == nil || g.spans == nil || g.escapes == nil {
		return
	}
	span, ok, err := g.spans.OpenSpan(ctx, task.ID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("open span lookup failed")
		return
	}
	if !ok || span.BoardColumn != string(from) || span.ReviewVerdict != domain.ReviewVerdictApprove {
		return
	}
	// Every reviewer that signed off missed what the human caught, not only
	// the one the span happened to record first.
	if approvers := g.quorum.Approvers(ctx, task.ID, from); len(approvers) > 0 {
		for _, id := range approvers {
			g.escapes.ApplyReviewEscape(ctx, task, id)
		}
		return
	}
	if span.AgentID == nil {
		return
	}
	g.escapes.ApplyReviewEscape(ctx, task, *span.AgentID)
}
