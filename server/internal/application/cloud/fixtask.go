package cloud

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const fixTaskRecentLogLimit = 20

// CreateFixTask opens a bug task in the environment's repository from a
// runtime error group: the write-up an agent needs to reproduce and fix the
// error without first having to go read the logs itself.
func (s *Service) CreateFixTask(ctx context.Context, envID uuid.UUID, group domain.RuntimeErrorGroup) (domain.BoardTask, error) {
	if s.tasks == nil {
		return domain.BoardTask{}, fmt.Errorf("board is not available")
	}
	env, err := s.environments.GetEnvironment(ctx, envID)
	if err != nil {
		return domain.BoardTask{}, err
	}

	var consoleURL string
	var recent []domain.RuntimeLogEntry
	if env.Bound() {
		if overview, err := s.Overview(ctx, envID); err == nil && overview.Detail != nil {
			consoleURL = overview.Detail.ConsoleURL
		}
		text := truncateRunes(firstLineOf(group.Message), 60)
		if page, err := s.Logs(ctx, envID, domain.RuntimeLogQuery{
			Since: group.FirstSeen, Until: s.now(), MinSeverity: domain.LogError, Text: text, Limit: fixTaskRecentLogLimit,
		}); err == nil {
			recent = page.Entries
		}
	}

	componentID := env.ComponentID
	return s.tasks.CreateTask(ctx, env.RepositoryID, domain.CreateBoardTaskRequest{
		Title:       "Fix: " + truncateRunes(firstLineOf(group.Message), 80),
		TaskType:    domain.TaskTypeBug,
		Description: renderFixTaskDescription(env, group, recent, consoleURL),
		ComponentID: &componentID,
		CreatedBy:   "system",
	})
}

// BoundErrorSource is where a component's error group was read when no
// cloud environment holds it: the data source of its errors.issues binding
// (a Sentry project).
type BoundErrorSource struct {
	Environment domain.DeployEnvironment
	Name        string
	Kind        domain.DataSourceKind
}

// CreateBoundFixTask is CreateFixTask for a group a component's errors
// binding read: the task goes to the component's repository, names the data
// source, and links the group's own page (an http(s) one only), there being
// no environment to read log lines or a console link from.
func (s *Service) CreateBoundFixTask(ctx context.Context, componentID uuid.UUID, src BoundErrorSource, group domain.RuntimeErrorGroup) (domain.BoardTask, error) {
	if s.tasks == nil {
		return domain.BoardTask{}, fmt.Errorf("board is not available")
	}
	if !src.Environment.Valid() {
		return domain.BoardTask{}, fmt.Errorf("unknown environment %q: %w", src.Environment, ErrInvalidInput)
	}
	comp, err := s.components.GetComponent(ctx, componentID)
	if err != nil {
		return domain.BoardTask{}, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Environment: %s\n", src.Environment)
	fmt.Fprintf(&b, "Data source: %s (%s)\n", src.Name, src.Kind)
	writeFixTaskBody(&b, group, nil, webLink(group.ExternalURL))
	return s.tasks.CreateTask(ctx, comp.RepositoryID, domain.CreateBoardTaskRequest{
		Title:       "Fix: " + truncateRunes(firstLineOf(group.Message), 80),
		TaskType:    domain.TaskTypeBug,
		Description: b.String(),
		ComponentID: &comp.ID,
		CreatedBy:   "system",
	})
}

// webLink keeps a link only when it is an http(s) page: the group came in a
// request body, and the task renders it as a link.
func webLink(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
		return ""
	}
	return u.String()
}

func renderFixTaskDescription(env domain.ComponentEnvironment, group domain.RuntimeErrorGroup, recent []domain.RuntimeLogEntry, consoleURL string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Environment: %s\n", env.Environment)

	resource := string(env.Provider)
	if env.Resource != nil && env.Resource.Name != "" {
		resource = strings.TrimSpace(resource + " " + env.Resource.Name)
	}
	fmt.Fprintf(&b, "Provider/resource: %s\n", resource)
	writeFixTaskBody(&b, group, recent, consoleURL)
	return b.String()
}

func writeFixTaskBody(b *strings.Builder, group domain.RuntimeErrorGroup, recent []domain.RuntimeLogEntry, consoleURL string) {
	fmt.Fprintf(b, "Count: %d\n", group.Count)
	fmt.Fprintf(b, "First seen: %s\n", group.FirstSeen.Format(time.RFC3339))
	fmt.Fprintf(b, "Last seen: %s\n", group.LastSeen.Format(time.RFC3339))

	b.WriteString("\n```\n")
	b.WriteString(group.Sample)
	b.WriteString("\n```\n")

	if len(recent) > 0 {
		b.WriteString("\nRecent log lines:\n")
		for _, e := range recent {
			fmt.Fprintf(b, "- %s [%s] %s\n", e.Timestamp.Format(time.RFC3339), e.Severity, firstLineOf(e.Message))
		}
	}

	if consoleURL != "" {
		fmt.Fprintf(b, "\n[View in provider console](%s)\n", consoleURL)
	}
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
