package notify

import (
	"strings"
	"sync"
	"time"
)

// DefaultCooldown is how long a source stays quiet about one key once it has
// notified about it.
const DefaultCooldown = 30 * time.Minute

// DefaultFlapLimit and DefaultFlapSettle: a key that changes state more than
// four times inside the window is flapping; it is told nothing more until it
// has stayed in one state for fifteen minutes, and then told where it ended.
const (
	DefaultFlapLimit  = 4
	DefaultFlapSettle = 15 * time.Minute
)

const cooldownSweepAbove = 256

// Cooldown is the re-notify rule the sources share: at most one notification
// per key per window. What a key is (an alert's identity, a release) is the
// source's choice.
type Cooldown struct {
	window time.Duration

	mu    sync.Mutex
	last  map[string]time.Time
	kinds map[string]delivered

	flapLimit int
	settle    time.Duration
	flaps     map[string]*flap
}

type delivered struct {
	kind string
	at   time.Time
}

// flap is one key's recent transitions told, and while it is held, the
// latest one that was not.
type flap struct {
	told    []time.Time
	holding bool
	held    Held
	heldAt  time.Time
}

// Held is a transition held back while its key flapped: the latest kind, and
// whatever the source attached to it to deliver it later.
type Held struct {
	Key     string
	Kind    string
	Payload any
}

func NewCooldown(window time.Duration) *Cooldown {
	if window <= 0 {
		window = DefaultCooldown
	}
	return &Cooldown{window: window, last: map[string]time.Time{}, kinds: map[string]delivered{}, flaps: map[string]*flap{}}
}

// WithFlapLimit makes AllowTransition tell at most limit transitions of a key
// per window. Past that it holds every transition of the key back until the
// key has stayed in one state for settle; Settled then hands back the last
// one, to be told. Call it before use.
func (c *Cooldown) WithFlapLimit(limit int, settle time.Duration) *Cooldown {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.flapLimit = max(limit, 0)
	c.settle = max(settle, 0)
	return c
}

// Allow reports whether key may notify at now, and if so starts its window.
func (c *Cooldown) Allow(key string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if last, ok := c.last[key]; ok && now.Sub(last) < c.window {
		return false
	}
	if len(c.last) > cooldownSweepAbove {
		for k, last := range c.last {
			if now.Sub(last) >= c.window {
				delete(c.last, k)
			}
		}
	}
	c.last[key] = now
	return true
}

// AllowTransition is Allow for a source whose notifications alternate
// between opposites (firing and its recovery): only a repeat of the kind last
// delivered for key is held back inside the window. A firing after a
// delivered recovery is always told — staying quiet then would leave the
// last word "recovered" while the thing is down again — unless the key is
// flapping (WithFlapLimit), when the last word waits for Settled instead.
func (c *Cooldown) AllowTransition(key, kind string, now time.Time) bool {
	return c.AllowTransitionHolding(key, kind, now, nil)
}

// AllowTransitionHolding is AllowTransition that keeps payload with a
// transition held back for flapping, for Settled to hand back.
func (c *Cooldown) AllowTransitionHolding(key, kind string, now time.Time, payload any) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	f := c.flap(key)
	if f != nil && f.holding {
		f.held, f.heldAt = Held{Key: key, Kind: kind, Payload: payload}, now
		return false
	}
	if prev, ok := c.kinds[key]; ok && prev.kind == kind && now.Sub(prev.at) < c.window {
		return false
	}
	if f != nil {
		f.told = c.recent(f.told, now)
		if len(f.told) >= c.flapLimit {
			f.holding, f.held, f.heldAt = true, Held{Key: key, Kind: kind, Payload: payload}, now
			return false
		}
		f.told = append(f.told, now)
	}
	c.tell(key, kind, now)
	return true
}

// Settled hands back, once each, the transitions held for the keys starting
// with prefix that have stayed in one state for the settle time, and ends
// their hold. A key that settled where it was last told hands back nothing.
func (c *Cooldown) Settled(prefix string, now time.Time) []Held {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []Held
	for key, f := range c.flaps {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		if !f.holding {
			if f.told = c.recent(f.told, now); len(f.told) == 0 {
				delete(c.flaps, key)
			}
			continue
		}
		if now.Sub(f.heldAt) < c.settle {
			continue
		}
		f.holding = false
		f.told = c.recent(f.told, now)
		if prev, ok := c.kinds[key]; ok && prev.kind == f.held.Kind {
			continue
		}
		f.told = append(f.told, now)
		c.tell(key, f.held.Kind, now)
		out = append(out, f.held)
	}
	return out
}

func (c *Cooldown) flap(key string) *flap {
	if c.flapLimit <= 0 {
		return nil
	}
	f, ok := c.flaps[key]
	if !ok {
		f = &flap{}
		c.flaps[key] = f
	}
	return f
}

func (c *Cooldown) recent(told []time.Time, now time.Time) []time.Time {
	kept := told[:0]
	for _, at := range told {
		if now.Sub(at) < c.window {
			kept = append(kept, at)
		}
	}
	return kept
}

func (c *Cooldown) tell(key, kind string, now time.Time) {
	if len(c.kinds) > cooldownSweepAbove {
		for k, prev := range c.kinds {
			if now.Sub(prev.at) >= c.window {
				delete(c.kinds, k)
			}
		}
	}
	c.kinds[key] = delivered{kind: kind, at: now}
}
