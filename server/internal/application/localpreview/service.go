// Package localpreview runs a task's branch on this machine so a human_uat
// reviewer can poke at it before approving — the manual counterpart to PM's
// own automated pass, which always exercises stage (see
// seeddata/skills/product-manager/pm-uat-review). Neither replaces the other:
// PM's pass is repeatable evidence attached to the criteria, this one is a
// person looking at the thing.
package localpreview

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// TaskReader is port.BoardTaskStore narrowed to the one read this package needs.
type TaskReader interface {
	Get(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.BoardTask, error)
}

// RepoRootResolver is session.RepositoryResolver narrowed the same way.
type RepoRootResolver interface {
	ResolveRootPath(ctx context.Context, repositoryID uuid.UUID) (string, error)
}

// GitWorkspacer is port.GitClient narrowed to what a preview needs: the SAME
// checkout the board runner, the pipeline and a task-bound chat already work
// in (see runtime.taskChatWorkspace) — running the reviewer's preview there
// rather than in a separate worktree guarantees it is exactly the code under
// review, and by human_uat nothing else is still editing that tree.
type GitWorkspacer interface {
	HasGit(rootPath string) bool
	EnsureTaskWorkspace(ctx context.Context, projectRoot, workspacePath, branch string) error
}

// stopGrace bounds how long Stop waits for SIGTERM before escalating to
// SIGKILL. Short on purpose — the desktop shell's own supervisor gives its
// backend 30s because it may be mid-request; a dev server has no in-flight
// work worth that wait.
const stopGrace = 10 * time.Second

// logTailLines bounds how much of a preview's own output Status returns.
// Enough to show what a dev server just printed, not a substitute for its
// real logs.
const logTailLines = 200

// urlPattern matches the address a dev server prints when it comes up ("Local:
// http://localhost:5173/", "Listening on 127.0.0.1:3000", ...). 0.0.0.0 is
// normalised to 127.0.0.1 below — a server bound there is reachable there, and
// 0.0.0.0 is not a URL a browser can be pointed at.
var urlPattern = regexp.MustCompile(`https?://(?:localhost|127\.0\.0\.1|0\.0\.0\.0)(?::\d+)?[^\s"'<>]*`)

type Deps struct {
	Tasks         TaskReader
	Repositories  RepoRootResolver
	Git           GitWorkspacer
	WorkspaceRoot string
}

// Service owns at most one running preview per repository — starting a second
// one for that repository stops the first, mirroring the single-child
// assumption the desktop shell's own supervisor makes about the backend it
// runs.
type Service struct {
	tasks         TaskReader
	repos         RepoRootResolver
	git           GitWorkspacer
	workspaceRoot string

	mu     sync.Mutex
	active map[uuid.UUID]*process
}

func NewService(deps Deps) *Service {
	return &Service{
		tasks:         deps.Tasks,
		repos:         deps.Repositories,
		git:           deps.Git,
		workspaceRoot: deps.WorkspaceRoot,
	}
}

// process is one running (or just-exited) preview's live state.
type process struct {
	preview domain.LocalPreview
	cmd     *exec.Cmd
	done    chan struct{}

	mu      sync.Mutex // guards everything below, and preview's mutable fields
	status  domain.LocalPreviewStatus
	url     string
	detail  string
	logTail []string
}

func (p *process) snapshot() domain.LocalPreview {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := p.preview
	out.Status = p.status
	out.URL = p.url
	out.Detail = p.detail
	out.LogTail = append([]string(nil), p.logTail...)
	return out
}

func (p *process) appendLine(line string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.logTail = append(p.logTail, line)
	if len(p.logTail) > logTailLines {
		p.logTail = p.logTail[len(p.logTail)-logTailLines:]
	}
	if p.url == "" {
		if m := urlPattern.FindString(line); m != "" {
			p.url = strings.Replace(m, "0.0.0.0", "127.0.0.1", 1)
			p.status = domain.LocalPreviewRunning
		}
	}
}

func (p *process) setDone(status domain.LocalPreviewStatus, detail string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	// A clean exit after the URL was already seen is still worth calling
	// "running" right up to the moment Stop or the process itself ends it —
	// but once it has, the status must say so regardless of what the last log
	// line implied.
	p.status = status
	if detail != "" {
		p.detail = detail
	}
}

// Start checks out the task's branch (or reuses the checkout already there)
// and runs a command in it. Only one preview per repository: an existing one
// is stopped first.
//
// commandOverride is normally "" — DetectRunCommand reads the checked-out
// tree itself once it exists, which is the only point a script name can
// actually be confirmed. A caller that already knows the command (a future
// per-repository setting, a test) may pass it instead and skip detection.
func (s *Service) Start(ctx context.Context, repositoryID, taskID uuid.UUID, commandOverride string) (domain.LocalPreview, error) {
	task, err := s.tasks.Get(ctx, repositoryID, taskID)
	if err != nil {
		return domain.LocalPreview{}, fmt.Errorf("read task: %w", err)
	}
	root, err := s.repos.ResolveRootPath(ctx, repositoryID)
	if err != nil {
		return domain.LocalPreview{}, fmt.Errorf("resolve repository: %w", err)
	}
	if s.git == nil || s.workspaceRoot == "" || !s.git.HasGit(root) {
		return domain.LocalPreview{}, fmt.Errorf("this repository has no git working copy to check a branch out of")
	}
	branch := domain.TaskBranchName(task)
	workspacePath, err := workspace.TaskDir(s.workspaceRoot, taskID)
	if err != nil {
		return domain.LocalPreview{}, err
	}
	if err := s.git.EnsureTaskWorkspace(ctx, root, workspacePath, branch); err != nil {
		return domain.LocalPreview{}, fmt.Errorf("check out branch %s: %w", branch, err)
	}

	command := strings.TrimSpace(commandOverride)
	if command == "" {
		command = DetectRunCommand(workspacePath)
	}
	if command == "" {
		return domain.LocalPreview{}, fmt.Errorf("could not detect a way to run this repository locally (looked for an npm dev/start script, a Makefile dev target, or a Go module)")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active == nil {
		s.active = make(map[uuid.UUID]*process)
	}
	if existing, ok := s.active[repositoryID]; ok {
		s.stopLocked(existing)
	}

	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = workspacePath
	cmd.Env = os.Environ()
	// Its own process group so Stop can signal every child a shell-wrapped dev
	// command spawns (webpack behind `npm run dev`, say), not just the shell.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return domain.LocalPreview{}, fmt.Errorf("open preview output: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return domain.LocalPreview{}, fmt.Errorf("open preview error output: %w", err)
	}

	p := &process{
		preview: domain.LocalPreview{
			RepositoryID: repositoryID,
			TaskID:       taskID,
			Branch:       branch,
			Command:      command,
			StartedAt:    time.Now(),
		},
		cmd:    cmd,
		done:   make(chan struct{}),
		status: domain.LocalPreviewStarting,
	}

	if err := cmd.Start(); err != nil {
		return domain.LocalPreview{}, fmt.Errorf("start %q: %w", command, err)
	}

	s.active[repositoryID] = p
	go pumpLines(stdout, p.appendLine)
	go pumpLines(stderr, p.appendLine)
	go s.wait(repositoryID, p)

	log.Info().Str("repository_id", repositoryID.String()).Str("task_id", taskID.String()).
		Str("branch", branch).Str("command", command).Msg("local preview started")
	return p.snapshot(), nil
}

func pumpLines(r io.Reader, onLine func(string)) {
	scanner := bufio.NewScanner(r)
	// A framework's own progress line (webpack, vite) can run well past
	// bufio's 64KiB default before it wraps — that overflow used to end the
	// pump early and silently stop detecting the preview's URL.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		onLine(scanner.Text())
	}
}

// wait owns the process after Start returns: it blocks on Wait(), records the
// exit, and clears the active slot IF this process is still the one occupying
// it (a Start that replaced it already did that itself).
func (s *Service) wait(repositoryID uuid.UUID, p *process) {
	err := p.cmd.Wait()
	close(p.done)
	p.mu.Lock()
	stopped := p.status == domain.LocalPreviewStopped
	p.mu.Unlock()
	switch {
	case stopped:
		// Stop already set the terminal status; an exit error here is just
		// the signal that ended it, not a failure to report.
	case err != nil:
		p.setDone(domain.LocalPreviewFailed, err.Error())
	default:
		p.setDone(domain.LocalPreviewFailed, "the command exited on its own")
	}
	s.mu.Lock()
	if s.active[repositoryID] == p {
		delete(s.active, repositoryID)
	}
	s.mu.Unlock()
}

// Status reports the repository's current preview, ok=false when none is
// running (or ever ran since this server started).
func (s *Service) Status(repositoryID uuid.UUID) (domain.LocalPreview, bool) {
	s.mu.Lock()
	p, ok := s.active[repositoryID]
	s.mu.Unlock()
	if !ok {
		return domain.LocalPreview{}, false
	}
	return p.snapshot(), true
}

// Stop ends the repository's running preview, if any. Not an error to call
// with nothing running — the button that calls this cannot always tell.
func (s *Service) Stop(repositoryID uuid.UUID) {
	s.mu.Lock()
	p, ok := s.active[repositoryID]
	if ok {
		delete(s.active, repositoryID)
	}
	s.mu.Unlock()
	if ok {
		s.stopProcess(p)
	}
}

// stopLocked is Stop's body for the caller that already holds s.mu (Start,
// replacing a previous preview) — it must not call Stop and deadlock on the
// same lock.
func (s *Service) stopLocked(p *process) {
	delete(s.active, p.preview.RepositoryID)
	go s.stopProcess(p)
}

func (s *Service) stopProcess(p *process) {
	p.mu.Lock()
	p.status = domain.LocalPreviewStopped
	p.mu.Unlock()
	if p.cmd.Process == nil {
		return
	}
	pgid := p.cmd.Process.Pid
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	select {
	case <-p.done:
		return
	case <-time.After(stopGrace):
	}
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	<-p.done
}

// DetectRunCommand guesses a dev/start command from the workspace's own
// tooling, for a repository with none configured. Convention, not
// configuration: the common frameworks all name their dev script the same
// couple of ways, and guessing wrong just leaves the button reporting "no
// command configured" the way an empty RunCommand always would.
func DetectRunCommand(dir string) string {
	if hasNPMScript(dir, "dev") {
		return "npm run dev"
	}
	if hasNPMScript(dir, "start") {
		return "npm start"
	}
	if fileExists(filepath.Join(dir, "Makefile")) && makeHasTarget(dir, "dev") {
		return "make dev"
	}
	if fileExists(filepath.Join(dir, "go.mod")) {
		return "go run ."
	}
	return ""
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func hasNPMScript(dir, script string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return false
	}
	// Good enough for a script-name check without a JSON dependency here:
	// looked up as a quoted key, which is all package.json ever uses.
	return strings.Contains(string(data), `"`+script+`":`)
}

func makeHasTarget(dir, target string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "Makefile"))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, target+":") {
			return true
		}
	}
	return false
}
