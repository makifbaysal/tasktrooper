package runtrace

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const RetentionInterval = 6 * time.Hour

const retentionBatch = 500

// Retention deletes traces whose run started more than the retention period
// ago. The events go with their run through the foreign key.
type Retention struct {
	store port.RunTraceStore
	days  int
	now   func() time.Time
}

func NewRetention(store port.RunTraceStore, days int) *Retention {
	if days <= 0 {
		days = domain.TraceRetentionDays
	}
	return &Retention{store: store, days: days, now: time.Now}
}

func (r *Retention) Sweep(ctx context.Context) {
	if r == nil || r.store == nil {
		return
	}
	cutoff := r.now().Add(-time.Duration(r.days) * 24 * time.Hour)
	total := 0
	for ctx.Err() == nil {
		n, err := r.store.DeleteBefore(ctx, cutoff, retentionBatch)
		if err != nil {
			log.Warn().Err(err).Msg("run trace retention: sweep failed")
			return
		}
		total += n
		if n < retentionBatch {
			break
		}
	}
	if total > 0 {
		log.Info().Int("runs", total).Int("days", r.days).Msg("run trace retention: old traces removed")
	}
}

func (r *Retention) Start(ctx context.Context, interval time.Duration) {
	if r == nil || r.store == nil {
		return
	}
	if interval <= 0 {
		interval = RetentionInterval
	}
	go func() {
		r.Sweep(ctx)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				r.Sweep(ctx)
			}
		}
	}()
}
