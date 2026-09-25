package board

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	githubapi "github.com/makifbaysal/tasktrooper/server/internal/adapter/vcs/github"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type TaskPRRecorder interface {
	SetTaskPullRequest(ctx context.Context, taskID uuid.UUID, url string, number int) error
}

func recordTaskPR(ctx context.Context, rec TaskPRRecorder, taskID uuid.UUID, prURL string) {
	if rec == nil || strings.TrimSpace(prURL) == "" {
		return
	}
	number, _ := domain.ParsePullRequestNumber(prURL)
	if err := rec.SetTaskPullRequest(ctx, taskID, strings.TrimSpace(prURL), number); err != nil {
		log.Warn().Err(err).Str("task_id", taskID.String()).Str("pr_url", prURL).
			Msg("recording the task's pull request failed")
	}
}

type TaskPRGit interface {
	HasGit(rootPath string) bool
	CommitAndPush(ctx context.Context, workspacePath, message string) error
	EnsurePullRequest(ctx context.Context, workspacePath string) (string, error)
	TaskGitInfo(ctx context.Context, workspacePath string) (domain.TaskGitInfo, error)
	TaskChangedFiles(ctx context.Context, workspacePath string) ([]string, error)
	OriginURL(ctx context.Context, rootPath string) string
	MergePullRequest(ctx context.Context, req domain.PullRequestMergeRequest) (domain.PullRequestMergeResult, error)
}

type TaskPRTasks interface {
	Get(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.BoardTask, error)
	SetTaskPullRequest(ctx context.Context, taskID uuid.UUID, url string, number int) error
	SetTaskMergeCommit(ctx context.Context, taskID uuid.UUID, sha string) error
}

type TaskPRLifecycleGates interface {
	CheckReviewChain(ctx context.Context, repositoryID, taskID uuid.UUID) error
	LatestTaskPipeline(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.TaskPipeline, error)
	AutoReleaseIfUndeployable(ctx context.Context, repositoryID, taskID uuid.UUID) bool
}

// ReleaseOpener is application/release.Service's OpenForMerge, narrowed to a
// tiny interface so board can depend on release without release depending
// back on board (release wakes a parked card through its own Waker
// interface instead — see release_waker.go).
type ReleaseOpener interface {
	OpenForMerge(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask, mergeSHA string) domain.ReleaseOpening
}

type RootPathResolver interface {
	ResolveRootPath(ctx context.Context, repositoryID uuid.UUID) (string, error)
}

type AgentResolver interface {
	GetAgent(ctx context.Context, id uuid.UUID) (domain.Agent, error)
}

type TaskPRServiceDeps struct {
	Tasks  TaskPRTasks
	Repos  RootPathResolver
	Git    TaskPRGit
	PRs    port.PullRequestClient
	Tokens TokenSource
	Agents AgentResolver
	LLM    port.LLMClient
	Gates  TaskPRLifecycleGates
	// Releases opens a release at merge (§3.1). When set, it replaces the
	// legacy Gates.AutoReleaseIfUndeployable call entirely — see
	// taskpr_merge.go.
	Releases      ReleaseOpener
	WorkspaceRoot string
}

type TaskPRService struct {
	tasks         TaskPRTasks
	repos         RootPathResolver
	git           TaskPRGit
	prs           port.PullRequestClient
	tokens        TokenSource
	agents        AgentResolver
	llm           port.LLMClient
	gates         TaskPRLifecycleGates
	releases      ReleaseOpener
	workspaceRoot string
}

func NewTaskPRService(deps TaskPRServiceDeps) *TaskPRService {
	return &TaskPRService{
		tasks:         deps.Tasks,
		repos:         deps.Repos,
		git:           deps.Git,
		prs:           deps.PRs,
		tokens:        deps.Tokens,
		agents:        deps.Agents,
		llm:           deps.LLM,
		gates:         deps.Gates,
		releases:      deps.Releases,
		workspaceRoot: deps.WorkspaceRoot,
	}
}

const maxPRDiffChars = 40000

const maxPRCommentBody = 4000

func (s *TaskPRService) TaskWorkspacePath(taskID uuid.UUID) string {
	if s.workspaceRoot == "" {
		return ""
	}
	path, err := workspace.TaskDir(s.workspaceRoot, taskID)
	if err != nil {
		return ""
	}
	return path
}

func (s *TaskPRService) PullRequest(ctx context.Context, repositoryID, taskID uuid.UUID, includeDiff bool) (domain.TaskPullRequest, error) {
	task, err := s.tasks.Get(ctx, repositoryID, taskID)
	if err != nil {
		return domain.TaskPullRequest{}, err
	}
	number, prURL := taskPRRef(task)
	if prURL == "" {
		return domain.TaskPullRequest{
			Note: "No pull request has been opened for this task yet. One is opened automatically when the task reaches code_review, and commit_task_changes opens it too when it pushes.",
		}, nil
	}
	out := domain.TaskPullRequest{Known: true, Number: number, URL: prURL}
	if number <= 0 {
		out.Note = "The pull request URL is recorded but its number could not be read from it, so the PR's files, comments and diff cannot be fetched. Open the URL instead."
		return out, nil
	}
	token := s.token(ctx)
	if token == "" {
		out.Note = "GitHub is not connected, so only the recorded PR link is available — no state, files, comments or diff."
		return out, nil
	}
	owner, repo, err := s.ownerRepo(ctx, repositoryID, taskID)
	if err != nil {
		out.Note = "The PR link is recorded but its GitHub owner/repo could not be resolved: " + err.Error()
		return out, nil
	}

	pr, err := s.prs.GetPullRequest(ctx, token, owner, repo, number)
	if err != nil {
		return domain.TaskPullRequest{}, fmt.Errorf("read pull request #%d: %w", number, err)
	}
	out.Title = pr.Title
	out.State = pr.State
	out.Draft = pr.Draft
	out.Merged = pr.Merged
	out.Mergeable = pr.MergeableState
	out.HeadRef = pr.HeadRef
	out.BaseRef = pr.BaseRef
	out.HeadSHA = pr.HeadSHA
	out.Additions = pr.Additions
	out.Deletions = pr.Deletions
	out.ChangedFiles = pr.ChangedFiles
	if pr.HTMLURL != "" {
		out.URL = pr.HTMLURL
	}

	var notes []string
	if files, ferr := s.prs.ListPullRequestFiles(ctx, token, owner, repo, number); ferr != nil {
		notes = append(notes, "changed files could not be listed: "+ferr.Error())
	} else {
		for _, f := range files {
			out.Files = append(out.Files, domain.PullRequestFile{
				Path:         f.Path,
				Status:       f.Status,
				Additions:    f.Additions,
				Deletions:    f.Deletions,
				PreviousPath: f.PreviousPath,
			})
		}
		if pr.ChangedFiles > len(out.Files) {
			notes = append(notes, fmt.Sprintf("the file list is capped: %d of %d files shown", len(out.Files), pr.ChangedFiles))
		}
	}
	if comments, cerr := s.prs.ListPullRequestReviewComments(ctx, token, owner, repo, number); cerr != nil {
		notes = append(notes, "review comments could not be listed: "+cerr.Error())
	} else {
		out.ReviewComments = mapPRComments(comments)
	}
	if comments, cerr := s.prs.ListIssueComments(ctx, token, owner, repo, number); cerr != nil {
		notes = append(notes, "PR conversation comments could not be listed: "+cerr.Error())
	} else {
		out.Comments = mapPRComments(comments)
	}
	if includeDiff {
		diff, truncated, derr := s.prs.PullRequestDiff(ctx, token, owner, repo, number, maxPRDiffChars)
		switch {
		case derr != nil:
			notes = append(notes, "the diff could not be fetched: "+derr.Error())
		default:
			out.Diff = diff
			out.DiffTruncated = truncated
			if truncated {
				notes = append(notes, "the diff is truncated; read the remaining files from the workspace instead of assuming they are unchanged")
			}
		}
	}
	out.Note = strings.Join(notes, "; ")
	return out, nil
}

// HEAD-before-vs-after, not a dirty-tree probe: git add -A stages untracked files, and a first implementation is all new files.
func (s *TaskPRService) CommitTaskChanges(ctx context.Context, repositoryID, taskID uuid.UUID, message string) (domain.TaskCommitResult, error) {
	task, err := s.tasks.Get(ctx, repositoryID, taskID)
	if err != nil {
		return domain.TaskCommitResult{}, err
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return domain.TaskCommitResult{}, fmt.Errorf("a commit message is required")
	}
	if s.git == nil {
		return domain.TaskCommitResult{}, fmt.Errorf("git is not configured on this deployment")
	}
	workspaceDir := s.TaskWorkspacePath(taskID)
	if workspaceDir == "" || !s.git.HasGit(workspaceDir) {
		return domain.TaskCommitResult{
			Message: "This task has no working copy on this machine, so there is nothing to commit. Make the changes in the task workspace first (an agent run on the task creates it).",
		}, nil
	}

	before := s.headSHA(ctx, workspaceDir)
	if err := s.git.CommitAndPush(ctx, workspaceDir, s.commitMessage(ctx, task, message)); err != nil {
		return domain.TaskCommitResult{}, fmt.Errorf("commit and push the task branch: %w", err)
	}
	info, infoErr := s.git.TaskGitInfo(ctx, workspaceDir)
	if infoErr != nil {
		log.Warn().Err(infoErr).Str("task_id", taskID.String()).Msg("task git info after commit failed")
	}
	result := domain.TaskCommitResult{
		Committed: info.HeadSHA != "" && info.HeadSHA != before,
		Branch:    info.Branch,
		SHA:       info.HeadSHA,
	}
	if files, ferr := s.git.TaskChangedFiles(ctx, workspaceDir); ferr == nil {
		result.ChangedFiles = files
	}

	prURL, prErr := s.git.EnsurePullRequest(ctx, workspaceDir)
	if prErr != nil {
		log.Info().Err(prErr).Str("task_id", taskID.String()).Msg("ensure PR after commit_task_changes failed")
	} else if prURL != "" {
		recordTaskPR(ctx, s.tasks, taskID, prURL)
		result.PRURL = prURL
		result.PRNumber, _ = domain.ParsePullRequestNumber(prURL)
	}
	if result.PRURL == "" {
		if _, existing := taskPRRef(task); existing != "" {
			result.PRURL = existing
			result.PRNumber = task.PRNumber
		}
	}

	switch {
	case !result.Committed:
		result.Message = "Nothing to commit — the task workspace matches what is already on the branch. The branch and its pull request are up to date."
	case result.PRURL == "":
		result.Message = "Committed and pushed. No pull request could be opened or found for the branch yet."
	default:
		result.Message = "Committed and pushed; the pull request now carries these changes."
	}
	return result, nil
}

func (s *TaskPRService) commitMessage(ctx context.Context, task domain.BoardTask, message string) string {
	agentRec := s.runningAgent(ctx)
	return writeCommitMessage(ctx, s.llm, commitDetails{
		TaskKey:   task.Key,
		Title:     message,
		AgentName: agentRec.Name,
		Writer:    agentWriterModel(agentRec),
	})
}

func (s *TaskPRService) runningAgent(ctx context.Context) domain.Agent {
	if s.agents == nil {
		return domain.Agent{}
	}
	agentID := registry.AgentIDFromContext(ctx)
	if agentID == uuid.Nil {
		return domain.Agent{}
	}
	agentRec, err := s.agents.GetAgent(ctx, agentID)
	if err != nil {
		log.Info().Err(err).Str("agent_id", agentID.String()).Msg("commit trailer: agent lookup failed")
		return domain.Agent{}
	}
	return agentRec
}

func (s *TaskPRService) CommentOnPullRequest(ctx context.Context, repositoryID, taskID uuid.UUID, body string, replyTo int64) (domain.PullRequestComment, error) {
	task, err := s.tasks.Get(ctx, repositoryID, taskID)
	if err != nil {
		return domain.PullRequestComment{}, err
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return domain.PullRequestComment{}, fmt.Errorf("a comment body is required")
	}
	number, prURL := taskPRRef(task)
	if prURL == "" {
		return domain.PullRequestComment{}, fmt.Errorf("this task has no pull request yet, so there is nothing to comment on — push the branch first (commit_task_changes)")
	}
	if number <= 0 {
		return domain.PullRequestComment{}, fmt.Errorf("the recorded pull request URL (%s) carries no readable number, so the comment cannot be posted through the API", prURL)
	}
	token := s.token(ctx)
	if token == "" {
		return domain.PullRequestComment{}, fmt.Errorf("GitHub is not connected, so nothing can be posted to the pull request")
	}
	owner, repo, err := s.ownerRepo(ctx, repositoryID, taskID)
	if err != nil {
		return domain.PullRequestComment{}, err
	}
	var comment port.PullRequestComment
	if replyTo > 0 {
		comment, err = s.prs.ReplyToReviewComment(ctx, token, owner, repo, number, replyTo, body)
	} else {
		comment, err = s.prs.CreateIssueComment(ctx, token, owner, repo, number, body)
	}
	if err != nil {
		return domain.PullRequestComment{}, fmt.Errorf("post pull request comment: %w", err)
	}
	return mapPRComment(comment), nil
}

func taskPRRef(task domain.BoardTask) (int, string) {
	url := strings.TrimSpace(task.PRURL)
	number := task.PRNumber
	if number <= 0 && url != "" {
		number, _ = domain.ParsePullRequestNumber(url)
	}
	return number, url
}

func (s *TaskPRService) token(ctx context.Context) string {
	if s.tokens == nil {
		return ""
	}
	token, err := s.tokens(ctx)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(token)
}

func (s *TaskPRService) ownerRepo(ctx context.Context, repositoryID, taskID uuid.UUID) (string, string, error) {
	if s.git == nil {
		return "", "", fmt.Errorf("git is not configured on this deployment")
	}
	if dir := s.TaskWorkspacePath(taskID); dir != "" && s.git.HasGit(dir) {
		if info, err := s.git.TaskGitInfo(ctx, dir); err == nil && info.Owner != "" && info.Repo != "" {
			return info.Owner, info.Repo, nil
		}
	}
	if s.repos == nil {
		return "", "", fmt.Errorf("the task has no workspace on this machine and no repository resolver is configured")
	}
	root, err := s.repos.ResolveRootPath(ctx, repositoryID)
	if err != nil {
		return "", "", err
	}
	owner, repo, ok := githubapi.ParseOwnerRepo(s.git.OriginURL(ctx, root))
	if !ok {
		return "", "", fmt.Errorf("the repository's origin is not a GitHub URL")
	}
	return owner, repo, nil
}

func (s *TaskPRService) headSHA(ctx context.Context, workspace string) string {
	info, err := s.git.TaskGitInfo(ctx, workspace)
	if err != nil {
		return ""
	}
	return info.HeadSHA
}

func mapPRComments(in []port.PullRequestComment) []domain.PullRequestComment {
	out := make([]domain.PullRequestComment, 0, len(in))
	for _, c := range in {
		out = append(out, mapPRComment(c))
	}
	return out
}

func mapPRComment(c port.PullRequestComment) domain.PullRequestComment {
	return domain.PullRequestComment{
		ID:        c.ID,
		Author:    c.Author,
		Body:      domain.TruncateHead(c.Body, maxPRCommentBody),
		Path:      c.Path,
		Line:      c.Line,
		InReplyTo: c.InReplyTo,
		URL:       c.HTMLURL,
		CreatedAt: c.CreatedAt,
	}
}
