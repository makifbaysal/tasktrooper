// Package tokencache keeps short-lived bearer tokens (a Google OAuth access
// token, an App Store Connect JWT) for the data source kinds that mint them
// from a stored credential, so a chart refresh does not mint one per query.
// Entries are keyed by a hash of the credential and what the token is for,
// so a rotated key mints anew and no key material is held here.
package tokencache

import (
	"container/list"
	"crypto/sha256"
	"sync"
	"time"
)

// RefreshMargin renews a token this long before it expires, so one handed to
// a request does not lapse in flight.
const RefreshMargin = time.Minute

// DefaultMaxEntries bounds the cache; past it the least recently used token
// goes, which costs one mint for it, not correctness.
const DefaultMaxEntries = 512

type entry struct {
	key     [sha256.Size]byte
	token   string
	expires time.Time
}

type Cache struct {
	mu    sync.Mutex
	limit int
	order *list.List
	items map[[sha256.Size]byte]*list.Element
}

func New() *Cache { return NewSized(DefaultMaxEntries) }

func NewSized(limit int) *Cache {
	return &Cache{limit: max(limit, 1), order: list.New(), items: map[[sha256.Size]byte]*list.Element{}}
}

// Key derives the cache key of a credential's token for one purpose (a
// scope, an audience).
func Key(credential, purpose string) [sha256.Size]byte {
	return sha256.Sum256([]byte(purpose + "\x00" + credential))
}

// Get answers a cached token still valid past now plus RefreshMargin.
func (c *Cache) Get(key [sha256.Size]byte, now time.Time) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return "", false
	}
	e := el.Value.(*entry)
	if !now.Before(e.expires.Add(-RefreshMargin)) {
		return "", false
	}
	c.order.MoveToFront(el)
	return e.token, true
}

// Put keeps a token until it expires. Expired entries go first, then the
// least recently used while the cache is full.
func (c *Cache) Put(key [sha256.Size]byte, token string, expires, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for el := c.order.Back(); el != nil; {
		prev := el.Prev()
		if e := el.Value.(*entry); !now.Before(e.expires) {
			c.order.Remove(el)
			delete(c.items, e.key)
		}
		el = prev
	}
	if el, ok := c.items[key]; ok {
		e := el.Value.(*entry)
		e.token, e.expires = token, expires
		c.order.MoveToFront(el)
		return
	}
	c.items[key] = c.order.PushFront(&entry{key: key, token: token, expires: expires})
	for c.order.Len() > c.limit {
		oldest := c.order.Back()
		c.order.Remove(oldest)
		delete(c.items, oldest.Value.(*entry).key)
	}
}
