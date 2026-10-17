package opsquery

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

type outcome[T any] struct {
	value T
	err   error
}

// fanOut runs call for every index, at most limit at once, each bounded by
// timeout. A call that ignores its context is abandoned at the deadline
// rather than waited for, and a panicking one fails only its own index: one
// stuck or broken provider must never hold up or crash the whole answer.
func fanOut[T any](ctx context.Context, n, limit int, timeout time.Duration, call func(ctx context.Context, i int) (T, error)) []outcome[T] {
	out := make([]outcome[T], n)
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		sem <- struct{}{}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			v, err := within(ctx, timeout, func(ctx context.Context) (T, error) { return call(ctx, i) })
			out[i] = outcome[T]{value: v, err: err}
		}(i)
	}
	wg.Wait()
	return out
}

func within[T any](ctx context.Context, timeout time.Duration, call func(ctx context.Context) (T, error)) (T, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	done := make(chan outcome[T], 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error().Interface("panic", r).Msg("opsquery: environment read panicked")
				done <- outcome[T]{err: errPanicked}
			}
		}()
		v, err := call(ctx)
		done <- outcome[T]{value: v, err: err}
	}()
	select {
	case o := <-done:
		return o.value, o.err
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
}
