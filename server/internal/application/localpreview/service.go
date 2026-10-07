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
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/proctree"
)

type TaskReader interface {
	Get(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.BoardTask, error)
}

type RepoRootResolver interface {
	ResolveRootPath(ctx context.Context, repositoryID uuid.UUID) (string, error)
}

type GitWorkspacer interface {
	HasGit(rootPath string) bool
	EnsureTaskWorkspace(ctx context.Context, projectRoot, workspacePath, branch string) error
}

const stopGrace = 10 * time.Second

const logTailLines = 200

var urlPattern = regexp.MustCompile(`https?://(?:localhost|127\.0\.0\.1|0\.0\.0\.0)(?::\d+)?[^\s"'<>]*`)

// portPattern is the fallback when a server prints its port without a full
// URL — Go's net/http default log line, Spring Boot's embedded-Tomcat
// banner, and bare framework conventions all do this. It matches "listening
// on :8080", "Listening on port 8080", "Tomcat started on port 8080 (http)"
// and "port(s): 8080"; group 1 is the port.
var portPattern = regexp.MustCompile(`(?i)\b(?:listening on(?: port)?|started on port(?:\(s\))?|port(?:\(s\))?)\s*:?\s*(\d{2,5})\b`)

type Deps struct {
	Tasks         TaskReader
	Repositories  RepoRootResolver
	Git           GitWorkspacer
	WorkspaceRoot string
}

type Service struct {
	tasks         TaskReader
	repos         RepoRootResolver
	git           GitWorkspacer
	workspaceRoot string

	mu     sync.Mutex
	active map[uuid.UUID]*process
}

func NewService(deps Deps) *Service {
	s := &Service{
		tasks:         deps.Tasks,
		repos:         deps.Repositories,
		git:           deps.Git,
		workspaceRoot: deps.WorkspaceRoot,
	}
	s.reapStale()
	return s
}

func (s *Service) reapStale() {
	if s.workspaceRoot == "" {
		return
	}
	for _, e := range loadState(s.workspaceRoot) {
		if e.PID <= 0 || !isStalePreview(e.PID, processCommand(e.PID), e.Command) {
			continue
		}
		log.Info().Int("pid", e.PID).Str("command", e.Command).
			Msg("local preview: stopping a preview a previous server left running")
		go stopStale(e.PID, stopGrace)
	}

	saveState(s.workspaceRoot, nil)
}

// commandMatches decides whether a persisted pid is still the preview that was
// started under it. The OS hands a pid out again once its process is gone —
// on Windows within seconds — so a pid alone could name anything the user
// runs. The live command line is `sh -c <command>`, or the command itself once
// the shell exec'd it, so containing it is the test. Where the command line
// cannot be read (Windows) nothing matches; the job object the preview ran in
// already ended it with the server that started it.
func commandMatches(live, started string) bool {
	live, started = strings.TrimSpace(live), strings.TrimSpace(started)
	return live != "" && started != "" && strings.Contains(live, started)
}

// isStalePreview is commandMatches plus the one shape it cannot see: the
// Maven and Gradle wrappers end in `exec java …`, so a `./mvnw spring-boot:run`
// preview's pid shows a java command line. That pid still leads the process
// group the preview was started in, which a recycled pid almost never does.
func isStalePreview(pid int, live, started string) bool {
	if commandMatches(live, started) {
		return true
	}
	return jvmWrapper.MatchString(started) && javaCommand.MatchString(live) && leadsOwnGroup(pid)
}

var (
	jvmWrapper  = regexp.MustCompile(`(^|[\s/])(mvnw|gradlew|mvn|gradle)(\.bat|\.cmd)?(\s|$)`)
	javaCommand = regexp.MustCompile(`(^|/)java(\.exe)?(\s|$)`)
)

type process struct {
	preview domain.LocalPreview
	cmd     *exec.Cmd
	tree    *proctree.Tree
	done    chan struct{}

	mu      sync.Mutex
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
		} else if m := portPattern.FindStringSubmatch(line); m != nil {
			p.url = "http://127.0.0.1:" + m[1]
			p.status = domain.LocalPreviewRunning
		}
	}
}

func (p *process) exited() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

func (p *process) setDone(status domain.LocalPreviewStatus, detail string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.status = status
	if detail != "" {
		p.detail = detail
	}
}

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
	var detectDetail string
	if command == "" {
		command, detectDetail = DetectRunCommand(workspacePath)
	}
	if command == "" {
		msg := "could not detect a way to run this repository locally (looked for an npm dev/start script, a Makefile dev/run target, a Maven or Gradle Spring Boot/Quarkus project, or a Go module)"
		if detectDetail != "" {
			msg += ": " + detectDetail
		}
		return domain.LocalPreview{}, fmt.Errorf("%s", msg)
	}

	releaseStaleDevServer(workspacePath)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active == nil {
		s.active = make(map[uuid.UUID]*process)
	}
	if existing, ok := s.active[repositoryID]; ok {
		s.stopLocked(existing)
	}

	cmd := shellCommand(command)
	cmd.Dir = workspacePath
	cmd.Env = os.Environ()

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

	tree, err := proctree.Start(cmd)
	if err != nil {
		return domain.LocalPreview{}, fmt.Errorf("start %q: %w", command, err)
	}
	p.tree = tree

	s.active[repositoryID] = p
	s.persistLocked()
	go pumpLines(stdout, p.appendLine)
	go pumpLines(stderr, p.appendLine)
	go s.wait(repositoryID, p)

	log.Info().Str("repository_id", repositoryID.String()).Str("task_id", taskID.String()).
		Str("branch", branch).Str("command", command).Msg("local preview started")
	return p.snapshot(), nil
}

func pumpLines(r io.Reader, onLine func(string)) {
	scanner := bufio.NewScanner(r)

	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		onLine(scanner.Text())
	}
}

func (s *Service) wait(repositoryID uuid.UUID, p *process) {
	err := p.cmd.Wait()
	close(p.done)
	p.mu.Lock()
	stopped := p.status == domain.LocalPreviewStopped
	p.mu.Unlock()
	switch {
	case stopped:

	case err != nil:
		p.setDone(domain.LocalPreviewFailed, err.Error())
	default:
		p.setDone(domain.LocalPreviewFailed, "the command exited on its own")
	}
	// A failed preview stays the repository's active one, so the reviewer
	// reads why it died instead of the panel quietly falling back to its
	// start button; the next Start or Stop clears it.
	s.mu.Lock()
	if s.active[repositoryID] == p {
		if stopped {
			delete(s.active, repositoryID)
		}
		s.persistLocked()
	}
	s.mu.Unlock()
}

func (s *Service) Status(repositoryID uuid.UUID) (domain.LocalPreview, bool) {
	s.mu.Lock()
	p, ok := s.active[repositoryID]
	s.mu.Unlock()
	if !ok {
		return domain.LocalPreview{}, false
	}
	return p.snapshot(), true
}

func (s *Service) Stop(repositoryID uuid.UUID) {
	s.mu.Lock()
	p, ok := s.active[repositoryID]
	if ok {
		delete(s.active, repositoryID)
		s.persistLocked()
	}
	s.mu.Unlock()
	if ok {
		s.stopProcess(p)
	}
}

func (s *Service) stopLocked(p *process) {
	delete(s.active, p.preview.RepositoryID)
	go s.stopProcess(p)
}

func (s *Service) persistLocked() {
	entries := make([]persistedEntry, 0, len(s.active))
	for repositoryID, p := range s.active {
		if p.cmd.Process == nil || p.exited() {
			continue
		}
		entries = append(entries, persistedEntry{RepositoryID: repositoryID, PID: p.cmd.Process.Pid, Command: p.preview.Command})
	}
	saveState(s.workspaceRoot, entries)
}

func (s *Service) stopProcess(p *process) {
	p.mu.Lock()
	p.status = domain.LocalPreviewStopped
	p.mu.Unlock()
	// An exited process's group id is free for the OS to hand out again.
	if p.cmd.Process == nil || p.exited() {
		return
	}
	terminateProcessGroup(p.cmd.Process.Pid)
	select {
	case <-p.done:
	case <-time.After(stopGrace):
		p.tree.Kill()
		<-p.done
	}
	p.tree.Close()
}

// Close stops every active preview and forgets them, so the next boot finds
// nothing of this process's to reap. Safe to call more than once.
func (s *Service) Close() {
	s.mu.Lock()
	procs := make([]*process, 0, len(s.active))
	for _, p := range s.active {
		procs = append(procs, p)
	}
	s.active = make(map[uuid.UUID]*process)
	s.persistLocked()
	s.mu.Unlock()

	var wg sync.WaitGroup
	for _, p := range procs {
		wg.Add(1)
		go func(p *process) {
			defer wg.Done()
			s.stopProcess(p)
		}(p)
	}
	wg.Wait()
}

// DetectRunCommand returns the shell command to run dir's project locally.
// command is "" when nothing recognised matched; detail, only ever set
// alongside an empty command, names what was ambiguous (today: a Go module
// with several cmd/*/main.go candidates and no root main) so the caller's
// error can tell the agent what commandOverride to pass instead of a bare
// "could not detect".
func DetectRunCommand(dir string) (command, detail string) {
	if hasNPMScript(dir, "dev") {
		return "npm run dev", ""
	}
	if hasNPMScript(dir, "start") {
		return "npm start", ""
	}
	if fileExists(filepath.Join(dir, "Makefile")) {
		if makeHasTarget(dir, "dev") {
			return "make dev", ""
		}
		if makeHasTarget(dir, "run") {
			return "make run", ""
		}
	}
	if cmd := mavenRunCommand(dir); cmd != "" {
		return cmd, ""
	}
	if cmd := gradleRunCommand(dir); cmd != "" {
		return cmd, ""
	}
	if fileExists(filepath.Join(dir, "go.mod")) {
		if hasRootMainPackage(dir) {
			return "go run .", ""
		}
		switch mains := cmdMainPackages(dir); len(mains) {
		case 0:
			// No root main.go and no cmd/<name>/main.go either; "go run ."
			// is the same best-effort guess this returned before cmd/ was
			// understood — still the least-wrong default for an unusual
			// layout DetectRunCommand does not otherwise recognise.
			return "go run .", ""
		case 1:
			return "go run ./cmd/" + mains[0], ""
		default:
			return "", fmt.Sprintf(
				"this Go module's main package is not at the root, and cmd/ holds several: %s — pass commandOverride with the one to run, e.g. %q",
				strings.Join(mains, ", "), "go run ./cmd/"+mains[0],
			)
		}
	}
	return "", ""
}

// packageMainPattern matches a top-level "package main" declaration; it is a
// text scan rather than go/parser because DetectRunCommand runs against a
// freshly checked-out workspace that need not build yet.
var packageMainPattern = regexp.MustCompile(`(?m)^package\s+main\s*$`)

// hasRootMainPackage reports whether dir itself (not a subdirectory) holds a
// "package main" .go file — the common case `go run .` targets.
func hasRootMainPackage(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		if packageMainPattern.Match(data) {
			return true
		}
	}
	return false
}

// cmdMainPackages lists the cmd/<name> directories that hold a main.go, sorted
// for a deterministic pick when exactly one exists.
func cmdMainPackages(dir string) []string {
	cmdDir := filepath.Join(dir, "cmd")
	entries, err := os.ReadDir(cmdDir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && fileExists(filepath.Join(cmdDir, e.Name(), "main.go")) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// mavenRunCommand detects a Maven-wrapper project and which plugin to run it
// with. Only the committed wrapper is used (mvnw), never a bare "mvn" — a
// task workspace has no guarantee the right Maven is on PATH.
func mavenRunCommand(dir string) string {
	if !fileExists(filepath.Join(dir, "mvnw")) {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(dir, "pom.xml"))
	if err != nil {
		return ""
	}
	content := string(data)
	switch {
	case strings.Contains(content, "quarkus"):
		return "./mvnw quarkus:dev"
	case strings.Contains(content, "spring-boot"):
		return "./mvnw spring-boot:run"
	}
	return ""
}

// gradleRunCommand is mavenRunCommand's Gradle-wrapper counterpart; Kotlin
// and Groovy build scripts carry the same plugin ids as plain text.
func gradleRunCommand(dir string) string {
	if !fileExists(filepath.Join(dir, "gradlew")) {
		return ""
	}
	var content string
	for _, name := range []string{"build.gradle.kts", "build.gradle"} {
		if data, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
			content = string(data)
			break
		}
	}
	if content == "" {
		return ""
	}
	switch {
	case strings.Contains(content, "quarkus"):
		return "./gradlew quarkusDev"
	case strings.Contains(content, "spring-boot") || strings.Contains(content, "org.springframework.boot"):
		return "./gradlew bootRun"
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
