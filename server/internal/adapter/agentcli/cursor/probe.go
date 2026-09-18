package cursor

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
	BinaryPath string
	Version    string
}

const probeVersionTimeout = 60 * time.Second

// DefaultProbeTimeout bounds the status check.
const DefaultProbeTimeout = 60 * time.Second

var authFailureMarkers = []string{
	"not authenticated",
	"not logged in",
	"invalid api key",
	"invalid_api_key",
	"unauthorized",
	"401",
}

// Probe reports whether this host can actually run a cursor-agent session.
func Probe(ctx context.Context, binary string) (ProbeResult, error) {
	resolved, err := ResolveBinary(binary)
	if err != nil {
		return ProbeResult{}, fmt.Errorf("%s. Install the Cursor CLI on this machine, or set CURSOR_AGENT_BIN to its path: %w",
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

// probeAuth runs `cursor-agent status` — the CLI's own documented way to
// report "whether you're authenticated" — rather than spending a full turn
// the way claudecode/antigravity/opencode do, because this CLI exposes the
// answer directly instead of only failing a real session.
func probeAuth(ctx context.Context, bin string) error {
	ctx, cancel := context.WithTimeout(ctx, DefaultProbeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "status")
	cmd.Env = childEnv(ctx)
	cmd.Dir = os.TempDir()
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	runErr := cmd.Run()
	out := buf.String()

	if looksUnauthenticated(out) {
		return fmt.Errorf("%s is installed but has no usable session%s. Sign the CLI in on this machine: %w",
			bin, tail(out), domain.ErrAgentCLIUnauthenticated)
	}
	if runErr != nil {
		return fmt.Errorf("%s did not report its status (%v)%s", bin, runErr, tail(out))
	}
	return nil
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
		binary = "cursor-agent"
	}
	path, err := exec.LookPath(binary)
	if err != nil {
		return "", fmt.Errorf("executable %q not found on PATH", binary)
	}
	return path, nil
}
