package mobile

import (
	"context"
	"errors"
	"sync"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
)

// device is what a mobile_* tool actually needs: issue a command, launch an
// app, let go. Both a single Session and the Pool satisfy it, which is what
// keeps "which phone" out of the tools entirely.
type device interface {
	Configured() bool
	run(ctx context.Context, method, path string, body, out interface{}) error
	launch(ctx context.Context, appID, appURL string) error
	Release(ctx context.Context)
}

// Pool is the several-phones version of a lease, and the reason the mobile
// tools take no device argument.
//
// A run is HANDED a phone rather than choosing one. That is not a convenience:
// an agent that picked a device would be picking from a list whose contention
// it cannot see, so it would either serialise every run onto the first phone or
// take one another run is already driving. So the first mobile_* call of a run
// walks the registered devices and takes the first one that is free; every
// later call in that run goes to the same phone, because launch-then-tap-then-
// screenshot only means anything against one device.
//
// Only when EVERY suitable phone is taken does this report busy, which is what
// the tools turn into a domain.ResourceBlock and the board turns into a parked
// task. With one phone registered that is exactly the old behaviour.
type Pool struct {
	mu sync.Mutex
	// sessions is in registration order, so allocation is stable and the first
	// phone stays the one a single-device installation always used.
	sessions []*Session
	// bound is run → phone for the length of a run. Keyed by the agent session
	// id, which is what identifies "one run" everywhere else in this process.
	bound map[uuid.UUID]*Session
	hub   Hub
}

func NewPool() *Pool { return &Pool{bound: map[uuid.UUID]*Session{}} }

// UseHub hands every phone, present and later registered, the keeper of an
// on-demand hub.
func (p *Pool) UseHub(h Hub) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.hub = h
	sessions := append([]*Session(nil), p.sessions...)
	p.mu.Unlock()
	for _, s := range sessions {
		s.setHub(h)
	}
}

// InUse reports whether any phone is leased or mid-command: what keeps an
// on-demand hub running past its idle time. Never blocks on a session busy on
// the network — that one is in use by definition.
func (p *Pool) InUse() bool {
	if p == nil {
		return false
	}
	for _, s := range p.snapshot() {
		if !s.idleAndFree() {
			return true
		}
	}
	return false
}

// Reconfigure points the pool at the currently registered devices.
//
// Sessions are matched by UDID and REUSED rather than rebuilt, because a
// registration changing is routine — a phone rebooted onto a new tailnet port,
// a second phone was added — and rebuilding would drop a lease a run is in the
// middle of using for a device that did not change at all.
func (p *Pool) Reconfigure(cfgs []Config) {
	if p == nil {
		return
	}
	p.mu.Lock()
	existing := make(map[string]*Session, len(p.sessions))
	for _, s := range p.sessions {
		existing[s.udid()] = s
	}
	next := make([]*Session, 0, len(cfgs))
	keep := make(map[*Session]bool, len(cfgs))
	for _, cfg := range cfgs {
		s, ok := existing[cfg.DeviceUDID]
		if !ok {
			s = NewSession(cfg)
			s.hub = p.hub
		} else {
			s.Reconfigure(cfg)
		}
		next = append(next, s)
		keep[s] = true
	}
	var dropped []*Session
	for _, s := range p.sessions {
		if !keep[s] {
			dropped = append(dropped, s)
		}
	}
	p.sessions = next
	for run, s := range p.bound {
		if !keep[s] {
			delete(p.bound, run)
		}
	}
	p.mu.Unlock()

	// Outside the lock: releasing talks to Appium over the network. A phone the
	// installation no longer claims must not stay held, or it stays away from
	// whoever does claim it until the hub's own timeout.
	for _, s := range dropped {
		s.Release(context.Background())
	}
}

// Configured reports whether any phone is attached at all. Nil-safe: a build
// with no device has a nil pool everywhere.
func (p *Pool) Configured() bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, s := range p.sessions {
		if s.Configured() {
			return true
		}
	}
	return false
}

// Probe answers the device sweeper's question — is there a phone a parked task
// could run on — without taking one. True as soon as ONE is free: the task was
// parked because all of them were busy, and it only needs one back.
func (p *Pool) Probe(ctx context.Context) bool {
	if p == nil {
		return false
	}
	for _, s := range p.snapshot() {
		if s.Probe(ctx) {
			return true
		}
	}
	return false
}

func (p *Pool) snapshot() []*Session {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]*Session(nil), p.sessions...)
}

// runOf identifies the run a tool call belongs to.
//
// uuid.Nil is a legitimate answer — a tool called outside an agent session — and
// deliberately behaves as one shared owner rather than as "everybody gets their
// own phone": unattributed callers cannot be told apart, so treating them as
// separate runs would hand two of them different phones and neither one back.
func runOf(ctx context.Context) uuid.UUID {
	if id := registry.SessionIDFromContext(ctx); id != uuid.Nil {
		return id
	}
	return registry.TaskIDFromContext(ctx)
}

// acquire returns the phone this run holds, taking a free one if it holds none.
func (p *Pool) acquire(ctx context.Context) (*Session, error) {
	run := runOf(ctx)
	p.mu.Lock()
	if s, ok := p.bound[run]; ok {
		p.mu.Unlock()
		return s, nil
	}
	candidates := append([]*Session(nil), p.sessions...)
	p.mu.Unlock()

	if len(candidates) == 0 {
		return nil, errNotConfigured
	}
	var lastErr error
	for _, s := range candidates {
		err := s.take(ctx, run)
		switch {
		case err == nil:
			p.mu.Lock()
			p.bound[run] = s
			p.mu.Unlock()
			log.Info().Str("udid", s.udid()).Str("run", run.String()).Msg("mobile: run took a device")
			return s, nil
		case errors.Is(err, errDeviceBusy):
			continue
		default:
			// A hub that will not answer for this phone is not a reason to fail
			// the run while another phone might work — but it is the error to
			// report if none of them do, because "all busy" would send the task
			// to wait for a device that is broken rather than taken.
			lastErr = err
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errDeviceBusy
}

func (p *Pool) run(ctx context.Context, method, path string, body, out interface{}) error {
	s, err := p.acquire(ctx)
	if err != nil {
		return err
	}
	return s.run(ctx, method, path, body, out)
}

func (p *Pool) launch(ctx context.Context, appID, appURL string) error {
	s, err := p.acquire(ctx)
	if err != nil {
		return err
	}
	return s.launch(ctx, appID, appURL)
}

// Release hands this run's phone back. Safe to call when the run holds none —
// which is the common case for a run that never touched a device.
func (p *Pool) Release(ctx context.Context) {
	if p == nil {
		return
	}
	run := runOf(ctx)
	p.mu.Lock()
	s, ok := p.bound[run]
	delete(p.bound, run)
	p.mu.Unlock()
	if ok {
		s.Release(ctx)
	}
}

// Close is the shutdown hook: every phone this pod holds goes back, so a
// restart does not leave leases the hub only reaps on its own timeout.
func (p *Pool) Close() {
	if p == nil {
		return
	}
	for _, s := range p.snapshot() {
		s.Close()
	}
}

// Devices reports the UDIDs currently in the pool, in allocation order. Used by
// the settings status, which asks each phone about itself.
func (p *Pool) Devices() []string {
	if p == nil {
		return nil
	}
	out := []string{}
	for _, s := range p.snapshot() {
		out = append(out, s.udid())
	}
	return out
}

// Busy reports whether this pod holds a lease on the named phone. It is how the
// settings page tells "in use" from "unreachable" for one device rather than
// for the installation.
func (p *Pool) Busy(udid string) bool {
	if p == nil {
		return false
	}
	for _, s := range p.snapshot() {
		if s.udid() == udid {
			return s.held()
		}
	}
	return false
}

// ProbeDevice answers whether one named phone is free, for the same reason
// Probe exists: without taking it.
func (p *Pool) ProbeDevice(ctx context.Context, udid string) bool {
	if p == nil {
		return false
	}
	for _, s := range p.snapshot() {
		if s.udid() == udid {
			return s.Probe(ctx)
		}
	}
	return false
}
