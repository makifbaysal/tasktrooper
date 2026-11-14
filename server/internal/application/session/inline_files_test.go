package session_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/session"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestInlineTextFileAttachmentsFoldsSmallTextFilesIntoContent(t *testing.T) {
	history := []domain.Message{{
		Role:    domain.RoleUser,
		Content: "what does this say?",
		Files: []domain.AttachedFile{
			{Filename: "notes.md", MediaType: "text/markdown", Data: base64.StdEncoding.EncodeToString([]byte("hello there"))},
		},
	}}

	session.InlineTextFileAttachmentsForTest(history)

	if len(history[0].Files) != 0 {
		t.Fatalf("inlined file should be removed from Files: %+v", history[0].Files)
	}
	if !strings.Contains(history[0].Content, "notes.md") || !strings.Contains(history[0].Content, "hello there") {
		t.Fatalf("file was not inlined into content: %q", history[0].Content)
	}
}

func TestInlineTextFileAttachmentsLeavesOpaqueTypesUnread(t *testing.T) {
	history := []domain.Message{{
		Role:    domain.RoleUser,
		Content: "see attached",
		Files: []domain.AttachedFile{
			{Filename: "report.pdf", MediaType: "application/pdf", Data: base64.StdEncoding.EncodeToString([]byte("%PDF"))},
		},
	}}

	session.InlineTextFileAttachmentsForTest(history)

	if len(history[0].Files) != 1 {
		t.Fatalf("opaque file should stay on Files, unread: %+v", history[0].Files)
	}
	if strings.Contains(history[0].Content, "%PDF") {
		t.Fatalf("opaque file bytes leaked into content: %q", history[0].Content)
	}
}

func TestInlineTextFileAttachmentsSkipsFilesOverTheCap(t *testing.T) {
	big := strings.Repeat("a", 200<<10+1)
	history := []domain.Message{{
		Role:    domain.RoleUser,
		Content: "see attached",
		Files: []domain.AttachedFile{
			{Filename: "big.txt", MediaType: "text/plain", Data: base64.StdEncoding.EncodeToString([]byte(big))},
		},
	}}

	session.InlineTextFileAttachmentsForTest(history)

	if len(history[0].Files) != 1 {
		t.Fatalf("oversized file should stay on Files instead of being inlined: %+v", history[0].Files)
	}
}
