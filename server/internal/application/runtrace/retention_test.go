package runtrace_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/runtrace"
	"github.com/makifbaysal/tasktrooper/server/internal/application/runtrace/runtracetest"
)

type countingStore struct {
	*runtracetest.MemStore
	cutoffs []time.Time
	batches []int
}

func (c *countingStore) DeleteBefore(_ context.Context, cutoff time.Time, limit int) (int, error) {
	c.cutoffs = append(c.cutoffs, cutoff)
	n := c.batches[0]
	c.batches = c.batches[1:]
	return n, nil
}

func TestRetentionSweepsNinetyDaysBackInBatchesUntilDrained(t *testing.T) {
	store := &countingStore{MemStore: runtracetest.NewMemStore(), batches: []int{500, 500, 3}}
	runtrace.NewRetention(store, 0).Sweep(context.Background())
	require.Len(t, store.cutoffs, 3)
	age := time.Since(store.cutoffs[0])
	require.InDelta(t, (90 * 24 * time.Hour).Seconds(), age.Seconds(), 5)
}

func TestRetentionOnNilStoreIsANoOp(t *testing.T) {
	runtrace.NewRetention(nil, 30).Sweep(context.Background())
	var r *runtrace.Retention
	r.Sweep(context.Background())
}
