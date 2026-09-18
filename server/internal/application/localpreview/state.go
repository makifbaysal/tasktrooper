package localpreview

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

// stateFileName records the one child process this service currently owns
// per repository, so a NEW Service (after this process crashes, is
// force-quit, or is replaced by an update) can find and reap what an OLD one
// left running instead of leaving it to squat a fixed dev-server port
// forever — see NewService's reapStale. The in-memory `active` map alone
// forgets everything on restart; the child itself, reparented by the OS,
// does not.
const stateFileName = "localpreview-state.json"

type persistedEntry struct {
	RepositoryID uuid.UUID `json:"repository_id"`
	PID          int       `json:"pid"`
}

func stateFilePath(workspaceRoot string) string {
	return filepath.Join(workspaceRoot, stateFileName)
}

// loadState reads what the previous process (if any) left recorded. Any
// problem reading or parsing it means nothing to reap — a missing or
// corrupt state file is not this function's problem to report, since the
// reap it enables is already best-effort.
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

// saveState overwrites the state file with exactly what is passed — callers
// hold s.mu and pass the full current picture, never a delta. A write
// failure just means the next restart's reap has stale or missing data,
// which is the same "best-effort" position loadState already takes.
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
