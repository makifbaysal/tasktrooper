package session

import (
	"encoding/base64"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// maxInlineFileBytes bounds a file folded into message content for a direct
// API provider with no file-reading tool of its own.
const maxInlineFileBytes = 200 << 10

var inlinableFileTypes = map[string]bool{
	"text/plain":       true,
	"text/markdown":    true,
	"text/csv":         true,
	"application/json": true,
}

// inlineTextFileAttachments folds small text-based files into their
// message's content for a direct-API provider: unlike a host-executed CLI,
// it has no file-reading tool of its own to hand a path to. Opaque types
// (pdf/docx/xlsx/zip) are left on Files, unread, on this path.
func inlineTextFileAttachments(history []domain.Message) {
	for i := range history {
		msg := &history[i]
		if len(msg.Files) == 0 {
			continue
		}
		kept := make([]domain.AttachedFile, 0, len(msg.Files))
		for _, f := range msg.Files {
			data, ok := decodeInlinableFile(f)
			if !ok {
				kept = append(kept, f)
				continue
			}
			msg.Content = msg.Content + "\n\n[Attached file: " + f.Filename + "]\n" + string(data) + "\n"
		}
		msg.Files = kept
	}
}

func decodeInlinableFile(f domain.AttachedFile) ([]byte, bool) {
	if !inlinableFileTypes[strings.ToLower(strings.TrimSpace(f.MediaType))] {
		return nil, false
	}
	data, err := base64.StdEncoding.DecodeString(f.Data)
	if err != nil || len(data) == 0 || len(data) > maxInlineFileBytes {
		return nil, false
	}
	return data, true
}
