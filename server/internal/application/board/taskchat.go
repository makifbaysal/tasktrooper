package board

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type TaskChatSessions interface {
	Get(ctx context.Context, id uuid.UUID) (domain.Session, error)
	Create(ctx context.Context, title, model, workspaceDir string, projectID, agentID *uuid.UUID, expiresAt *time.Time) (domain.Session, error)
	BindTask(ctx context.Context, id, taskID uuid.UUID) error
	FindByTask(ctx context.Context, taskID uuid.UUID) (domain.Session, bool, error)
	AppendMessage(ctx context.Context, sessionID uuid.UUID, role domain.Role, content string, toolCalls []byte, clarification []byte) (domain.SessionMessage, error)
}

type TaskChatAgents interface {
	GetAgent(ctx context.Context, id uuid.UUID) (domain.Agent, error)
}

type TaskChatColumns interface {
	AgentsForColumn(ctx context.Context, column, taskType string) ([]uuid.UUID, error)
}

type TaskChatOpenerDeps struct {
	Tasks    TaskPRTasks
	Sessions TaskChatSessions
	Agents   TaskChatAgents
	Columns  TaskChatColumns
	Repos    RootPathResolver
	Criteria port.AcceptanceCriterionStore
	Roles    port.RoleResolver
	RepoArea RepoAreaFunc
}

// One thread per task, forever: a second chat would split a conversation both sides have already settled.
type TaskChatOpener struct {
	tasks    TaskPRTasks
	sessions TaskChatSessions
	agents   TaskChatAgents
	columns  TaskChatColumns
	repos    RootPathResolver
	criteria port.AcceptanceCriterionStore
	roles    port.RoleResolver
	repoArea RepoAreaFunc
}

func NewTaskChatOpener(deps TaskChatOpenerDeps) *TaskChatOpener {
	return &TaskChatOpener{
		tasks:    deps.Tasks,
		sessions: deps.Sessions,
		agents:   deps.Agents,
		columns:  deps.Columns,
		repos:    deps.Repos,
		criteria: deps.Criteria,
		roles:    deps.Roles,
		repoArea: deps.RepoArea,
	}
}

// Reading the task through its repository is the ownership check: another repo's id comes back not-found, no chat opens.
func (o *TaskChatOpener) Open(ctx context.Context, repositoryID, taskID uuid.UUID) (uuid.UUID, uuid.UUID, error) {
	if o.sessions == nil || o.tasks == nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("task chat is not available on this deployment")
	}
	task, err := o.tasks.Get(ctx, repositoryID, taskID)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	if sess, ok, findErr := o.sessions.FindByTask(ctx, taskID); findErr != nil {
		return uuid.Nil, uuid.Nil, findErr
	} else if ok {
		return sess.ID, agentIDOf(sess), nil
	}

	if existing := task.ClarificationSessionID; existing != nil {
		if sess, getErr := o.sessions.Get(ctx, *existing); getErr == nil {
			if bindErr := o.sessions.BindTask(ctx, sess.ID, taskID); bindErr != nil {
				return uuid.Nil, uuid.Nil, bindErr
			}
			return sess.ID, agentIDOf(sess), nil
		}
		log.Info().Str("task_id", taskID.String()).Str("session_id", existing.String()).
			Msg("task chat: recorded clarification thread is gone, opening a new one")
	}

	agentRec, hasAgent := o.resolveAgent(ctx, task)
	var agentID *uuid.UUID
	model := ""
	if hasAgent {
		id := agentRec.ID
		agentID = &id
		model = agentRec.Model
	}
	rootPath := ""
	if o.repos != nil {
		if resolved, rootErr := o.repos.ResolveRootPath(ctx, repositoryID); rootErr == nil {
			rootPath = resolved
		}
	}
	title := domain.TruncateHead(strings.TrimSpace(task.Key+" "+task.Title), 120)
	sess, err := o.sessions.Create(ctx, title, model, rootPath, &repositoryID, agentID, nil)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if err := o.sessions.BindTask(ctx, sess.ID, taskID); err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	o.seedOpeningMessage(ctx, sess.ID, task)
	return sess.ID, agentIDOf(sess), nil
}

func (o *TaskChatOpener) resolveAgent(ctx context.Context, task domain.BoardTask) (domain.Agent, bool) {
	if o.agents == nil {
		return domain.Agent{}, false
	}
	if task.AssigneeAgentID != nil {
		if agentRec, err := o.agents.GetAgent(ctx, *task.AssigneeAgentID); err == nil && agentRec.Enabled {
			return agentRec, true
		}
	}
	if o.columns == nil {
		return domain.Agent{}, false
	}
	ids, err := o.columns.AgentsForColumn(ctx, string(task.Column), string(task.TaskType))
	if err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("task chat: column agent lookup failed")
		return domain.Agent{}, false
	}
	ids = subscribersForTaskArea(ctx, o.roles, o.repoArea, task, ids)
	for _, id := range ids {
		if agentRec, err := o.agents.GetAgent(ctx, id); err == nil && agentRec.Enabled {
			return agentRec, true
		}
	}
	return domain.Agent{}, false
}

func (o *TaskChatOpener) seedOpeningMessage(ctx context.Context, sessionID uuid.UUID, task domain.BoardTask) {
	var criteria []domain.AcceptanceCriterion
	if o.criteria != nil {
		if items, err := o.criteria.ListByTask(ctx, task.ID); err == nil {
			criteria = items
		}
	}
	content := prompt.TaskChatOpeningMessage(task, criteria)
	if content == "" {
		return
	}
	if _, err := o.sessions.AppendMessage(ctx, sessionID, domain.RoleAssistant, content, nil, nil); err != nil {
		log.Warn().Err(err).Str("session_id", sessionID.String()).Msg("task chat: opening message append failed")
	}
}

func agentIDOf(sess domain.Session) uuid.UUID {
	if sess.AgentID == nil {
		return uuid.Nil
	}
	return *sess.AgentID
}
