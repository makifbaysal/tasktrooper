package board

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"

	"github.com/makifbaysal/tasktrooper/server/internal/application/memory"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// A revision usually follows the reviewer's and QA's own runs, so the last run
// by the same agent can sit further back than the quota-park history reaches.
const previousRunsDepth = 12

type runContextBlocks struct {
	score, kpi, memory, diff string
	revisionPR, pipeline     string
	analysis, questions      string
	review                   string
	comments                 []domain.TaskComment
	prevRuns                 []domain.TaskAgentRun
	prevRunsErr              error
}

// gatherRunContext fetches a run's independent context blocks side by side.
// Each is a read against the store, git or GitHub that waits on nothing but
// itself; one after another they were most of a run's start-up time. Every
// goroutine writes its own field, so the struct needs no lock.
func (r *Runner) gatherRunContext(ctx, runCtx context.Context, job RunJob, wf domain.Workflow, agentID uuid.UUID, repoName, taskWorkspace string) runContextBlocks {
	var b runContextBlocks
	var g errgroup.Group
	if r.perfStore != nil {
		g.Go(func() error {
			if perfScore, err := r.perfStore.GetScore(runCtx, agentID); err == nil {
				recent, _ := r.perfStore.RecentEvents(runCtx, agentID, 5)
				b.score = prompt.ScoreContextMessage(perfScore, recent)
			}
			return nil
		})
	}
	if r.kpis != nil {
		g.Go(func() error {
			if defs, err := r.kpis.ListByAgent(runCtx, agentID); err == nil && len(defs) > 0 {
				latest, _ := r.kpis.LatestResults(runCtx, agentID)
				b.kpi = prompt.KPIContextMessage(defs, latest)
			}
			return nil
		})
	}
	if r.memories != nil {
		g.Go(func() error {
			repositoryID := job.RepositoryID
			if mems := memory.Recall(runCtx, r.memories, agentID, &repositoryID, 8); len(mems) > 0 {
				b.memory = prompt.MemoryContextMessage(mems, repoName)
			}
			return nil
		})
	}
	if taskWorkspace != "" && r.git != nil {
		g.Go(func() error {
			if diff, err := r.git.TaskDiff(ctx, taskWorkspace); err == nil && diff != "" {
				b.diff = reviewDiffMessage(wf, job.Task.Column, diff)
			}
			return nil
		})
	}
	if job.isRevision() {
		g.Go(func() error {
			b.revisionPR = r.revisionPRComments(ctx, job)
			return nil
		})
		g.Go(func() error {
			b.review = r.reviewAnnotationsMessage(ctx, job)
			return nil
		})
		if r.pipelines != nil {
			g.Go(func() error {
				b.pipeline = r.revisionPipelineFailure(ctx, job)
				return nil
			})
		}
	}
	g.Go(func() error {
		b.analysis = r.analysisContext(ctx, job)
		return nil
	})
	g.Go(func() error {
		b.questions = r.openQuestionsContext(ctx, job)
		return nil
	})
	g.Go(func() error {
		b.comments = r.taskComments(ctx, job)
		return nil
	})
	g.Go(func() error {
		b.prevRuns, b.prevRunsErr = r.runs.ListByTask(runCtx, job.Task.ID, previousRunsDepth)
		return nil
	})
	_ = g.Wait()
	return b
}

func (r *Runner) revisionPipelineFailure(ctx context.Context, job RunJob) string {
	pl, err := r.pipelines.LatestByTask(ctx, job.Task.ID)
	if err != nil {
		if !errors.Is(err, domain.ErrPipelineNotFound) {
			log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("fetch latest pipeline for revision context failed")
		}
		return ""
	}
	if pl.Status != domain.PipelineStatusFailed {
		return ""
	}
	return revisionPipelineFailureKey.Render(revisionPipelineFailureInput{Report: pipelineFailureReport(pl)})
}

type runRepositoryKey struct{}

// withRunRepository carries the repository the run already resolved to the
// steps after the agent (verification), which used to resolve it again.
func withRunRepository(ctx context.Context, repo domain.Repository) context.Context {
	return context.WithValue(ctx, runRepositoryKey{}, repo)
}

func runRepositoryFrom(ctx context.Context) (domain.Repository, bool) {
	repo, ok := ctx.Value(runRepositoryKey{}).(domain.Repository)
	return repo, ok
}

// revisionCLISession is the Claude Code session a revision run continues: the
// one this agent's own last run on the task ended in, in this same workspace
// (a session is bound to the directory it ran in) and by Claude Code itself
// (the agent may have run on another CLI then). Runs by other agents — the
// reviewer, QA — are skipped; if the agent's own last run left no such
// session, there is nothing to continue and the revision starts fresh.
func revisionCLISession(prevRuns []domain.TaskAgentRun, currentRunID, agentID uuid.UUID, workDir string) string {
	for _, prev := range prevRuns {
		if prev.ID == currentRunID || prev.AgentID != agentID {
			continue
		}
		if prev.CLISessionID == "" || prev.CLIProvider != domain.LLMProviderClaudeCode || !sameWorkspace(prev.WorkspacePath, workDir) {
			return ""
		}
		return prev.CLISessionID
	}
	return ""
}

func sameWorkspace(recorded, current string) bool {
	if recorded == "" || current == "" {
		return false
	}
	return filepath.Clean(recorded) == filepath.Clean(current)
}

// revisionResumePrompt is everything new since the session last ran: the
// stage's own instructions and the feedback that sent the task back. The
// resumed session already holds the task, the code and its own reasoning.
func revisionResumePrompt(task domain.BoardTask, trigger string, feedback ...string) string {
	blocks := make([]string, 0, len(feedback))
	for _, f := range feedback {
		if f = strings.TrimSpace(f); f != "" {
			blocks = append(blocks, f)
		}
	}
	return revisionResumeKey.Render(revisionResumeInput{
		Task:     strings.TrimSpace(task.Key + " " + task.Title),
		Trigger:  strings.TrimSpace(trigger),
		Feedback: blocks,
	})
}

// followUpHistory is what a follow-up turn (criteria sweep, review verdict)
// continues from: the run's own transcript when the in-process loop kept one —
// every tool call and result, the prompt-cache prefix unchanged — else the
// opening context plus the final answer, which is all a host-executed session
// needs since it resumes its own transcript.
func followUpHistory(opening []domain.Message, resp domain.AgentResponse) []domain.Message {
	if len(resp.Transcript) > 0 {
		return append([]domain.Message(nil), resp.Transcript...)
	}
	out := make([]domain.Message, len(opening), len(opening)+1)
	copy(out, opening)
	return append(out, domain.Message{Role: domain.RoleAssistant, Content: resp.Message.Content})
}
