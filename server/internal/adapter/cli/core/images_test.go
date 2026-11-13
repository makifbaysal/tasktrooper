package core

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestStageImagesWritesFilesTheCLICanRead(t *testing.T) {
	png := []byte("\x89PNG fake")
	history := []domain.Message{
		{Role: domain.RoleSystem, Content: "sys"},
		{Role: domain.RoleUser, Content: "why did this bounce?", Images: []domain.ToolResultImage{
			{MediaType: "image/png", Data: base64.StdEncoding.EncodeToString(png)},
		}},
	}

	staged := StageImages(history)
	defer staged.Cleanup()

	if len(history[1].Images) != 1 || strings.Contains(history[1].Content, "attached") {
		t.Fatalf("caller's history was modified: %+v", history[1])
	}
	msg := staged.History[1]
	if len(msg.Images) != 0 {
		t.Fatalf("staged message still carries raw images")
	}
	if !strings.HasPrefix(msg.Content, "why did this bounce?") {
		t.Fatalf("user text lost: %q", msg.Content)
	}
	note := staged.LastUserNote()
	if note == "" || !strings.Contains(msg.Content, note) {
		t.Fatalf("no image note on the message: %q", msg.Content)
	}
	var path string
	for _, line := range strings.Split(note, "\n") {
		if strings.HasPrefix(line, "- ") {
			path = strings.TrimPrefix(line, "- ")
		}
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(png) {
		t.Fatalf("staged file %q = %q, %v", path, got, err)
	}
	if !strings.HasSuffix(path, ".png") {
		t.Fatalf("extension lost: %q", path)
	}

	staged.Cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("cleanup left %q behind", path)
	}
}

func TestStageImagesWithoutImagesIsANoop(t *testing.T) {
	history := []domain.Message{{Role: domain.RoleUser, Content: "hi"}}
	staged := StageImages(history)
	defer staged.Cleanup()
	if staged.History[0].Content != "hi" || staged.LastUserNote() != "" {
		t.Fatalf("unexpected staging: %+v", staged)
	}
	if WithNote("hi", staged.LastUserNote()) != "hi" {
		t.Fatalf("WithNote changed text without a note")
	}
}

func TestLastUserNoteOnlyForTheNewestUserMessage(t *testing.T) {
	img := []domain.ToolResultImage{{MediaType: "image/jpeg", Data: base64.StdEncoding.EncodeToString([]byte("jpg"))}}
	history := []domain.Message{
		{Role: domain.RoleUser, Content: "old", Images: img},
		{Role: domain.RoleAssistant, Content: "ok"},
		{Role: domain.RoleUser, Content: "new"},
	}
	staged := StageImages(history)
	defer staged.Cleanup()
	if note := staged.LastUserNote(); note != "" {
		t.Fatalf("the newest message has no image, got note %q", note)
	}
	if !strings.Contains(staged.History[0].Content, ".jpg") {
		t.Fatalf("older image not staged: %q", staged.History[0].Content)
	}
}
