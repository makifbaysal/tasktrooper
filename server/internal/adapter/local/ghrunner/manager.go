// Package ghrunner makes this computer a self-hosted GitHub Actions runner for
// repositories the person picks in Settings → Integrations → GitHub.
//
// Each repository gets its own runner install (a just-in-time runner writes
// its registration into its install directory, so two cannot share one) and
// a loop: ask GitHub for a config good for one job, start the runner with it,
// and when the job is done and the runner has removed itself, start again.
// The config reaches the runner in ACTIONS_RUNNER_INPUT_JITCONFIG, which the
// runner reads and deletes from its own environment before it starts a job;
// never argv. It is only that one runner's registration, which the runner
// writes into its install directory anyway. The runner and every job it runs
// get the child allowlist environment, so nothing this server holds (its API
// key, the database password) reaches code pushed to the repository.
package ghrunner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"

	githubapi "github.com/makifbaysal/tasktrooper/server/internal/adapter/vcs/github"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type API interface {
	RepoAccess(ctx context.Context, owner, repo string) (githubapi.RepoAccess, error)
	GenerateJITConfig(ctx context.Context, owner, repo, name string, labels []string, workFolder string) (githubapi.JITRunner, error)
	DeleteRunner(ctx context.Context, owner, repo string, id int64) error
}

type Store interface {
	GitHubRunnerConfig(ctx context.Context) (domain.GitHubRunnerConfig, error)
	SetGitHubRunnerConfig(ctx context.Context, cfg domain.GitHubRunnerConfig) error
}

type Config struct {
	// Dir holds the downloaded package and one install per repository.
	Dir   string
	API   API
	Store Store
	// Candidates lists the GitHub repositories of this install's projects.
	Candidates func(ctx context.Context) ([]string, error)
	Environ    func() []string
	Hostname   string
	HTTPClient *http.Client
	// Dist replaces the package for this platform; tests point it at a fake.
	Dist     *Distribution
	RetryMin time.Duration
	RetryMax time.Duration
	// StopGrace is how long a stopping runner gets before it is killed.
	StopGrace time.Duration
	// Launcher runs the runner processes. Left nil it is this machine, built
	// from Dir, Environ, HTTPClient, Dist and StopGrace.
	Launcher Launcher
}

type Manager struct {
	cfg       Config
	launcher  Launcher
	supported bool
	host      string

	configMu sync.Mutex

	mu      sync.Mutex
	enabled bool
	repos   []string
	slots   map[string]*slot
	closed  bool
}

func New(cfg Config) *Manager {
	if cfg.RetryMin <= 0 {
		cfg.RetryMin = 15 * time.Second
	}
	if cfg.RetryMax <= 0 {
		cfg.RetryMax = 5 * time.Minute
	}
	if cfg.Hostname == "" {
		cfg.Hostname, _ = os.Hostname()
	}
	if cfg.Launcher == nil {
		cfg.Launcher = NewLocal(LocalConfig{
			Dir: cfg.Dir, Environ: cfg.Environ, HTTPClient: cfg.HTTPClient, Dist: cfg.Dist, StopGrace: cfg.StopGrace,
		})
	}
	m := &Manager{
		cfg:       cfg,
		launcher:  cfg.Launcher,
		host:      runnerHost(cfg.Hostname),
		slots:     map[string]*slot{},
		supported: cfg.Launcher.Supported(),
	}
	if cfg.API == nil || cfg.Store == nil {
		m.supported = false
	}
	return m
}

// Start brings up the runners a previous launch left switched on.
func (m *Manager) Start(ctx context.Context) {
	if !m.supported {
		return
	}
	m.configMu.Lock()
	defer m.configMu.Unlock()
	m.launcher.Reap()
	saved, err := m.cfg.Store.GitHubRunnerConfig(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("github runner: reading its settings failed; it stays off for this launch")
		return
	}
	m.mu.Lock()
	m.enabled, m.repos = saved.Enabled, normalizeSaved(saved.Repos)
	m.mu.Unlock()
	m.reconcile()
}

func (m *Manager) Status(ctx context.Context) (domain.GitHubRunnerStatus, error) {
	out := domain.GitHubRunnerStatus{
		Supported:  m.supported,
		Labels:     m.launcher.Labels(),
		Version:    Version,
		Repos:      []domain.GitHubRunnerRepoStatus{},
		Candidates: []string{},
	}
	if !m.supported {
		return out, nil
	}
	saved, err := m.cfg.Store.GitHubRunnerConfig(ctx)
	if err != nil {
		return out, err
	}
	m.mu.Lock()
	out.Enabled = saved.Enabled
	for _, repo := range normalizeSaved(saved.Repos) {
		if s := m.slots[key(repo)]; s != nil {
			out.Repos = append(out.Repos, s.snapshot())
			continue
		}
		out.Repos = append(out.Repos, domain.GitHubRunnerRepoStatus{Repo: repo, State: domain.GitHubRunnerStopped})
	}
	m.mu.Unlock()
	seen := map[string]bool{}
	var candidates []string
	add := func(repo string) {
		if k := key(repo); !seen[k] {
			seen[k] = true
			candidates = append(candidates, repo)
		}
	}
	if m.cfg.Candidates != nil {
		list, err := m.cfg.Candidates(ctx)
		if err != nil {
			log.Warn().Err(err).Msg("github runner: listing the projects' repositories failed")
		}
		for _, repo := range list {
			if repoPattern.MatchString(repo) {
				add(repo)
			}
		}
	}
	for _, r := range out.Repos {
		add(r.Repo)
	}
	sort.Slice(candidates, func(i, j int) bool { return strings.ToLower(candidates[i]) < strings.ToLower(candidates[j]) })
	if candidates != nil {
		out.Candidates = candidates
	}
	return out, nil
}

var repoPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})/[A-Za-z0-9._-]{1,100}$`)

// Configure saves the switch and the repository list and brings the runners
// in line with them. A repository that was not on the list already is
// checked first: it must exist, be private, and the connection must be an
// admin of it, since only an admin may add a runner.
func (m *Manager) Configure(ctx context.Context, req domain.GitHubRunnerConfig) (domain.GitHubRunnerStatus, error) {
	m.configMu.Lock()
	defer m.configMu.Unlock()
	if !m.supported {
		return domain.GitHubRunnerStatus{}, &domain.GitHubRunnerRepoError{Code: domain.GitHubRunnerErrUnsupported}
	}
	repos, err := normalize(req.Repos)
	if err != nil {
		return domain.GitHubRunnerStatus{}, err
	}
	saved, err := m.cfg.Store.GitHubRunnerConfig(ctx)
	if err != nil {
		return domain.GitHubRunnerStatus{}, err
	}
	known := map[string]bool{}
	for _, r := range normalizeSaved(saved.Repos) {
		known[key(r)] = true
	}
	for _, repo := range repos {
		if known[key(repo)] {
			continue
		}
		if err := m.check(ctx, repo); err != nil {
			return domain.GitHubRunnerStatus{}, err
		}
	}
	next := domain.GitHubRunnerConfig{Enabled: req.Enabled, Repos: repos}
	if err := m.cfg.Store.SetGitHubRunnerConfig(ctx, next); err != nil {
		return domain.GitHubRunnerStatus{}, err
	}
	m.mu.Lock()
	m.enabled, m.repos = next.Enabled, repos
	m.mu.Unlock()
	m.reconcile()
	return m.Status(ctx)
}

func (m *Manager) check(ctx context.Context, repo string) error {
	owner, name, _ := strings.Cut(repo, "/")
	access, err := m.cfg.API.RepoAccess(ctx, owner, name)
	if err != nil {
		code, detail := classify(err)
		return &domain.GitHubRunnerRepoError{Repo: repo, Code: code, Detail: detail}
	}
	if !access.Private {
		return &domain.GitHubRunnerRepoError{Repo: repo, Code: domain.GitHubRunnerErrPublicRepo}
	}
	if !access.Admin {
		return &domain.GitHubRunnerRepoError{Repo: repo, Code: domain.GitHubRunnerErrNeedsAdmin}
	}
	return nil
}

// Close stops every runner, a job in progress included: the app is quitting.
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	slots := make([]*slot, 0, len(m.slots))
	for k, s := range m.slots {
		slots = append(slots, s)
		delete(m.slots, k)
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, s := range slots {
		wg.Add(1)
		go func(s *slot) {
			defer wg.Done()
			s.stop()
		}(s)
	}
	wg.Wait()
}

// reconcile starts a runner for every wanted repository without one and stops
// the rest. A stopped repository's install is deleted with it: it is a few
// hundred MB, and a repository put back on the list unpacks a fresh one.
func (m *Manager) reconcile() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	want := map[string]string{}
	if m.enabled {
		for _, r := range m.repos {
			want[key(r)] = r
		}
	}
	var stopping []*slot
	for k, s := range m.slots {
		if _, ok := want[k]; !ok {
			stopping = append(stopping, s)
			delete(m.slots, k)
		}
	}
	for k, repo := range want {
		if _, ok := m.slots[k]; ok {
			continue
		}
		s := m.newSlot(repo)
		m.slots[k] = s
		go s.run()
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, s := range stopping {
		wg.Add(1)
		go func(s *slot) {
			defer wg.Done()
			s.stop()
			m.launcher.Remove(s.repo)
		}(s)
	}
	wg.Wait()
}

func key(repo string) string { return strings.ToLower(strings.TrimSpace(repo)) }

func normalize(repos []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, r := range repos {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if !repoPattern.MatchString(r) {
			return nil, &domain.GitHubRunnerRepoError{Repo: r, Code: domain.GitHubRunnerErrInvalidRepo}
		}
		if seen[key(r)] {
			continue
		}
		seen[key(r)] = true
		out = append(out, r)
	}
	if len(out) > domain.GitHubRunnerMaxRepos {
		return nil, &domain.GitHubRunnerRepoError{Code: domain.GitHubRunnerErrTooManyRepos, Detail: fmt.Sprintf("at most %d", domain.GitHubRunnerMaxRepos)}
	}
	return out, nil
}

func normalizeSaved(repos []string) []string {
	out, err := normalize(repos)
	if err != nil {
		return []string{}
	}
	return out
}

func classify(err error) (string, string) {
	if errors.Is(err, githubapi.ErrNotConnected) || errors.Is(err, domain.ErrGitHubConnectionExpired) {
		return domain.GitHubRunnerErrNotConnected, ""
	}
	detail := githubapi.APIMessage(err)
	switch githubapi.HTTPStatus(err) {
	case http.StatusNotFound:
		return domain.GitHubRunnerErrNotFound, detail
	case http.StatusUnauthorized, http.StatusForbidden:
		return domain.GitHubRunnerErrForbidden, detail
	}
	return domain.GitHubRunnerErrGitHubRefused, detail
}

var nonHost = regexp.MustCompile(`[^a-z0-9-]+`)

func runnerHost(hostname string) string {
	h := strings.ToLower(strings.TrimSuffix(hostname, ".local"))
	h = strings.Trim(nonHost.ReplaceAllString(h, "-"), "-")
	if len(h) > 30 {
		h = strings.Trim(h[:30], "-")
	}
	if h == "" {
		h = "computer"
	}
	return h
}

type slot struct {
	m     *Manager
	repo  string
	owner string
	name  string

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	mu     sync.Mutex
	status domain.GitHubRunnerRepoStatus
}

func (m *Manager) newSlot(repo string) *slot {
	owner, name, _ := strings.Cut(repo, "/")
	ctx, cancel := context.WithCancel(context.Background())
	return &slot{
		m:      m,
		repo:   repo,
		owner:  owner,
		name:   name,
		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
		status: domain.GitHubRunnerRepoStatus{Repo: repo, State: domain.GitHubRunnerStarting},
	}
}

func (s *slot) snapshot() domain.GitHubRunnerRepoStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.status
	if out.LastFinishedAt != nil {
		t := *out.LastFinishedAt
		out.LastFinishedAt = &t
	}
	return out
}

func (s *slot) update(fn func(st *domain.GitHubRunnerRepoStatus)) {
	s.mu.Lock()
	fn(&s.status)
	s.mu.Unlock()
}

func (s *slot) fail(code, detail string) {
	s.update(func(st *domain.GitHubRunnerRepoStatus) {
		st.State, st.Job, st.ErrorCode, st.ErrorDetail = domain.GitHubRunnerFailed, "", code, detail
	})
	log.Warn().Str("repo", s.repo).Str("code", code).Str("detail", detail).Msg("github runner stopped; retrying")
}

func (s *slot) stop() {
	s.cancel()
	<-s.done
}

func (s *slot) run() {
	defer close(s.done)
	defer s.update(func(st *domain.GitHubRunnerRepoStatus) {
		st.State, st.Job = domain.GitHubRunnerStopped, ""
	})
	wait := s.m.cfg.RetryMin
	for s.ctx.Err() == nil {
		if s.once() {
			wait = s.m.cfg.RetryMin
			continue
		}
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = min(wait*2, s.m.cfg.RetryMax)
	}
}

// once is one registration: install if needed, register, run until the
// runner exits. It reports whether the runner took a job, which is the only
// ending that starts the next one at once.
func (s *slot) once() bool {
	ctx := s.ctx
	err := s.m.launcher.Prepare(ctx, s.repo, func() {
		s.update(func(st *domain.GitHubRunnerRepoStatus) { st.State = domain.GitHubRunnerInstalling })
	})
	if err != nil {
		if ctx.Err() == nil {
			// A launcher names its own reasons (the computer is away, its
			// app is out of date); anything else is a failed download.
			var coded *domain.GitHubRunnerRepoError
			if errors.As(err, &coded) {
				s.fail(coded.Code, coded.Detail)
			} else {
				s.fail(domain.GitHubRunnerErrDownload, err.Error())
			}
		}
		return false
	}
	s.update(func(st *domain.GitHubRunnerRepoStatus) { st.State = domain.GitHubRunnerStarting })

	name := fmt.Sprintf("tasktrooper-%s-%s", s.m.host, randomSuffix())
	jit, err := s.m.cfg.API.GenerateJITConfig(ctx, s.owner, s.name, name, s.m.launcher.Labels(), "_work")
	if err != nil {
		if ctx.Err() == nil {
			code, detail := classify(err)
			s.fail(code, detail)
		}
		return false
	}

	var tookJob atomic.Bool
	onEvent := func(ev Event) {
		switch ev.Kind {
		case "listening":
			s.update(func(st *domain.GitHubRunnerRepoStatus) {
				st.State, st.Job, st.ErrorCode, st.ErrorDetail = domain.GitHubRunnerIdle, "", "", ""
			})
		case "running":
			tookJob.Store(true)
			s.update(func(st *domain.GitHubRunnerRepoStatus) {
				st.State, st.Job, st.ErrorCode, st.ErrorDetail = domain.GitHubRunnerBusy, ev.Job, "", ""
			})
		case "completed":
			now := time.Now().UTC()
			s.update(func(st *domain.GitHubRunnerRepoStatus) {
				st.LastJob, st.LastResult, st.LastFinishedAt = ev.Job, ev.Result, &now
			})
		}
	}

	proc, err := s.m.launcher.Start(ctx, s.repo, jit.EncodedConfig, onEvent)
	if err != nil {
		code, detail := domain.GitHubRunnerErrStart, err.Error()
		var coded *domain.GitHubRunnerRepoError
		if errors.As(err, &coded) {
			code, detail = coded.Code, coded.Detail
		}
		s.fail(code, detail)
		s.unregister(jit.ID)
		return false
	}
	exited := make(chan ProcessExit, 1)
	go func() { exited <- proc.Wait() }()
	var exit ProcessExit
	select {
	case exit = <-exited:
	case <-ctx.Done():
		proc.Terminate()
		exit = <-exited
	}

	if !tookJob.Load() || ctx.Err() != nil {
		s.unregister(jit.ID)
	}
	if ctx.Err() != nil {
		return false
	}
	if !tookJob.Load() {
		detail := exit.Output
		if exit.Err != nil {
			detail = strings.TrimSpace(exit.Err.Error() + "\n" + detail)
		}
		s.fail(domain.GitHubRunnerErrExited, detail)
		return false
	}
	return true
}

// unregister removes a registration that never ran its job, so GitHub does
// not list an offline runner. A context of its own: it runs while stopping.
func (s *slot) unregister(id int64) {
	if id == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.m.cfg.API.DeleteRunner(ctx, s.owner, s.name, id); err != nil {
		log.Warn().Err(err).Str("repo", s.repo).Int64("runner", id).Msg("github runner: removing an unused registration failed; GitHub drops it on its own")
	}
}

func randomSuffix() string {
	var b [3]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
