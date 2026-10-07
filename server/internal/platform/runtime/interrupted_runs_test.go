package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type failingActivityStore struct {
	port.ActivityStore
	calls  int
	failed int
	err    error
}

func (s *failingActivityStore) FailInterruptedRuns(context.Context) (int, error) {
	s.calls++
	return s.failed, s.err
}

func TestInterruptedRunsAreFailedOnlyOnADatabaseThisProcessStarted(t *testing.T) {
	tests := []struct {
		name     string
		store    port.ActivityStore
		embedded bool
		fails    bool
	}{
		{"embedded database", &failingActivityStore{}, true, true},
		{"DATABASE_URL: another host may be running turns", &failingActivityStore{}, false, false},
		{"store that cannot fail runs", struct{ port.ActivityStore }{}, true, false},
		{"no activity store", nil, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := interruptedRunFailing(tt.store, Options{EmbeddedPostgres: tt.embedded})
			assert.Equal(t, tt.fails, got != nil)
		})
	}
}

func TestFailInterruptedRunsSurvivesAStoreError(t *testing.T) {
	tests := []struct {
		name  string
		store *failingActivityStore
	}{
		{"runs settled", &failingActivityStore{failed: 2}},
		{"nothing to settle", &failingActivityStore{}},
		{"store error", &failingActivityStore{err: errors.New("connection refused")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failInterruptedRuns(context.Background(), tt.store)
			assert.Equal(t, 1, tt.store.calls)
		})
	}
}
