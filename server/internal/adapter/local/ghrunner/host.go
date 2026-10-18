package ghrunner

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Host states, as a remote Manager reads them.
const (
	HostPreparing = "preparing" // unpacking the runner for the repository
	HostReady     = "ready"     // installed, nothing running
	HostStarting  = "starting"  // a runner is up, not yet listening
	HostIdle      = "idle"      // listening for a job
	HostBusy      = "busy"      // running a job
	HostExited    = "exited"    // the last runner ended; Output says how
	HostFailed    = "failed"    // preparing or starting it failed; Error says why
)

var (
	ErrHostNotReady = errors.New("the runner is not installed for this repository yet")
	ErrHostBusy     = errors.New("a runner is already going for this repository")
)

// HostRepo is one repository's runner on a host.
type HostRepo struct {
	Repo           string     `json:"repo"`
	State          string     `json:"state"`
	Job            string     `json:"job,omitempty"`
	LastJob        string     `json:"last_job,omitempty"`
	LastResult     string     `json:"last_result,omitempty"`
	LastFinishedAt *time.Time `json:"last_finished_at,omitempty"`
	// TookJob is whether the runner that exited had started a job.
	TookJob bool `json:"took_job,omitempty"`
	// Output is the last lines the exited runner printed; Error is why a
	// preparation or start failed, or how the runner ended.
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
}

// HostStatus is everything a host runs, and what it can run.
type HostStatus struct {
	Supported bool       `json:"supported"`
	Labels    []string   `json:"labels"`
	Version   string     `json:"version"`
	Repos     []HostRepo `json:"repos"`
}

// Host is the computer's side of a remote Manager. The Manager decides what
// should run — it holds the GitHub connection and mints each runner's
// one-job config — and this installs the runner, starts it with that config
// and reports how it is doing. Nothing here calls GitHub, and nothing here
// remembers a config: the next one arrives when the last runner is done.
type Host struct {
	l *Local

	mu    sync.Mutex
	repos map[string]*hostRepo
}

type hostRepo struct {
	HostRepo
	proc    Process
	preping bool
}

func NewHost(cfg LocalConfig) *Host {
	h := &Host{l: NewLocal(cfg), repos: map[string]*hostRepo{}}
	if h.l.Supported() {
		h.l.Reap()
	}
	return h
}

func (h *Host) Status() HostStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := HostStatus{Supported: h.l.Supported(), Labels: h.l.Labels(), Version: Version, Repos: []HostRepo{}}
	for _, r := range h.repos {
		c := r.HostRepo
		if c.LastFinishedAt != nil {
			t := *c.LastFinishedAt
			c.LastFinishedAt = &t
		}
		out.Repos = append(out.Repos, c)
	}
	return out
}

func (h *Host) repoLocked(repo string) *hostRepo {
	k := key(repo)
	r := h.repos[k]
	if r == nil {
		r = &hostRepo{HostRepo: HostRepo{Repo: repo}}
		h.repos[k] = r
	}
	return r
}

// Prepare installs the runner for repo in the background. It answers at once;
// the state says when it is done.
func (h *Host) Prepare(repo string) error {
	if !h.l.Supported() {
		return errors.New("a GitHub runner cannot run on this platform")
	}
	h.mu.Lock()
	r := h.repoLocked(repo)
	if r.preping || r.proc != nil {
		h.mu.Unlock()
		return nil
	}
	r.preping = true
	r.State, r.Error = HostPreparing, ""
	h.mu.Unlock()

	go func() {
		err := h.l.Prepare(context.Background(), repo, nil)
		h.mu.Lock()
		defer h.mu.Unlock()
		r.preping = false
		if err != nil {
			r.State, r.Error = HostFailed, err.Error()
			return
		}
		r.State, r.Error = HostReady, ""
	}()
	return nil
}

// Start runs a runner for repo with encodedJIT. The repository must have been
// prepared.
func (h *Host) Start(repo, encodedJIT string) error {
	if strings.TrimSpace(encodedJIT) == "" {
		return errors.New("a runner needs its config")
	}
	h.mu.Lock()
	r := h.repoLocked(repo)
	if r.proc != nil {
		h.mu.Unlock()
		return ErrHostBusy
	}
	if r.preping || (r.State != HostReady && r.State != HostExited && r.State != HostFailed) {
		h.mu.Unlock()
		return ErrHostNotReady
	}
	h.mu.Unlock()

	onEvent := func(ev Event) {
		h.mu.Lock()
		defer h.mu.Unlock()
		switch ev.Kind {
		case "listening":
			r.State, r.Job = HostIdle, ""
		case "running":
			r.TookJob = true
			r.State, r.Job = HostBusy, ev.Job
		case "completed":
			now := time.Now().UTC()
			r.LastJob, r.LastResult, r.LastFinishedAt = ev.Job, ev.Result, &now
		}
	}
	h.mu.Lock()
	r.State, r.Job, r.TookJob, r.Output, r.Error = HostStarting, "", false, "", ""
	h.mu.Unlock()
	proc, err := h.l.Start(context.Background(), repo, encodedJIT, onEvent)
	if err != nil {
		h.mu.Lock()
		r.State, r.Error = HostFailed, err.Error()
		h.mu.Unlock()
		return err
	}
	h.mu.Lock()
	r.proc = proc
	h.mu.Unlock()

	go func() {
		exit := proc.Wait()
		h.mu.Lock()
		defer h.mu.Unlock()
		if r.proc == proc {
			r.proc = nil
		}
		r.State, r.Job, r.Output = HostExited, "", exit.Output
		if exit.Err != nil {
			r.Error = exit.Err.Error()
		}
		log.Debug().Str("repo", repo).Bool("took_job", r.TookJob).Msg("github runner host: a runner ended")
	}()
	return nil
}

// Stop ends repo's runner, a job in progress included, and with remove also
// deletes its install.
func (h *Host) Stop(repo string, remove bool) {
	h.mu.Lock()
	r := h.repos[key(repo)]
	var proc Process
	if r != nil {
		proc = r.proc
	}
	h.mu.Unlock()
	if proc != nil {
		proc.Terminate()
		proc.Wait()
	}
	if remove {
		h.l.Remove(repo)
		h.mu.Lock()
		delete(h.repos, key(repo))
		h.mu.Unlock()
	}
}

// Close stops every runner: the executor is shutting down.
func (h *Host) Close() {
	h.mu.Lock()
	var procs []Process
	for _, r := range h.repos {
		if r.proc != nil {
			procs = append(procs, r.proc)
		}
	}
	h.mu.Unlock()
	var wg sync.WaitGroup
	for _, p := range procs {
		wg.Add(1)
		go func(p Process) {
			defer wg.Done()
			p.Terminate()
			p.Wait()
		}(p)
	}
	wg.Wait()
}
