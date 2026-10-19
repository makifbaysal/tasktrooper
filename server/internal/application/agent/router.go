package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type Runner interface {
	Run(ctx context.Context, messages []domain.Message, model string, provider domain.LLMProviderType, policy domain.ToolPolicy, opts ...RunOption) (domain.AgentResponse, error)
	RunTask(ctx context.Context, messages []domain.Message, model string, provider domain.LLMProviderType, policy domain.ToolPolicy, opts ...RunOption) (domain.AgentResponse, error)
	RunStream(ctx context.Context, messages []domain.Message, model string, provider domain.LLMProviderType, policy domain.ToolPolicy, onToken func(string), opts ...RunOption) (domain.AgentResponse, error)
}

var (
	_ Runner = (*Loop)(nil)
	_ Runner = (*Router)(nil)
)

type Router struct {
	loop     Runner
	mu       sync.RWMutex
	executor port.TaskExecutor
}

func NewRouter(loop Runner) *Router {
	return &Router{loop: loop}
}

func (r *Router) SetTaskExecutor(ex port.TaskExecutor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.executor = ex
}

func (r *Router) taskExecutor() port.TaskExecutor {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.executor
}

func (r *Router) SupportsHostExecution(provider domain.LLMProviderType) bool {
	ex := r.taskExecutor()
	return ex != nil && ex.Supports(provider)
}

func (r *Router) Run(ctx context.Context, messages []domain.Message, model string, provider domain.LLMProviderType, policy domain.ToolPolicy, opts ...RunOption) (domain.AgentResponse, error) {
	if !domain.RequiresHostExecutor(provider) {
		return r.loop.Run(ctx, messages, model, provider, policy, opts...)
	}
	return r.execute(ctx, messages, model, provider, policy, opts)
}

func (r *Router) RunTask(ctx context.Context, messages []domain.Message, model string, provider domain.LLMProviderType, policy domain.ToolPolicy, opts ...RunOption) (domain.AgentResponse, error) {
	if !domain.RequiresHostExecutor(provider) {
		return r.loop.RunTask(ctx, messages, model, provider, policy, opts...)
	}

	return r.execute(ctx, messages, model, provider, policy, opts)
}

func (r *Router) RunStream(ctx context.Context, messages []domain.Message, model string, provider domain.LLMProviderType, policy domain.ToolPolicy, onToken func(string), opts ...RunOption) (domain.AgentResponse, error) {
	return r.loop.RunStream(ctx, messages, model, provider, policy, onToken, opts...)
}

func (r *Router) execute(
	ctx context.Context,
	messages []domain.Message,
	model string,
	provider domain.LLMProviderType,
	policy domain.ToolPolicy,
	opts []RunOption,
) (domain.AgentResponse, error) {
	ex := r.taskExecutor()
	if ex == nil || !ex.Supports(provider) {
		return domain.AgentResponse{}, domain.ErrHostExecutedProvider(provider)
	}

	cfg := applyRunOptions(opts)

	workDir := registry.EffectiveWorkspaceDir(ctx)
	if workDir == "" {
		if !cfg.scratchWorkspace {
			return domain.AgentResponse{}, fmt.Errorf(
				"this agent runs on %s, which is a process and needs a working directory, "+
					"but this run was started without one; scope the work to a repository and try again", provider)
		}
		dir, err := os.MkdirTemp("", "tt-cli-run-")
		if err != nil {
			return domain.AgentResponse{}, fmt.Errorf("create a scratch workspace for the %s session: %w", provider, err)
		}
		defer func() {
			if rmErr := os.RemoveAll(dir); rmErr != nil {
				log.Warn().Err(rmErr).Str("dir", dir).Msg("could not remove the cli session's scratch workspace")
			}
		}()
		if err := writeScratchFiles(dir, cfg.scratchFiles); err != nil {
			log.Warn().Err(err).Str("dir", dir).Msg("could not seed the cli session's scratch workspace; it starts empty")
		}
		workDir = dir
	}

	log.Debug().
		Str("provider", string(provider)).
		Str("model", model).
		Str("label", cfg.cliLabel).
		Str("work_dir", workDir).
		Msg("routing an agentic run to the host executor")

	req := domain.TaskExecution{
		History:   messages,
		Model:     model,
		Provider:  provider,
		MaxTurns:  cfg.maxTurns,
		Effort:    cfg.effort,
		Policy:    policy,
		WorkDir:   workDir,
		Env:       registry.SessionEnvFromContext(ctx),
		TaskKey:   cfg.cliLabel,
		TaskTitle: cfg.cliTitle,
	}

	// Resume the CLI session with the trailing instruction rather than replaying the whole flattened history.
	session := CLISessionFromContext(ctx)
	if id := session.ID(); id != "" {
		if tail := resumeTail(messages); tail != "" {
			req.ResumeSessionID = id
			req.Prompt = tail
		} else {
			log.Debug().Str("provider", string(provider)).
				Msg("cli session available but the run has no trailing user instruction to resume with; sending the full history")
		}
	}

	resp, err := ex.Execute(ctx, req)
	if resp.CLISessionID != "" {
		session.Set(resp.CLISessionID)
	}
	return resp, err
}

// The trailing user-role messages a resumed session needs; "" when there is nothing to isolate.
func resumeTail(messages []domain.Message) string {
	if len(messages) == 0 || messages[len(messages)-1].Role != domain.RoleUser {
		return ""
	}
	lastAssistant := -1
	for i, m := range messages {
		if m.Role == domain.RoleAssistant {
			lastAssistant = i
		}
	}
	if lastAssistant == -1 {
		return ""
	}
	var parts []string
	for _, m := range messages[lastAssistant+1:] {
		if m.Role != domain.RoleUser {
			continue
		}
		parts = append(parts, m.Content)
	}
	return strings.Join(parts, "\n\n")
}

// The keys are caller data, so a path that would leave the directory is refused rather than cleaned into somewhere else.
func writeScratchFiles(dir string, files map[string]string) error {
	for rel, body := range files {
		clean := filepath.Clean(filepath.FromSlash(rel))
		if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return fmt.Errorf("scratch file %q is outside the scratch directory", rel)
		}
		path := filepath.Join(dir, clean)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			return err
		}
	}
	return nil
}
