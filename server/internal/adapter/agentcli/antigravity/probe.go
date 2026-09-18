package antigravity

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/childenv"
)

// ProbeResult is what a successful probe learned about this host's CLI.
type ProbeResult struct {
	// BinaryPath is the absolute path ResolveBinary found — the same one the
	// executor runs.
	BinaryPath string
	// Version is whatever `agy --version` printed, trimmed to one line.
	Version string
}

// probeVersionTimeout bounds `agy --version`.
const probeVersionTimeout = 60 * time.Second

// DefaultProbeTimeout bounds the whole probe, including the one-turn session
// that proves the CLI is signed in.
const DefaultProbeTimeout = 3 * time.Minute

// probePrompt is what the auth session is asked. It exists to make the CLI
// authenticate and answer, so it is the cheapest possible turn.
const probePrompt = "Reply with exactly: ok"

// authFailureMarkers are the substrings that mean "this binary has no usable
// session", matched case-insensitively against the CLI's own output.
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

// Probe reports whether this host can actually run an Antigravity session.
func Probe(ctx context.Context, binary string) (ProbeResult, error) {
	resolved, err := ResolveBinary(binary)
	if err != nil {
		return ProbeResult{}, fmt.Errorf("%s. Install the Antigravity CLI on this machine, or set ANTIGRAVITY_BIN to its path: %w",
			err.Error(), domain.ErrAgentCLIBinaryMissing)
	}

	version, out, err := probeVersion(ctx, resolved)
	if err != nil {
		return ProbeResult{}, fmt.Errorf("%s was found but would not run (%v)%s: %w",
			resolved, err, tail(out), domain.ErrAgentCLIBinaryMissing)
	}

	if err := probeAuth(ctx, resolved); err != nil {
		return ProbeResult{}, err
	}
	return ProbeResult{BinaryPath: resolved, Version: version}, nil
}

func probeVersion(ctx context.Context, bin string) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, probeVersionTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "--version")
	cmd.Env = childEnv(ctx)
	cmd.Dir = os.TempDir()
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	out := buf.String()
	if err != nil {
		return "", out, err
	}
	line := strings.TrimSpace(out)
	if idx := strings.IndexByte(line, '\n'); idx >= 0 {
		line = strings.TrimSpace(line[:idx])
	}
	return line, out, nil
}

func probeAuth(ctx context.Context, bin string) error {
	ctx, cancel := context.WithTimeout(ctx, DefaultProbeTimeout)
	defer cancel()

	dir, err := os.MkdirTemp("", "tt-cli-probe-")
	if err != nil {
		return fmt.Errorf("probe workspace could not be created: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	args := []string{
		"-p", probePrompt,
		"--output-format", "json",
		// AGY doesn't have a max-turns flag in print mode, but it does have dangerously-skip-permissions
		"--dangerously-skip-permissions",
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = childEnv(ctx)
	cmd.Dir = dir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	runErr := cmd.Run()
	out := buf.String()
	if runErr == nil {
		return nil
	}
	if looksUnauthenticated(out) {
		return fmt.Errorf("%s is installed but has no usable session%s. Sign the CLI in on this machine: %w",
			bin, tail(out), domain.ErrAgentCLIUnauthenticated)
	}
	return fmt.Errorf("%s did not complete a test session (%v)%s", bin, runErr, tail(out))
}

func looksUnauthenticated(out string) bool {
	lower := strings.ToLower(out)
	for _, marker := range authFailureMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

const probeOutputMax = 600

func tail(out string) string {
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) > probeOutputMax {
		trimmed = "…" + trimmed[len(trimmed)-probeOutputMax:]
	}
	return ": " + strings.Join(strings.Fields(trimmed), " ")
}

// childEnv provides a safe environment for the subprocess.
func childEnv(ctx context.Context) []string {
	return childenv.For(os.Environ(), nil)
}

// ResolveBinary finds the given binary on PATH.
func ResolveBinary(binary string) (string, error) {
	if binary == "" {
		binary = "agy"
	}
	path, err := exec.LookPath(binary)
	if err != nil {
		return "", fmt.Errorf("executable %q not found on PATH", binary)
	}
	return path, nil
}
