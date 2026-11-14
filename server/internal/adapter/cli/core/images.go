package core

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type attachedImagesInput struct {
	Paths []string
}

var attachedImagesKey = prompt.Define("cli.attached_images", attachedImagesInput{
	Paths: []string{"/tmp/tt-cli-images-1/1-1.png"},
})

type attachedFilesInput struct {
	Paths []string
}

var attachedFilesKey = prompt.Define("cli.attached_files", attachedFilesInput{
	Paths: []string{"/tmp/tt-cli-images-1/1-1-report.pdf"},
})

// StagedImages is a history whose images were written to files for a CLI.
type StagedImages struct {
	// History is a copy of the input with every image replaced by a note
	// naming its file; the caller's slice is not modified.
	History []domain.Message
	// notes holds the note added to each message, by index.
	notes map[int]string
	dir   string
}

// Cleanup removes the staged files; safe on a zero value.
func (s StagedImages) Cleanup() {
	if s.dir != "" {
		_ = os.RemoveAll(s.dir)
	}
}

// LastUserNote is the note on the last user message, for a resumed CLI
// session that is sent only the new message text instead of the history.
func (s StagedImages) LastUserNote() string {
	for i := len(s.History) - 1; i >= 0; i-- {
		if s.History[i].Role == domain.RoleUser {
			return s.notes[i]
		}
	}
	return ""
}

// StageImages gives a CLI the images and other attached files a chat message
// carries. These CLIs take one prose prompt, so an image or file on
// domain.Message.Images/Files would otherwise be dropped and the model
// answers without ever seeing it. Each image is written to a 0600 file named
// by index in a fresh temp dir; each file is written under its own
// subdirectory so it keeps its original name (unlike an image, a file's name
// can carry meaning the model needs). Either way the message gets a note
// naming the paths for the CLI's own file-reading tool.
func StageImages(history []domain.Message) StagedImages {
	if !hasAttachments(history) {
		return StagedImages{History: history}
	}
	dir, err := os.MkdirTemp("", "tt-cli-images-")
	if err != nil {
		log.Warn().Err(err).Msg("agent cli: could not stage chat attachments, sending the message without them")
		return StagedImages{History: history}
	}
	staged := StagedImages{History: make([]domain.Message, len(history)), notes: map[int]string{}, dir: dir}
	copy(staged.History, history)
	for i := range staged.History {
		msg := &staged.History[i]
		var noteParts []string

		if len(msg.Images) > 0 {
			var paths []string
			for j, img := range msg.Images {
				data, err := base64.StdEncoding.DecodeString(img.Data)
				if err != nil || len(data) == 0 {
					log.Warn().Err(err).Msg("agent cli: chat image is not valid base64, skipping it")
					continue
				}
				path := filepath.Join(dir, strconv.Itoa(i+1)+"-"+strconv.Itoa(j+1)+imageExt(img.MediaType))
				if err := os.WriteFile(path, data, 0o600); err != nil {
					log.Warn().Err(err).Msg("agent cli: writing a chat image failed, skipping it")
					continue
				}
				paths = append(paths, path)
			}
			msg.Images = nil
			if len(paths) > 0 {
				note := strings.TrimSpace(attachedImagesKey.Render(attachedImagesInput{Paths: paths}))
				noteParts = append(noteParts, note)
			}
		}

		if len(msg.Files) > 0 {
			var paths []string
			for j, f := range msg.Files {
				data, err := base64.StdEncoding.DecodeString(f.Data)
				if err != nil || len(data) == 0 {
					log.Warn().Err(err).Msg("agent cli: chat file is not valid base64, skipping it")
					continue
				}
				subdir := filepath.Join(dir, strconv.Itoa(i+1)+"-"+strconv.Itoa(j+1))
				if err := os.MkdirAll(subdir, 0o700); err != nil {
					log.Warn().Err(err).Msg("agent cli: could not stage a chat file, skipping it")
					continue
				}
				path := filepath.Join(subdir, sanitizeFilename(f.Filename))
				if err := os.WriteFile(path, data, 0o600); err != nil {
					log.Warn().Err(err).Msg("agent cli: writing a chat file failed, skipping it")
					continue
				}
				paths = append(paths, path)
			}
			msg.Files = nil
			if len(paths) > 0 {
				note := strings.TrimSpace(attachedFilesKey.Render(attachedFilesInput{Paths: paths}))
				noteParts = append(noteParts, note)
			}
		}

		if len(noteParts) == 0 {
			continue
		}
		note := strings.Join(noteParts, "\n\n")
		staged.notes[i] = note
		msg.Content = strings.TrimSpace(msg.Content + "\n\n" + note)
	}
	return staged
}

// WithNote appends note to text when there is one.
func WithNote(text, note string) string {
	if note == "" {
		return text
	}
	return strings.TrimSpace(text + "\n\n" + note)
}

func hasAttachments(history []domain.Message) bool {
	for _, m := range history {
		if len(m.Images) > 0 || len(m.Files) > 0 {
			return true
		}
	}
	return false
}

// sanitizeFilename strips any directory component from a user-supplied name
// so it cannot escape the per-file staging subdirectory.
func sanitizeFilename(name string) string {
	base := filepath.Base(strings.TrimSpace(name))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return "file"
	}
	return base
}

func imageExt(mediaType string) string {
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ".png"
	}
}
