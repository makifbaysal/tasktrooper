package antigravity

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cli/core"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/winshim"
)

type ProbeResult = core.ProbeResult

const DefaultProbeTimeout = 3 * time.Minute

const probePrompt = "Reply with exactly: ok"

var authFailureMarkers = []string{
	"invalid api key",
	"invalid_api_key",
	"authentication_error",
	"authentication failed",
	"not logged in",
	"no credentials",
	"credentials not found",
	"unauthorized",
	"401",
}

func Probe(ctx context.Context, binary string) (core.ProbeResult, error) {
	return core.Probe(ctx, binary, "", probeDialog)
}

// probeDialog runs a one-turn print-mode session and treats a non-zero exit as
// the session report, reading the auth markers off its output when it fails.
var probeDialog = core.ProbeDialog{
	DefaultBinary: "agy",
	MissingHint:   "Install the Antigravity CLI on this machine, or set ANTIGRAVITY_BIN to its path",
	Markers:       authFailureMarkers,
	Timeout:       DefaultProbeTimeout,
	Run: func(ctx context.Context, bin, dir, _ string) (string, error) {
		cmd := winshim.Command(ctx, bin, "-p", probePrompt, "--output-format", "json", "--dangerously-skip-permissions")
		cmd.Env = core.ProbeEnv()
		cmd.Dir = dir
		var buf bytes.Buffer
		cmd.Stdout = &buf
		cmd.Stderr = &buf

		runErr := cmd.Run()
		out := buf.String()
		if runErr == nil {
			return out, nil
		}
		if core.LooksUnauthenticated(out, authFailureMarkers) {
			return out, fmt.Errorf("%s is installed but has no usable session%s. Sign the CLI in on this machine: %w",
				bin, core.Tail(out), domain.ErrAgentCLIUnauthenticated)
		}
		return out, fmt.Errorf("%s did not complete a test session (%v)%s", bin, runErr, core.Tail(out))
	},
	FailError: func(bin string, runErr error, out string) error {
		return fmt.Errorf("%s did not complete a test session (%v)%s", bin, runErr, core.Tail(out))
	},
}

func ResolveBinary(binary string) (string, error) {
	resolved, err := core.LookPath(binary, "agy")
	if err != nil {
		return "", fmt.Errorf("executable %q not found on PATH", binary)
	}
	return resolved, nil
}