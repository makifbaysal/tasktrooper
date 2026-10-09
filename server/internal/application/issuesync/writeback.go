package issuesync

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// writeBackTracked tells the issue which tasks now carry it. Backgrounded: an
// import must not wait on a GitHub/Jira round trip, and a failure here must
// never undo a task that already exists.
func (s *Service) writeBackTracked(taskKeys []string, link domain.IssueLink) {
	keys := nonEmpty(taskKeys)
	if len(keys) == 0 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), s.writeBackTimeout)
		defer cancel()
		if !s.writeBackEnabled(ctx) {
			return
		}
		text := fmt.Sprintf("Tracked in TaskTrooper as %s.", strings.Join(keys, ", "))
		if err := s.commentOnIssue(ctx, link, text); err != nil {
			log.Warn().Err(err).Str("issue", link.ExternalKey).Str("provider", string(link.Provider)).
				Msg("issuesync: tracked comment failed")
		}
	}()
}

// TaskMoved implements board.TaskNotifier. It fires inside Dispatch for every
// task.moved event, whatever moved the task.
func (s *Service) TaskMoved(_ context.Context, task domain.BoardTask) {
	if s.links == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), s.writeBackTimeout)
		defer cancel()
		s.handleTaskMoved(ctx, task)
	}()
}

// TaskResumed implements board.TaskNotifier. A resume re-enters the column the
// task was parked from, so the issue has nothing new to hear.
func (s *Service) TaskResumed(context.Context, domain.BoardTask, string) {}

func (s *Service) handleTaskMoved(ctx context.Context, task domain.BoardTask) {
	link, err := s.links.GetByTask(ctx, task.ID)
	if err != nil {
		return
	}
	if link.ClosedAt != nil || !s.writeBackEnabled(ctx) {
		return
	}
	if finishedColumn(task.Column) {
		siblings, err := s.links.ListByIssue(ctx, link.Provider, link.ExternalKey)
		if err != nil {
			log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("issuesync: listing the issue's tasks failed")
			return
		}
		if s.allFinished(ctx, task, siblings) {
			s.writeBackDone(ctx, task, link, siblings)
			return
		}
	}
	if string(task.Column) == link.LastColumn {
		return
	}
	text := fmt.Sprintf("TaskTrooper: %s moved to %s.", task.Key, s.columnLabel(ctx, task.Column))
	if err := s.commentOnIssue(ctx, link, text); err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Str("provider", string(link.Provider)).
			Msg("issuesync: move comment failed")
		return
	}
	if err := s.links.UpdateColumn(ctx, task.ID, string(task.Column)); err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("issuesync: recording write-back column failed")
	}
}

func finishedColumn(col domain.TaskColumn) bool {
	return col == domain.TaskColumnDone || col == domain.TaskColumnReleased
}

// allFinished is true once every task the issue became is done or released; a
// sibling deleted from the board no longer holds the issue open.
func (s *Service) allFinished(ctx context.Context, moved domain.BoardTask, siblings []domain.IssueLink) bool {
	for _, l := range siblings {
		if l.TaskID == moved.ID {
			continue
		}
		if s.tasks == nil {
			return false
		}
		t, err := s.tasks.GetTask(ctx, l.RepositoryID, l.TaskID)
		if errors.Is(err, domain.ErrBoardTaskNotFound) || errors.Is(err, port.ErrNotFound) {
			continue
		}
		if err != nil || !finishedColumn(t.Column) {
			return false
		}
	}
	return true
}

func (s *Service) writeBackDone(ctx context.Context, task domain.BoardTask, link domain.IssueLink, siblings []domain.IssueLink) {
	if err := s.closeIssue(ctx, link); err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Str("provider", string(link.Provider)).
			Msg("issuesync: closing the issue failed")
		return
	}
	keys := make([]string, 0, len(siblings))
	for _, l := range siblings {
		keys = append(keys, l.TaskKey)
	}
	if len(nonEmpty(keys)) == 0 {
		keys = []string{task.Key}
	}
	keys = nonEmpty(keys)
	text := fmt.Sprintf("TaskTrooper: %s is done.", keys[0])
	if len(keys) > 1 {
		text = fmt.Sprintf("TaskTrooper: %s are done.", strings.Join(keys, ", "))
	}
	if err := s.commentOnIssue(ctx, link, text); err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Str("provider", string(link.Provider)).
			Msg("issuesync: done comment failed")
	}
	at := s.now()
	if err := s.links.MarkIssueClosed(ctx, link.Provider, link.ExternalKey, at); err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("issuesync: recording issue closed failed")
	}
	if s.imports != nil {
		if imp, err := s.imports.GetByProviderKey(ctx, link.Provider, link.ExternalKey); err == nil {
			if err := s.imports.MarkClosed(ctx, imp.ID, at); err != nil {
				log.Warn().Err(err).Str("issue", link.ExternalKey).Msg("issuesync: recording import closed failed")
			}
		}
	}
}

func (s *Service) writeBackEnabled(ctx context.Context) bool {
	if s.syncSettings == nil {
		return domain.DefaultIssueSyncSettings().WriteBack
	}
	settings, err := s.syncSettings.IssueSyncSettings(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("issuesync: reading issue-sync settings failed, write-back skipped")
		return false
	}
	return settings.WriteBack
}

func (s *Service) columnLabel(ctx context.Context, column domain.TaskColumn) string {
	if s.columns != nil {
		if cols, err := s.columns.ListColumns(ctx); err == nil {
			for _, c := range cols {
				if c.Slug == string(column) {
					return c.Label
				}
			}
		}
	}
	return string(column)
}

func (s *Service) commentOnIssue(ctx context.Context, link domain.IssueLink, text string) error {
	switch link.Provider {
	case domain.IssueProviderGitHub:
		owner, repo, number, token, err := s.githubTarget(ctx, link)
		if err != nil {
			return err
		}
		return s.githubIssues.AddComment(ctx, token, owner, repo, number, text)
	case domain.IssueProviderJira:
		client, connected, err := s.jiraClient(ctx)
		if err != nil {
			return err
		}
		if !connected {
			return ErrSourceNotConfigured
		}
		return client.AddComment(ctx, link.ExternalKey, text)
	default:
		return fmt.Errorf("%w: unknown provider %q", ErrInvalidIssue, link.Provider)
	}
}

func (s *Service) closeIssue(ctx context.Context, link domain.IssueLink) error {
	switch link.Provider {
	case domain.IssueProviderGitHub:
		owner, repo, number, token, err := s.githubTarget(ctx, link)
		if err != nil {
			return err
		}
		return s.githubIssues.Close(ctx, token, owner, repo, number)
	case domain.IssueProviderJira:
		client, connected, err := s.jiraClient(ctx)
		if err != nil {
			return err
		}
		if !connected {
			return ErrSourceNotConfigured
		}
		_, err = client.TransitionToDone(ctx, link.ExternalKey)
		return err
	default:
		return fmt.Errorf("%w: unknown provider %q", ErrInvalidIssue, link.Provider)
	}
}

func (s *Service) githubTarget(ctx context.Context, link domain.IssueLink) (owner, repo string, number int, token string, err error) {
	if s.githubIssues == nil {
		return "", "", 0, "", ErrSourceNotConfigured
	}
	owner, repo, number, ok := parseGitHubIssueKey(link.ExternalKey)
	if !ok {
		return "", "", 0, "", fmt.Errorf("%w: %q", ErrInvalidIssue, link.ExternalKey)
	}
	token, err = s.githubToken(ctx)
	if err != nil || token == "" {
		return "", "", 0, "", ErrSourceNotConfigured
	}
	return owner, repo, number, token, nil
}

func nonEmpty(items []string) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		if strings.TrimSpace(it) != "" {
			out = append(out, it)
		}
	}
	return out
}
