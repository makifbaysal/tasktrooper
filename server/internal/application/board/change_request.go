package board

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	ChangeDeliveryNotified    = "agent_notified"
	ChangeDeliveryInterrupted = "interrupted"
	ChangeDeliveryStored      = "stored"

	changeRequestRounds = 2
)

type ChangeRequestRunner interface {
	ActiveRunForTask(taskID uuid.UUID) (uuid.UUID, bool)
}

type ChangeRequestInterrupter interface {
	InterruptRunAs(ctx context.Context, repositoryID, taskID, runID uuid.UUID, req domain.CreateTaskCommentRequest) (domain.TaskAgentRun, error)
}

type ChangeRequestDeps struct {
	Tasks       port.BoardTaskStore
	Runner      ChangeRequestRunner
	Interrupter ChangeRequestInterrupter
	Commenter   InterruptCommenter
}

// ChangeRequests delivers a change the stakeholder asked for to the run that is
// working the task. The change itself (description, criteria) is applied to the
// task through the usual services; this only makes sure the agent hears of it.
type ChangeRequests struct {
	tasks       port.BoardTaskStore
	runner      ChangeRequestRunner
	interrupter ChangeRequestInterrupter
	commenter   InterruptCommenter
}

func NewChangeRequests(deps ChangeRequestDeps) *ChangeRequests {
	return &ChangeRequests{tasks: deps.Tasks, runner: deps.Runner, interrupter: deps.Interrupter, commenter: deps.Commenter}
}

type ChangeDelivery struct {
	Outcome   string     `json:"outcome"`
	RunID     *uuid.UUID `json:"run_id,omitempty"`
	CommentID *uuid.UUID `json:"comment_id,omitempty"`
}

// Deliver records the request as a task comment (the durable copy every later
// run reads) and, when this install has a run holding the task, either leaves it
// for the end of that run's turn or, with interrupt, stops the run and starts
// the same agent again on the task as it stands.
func (s *ChangeRequests) Deliver(ctx context.Context, repositoryID, taskID uuid.UUID, authorAgentID uuid.UUID, summary string, interrupt bool) (ChangeDelivery, error) {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return ChangeDelivery{}, domain.ErrChangeRequestEmpty
	}
	if utf8.RuneCountInString(summary) > domain.MaxChangeRequestChars {
		return ChangeDelivery{}, domain.ErrChangeRequestTooLong
	}
	if s.tasks == nil || s.commenter == nil {
		return ChangeDelivery{}, fmt.Errorf("change requests are not available on this deployment")
	}
	if _, err := s.tasks.Get(ctx, repositoryID, taskID); err != nil {
		return ChangeDelivery{}, err
	}

	var liveRun uuid.UUID
	if s.runner != nil {
		if id, ok := s.runner.ActiveRunForTask(taskID); ok {
			liveRun = id
		}
	}
	content := domain.ChangeRequestComment(summary)

	if interrupt && liveRun != uuid.Nil && s.interrupter != nil {
		fresh, err := s.interrupter.InterruptRunAs(ctx, repositoryID, taskID, liveRun, domain.CreateTaskCommentRequest{
			Content:    content,
			AuthorType: "agent",
			AuthorID:   authorAgentID.String(),
		})
		if err == nil {
			return ChangeDelivery{Outcome: ChangeDeliveryInterrupted, RunID: &fresh.ID}, nil
		}
		if err != domain.ErrRunNotLive {
			return ChangeDelivery{}, err
		}
		liveRun = uuid.Nil
	}

	comment, err := s.commenter.AddComment(ctx, repositoryID, taskID, domain.CreateTaskCommentRequest{
		Content:       content,
		AuthorType:    "agent",
		AuthorID:      authorAgentID.String(),
		Informational: true,
	})
	if err != nil {
		return ChangeDelivery{}, err
	}
	out := ChangeDelivery{Outcome: ChangeDeliveryStored, CommentID: &comment.ID}
	if liveRun != uuid.Nil {
		out.Outcome = ChangeDeliveryNotified
		out.RunID = &liveRun
	}
	return out, nil
}

func (r *Runner) ActiveRunForTask(taskID uuid.UUID) (uuid.UUID, bool) {
	if r == nil {
		return uuid.Nil, false
	}
	r.activeMu.RLock()
	defer r.activeMu.RUnlock()
	id, ok := r.activeTasks[taskID]
	return id, ok
}

const changeRequestsLimit = 5

var changeRequestsHeaderKey = prompt.Define("board_context.change_requests_header", struct{}{})

type changeRequestFollowUpInput struct{ Requests string }

var changeRequestFollowUpKey = prompt.Define("board_context.change_request_follow_up",
	changeRequestFollowUpInput{Requests: "- 2026-01-02 10:00Z add an export button"})

func changeRequestComments(comments []domain.TaskComment) []domain.TaskComment {
	var out []domain.TaskComment
	for _, c := range comments {
		if domain.IsChangeRequestComment(c.Content) {
			out = append(out, c)
		}
	}
	return out
}

func formatChangeRequests(items []domain.TaskComment) string {
	var sb strings.Builder
	for _, c := range items {
		content := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(c.Content), domain.ChangeRequestCommentPrefix))
		if len(content) > domain.MaxChangeRequestChars {
			content = truncateHead(content, domain.MaxChangeRequestChars) + "…"
		}
		stamp := ""
		if !c.CreatedAt.IsZero() {
			stamp = c.CreatedAt.UTC().Format("2006-01-02 15:04Z") + " "
		}
		sb.WriteString(fmt.Sprintf("- %s%s\n", stamp, content))
	}
	return sb.String()
}

// changeRequestsMessage is every run's opening view of what the stakeholder
// changed after the task was written: the newest few, oldest first.
func changeRequestsMessage(comments []domain.TaskComment) string {
	items := changeRequestComments(comments)
	if len(items) == 0 {
		return ""
	}
	if len(items) > changeRequestsLimit {
		items = items[len(items)-changeRequestsLimit:]
	}
	return prompt.Text(changeRequestsHeaderKey) + "\n" + formatChangeRequests(items)
}

func changeRequestIDs(comments []domain.TaskComment) map[uuid.UUID]bool {
	seen := map[uuid.UUID]bool{}
	for _, c := range changeRequestComments(comments) {
		seen[c.ID] = true
	}
	return seen
}

// sweepChangeRequests is the turn boundary a live run picks changes up at: after
// the agent's pass and before anything is handed on, any change request that
// arrived since the run read the task is put to it as a follow-up in the same
// session. seen holds the requests the run already has; it is updated in place.
func (r *Runner) sweepChangeRequests(
	ctx context.Context,
	job RunJob,
	agentRec domain.Agent,
	history []domain.Message,
	resp domain.AgentResponse,
	model string,
	policy domain.ToolPolicy,
	seen map[uuid.UUID]bool,
) (domain.AgentResponse, *domain.QuotaBlock) {
	current := resp
	for round := 1; round <= changeRequestRounds; round++ {
		var fresh []domain.TaskComment
		for _, c := range changeRequestComments(r.taskComments(ctx, job)) {
			if !seen[c.ID] {
				fresh = append(fresh, c)
			}
		}
		if len(fresh) == 0 {
			return current, nil
		}
		for _, c := range fresh {
			seen[c.ID] = true
		}
		ask := domain.Message{Role: domain.RoleUser, Content: changeRequestFollowUpKey.Render(changeRequestFollowUpInput{Requests: formatChangeRequests(fresh)})}
		turn := append(followUpHistory(history, current), ask)
		swept, err := r.agentLoop.RunTask(ctx, turn, model, agentRec.ProviderType, policy,
			agent.WithLightModel(agentRec.Model),
			agent.WithStableHead(len(history)),
			agent.WithCLILabel(fmt.Sprintf("%s change-request %d", job.Task.Key, round), job.Task.Title))
		if err != nil {
			if quotaErr, ok := domain.QuotaBlockOf(err); ok {
				return current, quotaErr
			}
			log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Int("round", round).
				Msg("change request follow-up failed; the next run reads the request with its opening context")
			return current, nil
		}
		if len(swept.Transcript) > 0 {
			current.Transcript = swept.Transcript
		}
		if strings.TrimSpace(swept.Message.Content) != "" {
			current.Message = swept.Message
		}
	}
	return current, nil
}
