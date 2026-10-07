package indexer

import (
	"context"
	"sync"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// cleanPassCommit returns the commit a finished pass leaves its stored hashes
// describing, or "" when there is none. The git shortcut trusts every hash git
// does not flag, which is only sound when those hashes are of committed
// content: a pass that indexed an uncommitted edit, later reverted, would leave
// stale chunks that neither git diff nor git status reveal. A pass over no
// files never reconciles the stored hashes, so it vouches for nothing.
func cleanPassCommit(paths []string, start, end *gitTreeState) string {
	if len(paths) == 0 || start == nil || end == nil || start.head == "" || start.head != end.head {
		return ""
	}
	if touchesIndexable(paths, start.dirty) || touchesIndexable(paths, end.dirty) {
		return ""
	}
	return start.head
}

func touchesIndexable(paths, dirty []string) bool {
	if len(dirty) == 0 {
		return false
	}
	indexable := make(map[string]struct{}, len(paths))
	for _, rel := range paths {
		indexable[rel] = struct{}{}
	}
	for _, rel := range dirty {
		if _, ok := indexable[rel]; ok {
			return true
		}
	}
	return false
}

// cleanPassLedger remembers, per index, the commit its last clean pass in this
// process vouched for. RestartIndexProject and RefreshIndexProject start a pass
// without waiting for the one they replace, so a pass that overlapped another
// on the same index neither uses nor records a commit: the other pass may have
// written chunks of content this one never saw.
type cleanPassLedger struct {
	mu    sync.Mutex
	slots map[uuid.UUID]*cleanPassSlot
}

type cleanPassSlot struct {
	commit  string
	running int
	starts  uint64
}

type passTicket struct {
	indexID uuid.UUID
	seq     uint64
	alone   bool
	trusted string
}

// begin forgets the recorded commit, so a pass that fails or is cancelled
// leaves nothing for the next one to trust.
func (l *cleanPassLedger) begin(indexID uuid.UUID) passTicket {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.slots == nil {
		l.slots = make(map[uuid.UUID]*cleanPassSlot)
	}
	slot := l.slots[indexID]
	if slot == nil {
		slot = &cleanPassSlot{}
		l.slots[indexID] = slot
	}
	slot.running++
	slot.starts++
	ticket := passTicket{indexID: indexID, seq: slot.starts, alone: slot.running == 1}
	if ticket.alone {
		ticket.trusted = slot.commit
	}
	slot.commit = ""
	return ticket
}

func (l *cleanPassLedger) end(ticket passTicket, commit string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	slot := l.slots[ticket.indexID]
	if slot == nil {
		return
	}
	slot.running--
	slot.commit = ""
	if commit != "" && ticket.alone && slot.starts == ticket.seq {
		slot.commit = commit
	}
	if slot.running == 0 && slot.commit == "" {
		delete(l.slots, ticket.indexID)
	}
}

type indexPassGit struct {
	ticket passTicket
	root   string
	start  *gitTreeState
	vouch  string
}

func (s *Service) beginGitPass(ctx context.Context, indexID uuid.UUID, root string) *indexPassGit {
	if !s.cfg.ReindexOnChange {
		return nil
	}
	pass := &indexPassGit{ticket: s.cleanPasses.begin(indexID), root: root}
	if state, ok := readGitTreeState(ctx, root); ok {
		pass.start = &state
	}
	return pass
}

func (s *Service) endGitPass(pass *indexPassGit) {
	if pass == nil {
		return
	}
	s.cleanPasses.end(pass.ticket, pass.vouch)
}

// changedFiles hashes only git's suspects when the index's commit is the one
// this process last saw a clean pass record, and every file otherwise.
func (p *indexPassGit) changedFiles(ctx context.Context, indexedCommit, absRoot string, paths []string, storedHashes map[string]string, force bool) FileChangeSet {
	if p != nil && !force && p.start != nil && p.ticket.trusted != "" && p.ticket.trusted == indexedCommit {
		if suspects, ok := gitSuspects(ctx, p.root, indexedCommit, *p.start, paths); ok {
			log.Debug().
				Str("index_id", p.ticket.indexID.String()).
				Int("files", len(paths)).
				Int("suspects", len(suspects)).
				Msg("hashing only the files git reports as changed since the indexed commit")
			return detectChangedFiles(absRoot, paths, storedHashes, func(rel string) bool {
				_, suspect := suspects[rel]
				return !suspect
			})
		}
	}
	return DetectChangedFiles(absRoot, paths, storedHashes)
}

func (p *indexPassGit) settle(ctx context.Context, paths []string) {
	if p == nil || p.start == nil || touchesIndexable(paths, p.start.dirty) {
		return
	}
	end, ok := readGitTreeState(ctx, p.root)
	if !ok {
		return
	}
	p.vouch = cleanPassCommit(paths, p.start, &end)
}
