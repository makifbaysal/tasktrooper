package board

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type FollowUpContinuationSuite struct {
	suite.Suite
	llm     *recordingLLM
	opening []domain.Message
	resp    domain.AgentResponse
}

func TestFollowUpContinuationSuite(t *testing.T) {
	suite.Run(t, new(FollowUpContinuationSuite))
}

func (s *FollowUpContinuationSuite) SetupTest() {
	s.llm = &recordingLLM{}
	s.opening = []domain.Message{
		{Role: domain.RoleSystem, Content: "persona"},
		{Role: domain.RoleUser, Content: "implement the export"},
	}
	final := domain.Message{Role: domain.RoleAssistant, Content: "implemented"}
	s.resp = domain.AgentResponse{
		Message: final,
		Transcript: append(append([]domain.Message(nil), s.opening...),
			domain.Message{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{
				ID: "c1", Type: "function", Function: domain.FunctionCall{Name: "read_file", Arguments: `{"path":"export.go"}`},
			}}},
			domain.Message{Role: domain.RoleTool, ToolCallID: "c1", Name: "read_file", Content: "package export"},
			final,
		),
	}
}

func (s *FollowUpContinuationSuite) runner(deps RunnerDeps) *Runner {
	deps.AgentLoop = agent.NewLoop(s.llm, toollessRegistry{}, 30, 30, 16000)
	return NewRunner(deps)
}

func (s *FollowUpContinuationSuite) TestTheCriteriaSweepContinuesTheRunInsteadOfRestartingIt() {
	r := s.runner(RunnerDeps{})
	r.SetWorkflows(workflowtest.Default().Reader())
	r.SetTaskUpdater(&settlingCriteriaUpdater{
		criteria:    []domain.AcceptanceCriterion{{ID: uuid.New(), Text: "archived rows are exported"}},
		settleAfter: 1,
	})

	r.sweepOpenCriteria(context.Background(), sweepJob(), domain.Agent{Model: "m"}, s.opening, s.resp, "m", domain.ToolPolicy{})

	s.Require().Len(s.llm.requests, 1)
	sent := s.llm.requests[0]
	s.Equal(s.resp.Transcript, sent.Messages[:len(s.resp.Transcript)], "the sweep sees the tool calls the run made")
	s.Equal(domain.RoleUser, sent.Messages[len(sent.Messages)-1].Role)
	s.Equal(len(s.opening), sent.CacheAnchorIndex, "the opening context stays the cached head")
}

func (s *FollowUpContinuationSuite) TestAFixRoundContinuesTheRunWithoutADigest() {
	dir, repo := redWorkspace(s.T())
	r := s.runner(RunnerDeps{Repositories: failingRepo{repo: repo}, VerifyFixAttempts: 1})

	r.verifyAndFix(context.Background(), RunJob{Task: domain.BoardTask{ID: uuid.New()}}, domain.Agent{Model: "m"},
		s.opening, s.resp, "m", domain.ToolPolicy{}, dir)

	s.Require().Len(s.llm.requests, 1)
	sent := s.llm.requests[0].Messages
	s.Equal(s.resp.Transcript, sent[:len(s.resp.Transcript)])
	s.Contains(sent[len(sent)-1].Content, "Automated verification failed")
	for _, m := range sent {
		s.False(agent.IsFindingsDigest(m.Content), "the transcript already says what the run did")
	}
	s.Equal(len(s.opening), s.llm.requests[0].CacheAnchorIndex)
}

func (s *FollowUpContinuationSuite) TestFollowUpHistoryFallsBackToTheFinalAnswerWithoutATranscript() {
	got := followUpHistory(s.opening, domain.AgentResponse{Message: domain.Message{Content: "done"}})

	s.Equal(append(append([]domain.Message(nil), s.opening...), domain.Message{Role: domain.RoleAssistant, Content: "done"}), got)
}
