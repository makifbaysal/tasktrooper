package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/winshim"
)

type ProbeResult struct {
	BinaryPath string
	Version    string
}

const probeVersionTimeout = 60 * time.Second

// DefaultProbeTimeout is the auth-check budget for prompt-based flavors; a
// flavor overrides it in its dialog when its check is cheaper.
const DefaultProbeTimeout = 3 * time.Minute

const probePrompt = "Reply with exactly: ok"

// LookPath resolves a configured binary name to an absolute path, applying the
// flavor's on-disk default when nothing is configured.
func LookPath(configured, defaultBinary string) (string, error) {
	bin := strings.TrimSpace(configured)
	if bin == "" {
		bin = defaultBinary
	}
	return exec.LookPath(bin)
}

// ProbeDialog is everything a CLI's availability probe differs in: what the
// install hint names, which markers prove a signed-out session, and how the
// auth check is launched. Run and Interpret belong to the flavor; the shared
// body (resolve, --version, marker handling) lives here.
type ProbeDialog struct {
	DefaultBinary string
	MissingHint   string
	Markers       []string
	// Timeout is the auth-check budget. Zero means DefaultProbeTimeout.
	Timeout time.Duration
	// Run performs the CLI's auth check in a scratch dir and returns its
	// captured output. It must wrap a signed-out outcome in
	// domain.ErrAgentCLIUnauthenticated itself: flavors differ in whether the
	// marker is only examined after a non-zero exit.
	Run func(ctx context.Context, bin, dir, settingSources string) (out string, err error)
	// FailError renders the "the CLI ran but did not pass its check" error.
	FailError func(bin string, runErr error, out string) error
}

// Probe establishes that the CLI is installed, runs, and has a usable session.
func Probe(ctx context.Context, binary, settingSources string, d ProbeDialog) (ProbeResult, error) {
	resolved, err := LookPath(binary, d.DefaultBinary)
	if err != nil {
		return ProbeResult{}, fmt.Errorf("%s. %s: %w", err.Error(), d.MissingHint, domain.ErrAgentCLIBinaryMissing)
	}

	version, out, err := probeVersion(ctx, resolved)
	if err != nil {
		return ProbeResult{}, fmt.Errorf("%s was found but would not run (%v)%s: %w",
			resolved, err, Tail(out), domain.ErrAgentCLIBinaryMissing)
	}

	if err := probeAuth(ctx, resolved, settingSources, d); err != nil {
		return ProbeResult{}, err
	}
	return ProbeResult{BinaryPath: resolved, Version: version}, nil
}

func probeVersion(ctx context.Context, bin string) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, probeVersionTimeout)
	defer cancel()

	cmd := winshim.Command(ctx, bin, "--version")
	cmd.Env = ProbeEnv()
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

func probeAuth(ctx context.Context, bin, settingSources string, d ProbeDialog) error {
	timeout := d.Timeout
	if timeout <= 0 {
		timeout = DefaultProbeTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	dir, err := os.MkdirTemp("", "tt-cli-probe-")
	if err != nil {
		return fmt.Errorf("probe workspace could not be created: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	out, runErr := d.Run(ctx, bin, dir, settingSources)
	if runErr == nil {
		return nil
	}
	if errors.Is(runErr, domain.ErrAgentCLIUnauthenticated) {
		return runErr
	}
	return d.FailError(bin, runErr, out)
}

func LooksUnauthenticated(out string, markers []string) bool {
	lower := strings.ToLower(out)
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// ProbeEnv is the minimal child environment for the CLI probes (MinimalEnv),
// so an installed binary can find its helpers without inheriting anything
// that could leak into whatever it launches.
func ProbeEnv() []string {
	return MinimalEnv()
}