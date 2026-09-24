package board

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/activity"
	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type VerifyFindingsSuite struct {
	suite.Suite
}

func TestVerifyFindingsSuite(t *testing.T) {
	suite.Run(t, new(VerifyFindingsSuite))
}

type recordingLLM struct {
	requests []domain.AgentRequest
}

func (l *recordingLLM) Chat(_ context.Context, req domain.AgentRequest) (domain.AgentResponse, error) {
	l.requests = append(l.requests, req)
	return domain.AgentResponse{Message: domain.Message{
		Role: domain.RoleAssistant, Content: "I fixed the undefined symbol.",
	}}, nil
}

func (l *recordingLLM) ChatStream(ctx context.Context, req domain.AgentRequest, _ func(string)) (domain.AgentResponse, error) {
	return l.Chat(ctx, req)
}

func (l *recordingLLM) Models(context.Context) ([]string, error) { return nil, nil }

func (l *recordingLLM) Embed(context.Context, string, string) ([]float32, error) { return nil, nil }

type toollessRegistry struct{ port.ToolRegistry }

func (toollessRegistry) DefinitionsForPolicy(domain.ToolPolicy) []domain.ToolDefinition { return nil }

type tracedStore struct {
	port.ActivityStore
	steps []domain.SessionStep
}

func (s *tracedStore) CreateRun(context.Context, *uuid.UUID, string, string) (domain.SessionRun, error) {
	return domain.SessionRun{ID: uuid.New()}, nil
}

func (s *tracedStore) AppendStep(context.Context, uuid.UUID, string, []byte) error { return nil }

func (s *tracedStore) ListStepsByRun(context.Context, uuid.UUID) ([]domain.SessionStep, error) {
	return s.steps, nil
}

type failingRepo struct{ repo domain.Repository }

func (f failingRepo) ResolveRootPath(context.Context, uuid.UUID) (string, error) { return "", nil }
func (f failingRepo) ResolveDescription(context.Context, uuid.UUID) (string, error) {
	return "", nil
}
func (f failingRepo) ResolveRepository(context.Context, uuid.UUID) (domain.Repository, error) {
	return f.repo, nil
}
func traceStep(t *testing.T, kind string, payload any) domain.SessionStep {
	t.Helper()
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	return domain.SessionStep{StepType: kind, Payload: data}
}

func (s *VerifyFindingsSuite) runTrace() []domain.SessionStep {
	t := s.T()
	return []domain.SessionStep{
		traceStep(t, "tool_call_start", map[string]string{
			"tool": "grep_code", "call_id": "1", "arguments": `{"pattern":"ResolveVerifyStages"}`,
		}),
		traceStep(t, "tool_call_result", map[string]any{
			"tool": "grep_code", "call_id": "1", "content": "commands.go:125", "is_error": false,
		}),
		traceStep(t, "tool_call_start", map[string]string{
			"tool": "read_file", "call_id": "2", "arguments": `{"path":"internal/application/board/commands.go"}`,
		}),
		traceStep(t, "tool_call_result", map[string]any{
			"tool": "read_file", "call_id": "2", "content": "package board", "is_error": false,
		}),
		traceStep(t, "tool_call_start", map[string]string{
			"tool": "edit_file", "call_id": "3", "arguments": `{"path":"internal/application/board/verify.go"}`,
		}),
		traceStep(t, "tool_call_result", map[string]any{
			"tool": "edit_file", "call_id": "3", "content": "edited", "is_error": false,
		}),
		traceStep(t, "tool_call_start", map[string]string{
			"tool": "run_terminal", "call_id": "4", "arguments": `{"command":"go build ./..."}`,
		}),
		traceStep(t, "tool_call_result", map[string]any{
			"tool": "run_terminal", "call_id": "4", "content": "exit error: exit status 1\noutput:\nbroken", "is_error": true,
		}),
	}
}

func (s *VerifyFindingsSuite) alwaysFailingWorkspace() (string, domain.Repository) {
	dir := s.T().TempDir()
	s.Require().NoError(os.WriteFile(filepath.Join(dir, "verify.sh"), []byte("echo boom >&2\nexit 1\n"), 0o700))
	return dir, domain.Repository{VerifyCommand: "sh verify.sh"}
}

func (s *VerifyFindingsSuite) TestFixRoundIsToldWhatTheRunAlreadyDid() {
	dir, repo := s.alwaysFailingWorkspace()
	llm := &recordingLLM{}
	store := &tracedStore{steps: s.runTrace()}
	r := NewRunner(RunnerDeps{
		AgentLoop:         agent.NewLoop(llm, toollessRegistry{}, 3, 3, 16000),
		Repositories:      failingRepo{repo: repo},
		ActivityStore:     store,
		VerifyFixAttempts: 1,
	})

	ctx, rec, err := activity.StartRun(context.Background(), store, nil, "req-1", "model")
	s.Require().NoError(err)
	s.Require().NotNil(rec)

	job := RunJob{Task: domain.BoardTask{ID: uuid.New()}, RepositoryID: uuid.New()}
	history := []domain.Message{{Role: domain.RoleUser, Content: "fix the board"}}
	resp := domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: "I edited verify.go."}}

	r.verifyAndFix(ctx, job, domain.Agent{Model: "m"}, history, resp, "m", domain.ToolPolicy{}, dir)

	s.Require().Len(llm.requests, 1, "one failed verification must buy exactly one fix round")
	digest := s.findDigest(llm.requests[0].Messages)
	s.Require().NotEmpty(digest, "the fix round must carry a findings digest")

	s.Contains(digest, "internal/application/board/verify.go (edit_file)")
	s.Contains(digest, "Files read: internal/application/board/commands.go")
	s.Contains(digest, `"ResolveVerifyStages" (grep_code)`)
	s.Contains(digest, "`go build ./...` → exit status 1")

	contents := messageContents(llm.requests[0].Messages)
	s.Contains(contents, "fix the board")
	s.Contains(contents, "I edited verify.go.")
	s.Contains(contents, "Automated verification failed")
}

func (s *VerifyFindingsSuite) TestVerdictIsFalseWhenChecksStayRed() {
	dir, repo := s.alwaysFailingWorkspace()
	r := NewRunner(RunnerDeps{
		AgentLoop:         agent.NewLoop(&recordingLLM{}, toollessRegistry{}, 3, 3, 16000),
		Repositories:      failingRepo{repo: repo},
		VerifyFixAttempts: 1,
	})

	_, verified, _ := r.verifyAndFix(context.Background(), RunJob{Task: domain.BoardTask{ID: uuid.New()}}, domain.Agent{},
		[]domain.Message{{Role: domain.RoleUser, Content: "fix the board"}},
		domain.AgentResponse{Message: domain.Message{Content: "done"}}, "m", domain.ToolPolicy{}, dir)

	s.False(verified, "a run whose checks never went green must not be handed to review")
}

func (s *VerifyFindingsSuite) TestVerdictIsTrueWhenThereIsNothingToCheck() {
	r := NewRunner(RunnerDeps{
		AgentLoop:         agent.NewLoop(&recordingLLM{}, toollessRegistry{}, 3, 3, 16000),
		Repositories:      failingRepo{repo: domain.Repository{}},
		VerifyFixAttempts: 1,
	})

	_, verified, _ := r.verifyAndFix(context.Background(), RunJob{Task: domain.BoardTask{ID: uuid.New()}}, domain.Agent{},
		[]domain.Message{{Role: domain.RoleUser, Content: "fix the board"}},
		domain.AgentResponse{Message: domain.Message{Content: "done"}}, "m", domain.ToolPolicy{}, s.T().TempDir())

	s.True(verified)
}

func (s *VerifyFindingsSuite) TestSecondRoundReplacesTheFirstDigest() {
	dir, repo := s.alwaysFailingWorkspace()
	llm := &recordingLLM{}
	store := &tracedStore{steps: s.runTrace()}
	r := NewRunner(RunnerDeps{
		AgentLoop:         agent.NewLoop(llm, toollessRegistry{}, 3, 3, 16000),
		Repositories:      failingRepo{repo: repo},
		ActivityStore:     store,
		VerifyFixAttempts: 2,
	})

	ctx, _, err := activity.StartRun(context.Background(), store, nil, "req-2", "model")
	s.Require().NoError(err)

	r.verifyAndFix(ctx, RunJob{Task: domain.BoardTask{ID: uuid.New()}}, domain.Agent{},
		[]domain.Message{{Role: domain.RoleUser, Content: "fix the board"}},
		domain.AgentResponse{Message: domain.Message{Content: "done"}}, "m", domain.ToolPolicy{}, dir)

	s.Require().Len(llm.requests, 2, "two failed verifications must buy two fix rounds")
	digests := 0
	for _, m := range llm.requests[1].Messages {
		if agent.IsFindingsDigest(m.Content) {
			digests++
		}
	}
	s.Equal(1, digests, "the stale digest must be replaced, not stacked")
}

func (s *VerifyFindingsSuite) TestNoTraceLeavesTheFixRoundUnchanged() {
	dir, repo := s.alwaysFailingWorkspace()
	llm := &recordingLLM{}
	r := NewRunner(RunnerDeps{
		AgentLoop:         agent.NewLoop(llm, toollessRegistry{}, 3, 3, 16000),
		Repositories:      failingRepo{repo: repo},
		VerifyFixAttempts: 1,
	})

	r.verifyAndFix(context.Background(), RunJob{Task: domain.BoardTask{ID: uuid.New()}}, domain.Agent{},
		[]domain.Message{{Role: domain.RoleUser, Content: "fix the board"}},
		domain.AgentResponse{Message: domain.Message{Content: "done"}}, "m", domain.ToolPolicy{}, dir)

	s.Require().Len(llm.requests, 1)
	s.Empty(s.findDigest(llm.requests[0].Messages))
}

func (s *VerifyFindingsSuite) findDigest(messages []domain.Message) string {
	for _, m := range messages {
		if agent.IsFindingsDigest(m.Content) {
			s.Equal(domain.RoleSystem, m.Role, "the digest is context, not a turn either party took")
			return m.Content
		}
	}
	return ""
}

func messageContents(messages []domain.Message) string {
	var all string
	for _, m := range messages {
		all += m.Content + "\n"
	}
	return all
}
