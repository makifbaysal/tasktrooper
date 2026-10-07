package indexer

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

// skeletonCacheEntries bounds the cache: one entry per workspace an index is
// injected into, and a skeleton is at most a few thousand tokens of text.
const skeletonCacheEntries = 32

// skeletonCache keeps rendered code skeletons so a task's later runs (review,
// QA, revision) do not re-parse up to the mapper's file cap with tree-sitter
// every time. Entries are keyed by the index commit and by the state of the
// tree the skeleton was parsed from (see worktreeFingerprint), so a re-index
// or an edit in the workspace replaces them.
type skeletonCache struct {
	mu    sync.Mutex
	order *list.List
	items map[string]*list.Element
	limit int
}

type skeletonEntry struct {
	key      string
	skeleton string
}

func newSkeletonCache(limit int) *skeletonCache {
	return &skeletonCache{order: list.New(), items: make(map[string]*list.Element), limit: max(limit, 1)}
}

func (c *skeletonCache) get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return "", false
	}
	c.order.MoveToFront(el)
	return el.Value.(*skeletonEntry).skeleton, true
}

func (c *skeletonCache) put(key, skeleton string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		el.Value.(*skeletonEntry).skeleton = skeleton
		c.order.MoveToFront(el)
		return
	}
	c.items[key] = c.order.PushFront(&skeletonEntry{key: key, skeleton: skeleton})
	for c.order.Len() > c.limit {
		oldest := c.order.Back()
		c.order.Remove(oldest)
		delete(c.items, oldest.Value.(*skeletonEntry).key)
	}
}

// worktreeFingerprint names what the files under root hold right now: the
// checked-out commit, plus the size and modification time of every path git
// reports as changed or untracked. A workspace is edited by the runs that
// share it, long before the index catches up, so the index commit alone would
// hand a reviewer the skeleton of the code before the change. Unavailable
// (false) when root is not the top level of a git work tree.
func worktreeFingerprint(ctx context.Context, root string) (string, bool) {
	state, ok := readGitTreeState(ctx, root)
	if !ok || state.head == "" {
		return "", false
	}
	dirty := slices.Clone(state.dirty)
	slices.Sort(dirty)
	h := sha256.New()
	for _, rel := range dirty {
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			fmt.Fprintf(h, "%s\x00-\x00", rel)
			continue
		}
		fmt.Fprintf(h, "%s\x00%d\x00%d\x00", rel, info.Size(), info.ModTime().UnixNano())
	}
	return state.head + "+" + hex.EncodeToString(h.Sum(nil)[:12]), true
}
