// Package tokencache keeps short-lived bearer tokens (a Google OAuth access
// token, an App Store Connect JWT) for the data source kinds that mint them
// from a stored credential, so a chart refresh does not mint one per query.
// Entries are keyed by a hash of the credential and what the token is for,
// so a rotated key mints anew and no key material is held here.
package tokencache

import (
	"crypto/sha256"
	"sync"
	"time"
)

// RefreshMargin renews a token this long before it expires, so one handed to
// a request does not lapse in flight.
const RefreshMargin = time.Minute

const maxEntries = 64

type entry struct {
	token   string
	expires time.Time
}

type Cache struct {
	mu      sync.Mutex
	entries map[[sha256.Size]byte]entry
}

func New() *Cache { return &Cache{entries: map[[sha256.Size]byte]entry{}} }

// Key derives the cache key of a credential's token for one purpose (a
// scope, an audience).
func Key(credential, purpose string) [sha256.Size]byte {
	return sha256.Sum256([]byte(purpose + "\x00" + credential))
}

// Get answers a cached token still valid past now plus RefreshMargin.
func (c *Cache) Get(key [sha256.Size]byte, now time.Time) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || !now.Before(e.expires.Add(-RefreshMargin)) {
		return "", false
	}
	return e.token, true
}

// Put keeps a token until it expires. Expired entries go first; a full cache
// is emptied rather than grown, which costs a mint, not correctness.
func (c *Cache) Put(key [sha256.Size]byte, token string, expires, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, e := range c.entries {
		if !now.Before(e.expires) {
			delete(c.entries, k)
		}
	}
	if len(c.entries) >= maxEntries {
		clear(c.entries)
	}
	c.entries[key] = entry{token: token, expires: expires}
}
