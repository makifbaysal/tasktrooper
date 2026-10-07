package board

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type RevisionResumeSuite struct {
	suite.Suite
	runs *recordingRunStore
	ex   *fakeExecutor
}

func TestRevisionResumeSuite(t *testing.T) {
	suite.Run(t, new(RevisionResumeSuite))
}

type revisionFeedbackUpdater struct {
	fakeTaskUpdater
	feedback []domain.TaskComment
}

func (u *revisionFeedbackUpdater) ListComments(context.Context, uuid.UUID, uuid.UUID) ([]domain.TaskComment, error) {
	return u.feedback, nil
}

func (s *RevisionResumeSuite) SetupTest() {
	s.runs = &recordingRunStore{}
	s.ex = &fakeExecutor{
		supports: domain.LLMProviderClaudeCode,
		resp:     domain.AgentResponse{Message: domain.Message{Content: "renamed it"}, CLISessionID: "sess-after"},
	}
}

func (s *RevisionResumeSuite) revisionRun() (*Runner, RunJob) {
	updater := &revisionFeedbackUpdater{feedback: []domain.TaskComment{
		{AuthorType: "agent", Content: "Rename parseThing to parseInvoice before this can merge."},
	}}
	r, job := criteriaSweepRunner(s.T(), s.runs, s.ex, updater)
	job.Task.Column = domain.TaskColumnNeedRevision
	return r, job
}

func (s *RevisionResumeSuite) TestARevisionResumesTheAgentsOwnSessionWithTheFeedback() {
	r, job := s.revisionRun()
	workDir, err := r.projects.ResolveRootPath(context.Background(), job.RepositoryID)
	s.Require().NoError(err)
	s.runs.prev = []domain.TaskAgentRun{
		{ID: uuid.New(), AgentID: uuid.New(), CLISessionID: "sess-reviewer", WorkspacePath: workDir},
		{ID: uuid.New(), AgentID: job.Run.AgentID, CLISessionID: "sess-dev", WorkspacePath: workDir, Status: domain.TaskAgentRunStatusCompleted, CLIProvider: domain.LLMProviderClaudeCode},
	}

	s.Require().NoError(r.execute(context.Background(), context.Background(), func() {}, job))

	main := s.ex.requests()[0]
	s.Equal("sess-dev", main.ResumeSessionID, "the reviewer's session is not this agent's to continue")
	s.Contains(main.Prompt, "Rename parseThing to parseInvoice", "the resumed session is handed the feedback")
	s.Contains(main.Prompt, "tt-42 Executor seam")
	s.NotEmpty(main.History, "the full context still travels for the fresh-session fallback")
	s.Equal("sess-after", s.runs.row().CLISessionID, "the session this run ended in is what the next revision resumes")
	s.Equal(domain.LLMProviderClaudeCode, s.runs.row().CLIProvider)
}

func (s *RevisionResumeSuite) TestARevisionInAnotherWorkspaceStartsFresh() {
	r, job := s.revisionRun()
	s.runs.prev = []domain.TaskAgentRun{
		{ID: uuid.New(), AgentID: job.Run.AgentID, CLISessionID: "sess-dev", WorkspacePath: "/elsewhere/task", CLIProvider: domain.LLMProviderClaudeCode},
	}

	s.Require().NoError(r.execute(context.Background(), context.Background(), func() {}, job))

	main := s.ex.requests()[0]
	s.Empty(main.ResumeSessionID)
	s.Empty(main.Prompt)
}

func (s *RevisionResumeSuite) TestAFirstRunNeverResumesAnEarlierSession() {
	r, job := criteriaSweepRunner(s.T(), s.runs, s.ex, &fakeTaskUpdater{})
	workDir, err := r.projects.ResolveRootPath(context.Background(), job.RepositoryID)
	s.Require().NoError(err)
	s.runs.prev = []domain.TaskAgentRun{
		{ID: uuid.New(), AgentID: job.Run.AgentID, CLISessionID: "sess-dev", WorkspacePath: workDir, CLIProvider: domain.LLMProviderClaudeCode},
	}

	s.Require().NoError(r.execute(context.Background(), context.Background(), func() {}, job))

	s.Empty(s.ex.requests()[0].ResumeSessionID, "only a revision continues the earlier session")
}

func (s *RevisionResumeSuite) TestRevisionSessionSelection() {
	agentID, other, current := uuid.New(), uuid.New(), uuid.New()
	cc := domain.LLMProviderClaudeCode
	tests := []struct {
		name string
		runs []domain.TaskAgentRun
		want string
	}{
		{"skips other agents' runs", []domain.TaskAgentRun{
			{ID: uuid.New(), AgentID: other, CLISessionID: "qa", CLIProvider: cc, WorkspacePath: "/ws"},
			{ID: uuid.New(), AgentID: agentID, CLISessionID: "dev", CLIProvider: cc, WorkspacePath: "/ws/"},
		}, "dev"},
		{"skips the current run", []domain.TaskAgentRun{
			{ID: current, AgentID: agentID, CLISessionID: "now", CLIProvider: cc, WorkspacePath: "/ws"},
			{ID: uuid.New(), AgentID: agentID, CLISessionID: "dev", CLIProvider: cc, WorkspacePath: "/ws"},
		}, "dev"},
		{"own last run left no session", []domain.TaskAgentRun{
			{ID: uuid.New(), AgentID: agentID, WorkspacePath: "/ws"},
			{ID: uuid.New(), AgentID: agentID, CLISessionID: "older", CLIProvider: cc, WorkspacePath: "/ws"},
		}, ""},
		{"workspace unknown", []domain.TaskAgentRun{
			{ID: uuid.New(), AgentID: agentID, CLISessionID: "dev", CLIProvider: cc},
		}, ""},
		{"session from another CLI", []domain.TaskAgentRun{
			{ID: uuid.New(), AgentID: agentID, CLISessionID: "dev", CLIProvider: domain.LLMProviderCursorAgent, WorkspacePath: "/ws"},
		}, ""},
		{"provider never recorded", []domain.TaskAgentRun{
			{ID: uuid.New(), AgentID: agentID, CLISessionID: "dev", WorkspacePath: "/ws"},
		}, ""},
		{"no previous run", nil, ""},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.Equal(tt.want, revisionCLISession(tt.runs, current, agentID, "/ws"))
		})
	}
}

func (s *RevisionResumeSuite) TestRevisionResumePromptCarriesEveryNonEmptyBlock() {
	got := revisionResumePrompt(domain.BoardTask{Key: "tt-7", Title: "Export"}, "Fix the review comments.", "", "## Pipeline failure\njob test failed", "  ")

	s.Contains(got, "tt-7 Export")
	s.Contains(got, "Fix the review comments.\n\n## Pipeline failure\njob test failed")
	s.NotContains(got, "\n\n\n", "an empty block leaves no gap")
}

// gatedExecutor parks every request the way claudecode.Executor does while
// another session holds the quota gate: nothing is spawned, and the block
// carries the session the request asked to resume.
type gatedExecutor struct {
	mu   sync.Mutex
	gate bool
	reqs []domain.TaskExecution
}

func (g *gatedExecutor) Supports(p domain.LLMProviderType) bool {
	return p == domain.LLMProviderClaudeCode
}

func (g *gatedExecutor) Execute(_ context.Context, req domain.TaskExecution) (domain.AgentResponse, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.reqs = append(g.reqs, req)
	if g.gate {
		return domain.AgentResponse{}, &domain.QuotaBlock{ResumeAt: time.Now().Add(time.Hour), CLISessionID: req.ResumeSessionID, Provider: domain.LLMProviderClaudeCode}
	}
	return domain.AgentResponse{Message: domain.Message{Content: "ok"}, CLISessionID: req.ResumeSessionID}, nil
}

func (s *RevisionResumeSuite) TestARevisionParkedBeforeItStartedStillHandsOverTheFeedbackOnResume() {
	ex := &gatedExecutor{gate: true}
	updater := &revisionFeedbackUpdater{feedback: []domain.TaskComment{
		{AuthorType: "agent", Content: "Rename parseThing to parseInvoice before this can merge."},
	}}
	r, job := criteriaSweepRunner(s.T(), s.runs, ex, updater)
	job.Task.Column = domain.TaskColumnNeedRevision
	workDir, err := r.projects.ResolveRootPath(context.Background(), job.RepositoryID)
	s.Require().NoError(err)
	devRun := domain.TaskAgentRun{ID: uuid.New(), AgentID: job.Run.AgentID, CLISessionID: "sess-dev", CLIProvider: domain.LLMProviderClaudeCode,
		WorkspacePath: workDir, Status: domain.TaskAgentRunStatusCompleted}
	reviewer := domain.TaskAgentRun{ID: uuid.New(), AgentID: uuid.New(), CLISessionID: "sess-rev", WorkspacePath: workDir}
	s.runs.prev = []domain.TaskAgentRun{reviewer, devRun}

	s.Require().NoError(r.execute(context.Background(), context.Background(), func() {}, job))
	parked := s.runs.row()
	s.Require().NotNil(parked.QuotaResumeAt)
	s.Equal("sess-dev", parked.CLISessionID)
	s.Equal(domain.LLMProviderClaudeCode, parked.CLIProvider)

	ex.gate = false
	parked.ID = uuid.New()
	s.runs.prev = []domain.TaskAgentRun{parked, reviewer, devRun}
	job.Run.ID = uuid.New()
	s.Require().NoError(r.execute(context.Background(), context.Background(), func() {}, job))

	resumed := ex.reqs[len(ex.reqs)-1]
	s.Equal("sess-dev", resumed.ResumeSessionID)
	s.Contains(resumed.Prompt, "Rename parseThing to parseInvoice", "the parked session never saw the feedback")
}
