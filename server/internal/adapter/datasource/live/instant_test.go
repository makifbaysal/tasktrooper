package live_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/live"
)

func TestInstantAnswersOnlyAWindowEndingAboutNow(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 30, 0, time.UTC)
	tests := []struct {
		name   string
		to     time.Time
		want   time.Time
		answer bool
	}{
		{"now", now, now, true},
		{"rounded down to the minute", now.Truncate(time.Minute), now.Truncate(time.Minute), true},
		{"in the future", now.Add(time.Hour), now, true},
		{"unset", time.Time{}, now, true},
		{"the window before", now.Add(-time.Hour), time.Time{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			at, ok := live.Instant(tt.to, now)
			assert.Equal(t, tt.answer, ok)
			assert.Equal(t, tt.want, at)
		})
	}
}
