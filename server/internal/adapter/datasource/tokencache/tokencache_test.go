package tokencache_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/tokencache"
)

func TestATokenIsKeptUntilARefreshMarginBeforeItExpires(t *testing.T) {
	now := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	c := tokencache.New()
	key := tokencache.Key("key-file", "scope-a")
	c.Put(key, "tok", now.Add(10*time.Minute), now)

	tests := []struct {
		name string
		at   time.Time
		ok   bool
	}{
		{"fresh", now.Add(time.Minute), true},
		{"just inside the margin", now.Add(10*time.Minute - tokencache.RefreshMargin - time.Second), true},
		{"within the margin", now.Add(10*time.Minute - tokencache.RefreshMargin), false},
		{"expired", now.Add(11 * time.Minute), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tok, ok := c.Get(key, tt.at)
			assert.Equal(t, tt.ok, ok)
			if ok {
				assert.Equal(t, "tok", tok)
			}
		})
	}
}

func TestAnotherCredentialOrPurposeIsAnotherEntry(t *testing.T) {
	now := time.Now()
	c := tokencache.New()
	c.Put(tokencache.Key("key-file", "scope-a"), "a", now.Add(time.Hour), now)
	_, ok := c.Get(tokencache.Key("rotated-key-file", "scope-a"), now)
	assert.False(t, ok)
	_, ok = c.Get(tokencache.Key("key-file", "scope-b"), now)
	assert.False(t, ok)
}

func TestAFullCacheDropsTheLeastRecentlyUsedTokenOnly(t *testing.T) {
	now := time.Now()
	c := tokencache.NewSized(2)
	a, b, d := tokencache.Key("a", "s"), tokencache.Key("b", "s"), tokencache.Key("d", "s")
	c.Put(a, "a", now.Add(time.Hour), now)
	c.Put(b, "b", now.Add(time.Hour), now)
	_, _ = c.Get(a, now)
	c.Put(d, "d", now.Add(time.Hour), now)

	_, ok := c.Get(b, now)
	assert.False(t, ok, "the least recently used goes")
	for key, want := range map[[32]byte]string{a: "a", d: "d"} {
		tok, ok := c.Get(key, now)
		assert.True(t, ok)
		assert.Equal(t, want, tok)
	}
}

func TestTheDefaultCacheHoldsHundredsOfCredentials(t *testing.T) {
	now := time.Now()
	c := tokencache.New()
	for i := range tokencache.DefaultMaxEntries {
		c.Put(tokencache.Key(fmt.Sprint(i), "s"), "tok", now.Add(time.Hour), now)
	}
	_, ok := c.Get(tokencache.Key("0", "s"), now)
	assert.True(t, ok, "a full cache is not emptied")
}
