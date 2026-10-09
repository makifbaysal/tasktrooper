package executor

import (
	"context"
	"path/filepath"
	"sync"
	"time"

	"github.com/stretchr/testify/mock"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/proctree"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

// fakeSurfaces records what the service asked to serve instead of listening.
type fakeSurfaces struct {
	mu      sync.Mutex
	served  []port.ToolSurface
	closed  int
	failing error
}

func (f *fakeSurfaces) Serve(surface port.ToolSurface) (port.ServedToolSurface, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failing != nil {
		return port.ServedToolSurface{}, f.failing
	}
	f.served = append(f.served, surface)
	return port.ServedToolSurface{URL: "http://127.0.0.1:1/mcp", Token: "surface-token-000000", Close: func() {
		f.mu.Lock()
		f.closed++
		f.mu.Unlock()
	}}, nil
}

func (f *fakeSurfaces) last() port.ToolSurface {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.served[len(f.served)-1]
}

func (f *fakeSurfaces) closedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

func (s *ServiceSuite) withSurfaces(index port.LocalCodeIndex) *fakeSurfaces {
	surfaces := &fakeSurfaces{}
	deps := s.svc.deps
	deps.Surfaces = surfaces
	deps.HostTools = []port.ToolExecutor{s.tool("http_request"), s.tool("browser_navigate")}
	deps.WorkspaceTools = []port.ToolExecutor{s.local, s.tool("download_file"), s.tool("get_symbol_skeleton"), s.tool("run_terminal")}
	deps.Index = index
	deps.IndexToolNames = []string{"codebase_search", "expand_symbol_context", "get_symbol_skeleton"}
	s.svc = NewService(deps)
	return surfaces
}

func (s *ServiceSuite) openRequest() MCPOpen {
	return MCPOpen{
		RunID:     "cli-1",
		Workspace: workspaceRel,
		CloudMCP:  &RemoteTools{URL: "https://cloud.example/api/mcp", Token: "run-token-123456", ServerName: "tasktrooper"},
	}
}

func servedSorted(surface port.ToolSurface) []string {
	return servedNames(surface.Registry, surface.Policy)
}

func (s *ServiceSuite) TestASurfaceServesTheCloudsToolsAndThisComputersWithLocalWinning() {
	surfaces := s.withSurfaces(nil)
	cloudBoard := s.tool("list_board_tasks")
	cloudHTTP := s.tool("http_request")
	s.expectRemote(cloudBoard, cloudHTTP, s.tool("codebase_search"), s.tool("browser_click"))

	opened, failure := s.svc.OpenMCP(context.Background(), s.openRequest())
	s.Require().Nil(failure)
	s.Equal("tasktrooper", opened.ServerName)
	s.Equal("surface-token-000000", opened.Token)

	surface := surfaces.last()
	s.Equal("tasktrooper", surface.Name)
	s.Equal([]string{"browser_navigate", "download_file", "get_symbol_skeleton", "http_request", "list_board_tasks"}, servedSorted(surface))
	s.Equal(opened.Tools, servedSorted(surface))

	s.local.AssertNotCalled(s.T(), "Execute", mock.Anything, mock.Anything)
	localHTTP := surfaces.last().Registry
	s.svc.deps.HostTools[0].(*mocks.ToolExecutor).On("Execute", mock.Anything, "{}").Return(domain.ToolResult{Content: "local"}).Once()
	result := localHTTP.ExecuteWithPolicy(surface.Context, domain.ToolCall{Function: domain.FunctionCall{Name: "http_request", Arguments: "{}"}}, surface.Policy)
	s.Equal("local", result.Content, "a local tool must win a name clash with the cloud's")
	cloudHTTP.AssertNotCalled(s.T(), "Execute", mock.Anything, mock.Anything)

	s.Equal(filepath.Join(s.root, workspaceRel), registry.EffectiveWorkspaceDir(surface.Context))
	s.Equal("mcp:cli-1", proctree.ScopeFrom(surface.Context))
	s.True(s.svc.CloseMCP("cli-1").Closed)
	s.True(s.closed, "the coordination session must close with the surface")
	s.Equal(1, surfaces.closedCount())
	s.Error(surface.Context.Err())
}

func (s *ServiceSuite) TestASurfaceWithoutACloudOrAWorkspaceServesTheHostToolsAlone() {
	surfaces := s.withSurfaces(nil)
	opened, failure := s.svc.OpenMCP(context.Background(), MCPOpen{RunID: "cli-2"})
	s.Require().Nil(failure)
	s.Equal(defaultSurfaceName, opened.ServerName)
	s.Equal([]string{"browser_navigate", "http_request"}, servedSorted(surfaces.last()))
	s.svc.CloseMCP("cli-2")
}

func (s *ServiceSuite) TestASurfaceAttachesTheIndexTheRunNames() {
	index := mocks.NewLocalCodeIndex(s.T())
	surfaces := s.withSurfaces(index)
	s.expectRemote()
	search := s.tool("codebase_search")
	ref := port.LocalIndexRef{RepoKey: "repo-1", Branch: "tt/task-1"}
	passed := make(chan struct{})
	index.On("Ensure", mock.Anything, port.LocalIndexEnsure{LocalIndexRef: ref, Dir: filepath.Join(s.root, workspaceRel)}, mock.Anything).
		Run(func(mock.Arguments) { close(passed) }).Return(port.LocalIndexState{}, nil).Once()
	index.On("Attach", mock.Anything, ref, nil).Return(port.LocalIndexAttachment{Branch: "tt/task-1", Tools: []port.ToolExecutor{search}}, nil).Once()

	req := s.openRequest()
	req.Index = &IndexRef{RepoKey: " repo-1 ", Branch: "tt/task-1", WaitMS: 5000}
	_, failure := s.svc.OpenMCP(context.Background(), req)
	s.Require().Nil(failure)
	<-passed
	s.Contains(servedSorted(surfaces.last()), "codebase_search")
	s.Equal("tt/task-1", registry.BranchFromContext(surfaces.last().Context))
	s.svc.CloseMCP("cli-1")
}

func (s *ServiceSuite) TestToolPolicyIsTheSurfaces() {
	surfaces := s.withSurfaces(nil)
	s.expectRemote(s.tool("list_board_tasks"))
	req := s.openRequest()
	req.ToolPolicy = domain.ToolPolicy{AllowTools: []string{"list_board_tasks", "browser_*"}}
	opened, failure := s.svc.OpenMCP(context.Background(), req)
	s.Require().Nil(failure)
	s.Equal([]string{"browser_navigate", "list_board_tasks"}, opened.Tools)
	s.Equal(req.ToolPolicy, surfaces.last().Policy)
	s.svc.CloseMCP("cli-1")
}

func (s *ServiceSuite) TestOneSurfacePerRunAndItEndsAtItsTimeoutOrShutdown() {
	surfaces := s.withSurfaces(nil)
	s.expectRemote()
	req := s.openRequest()
	req.TimeoutMS = 100
	_, failure := s.svc.OpenMCP(context.Background(), req)
	s.Require().Nil(failure)
	_, failure = s.svc.OpenMCP(context.Background(), req)
	s.Require().NotNil(failure)
	s.Equal(CodeConflict, failure.Code)

	s.Eventually(func() bool { return surfaces.closedCount() == 1 }, 5*time.Second, 10*time.Millisecond)
	s.False(s.svc.CloseMCP("cli-1").Closed, "a surface past its timeout is already closed")

	s.expectRemote()
	req.TimeoutMS = 0
	_, failure = s.svc.OpenMCP(context.Background(), req)
	s.Require().Nil(failure)
	s.svc.Shutdown()
	s.Equal(2, surfaces.closedCount())
	_, failure = s.svc.OpenMCP(context.Background(), MCPOpen{RunID: "cli-3"})
	s.Require().NotNil(failure)
	s.Equal(CodeCancelled, failure.Code)
}

func (s *ServiceSuite) TestASurfaceIsRefusedBeforeAnythingOpens() {
	s.Equal(CodeNotReady, func() string {
		_, f := s.svc.OpenMCP(context.Background(), s.openRequest())
		return f.Code
	}(), "an executor with no surface server answers not_ready")

	surfaces := s.withSurfaces(nil)
	for name, req := range map[string]MCPOpen{
		"no run id":         {Workspace: workspaceRel},
		"outside the root":  {RunID: "x", Workspace: "../elsewhere"},
		"negative timeout":  {RunID: "x", TimeoutMS: -1},
		"index without key": {RunID: "x", Index: &IndexRef{}},
		"PATH in env":       {RunID: "x", Env: map[string]string{"PATH": "/tmp"}},
	} {
		_, failure := s.svc.OpenMCP(context.Background(), req)
		s.Require().NotNil(failure, name)
		s.Equal(CodeBadRequest, failure.Code, name)
	}
	s.Empty(surfaces.served)

	s.remote.On("Connect", mock.Anything, mock.Anything).Return(nil, nil, context.DeadlineExceeded).Once()
	_, failure := s.svc.OpenMCP(context.Background(), s.openRequest())
	s.Require().NotNil(failure)
	s.Equal(CodeUpstream, failure.Code)
	s.NotContains(failure.Message, "run-token-123456")
	s.False(s.svc.CloseMCP("cli-1").Closed, "a surface that failed to open leaves no run behind")
}

func (s *ServiceSuite) TestLocalToolNamesAreWhatASurfaceCanServe() {
	s.Nil(s.svc.LocalToolNames(), "no surface server, nothing to advertise")
	s.withSurfaces(nil)
	s.Equal([]string{"browser_navigate", "download_file", "get_symbol_skeleton", "http_request"}, s.svc.LocalToolNames())
	s.withSurfaces(mocks.NewLocalCodeIndex(s.T()))
	s.Equal([]string{"browser_navigate", "codebase_search", "download_file", "expand_symbol_context", "get_symbol_skeleton", "http_request"},
		s.svc.LocalToolNames())
}

func (s *ServiceSuite) TestACallerThatGivesUpWhileItOpensLeavesNothingOpen() {
	surfaces := s.withSurfaces(nil)
	ctx, giveUp := context.WithCancel(context.Background())
	s.remote.On("Connect", mock.Anything, mock.Anything).Run(func(mock.Arguments) { giveUp() }).
		Return([]port.ToolExecutor{}, func() { s.closed = true }, nil).Once()
	_, failure := s.svc.OpenMCP(ctx, s.openRequest())
	s.Require().NotNil(failure)
	s.Equal(CodeCancelled, failure.Code)
	s.True(s.closed, "the coordination session opened for it was closed")
	s.Equal(1, surfaces.closedCount())
	s.False(s.svc.CloseMCP("cli-1").Closed)

	s.expectRemote()
	opened, failure := s.svc.OpenMCP(context.Background(), s.openRequest())
	s.Require().Nil(failure)
	s.NotEmpty(opened.URL)
	s.svc.CloseMCP("cli-1")
}

func (s *ServiceSuite) TestASurfaceRecordsEachLocalToolCallOnceAndNotTheCloudsOwn() {
	surfaces := s.withSurfaces(nil)
	cloudBoard := s.tool("list_board_tasks")
	cloudBoard.On("Execute", mock.Anything, "{}").Return(domain.ToolResult{Content: "ok"}).Once()
	s.expectRemote(cloudBoard)
	browser := s.svc.deps.HostTools[1].(*mocks.ToolExecutor)
	browser.On("Execute", mock.Anything, "{}").Return(domain.ToolResult{Content: "boom", IsError: true}).Once()
	browser.On("Execute", mock.Anything, "{}").Return(domain.ToolResult{Content: "ok"}).Once()

	_, failure := s.svc.OpenMCP(context.Background(), s.openRequest())
	s.Require().Nil(failure)
	surface := surfaces.last()
	for _, name := range []string{"browser_navigate", "list_board_tasks", "browser_navigate"} {
		surface.Registry.ExecuteWithPolicy(surface.Context, domain.ToolCall{Function: domain.FunctionCall{Name: name, Arguments: "{}"}}, surface.Policy)
	}

	first := s.svc.MCPCallsSince("cli-1", 0)
	s.Require().Len(first.Calls, 2)
	s.Equal("browser_navigate", first.Calls[0].Name)
	s.True(first.Calls[0].IsError)
	s.False(first.Calls[1].IsError)
	s.Equal(2, first.Next)
	s.False(first.Closed)

	again := s.svc.MCPCallsSince("cli-1", first.Next)
	s.Empty(again.Calls)
	s.Equal(2, again.Next)

	s.svc.CloseMCP("cli-1")
	s.True(s.svc.MCPCallsSince("cli-1", 2).Closed)
}
