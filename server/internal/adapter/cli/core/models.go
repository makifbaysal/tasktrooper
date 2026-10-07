package core

import (
	"bytes"
	"context"
	"os"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/winshim"
)

// ModelsTimeout is the budget for a `models` listing; 30 seconds is plenty
// and keeps a hung listing from stalling a task forever.
const ModelsTimeout = 30 * time.Second

const modelsOutputMax = 2000

// RunModels executes the CLI's `models` listing subcommand and returns its
// captured output and stderr. Flavors whose CLIs have no such subcommand skip
// calling it.
func RunModels(ctx context.Context, bin string) (out, errOut string, err error) {
	ctx, cancel := context.WithTimeout(ctx, ModelsTimeout)
	defer cancel()

	cmd := winshim.Command(ctx, bin, "models")
	cmd.Env = MinimalEnv()
	cmd.Dir = os.TempDir()
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return "", errBuf.String(), err
	}
	return outBuf.String(), errBuf.String(), nil
}

// ModelsLines splits a `models` listing into trimmed non-empty lines.
func ModelsLines(out string) []string {
	if out == "" {
		return nil
	}
	lines := strings.Split(out, "\n")
	res := make([]string, 0, len(lines))
	for i := range lines {
		if line := strings.TrimSpace(lines[i]); line != "" {
			res = append(res, line)
		}
	}
	return res
}

// MaxModels caps a model listing at max entries, mirroring the hard ceiling
// CLIs apply so a long remote listing cannot bloat the payload.
func MaxModels(names []string, max int) []string {
	if len(names) <= max {
		return names
	}
	return names[:max]
}