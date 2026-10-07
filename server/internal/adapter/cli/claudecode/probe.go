package claudecode

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cli/core"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/winshim"
)

type ProbeResult struct {
	BinaryPath string
	Version string
}

const probeVersionTimeout = 60 * time.Second

const DefaultProbeTimeout = 3 * time.Minute

const probePrompt = "Reply with exactly: ok"

var authFailureMarkers = []string{
	"invalid api key",
	"invalid_api_key",
	"authentication_error",
	"authentication failed",
	"please run /login",
	"run /login",
	"claude login",
	"not logged in",
	"no credentials",
	"credentials not found",
	"oauth token has expired",
	"token has expired",
	"unauthorized",
	"401",
}

func Probe(ctx context.Context, binary string, settingSources string) (ProbeResult, error) {
	resolved, err := ResolveBinary(binary)
	if err != nil {
		return ProbeResult{}, fmt.Errorf("%s. Install the agent cli on this machine, or set CLAUDE_CODE_BIN to its path: %w",
			err.Error(), domain.ErrAgentCLIBinaryMissing)
	}

	version, out, err := probeVersion(ctx, resolved)
	if err != nil {
return ProbeResult{}, fmt.Errorf("%s was found but would not run (%v)%s: %w",
		resolved, err, core.Tail(out), domain.ErrAgentCLIBinaryMissing)
	}

	if err := probeAuth(ctx, resolved, settingSources); err != nil {
		return ProbeResult{}, err
	}
	return ProbeResult{BinaryPath: resolved, Version: version}, nil
}

func probeVersion(ctx context.Context, bin string) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, probeVersionTimeout)
	defer cancel()

	cmd := winshim.Command(ctx, bin, "--version")
	cmd.Env = claudeChildEnv(ctx)
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

func probeAuth(ctx context.Context, bin string, settingSources string) error {
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
		"--max-turns", "1",
		"--setting-sources", normalizeSettingSources(settingSources),
		"--dangerously-skip-permissions",
	}
	cmd := winshim.Command(ctx, bin, args...)
	cmd.Env = claudeChildEnv(ctx)
	cmd.Dir = dir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	runErr := cmd.Run()
	out := buf.String()
	if runErr == nil {
		return nil
	}
	if core.LooksUnauthenticated(out, authFailureMarkers) {
		return fmt.Errorf("%s is installed but has no usable session%s. Sign the cli in on this machine, or set CLAUDE_CODE_OAUTH_TOKEN: %w",
			bin, core.Tail(out), domain.ErrAgentCLIUnauthenticated)
	}

	return fmt.Errorf("%s did not complete a test session (%v)%s", bin, runErr, core.Tail(out))
}
