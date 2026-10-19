package evolution

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Golden fixtures pin promote.go/golden.go's LLM-facing prose exactly as it
// read before the move to catalog/system/prompts/evolution/*.md (see the
// prompt library program). The move must keep these byte-identical.

func TestTeamPromotionSystemPromptGolden(t *testing.T) {
	want := `You curate a software team's shared memory. Some entries are not memories at all — they are reusable know-how that belongs in the skill catalog, where agents load it while working.

Promote an entry ONLY when it teaches a durable, reusable method: a how-to, technique, checklist, convention, or workflow that will keep paying off in future tasks. When promoting, rewrite the content as concise instructional markdown an agent can follow.

Do NOT promote: one-off facts, project status, decisions or events, user/stakeholder preferences, credentials or URLs, and repository-specific trivia that teaches no transferable method. Those stay memories. An empty promotions array is a valid and often correct answer.

For each promotion set agents to the roster names the skill is relevant for; use an empty array when it fits the whole team.

Respond with a single JSON object matching the provided schema.`
	if teamPromotionSystemPrompt != want {
		t.Fatalf("teamPromotionSystemPrompt =\n%q\nwant\n%q", teamPromotionSystemPrompt, want)
	}
}

func TestSaveClassifierSystemPromptGolden(t *testing.T) {
	want := `An agent on a software team is about to save a note to its long-term memory. Decide whether the note is actually reusable know-how that belongs in the skill catalog instead.

Set skill=true ONLY when the note teaches a durable, reusable method: a how-to, technique, checklist, convention, or workflow worth loading in future tasks. Then rewrite it as concise instructional markdown in content and give it a short kebab-case name and a one-line description.

Set skill=false for everything else: one-off facts, status, events, preferences, credentials, and notes tied to a single repository's current state. When in doubt, skill=false — a wrong memory is cheap, a wrong skill pollutes the catalog. Fill unused fields with empty strings.

Respond with a single JSON object matching the provided schema.`
	if saveClassifierSystemPrompt != want {
		t.Fatalf("saveClassifierSystemPrompt =\n%q\nwant\n%q", saveClassifierSystemPrompt, want)
	}
}

func TestTeamPromotionAgentLineGolden(t *testing.T) {
	got := teamPromotionAgentLine(domain.Agent{Name: "backend-developer", Description: "Ships Go services."})
	want := "- backend-developer: Ships Go services."
	if got != want {
		t.Fatalf("teamPromotionAgentLine =\n%q\nwant\n%q", got, want)
	}
}

func TestTeamPromotionMemoryLineGolden(t *testing.T) {
	id := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	t.Run("global, categorized", func(t *testing.T) {
		got := teamPromotionMemoryLine(domain.AgentMemory{ID: id, Category: "workflow", Content: "Always run go vet before committing."})
		want := "- id: 00000000-0000-0000-0000-000000000001 | category: workflow | scope: global\n  Always run go vet before committing."
		if got != want {
			t.Fatalf("teamPromotionMemoryLine =\n%q\nwant\n%q", got, want)
		}
	})
	t.Run("repository-specific, no category, multiline content", func(t *testing.T) {
		repoID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
		got := teamPromotionMemoryLine(domain.AgentMemory{ID: id, RepositoryID: &repoID, Content: "Line one\nLine two"})
		want := "- id: 00000000-0000-0000-0000-000000000001 | category: - | scope: repository-specific\n  Line one\n  Line two"
		if got != want {
			t.Fatalf("teamPromotionMemoryLine =\n%q\nwant\n%q", got, want)
		}
	})
}

func TestTeamPromotionUserMessageGolden(t *testing.T) {
	id := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	agents := []domain.Agent{
		{Name: "backend-developer", Description: "Ships Go services."},
		{Name: "qa-agent", Description: "Runs the test suite."},
	}
	memories := []domain.AgentMemory{
		{ID: id, Category: "workflow", Content: "Always run go vet before committing."},
	}
	got := teamPromotionUserMessage(memories, agents)
	want := "## Agent roster\n" +
		"- backend-developer: Ships Go services.\n" +
		"- qa-agent: Runs the test suite.\n" +
		"\n## Team memories\n" +
		"- id: 00000000-0000-0000-0000-000000000001 | category: workflow | scope: global\n  Always run go vet before committing.\n"
	if got != want {
		t.Fatalf("teamPromotionUserMessage =\n%q\nwant\n%q", got, want)
	}
}

func TestRetryNotJSONMessageGolden(t *testing.T) {
	got := retryNotJSONMessage(errors.New("unexpected end of JSON input"))
	want := "Your previous output was not valid JSON (unexpected end of JSON input). Respond again with ONLY the JSON object, no prose, no code fences."
	if got != want {
		t.Fatalf("retryNotJSONMessage =\n%q\nwant\n%q", got, want)
	}
}
