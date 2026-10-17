package signals

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/singleflight"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const (
	goldenCacheTTL     = time.Minute
	goldenCacheSize    = 64
	queuesCacheTTL     = 30 * time.Second
	queuesCacheSize    = 128
	discoveryTTL       = 5 * time.Minute
	discoveryCacheSize = 256
)

// SetCacheScope keys every process-wide cache of the service (golden
// boards, queue tables, discovered recipe matches) by scope(ctx) as well, so a process
// serving several tenants never answers one from another's entry. Without it
// the scope is "", one for the whole process. Call it before serving.
func (s *Service) SetCacheScope(scope func(ctx context.Context) string) { s.scope = scope }

func (s *Service) cacheScope(ctx context.Context) string {
	if s.scope == nil {
		return ""
	}
	return s.scope(ctx)
}

// sourceChanged drops what was read through a source saved again or deleted:
// every scope's, since the change comes with no request to name one.
func (s *Service) sourceChanged(id uuid.UUID) {
	s.goldenCache.clear()
	s.queuesCache.clear()
	s.tablesCache.clear()
	s.discovery.drop(id)
}

// version fingerprints what an answer was read through: every source's id,
// kind and updated_at, and every recipe as it stands. A source saved again,
// or a user recipe changed, by this process or any other, misses the cache.
func (l lookup) version() string {
	h := sha256.New()
	ids := slices.SortedFunc(maps.Keys(l.sources), func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	for _, id := range ids {
		ds := l.sources[id]
		fmt.Fprintf(h, "%s\x00%s\x00%d\x00", id, ds.Kind, ds.UpdatedAt.UnixNano())
	}
	for _, id := range slices.Sorted(maps.Keys(l.recipes)) {
		raw, _ := json.Marshal(l.recipes[id])
		h.Write(raw)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)[:16])
}

// answerCache keeps answers for ttl, at most size of them, each keyed by its
// cache scope and "\x00" first. A generation moves on every clear, the whole
// cache's or one scope's, so a read that started before a binding changed
// does not store what it read.
type answerCache[V any] struct {
	ttl     time.Duration
	size    int
	mu      sync.Mutex
	gen     uint64
	scoped  map[string]uint64
	entries map[string]answerEntry[V]
}

type answerEntry[V any] struct {
	at    time.Time
	value V
}

func newAnswerCache[V any](ttl time.Duration, size int) *answerCache[V] {
	return &answerCache[V]{ttl: ttl, size: size, scoped: map[string]uint64{}, entries: map[string]answerEntry[V]{}}
}

func (c *answerCache[V]) get(key string, now time.Time) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || now.Sub(e.at) >= c.ttl {
		var zero V
		return zero, false
	}
	return e.value, true
}

// generation is the scope's: both counts only grow, so their sum moves
// whenever either clear happens.
func (c *answerCache[V]) generation(scope string) uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gen + c.scoped[scope]
}

func (c *answerCache[V]) put(scope, key string, value V, now time.Time, gen uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if gen != c.gen+c.scoped[scope] {
		return
	}
	for k, e := range c.entries {
		if now.Sub(e.at) >= c.ttl {
			delete(c.entries, k)
		}
	}
	if _, ok := c.entries[key]; !ok && len(c.entries) >= c.size {
		delete(c.entries, oldest(c.entries, func(e answerEntry[V]) time.Time { return e.at }))
	}
	c.entries[key] = answerEntry[V]{at: now, value: value}
}

func (c *answerCache[V]) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gen++
	clear(c.entries)
}

func (c *answerCache[V]) clearScope(scope string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.scoped[scope]++
	prefix := scope + "\x00"
	for k := range c.entries {
		if strings.HasPrefix(k, prefix) {
			delete(c.entries, k)
		}
	}
}

// shared reads key once for every caller asking for it at gen at the same
// time, and keeps the answer. The read outlives the request that started it
// (the others wait on it too), so it keeps that request's values but not its
// cancel; read's own budget bounds it.
func shared[V any](ctx context.Context, g *singleflight.Group, c *answerCache[V], scope, key string, gen uint64, now func() time.Time, read func(context.Context) V) (V, error) {
	detached := context.WithoutCancel(ctx)
	ch := g.DoChan(fmt.Sprintf("%s\x00%d", key, gen), func() (v any, err error) {
		defer func() {
			if r := recover(); r != nil {
				log.Error().Interface("panic", r).Msg("signals: a table read panicked")
				err = errPanicked
			}
		}()
		out := read(detached)
		c.put(scope, key, out, now(), gen)
		return out, nil
	})
	var zero V
	select {
	case r := <-ch:
		if r.Err != nil {
			return zero, r.Err
		}
		return r.Val.(V), nil
	case <-ctx.Done():
		return zero, ctx.Err()
	}
}

// discoveryCache keeps each source's recipe matches for discoveryTTL, keyed
// by its updated_at so an edited source is discovered again.
type discoveryCache struct {
	mu      sync.Mutex
	entries map[discoveryKey]discoveryEntry
}

type discoveryKey struct {
	scope string
	id    uuid.UUID
}

type discoveryEntry struct {
	version time.Time
	at      time.Time
	matches []domain.RecipeMatch
}

func newDiscoveryCache() *discoveryCache {
	return &discoveryCache{entries: map[discoveryKey]discoveryEntry{}}
}

func (c *discoveryCache) get(key discoveryKey, version, now time.Time) ([]domain.RecipeMatch, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || !e.version.Equal(version) || now.Sub(e.at) > discoveryTTL {
		return nil, false
	}
	return e.matches, true
}

func (c *discoveryCache) put(key discoveryKey, version, now time.Time, matches []domain.RecipeMatch) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, e := range c.entries {
		if now.Sub(e.at) > discoveryTTL {
			delete(c.entries, k)
		}
	}
	if _, ok := c.entries[key]; !ok && len(c.entries) >= discoveryCacheSize {
		delete(c.entries, oldest(c.entries, func(e discoveryEntry) time.Time { return e.at }))
	}
	c.entries[key] = discoveryEntry{version: version, at: now, matches: slices.Clone(matches)}
}

func (c *discoveryCache) drop(id uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.entries {
		if k.id == id {
			delete(c.entries, k)
		}
	}
}

func oldest[K comparable, E any](entries map[K]E, at func(E) time.Time) K {
	var key K
	var first time.Time
	found := false
	for k, e := range entries {
		if t := at(e); !found || t.Before(first) {
			key, first, found = k, t, true
		}
	}
	return key
}

// dropAnswers forgets the golden boards and tables of ctx's cache scope: a
// binding they were read through changed, and bindings are the scope's own.
func (s *Service) dropAnswers(ctx context.Context) {
	scope := s.cacheScope(ctx)
	s.goldenCache.clearScope(scope)
	s.queuesCache.clearScope(scope)
	s.tablesCache.clearScope(scope)
}
