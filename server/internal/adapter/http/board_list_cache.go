package http

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
)

// boardListMaxAge bounds how long a 304 may rest on the board version alone.
// The version counts this process's writes only, and one database can be
// served by more than one host (the postgres stores' hostRoots exists for
// that), so past this age the body is rebuilt and hashed again: another
// host's change shows within it.
const boardListMaxAge = 10 * time.Second

// boardListCache remembers the ETag of the last /v1/tasks body together with
// the board version read before that body was built.
type boardListCache struct {
	mu      sync.Mutex
	valid   bool
	version uint64
	etag    string
	expires time.Time
	now     func() time.Time
}

func (b *boardListCache) clock() time.Time {
	if b.now != nil {
		return b.now()
	}
	return time.Now()
}

func (b *boardListCache) current(version uint64) (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.valid || b.version != version {
		return "", false
	}
	if !b.clock().Before(b.expires) {
		return "", false
	}
	return b.etag, true
}

// remember never lets an older version overwrite a newer one: two requests
// can finish out of order, and the one that read the older version may hold
// the older body. expires is when the list itself goes stale (zero: never);
// the entry lasts no longer than boardListMaxAge either way.
func (b *boardListCache) remember(version uint64, etag string, expires time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.valid && version < b.version {
		return
	}
	if limit := b.clock().Add(boardListMaxAge); expires.IsZero() || expires.After(limit) {
		expires = limit
	}
	b.valid, b.version, b.etag, b.expires = true, version, etag, expires
}

func bodyETag(body []byte) string {
	sum := sha256.Sum256(body)
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}

// etagMatches is If-None-Match's weak comparison (RFC 9110 13.1.2).
func etagMatches(header, etag string) bool {
	if header == "" {
		return false
	}
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || strings.TrimPrefix(candidate, "W/") == etag {
			return true
		}
	}
	return false
}

func notModified(c *fiber.Ctx, etag string) error {
	c.Set(fiber.HeaderETag, etag)
	c.Status(fiber.StatusNotModified)
	return nil
}
