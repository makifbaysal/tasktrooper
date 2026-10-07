package core

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/childenv"
)

// TailWriter keeps only the trailing bytes of the stream written into it — the
// part worth quoting when a session fails and its head is long gone.
type TailWriter struct {
	max int
	buf []byte
}

func NewTailWriter(max int) *TailWriter {
	return &TailWriter{max: max}
}

func (w *TailWriter) Write(p []byte) (int, error) {
	n := len(p)
	if w.max > 0 && n > w.max {
		p = p[n-w.max:]
	}
	w.buf = append(w.buf, p...)
	if w.max > 0 && len(w.buf) > w.max {
		w.buf = w.buf[len(w.buf)-w.max:]
	}
	return n, nil
}

func (w *TailWriter) String() string { return string(bytes.TrimSpace(w.buf)) }

const probeOutputMax = 600

// Tail flattens and truncates a process's captured output for embedding in an
// error message: one line, at most probeOutputMax bytes, prefixed with ": ".
func Tail(out string) string {
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) > probeOutputMax {
		trimmed = "…" + trimmed[len(trimmed)-probeOutputMax:]
	}
	return ": " + strings.Join(strings.Fields(trimmed), " ")
}

// ChildEnv builds the environment a CLI child is spawned with. useFullEnv
// starts from the server's environment and layers the session overlay plus the
// flavor's allowlisted passthrough (Claude Code's config/auth vars); otherwise
// the child gets only a toolchain (MinimalEnv) and nothing else. The caller
// appends the run's own allowlisted session env separately.
func ChildEnv(ctx context.Context, useFullEnv bool, passthrough []string) []string {
	if !useFullEnv {
		return MinimalEnv()
	}
	parent := os.Environ()
	overlay := append([]string{}, registry.TaskEnvFromContext(ctx)...)
	for _, name := range passthrough {
		if value, ok := os.LookupEnv(name); ok {
			overlay = append(overlay, name+"="+value)
		}
	}
	return childenv.For(parent, overlay)
}

// WriteSystemPromptFile persists a system prompt to a 0600 temp file and
// returns its path plus a cleanup. Empty prompts write nothing.
func WriteSystemPromptFile(systemPrompt string) (string, func(), error) {
	noop := func() {}
	if systemPrompt == "" {
		return "", noop, nil
	}
	f, err := os.CreateTemp("", "tt-cli-system-*.md")
	if err != nil {
		return "", noop, fmt.Errorf("create system prompt file: %w", err)
	}
	path := f.Name()
	cleanup := func() { _ = os.Remove(path) }
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		cleanup()
		return "", noop, fmt.Errorf("secure system prompt file: %w", err)
	}
	if _, err := f.WriteString(systemPrompt); err != nil {
		f.Close()
		cleanup()
		return "", noop, fmt.Errorf("write system prompt file: %w", err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", noop, fmt.Errorf("close system prompt file: %w", err)
	}
	return path, cleanup, nil
}