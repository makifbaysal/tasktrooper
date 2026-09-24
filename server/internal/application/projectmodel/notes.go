package projectmodel

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const RecordNoteToolName = "record_project_note"

func (s *Service) SaveUserNote(ctx context.Context, repoID uuid.UUID, req domain.SaveNoteRequest) (domain.ProjectNote, error) {
	if !domain.ValidNoteTopic(req.Topic) {
		return domain.ProjectNote{}, fmt.Errorf("%w: invalid topic %q", ErrInvalidInput, req.Topic)
	}
	if req.ComponentID != nil {
		comp, err := s.store.GetComponent(ctx, *req.ComponentID)
		if err != nil {
			return domain.ProjectNote{}, fmt.Errorf("save note: %w", err)
		}
		if comp.RepositoryID != repoID {
			return domain.ProjectNote{}, fmt.Errorf("%w: component belongs to a different repository", ErrInvalidInput)
		}
	}
	body := strings.TrimSpace(req.BodyMD)
	if body == "" {
		return domain.ProjectNote{}, fmt.Errorf("%w: body_md must not be empty", ErrInvalidInput)
	}

	note := domain.ProjectNote{
		ID:           uuid.New(),
		RepositoryID: repoID,
		ComponentID:  req.ComponentID,
		Topic:        req.Topic,
		BodyMD:       body,
		Author:       domain.NoteAuthorUser,
		Locked:       req.Locked,
		Stale:        false,
	}
	saved, err := s.store.SaveNote(ctx, note)
	if err != nil {
		return domain.ProjectNote{}, fmt.Errorf("save note: %w", err)
	}
	return saved, nil
}

func (s *Service) UpdateNote(ctx context.Context, noteID uuid.UUID, patch domain.NotePatch) (domain.ProjectNote, error) {
	note, err := s.store.GetNote(ctx, noteID)
	if err != nil {
		return domain.ProjectNote{}, fmt.Errorf("update note: %w", err)
	}
	if patch.BodyMD != nil {
		body := strings.TrimSpace(*patch.BodyMD)
		if body == "" {
			return domain.ProjectNote{}, fmt.Errorf("%w: body_md must not be empty", ErrInvalidInput)
		}
		note.BodyMD = body
	}
	if patch.Locked != nil {
		note.Locked = *patch.Locked
	}
	saved, err := s.store.SaveNote(ctx, note)
	if err != nil {
		return domain.ProjectNote{}, fmt.Errorf("update note: %w", err)
	}
	return saved, nil
}

func (s *Service) DeleteNote(ctx context.Context, noteID uuid.UUID) error {
	if err := s.store.DeleteNote(ctx, noteID); err != nil {
		return fmt.Errorf("delete note: %w", err)
	}
	return nil
}

// NoteWrite is one note an agent tries to record through RecordNoteToolName.
type NoteWrite struct {
	Topic         domain.NoteTopic
	BodyMD        string
	Evidence      []domain.SourceEvidence
	ComponentPath string
}

// NoteResult is the tool's per-write verdict, handed back to the agent so a
// rejected write can be corrected and resent in the same run.
type NoteResult struct {
	Topic         string `json:"topic"`
	ComponentPath string `json:"component_path,omitempty"`
	Accepted      bool   `json:"accepted"`
	Reason        string `json:"reason,omitempty"`
}

// RecordAgentNotes is the write path behind RecordNoteToolName: it validates
// every write against the current component list, the working copy on disk
// and whatever the human already locked or wrote, and only ever touches rows
// that pass every check.
func (s *Service) RecordAgentNotes(ctx context.Context, repoID uuid.UUID, writes []NoteWrite) ([]NoteResult, error) {
	repo, err := s.repos.Get(ctx, repoID)
	if err != nil {
		return nil, fmt.Errorf("record agent notes: %w", err)
	}
	components, err := s.store.ListComponents(ctx, repoID)
	if err != nil {
		return nil, fmt.Errorf("record agent notes: %w", err)
	}
	existing, err := s.store.ListNotes(ctx, repoID)
	if err != nil {
		return nil, fmt.Errorf("record agent notes: %w", err)
	}
	commit := noteSourceCommit(ctx, repo.RootPath)

	results := make([]NoteResult, 0, len(writes))
	accepted := 0
	for _, w := range writes {
		res := NoteResult{Topic: string(w.Topic), ComponentPath: w.ComponentPath}

		if !domain.ValidNoteTopic(w.Topic) {
			res.Reason = "unknown topic"
			results = append(results, res)
			continue
		}
		body := strings.TrimSpace(w.BodyMD)
		if body == "" {
			res.Reason = "body_md is empty"
			results = append(results, res)
			continue
		}

		var componentID *uuid.UUID
		if w.ComponentPath != "" {
			comp, ok := activeComponentByPath(components, w.ComponentPath)
			if !ok {
				res.Reason = fmt.Sprintf("no active component at path %q", w.ComponentPath)
				results = append(results, res)
				continue
			}
			componentID = &comp.ID
		}

		if prior, ok := findNoteByScope(existing, componentID, w.Topic); ok {
			if prior.Locked {
				res.Reason = "locked by the human"
				results = append(results, res)
				continue
			}
			if prior.Author == domain.NoteAuthorUser {
				res.Reason = "written by the human; ask them to unlock or edit it"
				results = append(results, res)
				continue
			}
		}

		valid, invalid := validNoteEvidence(repo.RootPath, w.Evidence)
		if len(valid) == 0 {
			reason := "every note needs at least one evidence path that exists in the repository"
			if len(invalid) > 0 {
				reason += "; not found: " + strings.Join(invalid, ", ")
			}
			res.Reason = reason
			results = append(results, res)
			continue
		}

		saved, err := s.store.SaveNote(ctx, domain.ProjectNote{
			RepositoryID: repoID,
			ComponentID:  componentID,
			Topic:        w.Topic,
			BodyMD:       body,
			Evidence:     valid,
			SourceCommit: commit,
			Stale:        false,
			Author:       domain.NoteAuthorAgent,
		})
		if err != nil {
			res.Reason = "save failed: " + err.Error()
			results = append(results, res)
			continue
		}
		existing = append(existing, saved)

		res.Accepted = true
		if len(invalid) > 0 {
			res.Reason = "stored without unverifiable evidence paths: " + strings.Join(invalid, ", ")
		}
		results = append(results, res)
		accepted++
	}

	if accepted > 0 {
		s.mu.Lock()
		s.notesWritten[repoID] += accepted
		s.mu.Unlock()
	}
	return results, nil
}

func activeComponentByPath(components []domain.Component, path string) (domain.Component, bool) {
	for _, c := range components {
		if c.Status == domain.ComponentStatusActive && c.Path == path {
			return c, true
		}
	}
	return domain.Component{}, false
}

func findNoteByScope(notes []domain.ProjectNote, componentID *uuid.UUID, topic domain.NoteTopic) (domain.ProjectNote, bool) {
	for _, n := range notes {
		if n.Topic != topic {
			continue
		}
		switch {
		case componentID == nil && n.ComponentID == nil:
			return n, true
		case componentID != nil && n.ComponentID != nil && *componentID == *n.ComponentID:
			return n, true
		}
	}
	return domain.ProjectNote{}, false
}

// validNoteEvidence keeps only the evidence paths that resolve inside root,
// stripping a trailing ":line" the way SourceEvidence.String() renders one so
// an agent can hand back exactly what a read-tool showed it.
func validNoteEvidence(root string, evidence []domain.SourceEvidence) (valid []domain.SourceEvidence, invalid []string) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, nil
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, nil
	}
	seen := map[string]bool{}
	for _, e := range evidence {
		raw := strings.TrimSpace(e.Path)
		if raw == "" || seen[raw] {
			continue
		}
		seen[raw] = true

		checkPath := raw
		if idx := strings.LastIndex(raw, ":"); idx > 0 {
			if _, err := strconv.Atoi(raw[idx+1:]); err == nil {
				checkPath = raw[:idx]
			}
		}
		if filepath.IsAbs(checkPath) {
			invalid = append(invalid, raw)
			continue
		}
		clean := filepath.Clean(filepath.FromSlash(checkPath))
		if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			invalid = append(invalid, raw)
			continue
		}
		if _, statErr := os.Stat(filepath.Join(absRoot, clean)); statErr != nil {
			invalid = append(invalid, raw)
			continue
		}
		valid = append(valid, e)
	}
	return valid, invalid
}

func noteSourceCommit(ctx context.Context, root string) string {
	if strings.TrimSpace(root) == "" {
		return ""
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "git", "rev-parse", "--short", "HEAD") //nolint:gosec
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
