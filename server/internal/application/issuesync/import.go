package issuesync

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// Imported is one successful import: the task opened straight from the issue,
// its link, and the issue-level record whose ConversionStatus says whether
// the product manager is about to replace that task with its own.
type Imported struct {
	Task   domain.BoardTask   `json:"task"`
	Link   domain.IssueLink   `json:"link"`
	Import domain.IssueImport `json:"import"`
}

// Import fetches the issue, opens a task for it and links the two. createdBy
// is the task's CreatedBy; actorUID is "" for an automatic import. They are
// separate so a member whose uid happens to be "github" is never read as the
// poller.
func (s *Service) Import(ctx context.Context, provider domain.IssueProvider, key string, repositoryID uuid.UUID, createdBy, actorUID string) (Imported, error) {
	if s.tasks == nil || s.links == nil || s.imports == nil {
		return Imported{}, ErrSourceNotConfigured
	}
	if err := s.rejectIfAlreadyImported(ctx, provider, key); err != nil {
		return Imported{}, err
	}

	var issue domain.ExternalIssue
	var req domain.CreateBoardTaskRequest
	var err error
	switch provider {
	case domain.IssueProviderGitHub:
		issue, req, err = s.fetchGitHub(ctx, key, repositoryID)
	case domain.IssueProviderJira:
		issue, req, err = s.fetchJira(ctx, key, repositoryID)
	default:
		return Imported{}, fmt.Errorf("%w: unknown provider %q", ErrInvalidIssue, provider)
	}
	if err != nil {
		return Imported{}, err
	}
	req.CreatedBy = createdBy
	task, err := s.tasks.CreateTask(ctx, repositoryID, req)
	if err != nil {
		return Imported{}, fmt.Errorf("create task: %w", err)
	}
	return s.finishImport(ctx, task, repositoryID, provider, issue, actorUID)
}

// rejectIfAlreadyImported lets an issue back in once every task it became has
// been deleted, the same way deleting an imported task always did.
func (s *Service) rejectIfAlreadyImported(ctx context.Context, provider domain.IssueProvider, key string) error {
	existing, err := s.imports.GetByProviderKey(ctx, provider, key)
	if errors.Is(err, port.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("check existing import: %w", err)
	}
	links, err := s.links.ListByIssue(ctx, provider, key)
	if err != nil {
		return fmt.Errorf("check existing import: %w", err)
	}
	if len(links) == 0 && !conversionInFlight(existing.ConversionStatus) {
		if err := s.imports.Delete(ctx, existing.ID); err != nil {
			return fmt.Errorf("forget stale import: %w", err)
		}
		return nil
	}
	return alreadyImportedError(existing, links)
}

func conversionInFlight(status domain.IssueConversionStatus) bool {
	return status == domain.IssueConversionPending || status == domain.IssueConversionConverting
}

func alreadyImportedError(imp domain.IssueImport, links []domain.IssueLink) error {
	out := &AlreadyImportedError{RepositoryID: imp.RepositoryID}
	if len(links) > 0 {
		out.TaskID = links[0].TaskID
		out.TaskKey = links[0].TaskKey
		out.RepositoryID = links[0].RepositoryID
	}
	return out
}

func (s *Service) fetchGitHub(ctx context.Context, key string, repositoryID uuid.UUID) (domain.ExternalIssue, domain.CreateBoardTaskRequest, error) {
	owner, repoName, number, ok := parseGitHubIssueKey(key)
	if !ok {
		return domain.ExternalIssue{}, domain.CreateBoardTaskRequest{}, fmt.Errorf("%w: %q is not \"owner/repo#N\"", ErrInvalidIssue, key)
	}
	if s.repos == nil || s.githubIssues == nil {
		return domain.ExternalIssue{}, domain.CreateBoardTaskRequest{}, ErrSourceNotConfigured
	}
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return domain.ExternalIssue{}, domain.CreateBoardTaskRequest{}, fmt.Errorf("get repository: %w", err)
	}
	remoteOwner, remoteName, ok := githubOwnerRepo(repo.RemoteURL)
	if !ok || !strings.EqualFold(remoteOwner, owner) || !strings.EqualFold(remoteName, repoName) {
		return domain.ExternalIssue{}, domain.CreateBoardTaskRequest{}, fmt.Errorf("%w: repository's GitHub remote is not %s/%s", ErrInvalidIssue, owner, repoName)
	}
	token, err := s.githubToken(ctx)
	if err != nil {
		return domain.ExternalIssue{}, domain.CreateBoardTaskRequest{}, fmt.Errorf("github token: %w", err)
	}
	if token == "" {
		return domain.ExternalIssue{}, domain.CreateBoardTaskRequest{}, fmt.Errorf("%w: github is not connected", ErrSourceNotConfigured)
	}
	issue, err := s.githubIssues.Get(ctx, token, owner, repoName, number)
	if err != nil {
		return domain.ExternalIssue{}, domain.CreateBoardTaskRequest{}, fmt.Errorf("get github issue: %w: %w", ErrUpstream, err)
	}
	taskType, err := s.resolveTaskType(ctx, hasBugLabel(issue.Labels))
	if err != nil {
		return domain.ExternalIssue{}, domain.CreateBoardTaskRequest{}, err
	}
	return issue, domain.CreateBoardTaskRequest{
		Title:       issue.Title,
		Description: buildDescription(issue.Key, issue.URL, issue.Body),
		TaskType:    taskType,
	}, nil
}

func (s *Service) fetchJira(ctx context.Context, key string, repositoryID uuid.UUID) (domain.ExternalIssue, domain.CreateBoardTaskRequest, error) {
	if !jiraKeyPattern.MatchString(key) {
		return domain.ExternalIssue{}, domain.CreateBoardTaskRequest{}, fmt.Errorf("%w: %q is not a Jira issue key", ErrInvalidIssue, key)
	}
	if repositoryID == uuid.Nil {
		return domain.ExternalIssue{}, domain.CreateBoardTaskRequest{}, fmt.Errorf("%w: repository_id required", ErrInvalidIssue)
	}
	client, connected, err := s.jiraClient(ctx)
	if err != nil {
		return domain.ExternalIssue{}, domain.CreateBoardTaskRequest{}, fmt.Errorf("jira settings: %w", err)
	}
	if !connected {
		return domain.ExternalIssue{}, domain.CreateBoardTaskRequest{}, fmt.Errorf("%w: jira is not connected", ErrSourceNotConfigured)
	}
	issue, err := client.GetIssue(ctx, key)
	if err != nil {
		return domain.ExternalIssue{}, domain.CreateBoardTaskRequest{}, fmt.Errorf("get jira issue: %w: %w", ErrUpstream, err)
	}
	taskType, err := s.resolveTaskType(ctx, strings.EqualFold(strings.TrimSpace(issue.IssueType), "bug"))
	if err != nil {
		return domain.ExternalIssue{}, domain.CreateBoardTaskRequest{}, err
	}
	priority, _ := jiraPriorityToTaskPriority(issue.Priority)
	return issue, domain.CreateBoardTaskRequest{
		Title:       issue.Title,
		Description: buildDescription(issue.Key, issue.URL, issue.Body),
		TaskType:    taskType,
		Priority:    priority,
	}, nil
}

// finishImport is the one place every successful import passes through,
// whatever provider or path got it here. Write-back and the conversion run in
// the background, so nothing after the import row and link are stored can turn
// Import into a reported failure.
func (s *Service) finishImport(ctx context.Context, task domain.BoardTask, repositoryID uuid.UUID, provider domain.IssueProvider, issue domain.ExternalIssue, actorUID string) (Imported, error) {
	status := domain.IssueConversionNone
	if s.conversionWanted(ctx) {
		status = domain.IssueConversionPending
	}
	taskID := task.ID
	imp, err := s.imports.Create(ctx, domain.IssueImport{
		Provider:         provider,
		ExternalKey:      issue.Key,
		RepositoryID:     repositoryID,
		URL:              issue.URL,
		Title:            issue.Title,
		ImportedBy:       actorUID,
		IntakeTaskID:     &taskID,
		ConversionStatus: status,
	})
	if err != nil {
		if !errors.Is(err, port.ErrIssueImportExists) {
			return Imported{}, fmt.Errorf("record issue import: %w", err)
		}
		// Lost the race to another import of the same issue. The task this call
		// opened was never handed to anyone, so it goes rather than sitting on
		// the board as a duplicate of the winner's.
		s.dropTask(ctx, repositoryID, task.ID)
		existing, getErr := s.imports.GetByProviderKey(ctx, provider, issue.Key)
		if getErr != nil {
			return Imported{}, &AlreadyImportedError{RepositoryID: repositoryID}
		}
		links, _ := s.links.ListByIssue(ctx, provider, issue.Key)
		return Imported{}, alreadyImportedError(existing, links)
	}
	link, err := s.links.Create(ctx, domain.IssueLink{
		TaskID:       task.ID,
		TaskKey:      task.Key,
		ImportedBy:   actorUID,
		RepositoryID: repositoryID,
		Provider:     provider,
		ExternalKey:  issue.Key,
		URL:          issue.URL,
		Title:        issue.Title,
		LastColumn:   string(task.Column),
	})
	if err != nil {
		return Imported{}, fmt.Errorf("record issue link: %w", err)
	}
	if status == domain.IssueConversionPending {
		s.wakeConversions()
	} else {
		s.writeBackTracked([]string{task.Key}, link)
	}
	return Imported{Task: task, Link: link, Import: imp}, nil
}

func (s *Service) dropTask(ctx context.Context, repositoryID, taskID uuid.UUID) {
	if err := s.tasks.DeleteTask(ctx, repositoryID, taskID); err != nil && !errors.Is(err, domain.ErrBoardTaskNotFound) {
		log.Warn().Err(err).Str("task_id", taskID.String()).Msg("issuesync: removing a task failed")
	}
}

// resolveTaskType answers "bug" only when the source marked the issue a bug
// and the workflow has that type; "" lets CreateTask apply the default.
func (s *Service) resolveTaskType(ctx context.Context, isBug bool) (domain.TaskType, error) {
	if !isBug || s.taskTypes == nil {
		return "", nil
	}
	exists, err := s.taskTypes.TaskTypeExists(ctx, domain.TaskTypeBug)
	if err != nil {
		return "", fmt.Errorf("check task type: %w", err)
	}
	if !exists {
		return "", nil
	}
	return domain.TaskTypeBug, nil
}
