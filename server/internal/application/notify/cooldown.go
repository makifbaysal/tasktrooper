package notify

import (
	"sync"
	"time"
)

// DefaultCooldown is how long a source stays quiet about one key once it has
// notified about it.
const DefaultCooldown = 30 * time.Minute

const cooldownSweepAbove = 256

// Cooldown is the re-notify rule the sources share: at most one notification
// per key per window. What a key is (an alert's identity, a release) is the
// source's choice.
type Cooldown struct {
	window time.Duration

	mu   sync.Mutex
	last map[string]time.Time
}

func NewCooldown(window time.Duration) *Cooldown {
	if window <= 0 {
		window = DefaultCooldown
	}
	return &Cooldown{window: window, last: map[string]time.Time{}}
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
