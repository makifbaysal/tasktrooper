package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"

	appcontext "github.com/makifbaysal/tasktrooper/server/internal/application/context"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

const indexContextText = "Relevant code: auth.go:RefreshToken"

type indexFixture struct {
	index  *mocks.LocalCodeIndex
	search *mocks.ToolExecutor
	repoID uuid.UUID
}

// withIndex rebuilds the service with a local code index.
func (s *ServiceSuite) withIndex() *indexFixture {
	fx := &indexFixture{index: mocks.NewLocalCodeIndex(s.T()), search: s.tool("codebase_search"), repoID: uuid.New()}
	s.svc = NewService(Deps{
		LLM:            s.llm,
		Providers:      map[string]Provider{"anthropic-main": {Type: domain.LLMProviderAnthropic, DefaultModel: "claude-sonnet-4-5"}},
		WorkspaceRoot:  s.root,
		WorkspaceTools: []port.ToolExecutor{s.local},
		Remote:         s.remote,
		Index:          fx.index,
		Limits: Limits{
			MaxIterations: 10, TaskMaxIterations: 10, MaxToolOutputChars: 16000,
			History: appcontext.Budget{KeepRecentMessages: 10},
		},
	})
	return fx
}

func (s *ServiceSuite) indexedRun(kind string) AgentRun {
	run := s.run(kind)
	run.Index = &IndexRef{RepoKey: "github.com/acme/app", Branch: "tt/task-1"}
	return run
}

// attachment answers the way the local index does: the context it builds
// reads the run's repository off the context the run hands it.
func (s *ServiceSuite) attachment(fx *indexFixture, rewrites *[]bool, chat *port.LLMClient) {
	fx.index.On("Attach", mock.Anything, port.LocalIndexRef{RepoKey: "github.com/acme/app", Branch: "tt/task-1"}, mock.Anything).
		Run(func(args mock.Arguments) {
			if chat != nil {
				*chat = args.Get(2).(port.LLMClient)
			}
		}).
		Return(port.LocalIndexAttachment{
			RepositoryID: fx.repoID,
			Branch:       "tt/task-1",
			Tools:        []port.ToolExecutor{fx.search},
			ContextMessage: func(ctx context.Context, _ []domain.Message, rewrite bool) (domain.Message, bool, error) {
				*rewrites = append(*rewrites, rewrite)
				s.Equal(fx.repoID, registry.ProjectIDFromContext(ctx))
				s.Equal("tt/task-1", registry.BranchFromContext(ctx))
				s.Equal(filepath.Join(s.root, workspaceRel), registry.WorkspaceDirFromContext(ctx))
				if rewrite && chat != nil {
					_, err := (*chat).Chat(ctx, domain.AgentRequest{Messages: []domain.Message{{Role: domain.RoleUser, Content: "rewrite"}}})
					s.NoError(err)
				}
				return domain.Message{Role: domain.RoleSystem, Content: indexContextText}, true, nil
			},
		}, nil).Once()
}

func (s *ServiceSuite) ensureRequest() port.LocalIndexEnsure {
	return port.LocalIndexEnsure{
		LocalIndexRef: port.LocalIndexRef{RepoKey: "github.com/acme/app", Branch: "tt/task-1"},
		Dir:           filepath.Join(s.root, workspaceRel),
	}
}

// ensured answers the run's own index pass: seeded from the base, two files
// embedded.
func (s *ServiceSuite) ensured(fx *indexFixture) {
	fx.index.On("Ensure", mock.Anything, s.ensureRequest(), mock.Anything).
		Run(func(args mock.Arguments) {
			progress := args.Get(2).(func(port.LocalIndexProgress))
			progress(port.LocalIndexProgress{Phase: port.LocalIndexPhaseSeeded, SeededFrom: "base"})
			progress(port.LocalIndexProgress{Phase: port.LocalIndexPhaseIndexing, FilesProcessed: 2, FilesTotal: 2})
		}).
		Return(port.LocalIndexState{
			RepoKey: "github.com/acme/app", Branch: "tt/task-1", Status: domain.IndexStatusCompleted,
			Files: 12, Chunks: 30, SeededFrom: "base",
		}, nil).Once()
}

func ensureSteps(events []Event) []ensureStep {
	var steps []ensureStep
	for _, ev := range events {
		if step, ok := ev.(*StepEvent); ok && step.Step == stepIndexEnsure {
			var data ensureStep
			_ = json.Unmarshal(step.Data, &data)
			steps = append(steps, data)
		}
	}
	return steps
}

func indexSteps(events []Event) []indexStep {
	var steps []indexStep
	for _, ev := range events {
		if step, ok := ev.(*StepEvent); ok && step.Step == stepIndexContext {
			var data indexStep
			_ = json.Unmarshal(step.Data, &data)
			steps = append(steps, data)
		}
	}
	return steps
}

func (s *ServiceSuite) TestBoardRunIndexesItsCheckoutThenGetsItsContextAndSearchesTheLocalIndex() {
	fx := s.withIndex()
	var rewrites []bool
	s.ensured(fx)
	s.attachment(fx, &rewrites, nil)
	withheldSearch := s.tool("codebase_search")
	s.expectRemote(s.tool("list_board_tasks"), withheldSearch)
	fx.search.On("Execute", mock.Anything, `{"query":"refresh token"}`).
		Run(func(args mock.Arguments) {
			ctx := args.Get(0).(context.Context)
			s.Equal(fx.repoID, registry.ProjectIDFromContext(ctx))
			s.Equal("tt/task-1", registry.BranchFromContext(ctx))
		}).
		Return(domain.ToolResult{Name: "codebase_search", Content: `{"results":[{"file_path":"auth.go"}]}`}).Once()

	var firstRequest domain.AgentRequest
	s.llm.On("Chat", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) { firstRequest = args.Get(1).(domain.AgentRequest) }).
		Return(toolCallTurn("c1", "codebase_search", `{"query":"refresh token"}`, domain.Usage{PromptTokens: 10}), nil).Once()
	s.llm.On("Chat", mock.Anything, mock.Anything).Return(finalTurn("Found it."), nil).Once()

	sink := &recordingSink{}
	result, failure := s.execute(s.indexedRun(KindBoard), sink)

	s.Require().Nil(failure)
	s.Equal([]string{"codebase_search"}, result.ToolUsage)
	s.Equal([]bool{false}, rewrites)
	s.Equal([]string{"codebase_search", "list_board_tasks", "read_file"}, toolNames(firstRequest))
	last := firstRequest.Messages[len(firstRequest.Messages)-1]
	s.Equal(domain.Message{Role: domain.RoleSystem, Content: indexContextText}, last)
	s.Equal("Do the task.", firstRequest.Messages[len(firstRequest.Messages)-2].Content)

	var searchSource string
	for _, ev := range sink.snapshot() {
		if call, ok := ev.(*ToolCallEvent); ok && call.Name == "codebase_search" {
			searchSource = call.Source
		}
	}
	s.Equal(SourceLocal, searchSource)
	steps := indexSteps(sink.snapshot())
	s.Require().Len(steps, 1)
	s.True(steps[0].Injected)
	s.Equal(1, steps[0].Tools)
	s.Empty(steps[0].Error)
	ensures := ensureSteps(sink.snapshot())
	s.Require().Len(ensures, 1)
	s.Equal(ensureStep{RepoKey: "github.com/acme/app", Branch: "tt/task-1", Status: "completed", Files: 12, Chunks: 30, SeededFrom: "base", DurationMS: ensures[0].DurationMS}, ensures[0])
	var order []string
	for _, ev := range sink.snapshot() {
		switch e := ev.(type) {
		case *IndexProgressEvent:
			order = append(order, "progress:"+e.Phase)
		case *StepEvent:
			if e.Step == stepIndexEnsure || e.Step == stepIndexContext {
				order = append(order, e.Step)
			}
		}
	}
	s.Equal([]string{"progress:seeded", "progress:indexing", stepIndexEnsure, stepIndexContext}, order)
}

func (s *ServiceSuite) TestChatRunRewritesItsQueryOnItsOwnModelAndPutsTheContextBeforeTheTurn() {
	fx := s.withIndex()
	var rewrites []bool
	var chat port.LLMClient
	s.ensured(fx)
	s.attachment(fx, &rewrites, &chat)
	s.llm.On("Chat", mock.Anything, mock.MatchedBy(func(req domain.AgentRequest) bool {
		return req.ProviderType == "anthropic-main" && req.Model == "claude-sonnet-4-5" && len(req.Tools) == 0
	})).Return(finalTurn("refresh token handler"), nil).Once()
	var streamed domain.AgentRequest
	s.llm.On("ChatStream", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) { streamed = args.Get(1).(domain.AgentRequest) }).
		Return(finalTurn("Here is how refresh works."), nil).Once()
	run := s.indexedRun(KindChat)
	run.MCP = nil

	result, failure := s.execute(run, &recordingSink{})

	s.Require().Nil(failure)
	s.Equal([]bool{true}, rewrites)
	s.Equal(int64(2), result.Usage.LLMCalls)
	n := len(streamed.Messages)
	s.Require().GreaterOrEqual(n, 2)
	s.Equal(domain.Message{Role: domain.RoleSystem, Content: indexContextText}, streamed.Messages[n-2])
	s.Equal(domain.RoleUser, streamed.Messages[n-1].Role)
}

func (s *ServiceSuite) TestARunWhoseIndexCannotBeBuiltOrReadRunsWithoutItAndSaysWhy() {
	fx := s.withIndex()
	fx.index.On("Ensure", mock.Anything, s.ensureRequest(), mock.Anything).
		Return(port.LocalIndexState{}, fmt.Errorf("%w: no embedding engine", port.ErrLocalIndexUnavailable)).Once()
	fx.index.On("Attach", mock.Anything, mock.Anything, mock.Anything).
		Return(port.LocalIndexAttachment{}, errors.New("the local code index is not available: no embedding engine")).Once()
	withheldSearch := s.tool("codebase_search")
	s.expectRemote(s.tool("list_board_tasks"), withheldSearch)
	var firstRequest domain.AgentRequest
	s.llm.On("Chat", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) { firstRequest = args.Get(1).(domain.AgentRequest) }).
		Return(finalTurn("Done without search."), nil).Once()

	sink := &recordingSink{}
	_, failure := s.execute(s.indexedRun(KindBoard), sink)

	s.Require().Nil(failure)
	s.Equal([]string{"list_board_tasks", "read_file"}, toolNames(firstRequest))
	s.Equal("Do the task.", firstRequest.Messages[len(firstRequest.Messages)-1].Content)
	steps := indexSteps(sink.snapshot())
	s.Require().Len(steps, 1)
	s.False(steps[0].Injected)
	s.Contains(steps[0].Error, "no embedding engine")
	ensures := ensureSteps(sink.snapshot())
	s.Require().Len(ensures, 1)
	s.Contains(ensures[0].Error, "no embedding engine")
	s.False(ensures[0].StillIndexing)
}

func (s *ServiceSuite) TestAPassTheRunStopsWaitingForGoesOnUntilShutdown() {
	fx := s.withIndex()
	passStarted := make(chan context.Context, 1)
	passEnded := make(chan error, 1)
	fx.index.On("Ensure", mock.Anything, s.ensureRequest(), mock.Anything).
		Run(func(args mock.Arguments) {
			ctx := args.Get(0).(context.Context)
			passStarted <- ctx
			<-ctx.Done()
			passEnded <- context.Cause(ctx)
		}).
		Return(port.LocalIndexState{}, context.Canceled).Once()
	var rewrites []bool
	s.attachment(fx, &rewrites, nil)
	s.expectRemote(s.tool("list_board_tasks"))
	s.llm.On("Chat", mock.Anything, mock.Anything).Return(finalTurn("Done."), nil).Once()
	run := s.indexedRun(KindBoard)
	run.Index.WaitMS = 20

	sink := &recordingSink{}
	_, failure := s.execute(run, sink)

	s.Require().Nil(failure)
	ensures := ensureSteps(sink.snapshot())
	s.Require().Len(ensures, 1)
	s.True(ensures[0].StillIndexing)
	s.Equal([]bool{false}, rewrites, "the run still reads whatever index is finished")
	passCtx := <-passStarted
	s.NoError(passCtx.Err(), "the pass outlives the run that started it")
	select {
	case <-passEnded:
		s.Fail("the pass ended with its run")
	default:
	}

	s.svc.Shutdown()

	select {
	case cause := <-passEnded:
		s.ErrorIs(cause, errShuttingDown)
	case <-time.After(5 * time.Second):
		s.Fail("shutdown did not stop the pass")
	}
}

func (s *ServiceSuite) TestARunWithoutACheckoutReadsItsIndexWithoutAPass() {
	fx := s.withIndex()
	s.expectRemote(s.tool("list_board_tasks"))
	s.llm.On("Chat", mock.Anything, mock.Anything).Return(finalTurn("Done."), nil).Once()
	run := s.indexedRun(KindBoard)
	run.Workspace = ""
	fx.index.On("Attach", mock.Anything, port.LocalIndexRef{RepoKey: "github.com/acme/app", Branch: "tt/task-1"}, mock.Anything).
		Return(port.LocalIndexAttachment{RepositoryID: fx.repoID, Branch: "tt/task-1"}, nil).Once()

	sink := &recordingSink{}
	_, failure := s.execute(run, sink)

	s.Require().Nil(failure)
	s.Empty(ensureSteps(sink.snapshot()))
	fx.index.AssertNotCalled(s.T(), "Ensure", mock.Anything, mock.Anything, mock.Anything)
}

func (s *ServiceSuite) TestARunWithoutAnIndexNeverTouchesIt() {
	s.withIndex()
	s.expectRemote(s.tool("list_board_tasks"), s.tool("codebase_search"))
	var firstRequest domain.AgentRequest
	s.llm.On("Chat", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) { firstRequest = args.Get(1).(domain.AgentRequest) }).
		Return(finalTurn("Done."), nil).Once()

	_, failure := s.execute(s.run(KindBoard), &recordingSink{})

	s.Require().Nil(failure)
	s.Equal([]string{"list_board_tasks", "read_file"}, toolNames(firstRequest))
}

func (s *ServiceSuite) TestEnsureStreamsProgressAndAnswersTheIndexState() {
	fx := s.withIndex()
	fx.index.On("Ensure", mock.Anything, port.LocalIndexEnsure{
		LocalIndexRef: port.LocalIndexRef{RepoKey: "github.com/acme/app", Branch: "tt/task-1"},
		Dir:           filepath.Join(s.root, workspaceRel),
	}, mock.Anything).
		Run(func(args mock.Arguments) {
			progress := args.Get(2).(func(port.LocalIndexProgress))
			progress(port.LocalIndexProgress{Phase: port.LocalIndexPhaseSeeded, SeededFrom: "base"})
			for i := 0; i <= 50; i++ {
				progress(port.LocalIndexProgress{Phase: port.LocalIndexPhaseIndexing, FilesProcessed: i, FilesTotal: 50})
			}
		}).
		Return(port.LocalIndexState{
			RepoKey: "github.com/acme/app", Branch: "tt/task-1", Status: domain.IndexStatusCompleted,
			Files: 50, Chunks: 120, Symbols: 40, EmbeddingModel: "nomic-embed-text-v1.5@onnx-int8", EmbeddingDims: 768, SeededFrom: "base",
		}, nil).Once()

	prepared, failure := s.svc.PrepareIndexEnsure(context.Background(), "", IndexEnsureRequest{
		Workspace: workspaceRel, RepoKey: "github.com/acme/app", Branch: "tt/task-1",
	})
	s.Require().Nil(failure)
	s.Equal("index:github.com/acme/app@tt/task-1", prepared.ID())
	sink := &recordingSink{}
	result, failure := prepared.Execute(sink)

	s.Require().Nil(failure)
	s.Equal("completed", result.Status)
	s.Equal("base", result.SeededFrom)
	s.Equal(120, result.Chunks)
	var phases []string
	var lastProcessed int
	for _, ev := range sink.snapshot() {
		p := ev.(*IndexProgressEvent)
		phases = append(phases, p.Phase)
		lastProcessed = p.FilesProcessed
	}
	s.Equal(port.LocalIndexPhaseSeeded, phases[0])
	s.Less(len(phases), 52)
	s.Equal(50, lastProcessed)
	s.False(s.svc.CancelIndexEnsure(prepared.ID()))
}

func (s *ServiceSuite) TestSearchMapsTheIndexErrors() {
	fx := s.withIndex()
	tests := []struct {
		name string
		err  error
		code string
	}{
		{"invalid", port.ErrLocalIndexInvalid, CodeBadRequest},
		{"no engine", port.ErrLocalIndexUnavailable, CodeNotReady},
		{"not built", port.ErrLocalIndexNotReady, CodeNotReady},
		{"engine failed", port.ErrLocalIndexEmbedder, CodeUpstream},
		{"anything else", errors.New("disk full"), CodeInternal},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			fx.index.On("Search", mock.Anything, mock.Anything).Return(port.LocalIndexSearchResult{}, tt.err).Once()

			_, failure := s.svc.SearchIndex(context.Background(), IndexSearchRequest{RepoKey: "app", Query: "auth"})

			s.Require().NotNil(failure)
			s.Equal(tt.code, failure.Code)
		})
	}
}

func (s *ServiceSuite) TestEnsureRefusesARequestWithoutACheckout() {
	s.withIndex()
	s.Require().NoError(os.WriteFile(filepath.Join(s.root, "file.txt"), []byte("x"), 0o644))
	tests := []struct {
		name string
		req  IndexEnsureRequest
	}{
		{"no repo key", IndexEnsureRequest{Workspace: workspaceRel}},
		{"no workspace", IndexEnsureRequest{RepoKey: "app"}},
		{"workspace outside the root", IndexEnsureRequest{RepoKey: "app", Workspace: "../.."}},
		{"workspace is a file", IndexEnsureRequest{RepoKey: "app", Workspace: "file.txt"}},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			_, failure := s.svc.PrepareIndexEnsure(context.Background(), "", tt.req)

			s.Require().NotNil(failure)
			s.Equal(CodeBadRequest, failure.Code)
		})
	}
}
