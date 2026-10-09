package catalogrepo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const (
	manifestFile = "catalog.yaml"
	promptFile   = "prompt.md"
	skillsDir    = "skills"
	rulesDir     = "rules"
	columnsDir   = "columns"
)

// LocalDirMarker distinguishes a source read straight from a directory from one
// checked out from git, both on the sync-state RepoRef.
const LocalDirMarker = "dir:"

// Reader reads the external agent catalog. Source is either a git URL — cloned
// into CacheDir on first sync and fast-fetched after — or a local directory,
// which is read where it stands. Both are safe to run repeatedly.
type Reader struct {
	Source   string
	CacheDir string

	vectors skillVectorsCache
}

func (r *Reader) ReadCatalog(ctx context.Context) ([]domain.UpstreamAgent, string, error) {
	dir, ref, err := r.checkout(ctx)
	if err != nil {
		return nil, "", err
	}
	agents, err := r.readAgents(dir)
	return agents, ref, err
}

// checkout returns the directory to read and the ref this sync maps to. A
// local source needs no work; a git source is materialised under CacheDir with
// a fetch so the directory always holds the remote's current HEAD.
func (r *Reader) checkout(ctx context.Context) (string, string, error) {
	if st, err := os.Stat(r.Source); err == nil && st.IsDir() {
		return r.Source, LocalDirMarker + r.Source, nil
	}
	if dir, err := os.Stat(r.CacheDir); err != nil || !dir.IsDir() {
		if cloneErr := gitClone(ctx, r.Source, r.CacheDir); cloneErr != nil {
			return "", "", cloneErr
		}
	} else {
		if pullErr := gitPull(ctx, r.CacheDir); pullErr != nil {
			return "", "", pullErr
		}
	}
	sha, err := gitHead(ctx, r.CacheDir)
	if err != nil {
		return "", "", err
	}
	return r.CacheDir, "commit:" + sha, nil
}

func (r *Reader) readAgents(root string) ([]domain.UpstreamAgent, error) {
	agentsRoot := filepath.Join(root, "agents")
	dirs, err := os.ReadDir(agentsRoot)
	if err != nil {
		return nil, fmt.Errorf("catalog: %s missing or unreadable: %w", agentsRoot, err)
	}
	var out []domain.UpstreamAgent
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		slug := d.Name()
		agent, etag, err := readAgentDir(filepath.Join(agentsRoot, slug), slug)
		if err != nil {
			return nil, err
		}
		agent.Etag = etag
		out = append(out, agent)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out, nil
}
