package cloud

import (
	"context"
	"fmt"
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

func renderFixTaskDescription(env domain.ComponentEnvironment, group domain.RuntimeErrorGroup, recent []domain.RuntimeLogEntry, consoleURL string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Environment: %s\n", env.Environment)

	resource := string(env.Provider)
	if env.Resource != nil && env.Resource.Name != "" {
		resource = strings.TrimSpace(resource + " " + env.Resource.Name)
	}
	fmt.Fprintf(&b, "Provider/resource: %s\n", resource)
	fmt.Fprintf(&b, "Count: %d\n", group.Count)
	fmt.Fprintf(&b, "First seen: %s\n", group.FirstSeen.Format(time.RFC3339))
	fmt.Fprintf(&b, "Last seen: %s\n", group.LastSeen.Format(time.RFC3339))

	b.WriteString("\n```\n")
	b.WriteString(group.Sample)
	b.WriteString("\n```\n")

	if len(recent) > 0 {
		b.WriteString("\nRecent log lines:\n")
		for _, e := range recent {
			fmt.Fprintf(&b, "- %s [%s] %s\n", e.Timestamp.Format(time.RFC3339), e.Severity, firstLineOf(e.Message))
		}
	}

	if consoleURL != "" {
		fmt.Fprintf(&b, "\n[View in provider console](%s)\n", consoleURL)
	}
	return b.String()
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
