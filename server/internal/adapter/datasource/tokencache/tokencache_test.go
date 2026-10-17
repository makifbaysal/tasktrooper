package tokencache_test

import (
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
