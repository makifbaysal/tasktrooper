package executor

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/application/toolchain"
)

const (
	maxEnvEntries    = 64
	maxEnvValueBytes = 4096
)

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

// runEnv refuses PATH: a PATH from the coordination side names directories on
// another machine, and this computer resolves its own from the checkout's
// version files.
func runEnv(env map[string]string) ([]string, *Failure) {
	if len(env) == 0 {
		return nil, nil
	}
	if len(env) > maxEnvEntries {
		return nil, badRequest("env has %d entries; at most %d", len(env), maxEnvEntries)
	}
	out := make([]string, 0, len(env))
	for name, value := range env {
		if !envName.MatchString(name) {
			return nil, badRequest("env name %q is not a variable name", name)
		}
		if strings.EqualFold(name, "PATH") {
			return nil, badRequest("env cannot set PATH: this computer resolves it from the checkout's toolchain files")
		}
		if len(value) > maxEnvValueBytes || strings.ContainsRune(value, 0) {
			return nil, badRequest("env %s is longer than %d bytes or holds a NUL", name, maxEnvValueBytes)
		}
		out = append(out, name+"="+value)
	}
	sort.Strings(out)
	return out, nil
}

// withRunEnv puts the run's env after the toolchain this computer resolves
// for the checkout, so both reach run_terminal: the shell resolves its own
// overlay only when the context carries none.
func (r *PreparedRun) withRunEnv(ctx context.Context) context.Context {
	if len(r.env) == 0 {
		return ctx
	}
	var overlay []string
	if r.workDir != "" {
		overlay = toolchain.Default.Overlay(r.workDir).Env
	}
	return registry.ContextWithTaskEnv(ctx, append(append([]string(nil), overlay...), r.env...))
}
