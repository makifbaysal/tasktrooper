package ghrunner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/childenv"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/proctree"
)

// Launcher is where a registration's runner process lives: on the machine
// the Manager runs on (Local), or on a person's own computer the Manager only
// talks to (the company edition's cloud, which holds the GitHub connection
// but must never run a repository's code itself).
type Launcher interface {
	// Supported reports whether a runner can run there at all.
	Supported() bool
	// Labels are the labels its runners register with.
	Labels() []string
	// Prepare makes repo's runner install ready, calling installing first when
	// it has to unpack one.
	Prepare(ctx context.Context, repo string, installing func()) error
	// Start runs a runner for repo with a just-in-time config, reporting the
	// lines of its output the status follows to onEvent.
	Start(ctx context.Context, repo, encodedJIT string, onEvent func(Event)) (Process, error)
	// Remove deletes repo's install, stopping what runs from it.
	Remove(repo string)
	// Reap stops runners a previous launch left running.
	Reap()
}

// Process is one running runner.
type Process interface {
	// Wait blocks until the runner exits. Err is nil for a clean exit, and
	// Output is the last lines it printed.
	Wait() ProcessExit
	// Terminate stops it, gracefully first.
	Terminate()
}

type ProcessExit struct {
	Err    error
	Output string
}

// LocalConfig is what running the runner on this machine needs.
type LocalConfig struct {
	// Dir holds the downloaded package and one install per repository.
	Dir        string
	Environ    func() []string
	HTTPClient *http.Client
	// Dist replaces the package for this platform; tests point it at a fake.
	Dist *Distribution
	// StopGrace is how long a stopping runner gets before it is killed.
	StopGrace time.Duration
}

// Local runs runners as processes of this machine.
type Local struct {
	cfg       LocalConfig
	dist      Distribution
	supported bool
	labels    []string

	installMu sync.Mutex
}

var _ Launcher = (*Local)(nil)

func NewLocal(cfg LocalConfig) *Local {
	if cfg.Environ == nil {
		cfg.Environ = os.Environ
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 30 * time.Minute}
	}
	if cfg.StopGrace <= 0 {
		cfg.StopGrace = 10 * time.Second
	}
	l := &Local{cfg: cfg, labels: labelsFor(runtime.GOOS, runtime.GOARCH)}
	if cfg.Dist != nil {
		l.dist, l.supported = *cfg.Dist, true
	} else {
		l.dist, l.supported = distributionFor(runtime.GOOS, runtime.GOARCH)
	}
	if cfg.Dir == "" {
		l.supported = false
	}
	return l
}

func (l *Local) Supported() bool { return l.supported }

func (l *Local) Labels() []string { return append([]string(nil), l.labels...) }

func (l *Local) dir(repo string) string { return installDir(l.cfg.Dir, repo) }

func (l *Local) Reap() {
	// A server that died hard leaves its runners going in their installs; a
	// new one started in the same directory would fight them for its files.
	if n, err := proctree.KillProcessesUnder(filepath.Join(l.cfg.Dir, "repos"), l.cfg.StopGrace); err != nil {
		log.Warn().Err(err).Msg("github runner: could not look for runners a previous server left running")
	} else if n > 0 {
		log.Info().Int("count", n).Msg("github runner: stopped runners a previous server left running")
	}
}

func (l *Local) archive(ctx context.Context) (string, error) {
	l.installMu.Lock()
	defer l.installMu.Unlock()
	return fetch(ctx, l.cfg.HTTPClient, l.dist, filepath.Join(l.cfg.Dir, "dist"))
}

func (l *Local) Prepare(ctx context.Context, repo string, installing func()) error {
	dir := l.dir(repo)
	if _, err := os.Stat(filepath.Join(dir, installedMarker)); err == nil {
		return nil
	}
	if installing != nil {
		installing()
	}
	archive, err := l.archive(ctx)
	if err != nil {
		return err
	}
	return install(archive, l.dist.Zip, dir)
}

func (l *Local) Remove(repo string) { _ = os.RemoveAll(l.dir(repo)) }

func (l *Local) Start(_ context.Context, repo, encodedJIT string, onEvent func(Event)) (Process, error) {
	dir := l.dir(repo)
	// Clear what the last registration left: its config files, which a new
	// just-in-time config rewrites, and the last job's checkout.
	for _, f := range []string{".runner", ".credentials", ".credentials_rsaparams", "_work"} {
		_ = os.RemoveAll(filepath.Join(dir, f))
	}
	out := &output{onEvent: onEvent}
	cmd := runCommand(dir)
	cmd.Dir = dir
	cmd.Env = childenv.For(l.cfg.Environ(), []string{"ACTIONS_RUNNER_INPUT_JITCONFIG=" + encodedJIT})
	cmd.Stdout, cmd.Stderr = out, out
	tree, err := proctree.Start(cmd)
	if err != nil {
		return nil, err
	}
	p := &localProcess{cmd: cmd, tree: tree, out: out, dir: dir, grace: l.cfg.StopGrace, exited: make(chan error, 1)}
	go func() { p.exited <- cmd.Wait() }()
	return p, nil
}

type localProcess struct {
	cmd    *exec.Cmd
	tree   *proctree.Tree
	out    *output
	dir    string
	grace  time.Duration
	exited chan error

	once sync.Once
	exit ProcessExit
	term sync.Once
}

func (p *localProcess) Wait() ProcessExit {
	p.once.Do(func() {
		err := <-p.exited
		p.tree.Close()
		_ = os.RemoveAll(filepath.Join(p.dir, "_work"))
		p.exit = ProcessExit{Err: err, Output: p.out.lastLines()}
	})
	return p.exit
}

func (p *localProcess) Terminate() {
	p.term.Do(func() { p.tree.Terminate(p.grace) })
}

func runCommand(dir string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		comspec := os.Getenv("ComSpec")
		if comspec == "" {
			comspec = `C:\Windows\System32\cmd.exe`
		}
		return exec.Command(comspec, "/d", "/c", "run.cmd")
	}
	return exec.Command(filepath.Join(dir, "run.sh"))
}

// installDir is repo's own install under root: a just-in-time runner writes
// its registration into its install directory, so two repositories cannot
// share one.
func installDir(root, repo string) string {
	sum := sha256Sum(strings.ToLower(strings.TrimSpace(repo)))
	return filepath.Join(root, "repos", sum)
}

func sha256Sum(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:6])
}
