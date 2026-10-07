package agentfs

import (
	"errors"
	"io/fs"
	"syscall"
	"testing"
	"time"
)

func TestRetrySharing(t *testing.T) {
	sharing := &fs.PathError{Op: "rename", Path: "x", Err: syscall.Errno(32)}
	accessDenied := &fs.PathError{Op: "rename", Path: "x", Err: syscall.Errno(5)}
	notFound := &fs.PathError{Op: "rename", Path: "x", Err: syscall.Errno(2)}
	tests := []struct {
		name       string
		goos       string
		failures   []error
		wantErr    error
		wantSleeps int
	}{
		{name: "windows waits out a sharing violation", goos: "windows", failures: []error{sharing, sharing}, wantSleeps: 2},
		{name: "windows waits out a replace refused as access denied", goos: "windows", failures: []error{accessDenied}, wantSleeps: 1},
		{name: "windows gives up after the last delay", goos: "windows", failures: repeat(sharing, len(sharingRetryDelays)+1), wantErr: sharing, wantSleeps: len(sharingRetryDelays)},
		{name: "windows does not retry an unrelated error", goos: "windows", failures: []error{notFound}, wantErr: notFound},
		{name: "unix never retries", goos: "linux", failures: []error{sharing}, wantErr: sharing},
		{name: "success needs no retry", goos: "windows"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			op := func() error {
				calls++
				if calls <= len(tt.failures) {
					return tt.failures[calls-1]
				}
				return nil
			}
			var slept []time.Duration
			err := retrySharing(tt.goos, func(d time.Duration) { slept = append(slept, d) }, op)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if len(slept) != tt.wantSleeps {
				t.Fatalf("slept %d times (%v), want %d", len(slept), slept, tt.wantSleeps)
			}
			for i, d := range slept {
				if d != sharingRetryDelays[i] {
					t.Fatalf("delay %d = %s, want %s", i, d, sharingRetryDelays[i])
				}
			}
		})
	}
}

func repeat(err error, n int) []error {
	out := make([]error, n)
	for i := range out {
		out[i] = err
	}
	return out
}
