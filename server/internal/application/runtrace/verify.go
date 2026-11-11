package runtrace

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type changeDetail struct {
	Path    string `json:"path"`
	From    string `json:"from"`
	To      string `json:"to"`
	Changed *bool  `json:"changed"`
}

func detailOf(raw json.RawMessage) changeDetail {
	var d changeDetail
	_ = json.Unmarshal(raw, &d)
	return d
}

func cleanPath(p string) string {
	if p == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(p))
}

func recordedChangePaths(events []domain.RunTraceEvent) map[string]bool {
	out := map[string]bool{}
	for _, ev := range events {
		if ev.Category != domain.TraceCategoryChange || ev.Status == domain.TraceStatusError {
			continue
		}
		d := detailOf(ev.Detail)
		for _, p := range []string{d.Path, d.From, d.To} {
			if p = cleanPath(p); p != "" && !filepath.IsAbs(p) {
				out[p] = true
			}
		}
	}
	return out
}

func readPaths(events []domain.RunTraceEvent) map[string]bool {
	out := map[string]bool{}
	for _, ev := range events {
		if ev.Category != domain.TraceCategoryRead {
			continue
		}
		if p := cleanPath(detailOf(ev.Detail).Path); p != "" && !filepath.IsAbs(p) {
			out[p] = true
		}
	}
	return out
}

// Compare sets what git shows changed against what the trace recorded.
func Compare(files []port.RunGitFile, changes, reads []domain.RunTraceEvent, tracked int) domain.RunTraceVerification {
	recorded := recordedChangePaths(changes)
	read := readPaths(reads)
	v := domain.RunTraceVerification{
		Available: true, Files: []domain.TraceFileChange{}, Unrecorded: []string{},
		RecordedOnly: []string{}, TrackedFiles: tracked,
	}
	inGit := map[string]bool{}
	for _, f := range files {
		p := cleanPath(f.Path)
		inGit[p] = true
		v.Files = append(v.Files, domain.TraceFileChange{Path: p, Status: f.Status, From: cleanPath(f.From)})
		if f.From != "" {
			inGit[cleanPath(f.From)] = true
		}
		if !recorded[p] && !(f.From != "" && recorded[cleanPath(f.From)]) {
			v.Unrecorded = append(v.Unrecorded, p)
		}
	}
	for _, ev := range changes {
		if ev.Category != domain.TraceCategoryChange || ev.Status == domain.TraceStatusError {
			continue
		}
		d := detailOf(ev.Detail)
		if d.Changed != nil && !*d.Changed {
			continue
		}
		for _, p := range []string{d.Path, d.To} {
			if p = cleanPath(p); p != "" && !filepath.IsAbs(p) && !inGit[p] {
				v.RecordedOnly = append(v.RecordedOnly, p)
				inGit[p] = true
			}
		}
	}
	sort.Slice(v.Files, func(i, j int) bool { return v.Files[i].Path < v.Files[j].Path })
	sort.Strings(v.Unrecorded)
	sort.Strings(v.RecordedOnly)
	touched := 0
	seen := map[string]bool{}
	for _, f := range v.Files {
		if f.Status != "A" && f.Status != "?" {
			touched++
			seen[f.Path] = true
		}
	}
	for p := range read {
		if !seen[p] {
			touched++
		}
	}
	v.Untouched = max(tracked-touched, 0)
	return v
}

func pagedEvents(ctx context.Context, store port.RunTraceStore, runID uuid.UUID, category string) []domain.RunTraceEvent {
	var out []domain.RunTraceEvent
	after := 0
	for {
		page, err := store.List(ctx, runID, port.RunTraceFilter{Category: category, After: after, Limit: 1000})
		if err != nil || len(page) == 0 {
			return out
		}
		out = append(out, page...)
		after = page[len(page)-1].Seq
		if len(page) < 1000 {
			return out
		}
	}
}

// Finalize reads git once the run is over and stamps the comparison on the
// run's trace. Without a git checkout it still closes the trace and says why
// the comparison is missing.
func Finalize(ctx context.Context, store port.RunTraceStore, git port.RunGitInspector, meta Meta, dropped int) {
	if store == nil || meta.RunID == uuid.Nil {
		return
	}
	pctx, cancel := persistCtx(ctx)
	defer cancel()
	v := domain.RunTraceVerification{ComputedAt: time.Now(), BaseSHA: meta.BaseSHA, Files: []domain.TraceFileChange{}, Unrecorded: []string{}, RecordedOnly: []string{}}
	head := ""
	switch {
	case git == nil || meta.WorkspacePath == "":
		v.Reason = "no_checkout"
	case meta.BaseSHA == "":
		v.Reason = "no_base"
	default:
		files, err := git.ChangesSince(pctx, meta.WorkspacePath, meta.BaseSHA)
		if err != nil {
			v.Reason = "git_failed"
			break
		}
		head, _ = git.HeadSHA(pctx, meta.WorkspacePath)
		tracked, _ := git.TrackedFileCount(pctx, meta.WorkspacePath, meta.BaseSHA)
		changes := pagedEvents(pctx, store, meta.RunID, domain.TraceCategoryChange)
		reads := pagedEvents(pctx, store, meta.RunID, domain.TraceCategoryRead)
		v = Compare(files, changes, reads, tracked)
		v.ComputedAt, v.BaseSHA, v.HeadSHA = time.Now(), meta.BaseSHA, head
	}
	_ = store.Finish(pctx, meta.RunID, head, dropped, &v)
}

func SummaryEffects(events []domain.RunTraceEvent) []domain.RunTraceEffect {
	out := []domain.RunTraceEffect{}
	for _, ev := range events {
		var d struct {
			Effect string `json:"effect"`
		}
		_ = json.Unmarshal(ev.Detail, &d)
		if d.Effect == "" || ev.Status == domain.TraceStatusError {
			continue
		}
		out = append(out, domain.RunTraceEffect{Seq: ev.Seq, Tool: ev.Tool, Kind: d.Effect, Target: strings.TrimSpace(ev.Target)})
	}
	return out
}
