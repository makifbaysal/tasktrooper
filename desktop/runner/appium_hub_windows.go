//go:build windows

package main

import (
	"context"
	"errors"
	"time"
)

// hubRecordPath is empty on Windows: the hub is in a KILL_ON_JOB_CLOSE job, so
// a runner that dies any way at all takes it down, and there is never a
// leftover to take back.
func hubRecordPath() string { return "" }

func processIdentity(context.Context, int) (string, string, error) {
	return "", "", errors.New("not needed on Windows")
}

func pidAlive(int) bool { return false }

func stopLeftover(p *hubProcess, _ time.Duration) { p.markDone() }
