package domain

import (
	"os/exec"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExitSignalReadsAKernelReportedSignal(t *testing.T) {
	err := exec.Command("sh", "-c", "kill -TERM $$").Run()
	require.Error(t, err)

	sig, ok := ExitSignal(err)
	assert.True(t, ok)
	assert.Equal(t, syscall.SIGTERM, sig)
}

// The Node CLIs trap SIGTERM and exit 143 themselves, so the kernel never
// reports them as signaled.
func TestExitSignalReadsATrappedSignalFromTheExitCode(t *testing.T) {
	err := exec.Command("sh", "-c", "exit 143").Run()
	require.Error(t, err)

	sig, ok := ExitSignal(err)
	assert.True(t, ok)
	assert.Equal(t, syscall.SIGTERM, sig)
}

func TestExitSignalIgnoresAnOrdinaryFailure(t *testing.T) {
	err := exec.Command("sh", "-c", "exit 1").Run()
	require.Error(t, err)

	_, ok := ExitSignal(err)
	assert.False(t, ok)
}

func TestSignalFromExitCode(t *testing.T) {
	tests := []struct {
		name   string
		goos   string
		code   int
		want   syscall.Signal
		wantOK bool
	}{
		{name: "143 on linux is a trapped SIGTERM", goos: "linux", code: 143, want: syscall.Signal(15), wantOK: true},
		{name: "130 on darwin is a trapped SIGINT", goos: "darwin", code: 130, want: syscall.Signal(2), wantOK: true},
		{name: "128 is not a signal", goos: "linux", code: 128},
		{name: "beyond the signal range is not a signal", goos: "linux", code: 160},
		{name: "windows has no signal exit convention", goos: "windows", code: 143},
		{name: "windows 129 is just an exit code", goos: "windows", code: 129},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sig, ok := signalFromExitCode(tt.goos, tt.code)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, sig)
		})
	}
}
