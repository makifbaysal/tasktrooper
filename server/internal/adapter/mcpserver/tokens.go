package mcpserver

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type Run struct {
	Ctx context.Context
	Policy domain.ToolPolicy
	TaskKey string
	SkillsOnDisk bool
	ExpiresAt time.Time
}

// canAsk is whether ask_user has somewhere to go: only a run that put a
// clarification sink on its context reads the question back once the CLI
// exits. Every other session is refused it, as before.
func (r Run) canAsk() bool {
	return domain.ClarificationSinkFrom(r.Ctx) != nil
}

func (r Run) live(now time.Time) bool {
	if r.Ctx == nil {
		return false
	}
	return r.ExpiresAt.IsZero() || now.Before(r.ExpiresAt)
}

const tokenBytes = 32

type RunTokenRegistry struct {
	mu   sync.RWMutex
	runs map[string]Run
	// now is time.Now, overridden by the expiry tests.
	now func() time.Time
}

func NewRunTokenRegistry() *RunTokenRegistry {
	return &RunTokenRegistry{runs: make(map[string]Run), now: time.Now}
}

func (r *RunTokenRegistry) clock() time.Time {
	if r.now == nil {
		return time.Now()
	}
	return r.now()
}

func (r *RunTokenRegistry) Mint(run Run) (string, error) {
	if run.Ctx == nil {
		return "", errors.New("mcpserver: a run token needs the run's context")
	}
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("mint mcp run token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(buf)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.runs[token] = run
	return token, nil
}

func (r *RunTokenRegistry) Revoke(token string) {
	if token == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.runs, token)
}

func (r *RunTokenRegistry) Lookup(token string) (Run, bool) {
	if token == "" {
		return Run{}, false
	}
	r.mu.RLock()
	run, ok := r.runs[token]
	r.mu.RUnlock()
	if !ok {
		return Run{}, false
	}
	if !run.live(r.clock()) {
		r.Revoke(token)
		return Run{}, false
	}
	return run, true
}

func (r *RunTokenRegistry) Live() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.runs)
}
