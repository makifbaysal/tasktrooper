package cursor

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cli/core"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/winshim"
)

type ProbeResult = core.ProbeResult

const DefaultProbeTimeout = 60 * time.Second

var authFailureMarkers = []string{
	"not authenticated",
	"not logged in",
	"invalid api key",
	"invalid_api_key",
	"unauthorized",
	"401",
}

func Probe(ctx context.Context, binary string) (core.ProbeResult, error) {
	return core.Probe(ctx, binary, "", probeDialog)
}

// probeDialog runs `status` — the CLI's own documented auth report — and, like
// the CLIs that spend a real turn, treats a signed-out message as its own
// error. Unlike them, cursor-agent exits non-zero on auth failure, so the
// markers are only a belt; the exit code is the primary signal.
var probeDialog = core.ProbeDialog{
	DefaultBinary: "cursor-agent",
	MissingHint:   "Install the Cursor CLI on this machine, or set CURSOR_AGENT_BIN to its path",
	Markers:       authFailureMarkers,
	Timeout:       DefaultProbeTimeout,
	Run: func(ctx context.Context, bin, dir, _ string) (string, error) {
		cmd := winshim.Command(ctx, bin, "status")
		cmd.Env = core.ProbeEnv()
		cmd.Dir = os.TempDir()
		var buf bytes.Buffer
		cmd.Stdout = &buf
		cmd.Stderr = &buf
		runErr := cmd.Run()
		out := buf.String()

		if core.LooksUnauthenticated(out, authFailureMarkers) {
			return out, fmt.Errorf("%s is installed but has no usable session%s. Sign the CLI in on this machine: %w",
				bin, core.Tail(out), domain.ErrAgentCLIUnauthenticated)
		}
		if runErr != nil {
			return out, fmt.Errorf("%s did not report its status (%v)%s", bin, runErr, core.Tail(out))
		}
		return out, nil
	},
	FailError: func(bin string, runErr error, out string) error {
		return fmt.Errorf("%s did not report its status (%v)%s", bin, runErr, core.Tail(out))
	},
}

func ResolveBinary(binary string) (string, error) {
	resolved, err := core.LookPath(binary, "cursor-agent")
	if err != nil {
		return "", fmt.Errorf("executable %q not found on PATH", binary)
	}
	return resolved, nil
}