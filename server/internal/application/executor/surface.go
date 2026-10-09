package executor

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/proctree"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// MCPOpen asks for a tool surface for one agent CLI run on this computer: the
// coordination endpoint's tools proxied, and this computer's own on top.
type MCPOpen struct {
	RunID      string            `json:"run_id"`
	Workspace  string            `json:"workspace"`
	ToolPolicy domain.ToolPolicy `json:"tool_policy"`
	CloudMCP   *RemoteTools      `json:"cloud_mcp,omitempty"`
	Index      *IndexRef         `json:"index,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	TimeoutMS  int64             `json:"timeout_ms,omitempty"`
}

type MCPSurface struct {
	V          int      `json:"v"`
	RunID      string   `json:"run_id"`
	URL        string   `json:"url"`
	Token      string   `json:"token"`
	ServerName string   `json:"server_name"`
	Tools      []string `json:"tools"`
}

type MCPClosed struct {
	V      int    `json:"v"`
	RunID  string `json:"run_id"`
	Closed bool   `json:"closed"`
}

const defaultSurfaceName = "tasktrooper"

// cliNativeTools are what an agent CLI does with tools of its own (Bash, Read,
// Write, Edit, Grep, Glob): served here too, the model would see two of each.
var cliNativeTools = map[string]bool{
	"run_terminal":  true,
	"read_file":     true,
	"write_file":    true,
	"edit_file":     true,
	"edit_lines":    true,
	"delete_file":   true,
	"move_file":     true,
	"grep_code":     true,
	"get_repo_tree": true,
}

func servedToCLI(name string) bool { return !cliNativeTools[name] }

type openSurface struct {
	runID  string
	cancel context.CancelFunc
	scope  string
	once   sync.Once
	calls  callLog

	mu          sync.Mutex
	served      port.ServedToolSurface
	closeRemote func()
}

func (o *openSurface) close() {
	o.once.Do(func() {
		o.cancel()
		o.mu.Lock()
		closeServed, closeRemote := o.served.Close, o.closeRemote
		o.mu.Unlock()
		if closeServed != nil {
			closeServed()
		}
		if closeRemote != nil {
			closeRemote()
		}
		proctree.Default.KillScope(o.scope, processGrace)
	})
}

// OpenMCP starts a run's tool surface. Once open it lasts until CloseMCP, its
// timeout or Shutdown, whatever happens to the request that opened it; a
// caller that gives up while it opens leaves nothing open.
func (s *Service) OpenMCP(ctx context.Context, req MCPOpen) (*MCPSurface, *Failure) {
	if s.deps.Surfaces == nil {
		return nil, &Failure{Code: CodeNotReady, Message: "this executor serves no tool surfaces"}
	}
	runID := strings.TrimSpace(req.RunID)
	if runID == "" {
		return nil, badRequest("run_id is required")
	}
	if len(runID) > maxRunIDLength {
		return nil, badRequest("run_id is longer than %d characters", maxRunIDLength)
	}
	if req.TimeoutMS < 0 {
		return nil, badRequest("timeout_ms cannot be negative")
	}
	workDir, failure := s.resolveWorkspace(req.Workspace)
	if failure != nil {
		return nil, failure
	}
	cloud := req.CloudMCP
	if cloud != nil && strings.TrimSpace(cloud.URL) == "" {
		cloud = nil
	}
	if cloud != nil && s.deps.Remote == nil {
		return nil, &Failure{Code: CodeInternal, Message: "this executor has no client for coordination endpoints"}
	}
	index, failure := validIndex(req.Index)
	if failure != nil {
		return nil, failure
	}
	env, failure := runEnv(req.Env)
	if failure != nil {
		return nil, failure
	}
	name := defaultSurfaceName
	if cloud != nil && strings.TrimSpace(cloud.ServerName) != "" {
		name = strings.TrimSpace(cloud.ServerName)
	}

	life, cancel := context.WithCancel(s.life)
	if req.TimeoutMS > 0 {
		var stop context.CancelFunc
		life, stop = context.WithTimeout(life, time.Duration(req.TimeoutMS)*time.Millisecond)
		parent := cancel
		cancel = func() { stop(); parent() }
	}
	surface := &openSurface{runID: runID, cancel: cancel, scope: "mcp:" + runID}
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		cancel()
		return nil, &Failure{Code: CodeCancelled, Message: errShuttingDown.Error()}
	}
	if _, busy := s.surfaces[runID]; busy {
		s.mu.Unlock()
		cancel()
		return nil, &Failure{Code: CodeConflict, Message: fmt.Sprintf("a tool surface for run %q is already open", runID)}
	}
	s.surfaces[runID] = surface
	s.mu.Unlock()
	opened := false
	stopOpening := context.AfterFunc(ctx, cancel)
	defer func() {
		stopOpening()
		if !opened {
			s.closeSurface(surface)
		}
	}()

	var remote []port.ToolExecutor
	if cloud != nil {
		tools, closeSession, err := s.deps.Remote.Connect(life, port.RemoteToolEndpoint{
			URL: cloud.URL, Token: cloud.Token, ServerName: cloud.ServerName,
		})
		if err != nil {
			return nil, &Failure{Code: CodeUpstream, Message: "the coordination tools could not be reached: " + err.Error()}
		}
		surface.mu.Lock()
		surface.closeRemote = closeSession
		surface.mu.Unlock()
		remote = tools
	}

	att := s.surfaceIndex(life, runID, workDir, index)
	built, sources := s.toolRegistry(remote, workDir, att, servedToCLI)
	tools := port.ToolRegistry(&recordingRegistry{ToolRegistry: built, sources: sources, log: &surface.calls})

	callCtx := registry.ContextWithWorkspaceDir(life, workDir)
	callCtx = withRunEnv(callCtx, workDir, env)
	callCtx = proctree.WithScope(callCtx, surface.scope)
	callCtx = indexContext(callCtx, att)

	served, err := s.deps.Surfaces.Serve(port.ToolSurface{Name: name, Registry: tools, Policy: req.ToolPolicy, Context: callCtx})
	if err != nil {
		return nil, &Failure{Code: CodeInternal, Message: "the tool surface could not be served: " + err.Error()}
	}
	surface.mu.Lock()
	surface.served = served
	surface.mu.Unlock()
	if !stopOpening() || life.Err() != nil {
		return nil, &Failure{Code: CodeCancelled, Message: "the tool surface was closed while it opened"}
	}
	context.AfterFunc(life, func() { s.closeSurface(surface) })
	opened = true

	names := servedNames(tools, req.ToolPolicy)
	log.Info().Str("run_id", runID).Str("server_name", name).Int("tools", len(names)).Bool("cloud", cloud != nil).
		Bool("index", att != nil).Msg("executor tool surface opened")
	return &MCPSurface{
		V: ProtocolVersion, RunID: runID, URL: served.URL, Token: served.Token, ServerName: name, Tools: names,
	}, nil
}

func servedNames(tools port.ToolRegistry, policy domain.ToolPolicy) []string {
	defs := tools.DefinitionsForPolicy(policy)
	names := make([]string, 0, len(defs))
	for _, def := range defs {
		names = append(names, def.Function.Name)
	}
	sort.Strings(names)
	return names
}

// surfaceIndex starts the checkout's index pass without waiting for it, unless
// the caller asked to wait: a CLI that starts now searches the base index until
// the branch's own is complete, as an agent.run does.
func (s *Service) surfaceIndex(life context.Context, runID, workDir string, ref *IndexRef) *port.LocalIndexAttachment {
	if ref == nil || s.deps.Index == nil {
		return nil
	}
	lref := port.LocalIndexRef{RepoKey: ref.RepoKey, Branch: ref.Branch}
	if workDir != "" {
		passed := make(chan struct{})
		go func() {
			defer close(passed)
			if _, err := s.deps.Index.Ensure(s.life, port.LocalIndexEnsure{LocalIndexRef: lref, Dir: workDir}, func(port.LocalIndexProgress) {}); err != nil {
				log.Warn().Err(err).Str("run_id", runID).Str("repo_key", ref.RepoKey).Msg("executor tool surface: index pass failed")
			}
		}()
		if ref.WaitMS > 0 {
			timer := time.NewTimer(min(time.Duration(ref.WaitMS)*time.Millisecond, maxIndexWait))
			select {
			case <-passed:
			case <-timer.C:
			case <-life.Done():
			}
			timer.Stop()
		}
	}
	att, err := s.deps.Index.Attach(life, lref, nil)
	if err != nil {
		log.Warn().Err(err).Str("run_id", runID).Msg("executor tool surface opens without its local index")
		return nil
	}
	return &att
}

func validIndex(ref *IndexRef) (*IndexRef, *Failure) {
	if ref == nil {
		return nil, nil
	}
	out := *ref
	out.RepoKey = strings.TrimSpace(out.RepoKey)
	out.Branch = strings.TrimSpace(out.Branch)
	if out.RepoKey == "" {
		return nil, badRequest("index.repo_key is required when a run names an index")
	}
	if out.WaitMS < 0 {
		return nil, badRequest("index.wait_ms cannot be negative")
	}
	return &out, nil
}

func (s *Service) closeSurface(surface *openSurface) {
	surface.close()
	s.mu.Lock()
	if s.surfaces[surface.runID] == surface {
		delete(s.surfaces, surface.runID)
	}
	s.mu.Unlock()
}

// CloseMCP reports whether runID had a surface open. One that already closed
// is not an error: the close and the surface's timeout race as a matter of
// course.
func (s *Service) CloseMCP(runID string) *MCPClosed {
	runID = strings.TrimSpace(runID)
	s.mu.Lock()
	surface, ok := s.surfaces[runID]
	s.mu.Unlock()
	if ok {
		s.closeSurface(surface)
		log.Info().Str("run_id", runID).Msg("executor tool surface closed")
	}
	return &MCPClosed{V: ProtocolVersion, RunID: runID, Closed: ok}
}

func (s *Service) closeAllSurfaces() {
	s.mu.Lock()
	surfaces := make([]*openSurface, 0, len(s.surfaces))
	for _, surface := range s.surfaces {
		surfaces = append(surfaces, surface)
	}
	s.mu.Unlock()
	for _, surface := range surfaces {
		s.closeSurface(surface)
	}
}

// LocalToolNames are the tools a surface on this computer can serve of its
// own; nil when it serves no surfaces.
func (s *Service) LocalToolNames() []string {
	if s.deps.Surfaces == nil {
		return nil
	}
	seen := map[string]bool{}
	var names []string
	add := func(name string) {
		if name != "" && servedToCLI(name) && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	for _, tool := range s.deps.HostTools {
		add(tool.Name())
	}
	for _, tool := range s.deps.WorkspaceTools {
		add(tool.Name())
	}
	for _, tool := range s.deps.MemberTools {
		add(tool.Name())
	}
	if s.deps.Index != nil {
		for _, name := range s.deps.IndexToolNames {
			add(name)
		}
	}
	sort.Strings(names)
	return names
}

// toolRegistry is a run's tools: the coordination endpoint's first, this
// computer's on top so a local tool wins a name clash, the index-backed ones
// last so get_symbol_skeleton answers by symbol. keep, when set, decides which
// names are registered at all.
func (s *Service) toolRegistry(remote []port.ToolExecutor, workDir string, index *port.LocalIndexAttachment, keep func(string) bool) (port.ToolRegistry, map[string]string) {
	inner := registry.New()
	sources := make(map[string]string)
	kept := func(name string) bool { return keep == nil || keep(name) }
	for _, tool := range remote {
		if remoteWithheld(tool.Name()) || !kept(tool.Name()) {
			continue
		}
		inner.Register(tool)
		sources[tool.Name()] = SourceRemote
	}
	local := append([]port.ToolExecutor(nil), s.deps.HostTools...)
	local = append(local, s.deps.MemberTools...)
	if workDir != "" {
		local = append(local, s.deps.WorkspaceTools...)
	}
	if index != nil {
		local = append(local, index.Tools...)
	}
	for _, tool := range local {
		if !kept(tool.Name()) {
			continue
		}
		inner.Register(tool)
		sources[tool.Name()] = SourceLocal
	}
	return inner, sources
}
