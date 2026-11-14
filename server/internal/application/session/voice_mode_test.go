package session

import (
	"strings"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestAppendVoiceModePromptGoesAfterTheLastSystemMessage(t *testing.T) {
	history := []domain.Message{
		{Role: domain.RoleSystem, Content: "agent"},
		{Role: domain.RoleSystem, Content: "language"},
		{Role: domain.RoleUser, Content: "merhaba"},
	}
	got := appendVoiceModePrompt(history, "tr")
	if len(got) != 4 {
		t.Fatalf("len = %d, want 4", len(got))
	}
	if got[2].Role != domain.RoleSystem || !strings.Contains(got[2].Content, "Turkish") {
		t.Fatalf("voice prompt not at index 2: %+v", got[2])
	}
	if !strings.Contains(got[2].Content, "`visual`") {
		t.Fatalf("voice prompt does not explain visual blocks: %q", got[2].Content)
	}
	if got[3].Content != "merhaba" {
		t.Fatalf("user message moved: %+v", got[3])
	}
	if len(history) != 3 || history[2].Content != "merhaba" {
		t.Fatal("input history was modified")
	}
}

func TestAppendVoiceModePromptNamesEnglish(t *testing.T) {
	got := appendVoiceModePrompt([]domain.Message{{Role: domain.RoleUser, Content: "hi"}}, "en")
	if len(got) != 2 || got[0].Role != domain.RoleSystem || !strings.Contains(got[0].Content, "English") {
		t.Fatalf("got %+v", got)
	}
}

func TestAppendVoiceModePromptIgnoresOtherValues(t *testing.T) {
	history := []domain.Message{{Role: domain.RoleUser, Content: "hi"}}
	for _, voice := range []string{"", "de", "TR", "tr-TR"} {
		if got := appendVoiceModePrompt(history, voice); len(got) != 1 {
			t.Fatalf("voice %q added a message", voice)
		}
	}
}
