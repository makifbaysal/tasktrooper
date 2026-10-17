package signals

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

const (
	defaultSeriesBudget = 30 * time.Second
	defaultGoldenBudget = 25 * time.Second
)

// errSourceTimedOut answers a read skipped because its data source already
// ran out of time once in the same request.
var errSourceTimedOut = fmt.Errorf("the data source did not answer an earlier query in time: %w", context.DeadlineExceeded)

// WithSeriesBudget bounds a whole series read; a signal still unread then
// answers a timeout.
func WithSeriesBudget(d time.Duration) Option {
	return func(s *Service) {
		if d > 0 {
			s.seriesBudget = d
		}
	}
}

// WithGoldenBudget bounds a whole golden read; a signal still unread then
// answers nulls.
func WithGoldenBudget(d time.Duration) Option {
	return func(s *Service) {
		if d > 0 {
			s.goldenBudget = d
		}
	}
}

// outcome: late marks a call cut off, or never started, because ctx (the
// request's budget) ran out.
type outcome[T any] struct {
	value T
	err   error
	late  bool
}

// fanOut runs call for every index, at most limit at once, each bounded by
// timeout and all of them by ctx: once ctx is done a call not yet started is
// not started, and one in flight is cut off. A call that ignores its context
// is abandoned at the deadline rather than waited for, and a panicking one
// fails only its own index: one slow or broken source must never hold up or
// crash the whole answer. Once a call of a source (source(i)) runs out of
// time, that source's later calls are not made and answer errSourceTimedOut.
func fanOut[T any](ctx context.Context, n, limit int, timeout time.Duration, source func(i int) uuid.UUID, call func(ctx context.Context, i int) (T, error)) []outcome[T] {
	out := make([]outcome[T], n)
	sem := make(chan struct{}, max(limit, 1))
	var mu sync.Mutex
	slow := map[uuid.UUID]bool{}
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			for k := i; k < n; k++ {
				out[k] = outcome[T]{err: ctx.Err(), late: true}
			}
			return out
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			id := source(i)
			mu.Lock()
			skip := slow[id]
			mu.Unlock()
			switch {
			case skip:
				out[i] = outcome[T]{err: errSourceTimedOut}
				return
			case ctx.Err() != nil:
				out[i] = outcome[T]{err: ctx.Err(), late: true}
				return
			}
			v, err := within(ctx, timeout, func(ctx context.Context) (T, error) { return call(ctx, i) })
			if errors.Is(err, context.DeadlineExceeded) {
				mu.Lock()
				slow[id] = true
				mu.Unlock()
			}
			out[i] = outcome[T]{value: v, err: err, late: err != nil && ctx.Err() != nil}
		}(i)
	}
	wg.Wait()
	return out
}

// within runs call bounded by timeout. A call that fails once its deadline
// passed failed for that reason, whatever it says: an adapter reports its own
// cut-off request as an upstream error.
func within[T any](ctx context.Context, timeout time.Duration, call func(ctx context.Context) (T, error)) (T, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	done := make(chan outcome[T], 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error().Interface("panic", r).Msg("signals: a series read panicked")
				done <- outcome[T]{err: errPanicked}
			}
		}()
		v, err := call(ctx)
		done <- outcome[T]{value: v, err: err}
	}()
	select {
	case o := <-done:
		if o.err != nil && ctx.Err() != nil {
			return o.value, ctx.Err()
		}
		return o.value, o.err
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
}
