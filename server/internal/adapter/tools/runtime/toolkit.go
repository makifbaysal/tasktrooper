// Package runtime exposes the agent-facing tools over a bound environment's
// live picture: get_environment, query_runtime_logs, list_runtime_errors and
// list_deployments. It sits beside adapter/tools/projectmodel rather than
// inside it — these tools read through cloud.Service, not the project model
// store, and only need port.ComponentStore for path resolution.
package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	cloudapp "github.com/makifbaysal/tasktrooper/server/internal/application/cloud"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// ToolKit is the read-only surface these tools need: component paths (to
// resolve the component argument) and the cloud service (environments, logs,
// errors, deployments).
type ToolKit struct {
	Components port.ComponentStore
	Cloud      *cloudapp.Service
}

func NewExecutors(k *ToolKit) []port.ToolExecutor {
	if k == nil || k.Components == nil || k.Cloud == nil {
		return nil
	}
	return []port.ToolExecutor{
		&getEnvironmentTool{kit: k},
		&queryRuntimeLogsTool{kit: k},
		&listRuntimeErrorsTool{kit: k},
		&listDeploymentsTool{kit: k},
	}
}

func toolError(name, message string) domain.ToolResult {
	return domain.ToolResult{Name: name, Content: message, IsError: true}
}

func toolJSON(name string, payload any) domain.ToolResult {
	raw, err := json.Marshal(payload)
	if err != nil {
		return toolError(name, fmt.Sprintf("marshal response: %v", err))
	}
	return domain.ToolResult{Name: name, Content: string(raw), IsError: false}
}

const repositoryIDProperty = "repository_id"

func repositoryIDSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":        "string",
		"description": "Repository UUID. Defaults to the run's own repository; pass this only to look at a different one.",
	}
}

func componentSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "string",
		"description": "A component's own path (e.g. \"apps/web\", or \".\" for a single-purpose repo). Omit it (or " +
			"pass \".\") when the repository has only one component — it resolves automatically; with more than one " +
			"it is required and the error names every path to choose from.",
	}
}

func environmentSchema(defaultDesc string) map[string]interface{} {
	return map[string]interface{}{
		"type":        "string",
		"enum":        []string{"production", "staging", "preview", "development"},
		"description": "Which environment to read. Defaults to " + defaultDesc + ".",
	}
}

// resolveRepositoryID mirrors the project model tools' own context fallback:
// an explicit repository_id overrides the run's own repository, and a call
// with neither gets an error naming both ways out.
func resolveRepositoryID(ctx context.Context, raw, toolName string) (uuid.UUID, error) {
	if trimmed := strings.TrimSpace(raw); trimmed != "" {
		id, err := uuid.Parse(trimmed)
		if err != nil {
			return uuid.Nil, fmt.Errorf("invalid repository_id: %w", err)
		}
		return id, nil
	}
	if id := registry.RepositoryIDFromContext(ctx); id != uuid.Nil {
		return id, nil
	}
	return uuid.Nil, fmt.Errorf("%s needs a repository in context or repository_id; this run has none", toolName)
}

func activeComponents(all []domain.Component) []domain.Component {
	out := make([]domain.Component, 0, len(all))
	for _, c := range all {
		if c.Status == domain.ComponentStatusActive {
			out = append(out, c)
		}
	}
	return out
}

// resolveComponent turns the component argument into one active component:
// a path resolves exactly; "" or "." resolves to the repository's only
// component, or its root ("." path) component when there is more than one —
// otherwise the error lists every path so the caller can retry with one.
func resolveComponent(ctx context.Context, store port.ComponentStore, repositoryID uuid.UUID, path string) (domain.Component, error) {
	all, err := store.ListComponents(ctx, repositoryID)
	if err != nil {
		return domain.Component{}, err
	}
	active := activeComponents(all)
	if len(active) == 0 {
		return domain.Component{}, fmt.Errorf("this repository has no active components")
	}

	trimmed := strings.TrimSpace(path)
	if trimmed != "" && trimmed != "." {
		for _, c := range active {
			if c.Path == trimmed {
				return c, nil
			}
		}
		return domain.Component{}, fmt.Errorf("no active component at path %q", trimmed)
	}

	if len(active) == 1 {
		return active[0], nil
	}
	for _, c := range active {
		if c.Path == "." {
			return c, nil
		}
	}
	paths := make([]string, 0, len(active))
	for _, c := range active {
		paths = append(paths, c.Path)
	}
	sort.Strings(paths)
	return domain.Component{}, fmt.Errorf(
		"this repository has %d components: pass component (one of %s)", len(active), strings.Join(paths, ", "))
}

// resolveBoundEnvironment resolves the component and, within it, one bound
// environment (default production). "Bound" — an account and a resource, not
// just a suggestion or a custom URL-only row — is the same test
// cloud.Service.Logs/Errors/Deployments make; failing it here first gives a
// clear, actionable message instead of the service's ErrNotConnected.
func resolveBoundEnvironment(ctx context.Context, kit *ToolKit, repositoryID uuid.UUID, componentPath, envArg string) (domain.ComponentEnvironment, domain.Component, error) {
	comp, err := resolveComponent(ctx, kit.Components, repositoryID, componentPath)
	if err != nil {
		return domain.ComponentEnvironment{}, domain.Component{}, err
	}

	env := domain.DeployEnvironment(strings.TrimSpace(envArg))
	if env == "" {
		env = domain.EnvironmentProduction
	}

	envs, err := kit.Cloud.ListEnvironments(ctx, repositoryID)
	if err != nil {
		return domain.ComponentEnvironment{}, comp, err
	}
	for _, e := range envs {
		if e.ComponentID == comp.ID && e.Environment == env && e.Bound() {
			return e, comp, nil
		}
	}
	return domain.ComponentEnvironment{}, comp, fmt.Errorf(
		"no %s environment is bound for %s — the human connects it on the repository's Deploy tab", env, comp.DisplayName())
}

// parseSinceDuration accepts a Go duration ("30m", "2h") plus a day suffix
// ("1d", "2.5d") time.ParseDuration does not understand.
func parseSinceDuration(raw string) (time.Duration, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, nil
	}
	if strings.HasSuffix(trimmed, "d") {
		n, err := strconv.ParseFloat(strings.TrimSuffix(trimmed, "d"), 64)
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("invalid since %q: use a duration like \"30m\", \"2h\", \"1d\"", raw)
		}
		return time.Duration(n * float64(24*time.Hour)), nil
	}
	d, err := time.ParseDuration(trimmed)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("invalid since %q: use a duration like \"30m\", \"2h\", \"1d\"", raw)
	}
	return d, nil
}

// firstLines caps a multi-line sample at n lines, marking whether it cut
// anything — a whole stack trace costs the same context budget as its first
// useful screenful.
func firstLines(s string, n int) (string, bool) {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s, false
	}
	return strings.Join(lines[:n], "\n"), true
}
