package board

import (
	"strings"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestPrependProjectContext(t *testing.T) {
	history := []domain.Message{{Role: domain.RoleUser, Content: "task"}}

	t.Run("brief is truncated to the cap", func(t *testing.T) {
		brief := strings.Repeat("z", maxInjectedBriefChars+500)
		out := prependProjectContext(history, "a demo service", brief)
		if len(out) != 2 {
			t.Fatalf("messages = %d, want context + task", len(out))
		}
		msg := out[0]
		if msg.Role != domain.RoleSystem {
			t.Fatalf("context message role = %s, want system", msg.Role)
		}
		if !strings.HasPrefix(msg.Content, "Project context: a demo service") {
			t.Fatalf("description must lead the message, got %q", msg.Content[:60])
		}
		if !strings.Contains(msg.Content, "## Project brief (maintained by TaskTrooper)") {
			t.Fatal("brief header missing")
		}
		if got := strings.Count(msg.Content, "z"); got != maxInjectedBriefChars {
			t.Fatalf("injected brief chars = %d, want the %d cap", got, maxInjectedBriefChars)
		}
	})

	t.Run("brief alone injects without a description", func(t *testing.T) {
		out := prependProjectContext(history, "", "## Stack\nGo + React")
		if len(out) != 2 {
			t.Fatalf("messages = %d, want context + task", len(out))
		}
		if strings.Contains(out[0].Content, "Project context:") {
			t.Fatalf("no description was given, got %q", out[0].Content)
		}
		if !strings.Contains(out[0].Content, "Go + React") {
			t.Fatal("brief body missing")
		}
	})

	t.Run("description alone keeps the old shape", func(t *testing.T) {
		out := prependProjectContext(history, "a demo service", "")
		if len(out) != 2 {
			t.Fatalf("messages = %d, want context + task", len(out))
		}
		if out[0].Content != "Project context: a demo service" {
			t.Fatalf("description-only message = %q", out[0].Content)
		}
	})

	t.Run("nothing to inject leaves history untouched", func(t *testing.T) {
		out := prependProjectContext(history, "", "")
		if len(out) != 1 {
			t.Fatalf("messages = %d, want the task alone", len(out))
		}
	})
}
