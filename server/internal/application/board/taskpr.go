package board

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	githubapi "github.com/makifbaysal/tasktrooper/server/internal/adapter/github"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// TaskPRRecorder persists the pull request a task's branch got. Nil-safe at every
// call site: a build with no board store still opens PRs, it just cannot remember
// them.
type TaskPRRecorder interface {
	SetTaskPullRequest(ctx context.Context, taskID uuid.UUID, url string, number int) error
}

// recordTaskPR stores a PR URL on its task, parsing the number out of the URL.
//
// A URL that does not parse is still stored: the number is what the PR API is
// keyed by, but the link is what a human clicks, and the whole point of the
// column is that "which PR is this task in?" stops requiring a working copy and a
// GitHub round-trip. Best-effort by design — every caller has already opened the
// PR by the time it gets here, so failing the run over the bookkeeping would
// trade a real result for a lost one.
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

// TaskPRGit is the slice of the git client the task-PR path needs. Satisfied by
// the git adapter; declared here so the service is testable against a fake.
type TaskPRGit interface {
	HasGit(rootPath string) bool
	CommitAndPush(ctx context.Context, workspacePath, message string) error
	EnsurePullRequest(ctx context.Context, workspacePath string) (string, error)
	TaskGitInfo(ctx context.Context, workspacePath string) (domain.TaskGitInfo, error)
	TaskChangedFiles(ctx context.Context, workspacePath string) ([]string, error)
	OriginURL(ctx context.Context, rootPath string) string
	// MergePullRequest squash-merges a PR every gate above it has already
	// cleared, and deletes its branch.
	MergePullRequest(ctx context.Context, req domain.PullRequestMergeRequest) (domain.PullRequestMergeResult, error)
}

// TaskPRTasks is the task half: read one task (repository-scoped, which is the
// ownership check) and record the PR it ends up with.
type TaskPRTasks interface {
	Get(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.BoardTask, error)
	SetTaskPullRequest(ctx context.Context, taskID uuid.UUID, url string, number int) error
	// SetTaskMergeCommit records the commit the merge produced. It is the board's
	// only record that the PR landed, and the dispatcher reads it.
	SetTaskMergeCommit(ctx context.Context, taskID uuid.UUID, sha string) error
}

// TaskPRLifecycleGates is the repository-level half of the merge decision, kept
// as an interface so this package does not import application/repository (which
// imports this one). Satisfied by *repository.Service.
//
// Both methods are the EXISTING gates, not new ones: CheckReviewChain is the
// same require_review_chain check that guards a move into done, and
// LatestTaskPipeline is the same build/test result QA reads with
// get_pipeline_status. The merge asks them again rather than trusting that the
// task is in done, because the column says a task passed the gates ONCE — and
// merging is irreversible.
type TaskPRLifecycleGates interface {
	CheckReviewChain(ctx context.Context, repositoryID, taskID uuid.UUID) error
	LatestTaskPipeline(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.TaskPipeline, error)
	// AutoReleaseIfUndeployable moves the task to released when the
	// repository has no deploy_target configured anywhere. Reports whether
	// it did.
	AutoReleaseIfUndeployable(ctx context.Context, repositoryID, taskID uuid.UUID) bool
}

// RootPathResolver resolves a repository's shared working copy. Used only to find
// origin's owner/repo when the task has no workspace of its own on this machine.
type RootPathResolver interface {
	ResolveRootPath(ctx context.Context, repositoryID uuid.UUID) (string, error)
}

// AgentResolver reads the agent a run belongs to, so a commit made from a tool
// call can say which agent made it. Optional everywhere: an unresolved agent
// costs the trailer, not the commit.
type AgentResolver interface {
	GetAgent(ctx context.Context, id uuid.UUID) (domain.Agent, error)
}

// TaskPRServiceDeps wires the collaborators of the task↔pull-request use cases.
type TaskPRServiceDeps struct {
	Tasks  TaskPRTasks
	Repos  RootPathResolver
	Git    TaskPRGit
	PRs    port.PullRequestClient
	Tokens TokenSource
	// Agents and LLM are what turn an agent-written commit message into the
	// same English, agent-stamped message the board runner writes. Both
	// optional: without them the agent's own wording is committed as-is.
	Agents AgentResolver
	LLM    port.LLMClient
	// Gates are the repository lifecycle checks the merge re-asks. Nil disables
	// merging entirely rather than merging ungated — a deployment that cannot
	// prove the review chain must not be the one that lands code.
	Gates         TaskPRLifecycleGates
	WorkspaceRoot string
}

// TaskPRService is what lets a human discuss a task's pull request with an agent
// and then have the agent act on it: read the PR (metadata, files, comments,
// bounded diff), commit and push what was asked for so the PR updates, and answer
// a reviewer in their own thread.
//
// It is deliberately one service rather than three tools' worth of logic: all
// three need the same four things resolved first (the task, its workspace, the
// PR's owner/repo/number, a token), and resolving that four ways would let them
// disagree about which PR a task is in.
type TaskPRService struct {
	tasks         TaskPRTasks
	repos         RootPathResolver
	git           TaskPRGit
	prs           port.PullRequestClient
	tokens        TokenSource
	agents        AgentResolver
	llm           port.LLMClient
	gates         TaskPRLifecycleGates
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
		workspaceRoot: deps.WorkspaceRoot,
	}
}

// maxPRDiffChars bounds the diff a tool result carries. The diff is the single
// biggest thing an agent can pull into its context, and a PR that touches a
// lockfile can be megabytes of it; past this size it crowds out the conversation
// it was fetched to inform. The agent is told the diff was cut so it reads the
// rest per-file rather than assuming it saw everything.
const maxPRDiffChars = 40000

// maxPRCommentBody keeps one runaway review comment from filling the result.
const maxPRCommentBody = 4000

// TaskWorkspacePath is where a task's isolated checkout lives. Derived, not
// stored — the same derivation the runner, the pipeline and the repository
// service already use.
//
// An empty string means "no workspace here", which every caller already
// handles.
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

// PullRequest reads the task's pull request.
//
// A task with no PR recorded is a normal answer (Known=false plus a note saying
// when one gets opened), not an error: an agent asked about a PR that does not
// exist yet must say so, and an error result would have it report a broken
// system instead. The same goes for GitHub not being connected — the URL we
// already know is still worth handing back.
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

	// Everything below is additive context. One of these failing (a permission
	// the token lacks, a rate limit) must not cost the caller the PR state it
	// already has, so each failure becomes a note instead of an error.
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

// CommitTaskChanges commits whatever is in the task's workspace, pushes it,
// makes sure the PR exists and records it. This is the step that makes "apply
// the change I just described" actually reach the pull request: until now only
// the board runner committed, at the end of its own run, so an agent asked to
// fix something in a chat changed files nobody would ever see.
//
// Nothing to commit is a result, not an error. The check is HEAD before against
// HEAD after rather than a dirty-tree probe, because the commit path stages with
// `git add -A` — a probe that missed untracked files would report "nothing to
// commit" for a change that consists entirely of new files, which is what a
// first implementation looks like.
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
		// No workspace means no edits were made here — there is literally
		// nothing to push. Creating one now would only produce an empty branch.
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

	// Ensure the PR even when nothing new was committed: the branch may have
	// been pushed for the first time just now, and a branch with no PR is a
	// change nobody can review. It is best-effort because GitHub refuses a PR
	// whose head has no commits beyond base, which is exactly the
	// nothing-to-commit case.
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

// commitMessage renders what an agent asked to commit into repository English
// and stamps the agent onto it, so a tool-driven commit is indistinguishable
// from one the board runner made. The agent is told to write English already;
// this is the guarantee, not the request — a board used in Turkish still
// produces an English history, which is what the PR title, `git log` and every
// reviewer downstream read.
func (s *TaskPRService) commitMessage(ctx context.Context, task domain.BoardTask, message string) string {
	agentRec := s.runningAgent(ctx)
	return writeCommitMessage(ctx, s.llm, commitDetails{
		TaskKey:   task.Key,
		Title:     message,
		AgentName: agentRec.Name,
		Writer:    agentWriterModel(agentRec),
	})
}

// runningAgent reads the agent behind the current tool call. Zero-valued when
// there is no agent in context or it cannot be read — the message is then
// committed as the agent wrote it, unstamped.
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

// CommentOnPullRequest posts a comment on the task's PR, or answers one review
// comment inside its own thread when replyTo names it.
//
// Replying in-thread rather than starting a new conversation comment is the whole
// point when a reviewer asked for something: a top-level comment leaves their
// thread unanswered and GitHub keeps showing it as unresolved.
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

// taskPRRef returns the task's PR number and URL, filling in the number from the
// URL when only the URL was stored (a row written before the number could be
// parsed, or by a path that had only the link).
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

// ownerRepo resolves the GitHub coordinates of the task's repository, preferring
// the task's own workspace (whose origin is the one its branch was pushed to) and
// falling back to the shared working copy when the task has no workspace here.
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
