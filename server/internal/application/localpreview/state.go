package localpreview

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

const stateFileName = "localpreview-state.json"

type persistedEntry struct {
	RepositoryID uuid.UUID `json:"repository_id"`
	PID          int       `json:"pid"`
	// Command is what the next boot checks the pid still runs before
	// signalling it; an entry without one is never signalled.
	Command string `json:"command,omitempty"`
}

func stateFilePath(workspaceRoot string) string {
	return filepath.Join(workspaceRoot, stateFileName)
}

func loadState(workspaceRoot string) []persistedEntry {
	data, err := os.ReadFile(stateFilePath(workspaceRoot))
	if err != nil {
		return nil
	}
	var entries []persistedEntry
	if json.Unmarshal(data, &entries) != nil {
		return nil
	}
	return entries
}

func saveState(workspaceRoot string, entries []persistedEntry) {
	if workspaceRoot == "" {
		return
	}
	data, err := json.Marshal(entries)
	if err != nil {
		return
	}
	_ = os.WriteFile(stateFilePath(workspaceRoot), data, 0o600)
}
