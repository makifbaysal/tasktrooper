package reviewhistory

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// DiffFile is one changed file of the task's pull request as git counts it.
type DiffFile struct {
	Path           string
	Added, Deleted int
	Binary         bool
}

func (d DiffFile) lines() int { return d.Added + d.Deleted }

// InjectedDiffBytes is how much of the pull request's diff a review run is
// handed in its opening context (board.reviewDiffLimit).
const InjectedDiffBytes = 24000

const maxListedFiles = 50

// VisibleInInjectedDiff lists the files whose diff header lies inside the
// first limit bytes of the combined stat-plus-patch text a review run was
// handed, and whether the text was cut.
func VisibleInInjectedDiff(combined string, limit int) (files []string, truncated bool) {
	const marker = "diff --git a/"
	pos := 0
	for {
		i := strings.Index(combined[pos:], marker)
		if i < 0 {
			break
		}
		start := pos + i
		if start >= limit {
			truncated = true
			break
		}
		rest := combined[start+len(marker):]
		end := strings.IndexByte(rest, '\n')
		if end < 0 {
			end = len(rest)
		}
		header := rest[:end]
		if j := strings.Index(header, " b/"); j >= 0 {
			files = append(files, header[:j])
		}
		pos = start + len(marker)
	}
	if len(combined) > limit {
		truncated = true
	}
	return files, truncated
}

// matchesChanged finds the changed file a recorded path stands for: the same
// path, or the changed path as a tail of an absolute or prefixed one.
func matchesChanged(changed map[string]string, p string) (string, bool) {
	c := cleanPath(p)
	if c == "" {
		return "", false
	}
	if orig, ok := changed[c]; ok {
		return orig, true
	}
	for key, orig := range changed {
		if strings.HasSuffix(c, "/"+key) {
			return orig, true
		}
	}
	return "", false
}

var inspectingCommand = []string{"git diff", "git show", "git log", "git blame", "cat ", "sed ", "head ", "tail ", "less ", "bat ", "rg ", "grep "}

// openedFiles are the changed files the run's recorded calls read or
// inspected: file reads by path, and shell commands that show a file.
func openedFiles(events []domain.RunTraceEvent, changed map[string]string) map[string]bool {
	opened := map[string]bool{}
	for _, e := range events {
		if e.Status == domain.TraceStatusError {
			continue
		}
		switch e.Category {
		case domain.TraceCategoryRead:
			var d struct {
				Path string `json:"path"`
			}
			_ = json.Unmarshal(e.Detail, &d)
			p := d.Path
			if p == "" {
				p = e.Target
			}
			if orig, ok := matchesChanged(changed, p); ok {
				opened[orig] = true
			}
		case domain.TraceCategoryExec:
			var d struct {
				Command string `json:"command"`
			}
			_ = json.Unmarshal(e.Detail, &d)
			cmd := strings.TrimSpace(d.Command)
			inspecting := false
			for _, prefix := range inspectingCommand {
				if strings.Contains(cmd, prefix) {
					inspecting = true
					break
				}
			}
			if !inspecting {
				continue
			}
			for key, orig := range changed {
				if strings.Contains(strings.ToLower(cmd), key) {
					opened[orig] = true
				}
			}
		}
	}
	return opened
}

func toolErrors(events []domain.RunTraceEvent) []domain.ReviewNotReviewed {
	type agg struct {
		n      int
		reason string
	}
	by := map[string]*agg{}
	var order []string
	for _, e := range events {
		if e.Status != domain.TraceStatusError {
			continue
		}
		if e.Category != domain.TraceCategoryExec && e.Category != domain.TraceCategoryExternal {
			continue
		}
		a := by[e.Tool]
		if a == nil {
			a = &agg{reason: truncate(strings.SplitN(strings.TrimSpace(e.Error), "\n", 2)[0], 160)}
			by[e.Tool] = a
			order = append(order, e.Tool)
		}
		a.n++
	}
	var out []domain.ReviewNotReviewed
	for _, tool := range order {
		out = append(out, domain.ReviewNotReviewed{Scope: tool, Reason: by[tool].reason, Kind: "check", Source: "auto", Count: by[tool].n})
	}
	return out
}

func underScope(scope, file string) bool {
	s, f := cleanPath(scope), cleanPath(file)
	s = strings.TrimSuffix(s, "/")
	return s != "" && (f == s || strings.HasPrefix(f, s+"/") || strings.HasSuffix(f, "/"+s))
}

// roundChangedFiles is what the round's reviewers had to cover: the files the
// task's recorded runs changed up to the end of the round, plus (for the
// latest round) what git shows now.
func roundChangedFiles(in Inputs, r *domain.ReviewHistoryRound, latest bool) map[string]DiffFile {
	files := map[string]DiffFile{}
	for _, t := range in.Traces {
		if t.Verification == nil || !t.Verification.Available {
			continue
		}
		if r.LeftAt != nil && t.FinishedAt != nil && t.FinishedAt.After(*r.LeftAt) {
			continue
		}
		for _, f := range t.Verification.Files {
			files[f.Path] = DiffFile{Path: f.Path}
		}
	}
	current := map[string]DiffFile{}
	for _, d := range in.Diff {
		current[d.Path] = d
	}
	if latest {
		for p, d := range current {
			files[p] = d
		}
	} else {
		for p := range files {
			if d, ok := current[p]; ok {
				files[p] = d
			}
		}
	}
	return files
}

func fillCoverage(r *domain.ReviewHistoryRound, latestCodeReview bool, in Inputs) {
	var agents []int
	for i, rv := range r.Reviewers {
		if rv.AgentID != nil {
			agents = append(agents, i)
		}
	}
	if len(agents) == 0 || in.Rules == nil {
		return
	}
	isCode := r.Gate == string(domain.TaskColumnCodeReview)
	cov := &domain.RoundCoverage{Reviewers: []domain.ReviewerCoverage{}, NobodyReviewed: []domain.UncoveredFile{}}

	var files map[string]DiffFile
	changedKey := map[string]string{}
	if isCode {
		files = roundChangedFiles(in, r, latestCodeReview)
		for p := range files {
			changedKey[cleanPath(p)] = p
		}
	}
	diffKnown := isCode && latestCodeReview && in.Patch != ""
	visible := map[string]bool{}
	if diffKnown {
		vis, truncated := VisibleInInjectedDiff(in.Patch, InjectedDiffBytes)
		for _, p := range vis {
			if orig, ok := changedKey[cleanPath(p)]; ok {
				visible[orig] = true
			}
		}
		cov.DiffTruncated = truncated
	}

	var reviewable []string
	skipped := map[string]bool{}
	for p, d := range files {
		cov.ChangedFiles++
		cov.ChangedLines += d.lines()
		if d.Binary || in.Rules.isSkipped(p) {
			skipped[p] = true
			continue
		}
		reviewable = append(reviewable, p)
	}
	sort.Strings(reviewable)

	covered := map[string]bool{}
	for _, idx := range agents {
		rv := r.Reviewers[idx]
		rc := domain.ReviewerCoverage{
			AgentID: rv.AgentID, AgentName: rv.AgentName, RunID: rv.RunID, Basis: []string{}, Aspects: []string{},
			UnreadFiles: []string{}, NotReviewed: []domain.ReviewNotReviewed{}, DiffKnown: diffKnown,
		}
		var events []domain.RunTraceEvent
		if rv.RunID != nil && in.RunEvents != nil {
			events = in.RunEvents(*rv.RunID)
		}
		if len(events) > 0 {
			rc.Basis = append(rc.Basis, "trace")
		}
		aspects := map[string]bool{}
		for _, e := range events {
			if e.Tool == "load_skill" {
				if a := in.Rules.aspectOfSkill(e.Target); a != "" {
					aspects[a] = true
				}
			}
		}
		var declared []domain.TaskReviewFinding
		end := in.Now.Add(time.Hour)
		if r.LeftAt != nil {
			end = r.LeftAt.Add(time.Minute)
		}
		for _, row := range in.Findings {
			if row.Gate != r.Gate || row.AgentID == nil || *row.AgentID != *rv.AgentID || row.CreatedAt.Before(r.EnteredAt) || row.CreatedAt.After(end) {
				continue
			}
			switch row.Kind {
			case domain.ReviewItemAspect:
				aspects[row.Message] = true
				declared = append(declared, row)
			case domain.ReviewItemNotReviewed:
				declared = append(declared, row)
			case domain.ReviewItemFinding:
				if row.Category != "" && row.Category != domain.ReviewCategoryAcceptanceCriterion {
					aspects[row.Category] = true
				}
			}
		}
		if len(declared) > 0 {
			rc.Basis = append(rc.Basis, "declared")
		}
		for a := range aspects {
			rc.Aspects = append(rc.Aspects, a)
		}
		sort.Strings(rc.Aspects)

		if isCode {
			opened := openedFiles(events, changedKey)
			role := in.Rules.roleOf(rv.AgentName)
			reviewed := map[string]bool{}
			outOfRole := []string{}
			for _, p := range reviewable {
				if opened[p] {
					rc.FilesOpened++
				}
				if opened[p] || visible[p] {
					reviewed[p] = true
				}
				if role != nil && matchAny(role.outOfRole, cleanPath(p)) {
					outOfRole = append(outOfRole, p)
					delete(reviewed, p)
				}
			}
			if diffKnown {
				rc.Basis = append(rc.Basis, "diff")
			}
			// What the reviewer says it could not verify takes that coverage
			// back, whatever its tool calls touched.
			var scopes []string
			for _, row := range declared {
				if row.Kind != domain.ReviewItemNotReviewed {
					continue
				}
				scopes = append(scopes, row.Scope)
				for p := range reviewed {
					if underScope(row.Scope, p) {
						delete(reviewed, p)
					}
				}
			}
			rc.FilesTotal = len(reviewable)
			rc.FilesReviewed = len(reviewed)
			for p, d := range files {
				if skipped[p] {
					continue
				}
				rc.LinesTotal += d.lines()
				if reviewed[p] {
					rc.LinesReviewed += d.lines()
				}
			}
			for _, p := range reviewable {
				if reviewed[p] {
					covered[p] = true
					continue
				}
				if role != nil && matchAny(role.outOfRole, cleanPath(p)) {
					continue
				}
				rc.UnreadCount++
				if len(rc.UnreadFiles) < maxListedFiles {
					rc.UnreadFiles = append(rc.UnreadFiles, p)
				}
			}
			if len(outOfRole) > 0 {
				rc.NotReviewed = append(rc.NotReviewed, domain.ReviewNotReviewed{
					Scope: role.Aspect, Reason: "outside_role", Kind: "role", Source: "auto",
					Count: len(outOfRole), Files: firstN(outOfRole, maxFlagFiles),
				})
			}
			if len(skipped) > 0 {
				var list []string
				for p := range skipped {
					list = append(list, p)
				}
				sort.Strings(list)
				rc.NotReviewed = append(rc.NotReviewed, domain.ReviewNotReviewed{
					Scope: "generated", Reason: "skipped", Kind: "skipped", Source: "auto", Count: len(list), Files: firstN(list, maxFlagFiles),
				})
			}
			if lim := in.Rules.Coverage.LargeFileLines; lim > 0 {
				var big []string
				for _, p := range reviewable {
					if !opened[p] && files[p].lines() >= lim {
						big = append(big, p)
					}
				}
				if len(big) > 0 {
					rc.NotReviewed = append(rc.NotReviewed, domain.ReviewNotReviewed{
						Scope: "large", Reason: "too_large", Kind: "large", Source: "auto", Count: len(big), Files: firstN(big, maxFlagFiles),
					})
				}
			}
			if rc.DiffKnown && cov.DiffTruncated && len(visible) < len(reviewable) {
				rc.NotReviewed = append(rc.NotReviewed, domain.ReviewNotReviewed{Scope: "diff", Reason: "diff_truncated", Kind: "truncated", Source: "auto"})
			}
		} else {
			for _, c := range in.Criteria {
				if c.Canceled {
					continue
				}
				rc.CriteriaTotal++
				for _, ck := range c.Checks {
					if ck.Role == domain.CriterionReviewRoleQA && ck.AgentID != nil && *ck.AgentID == *rv.AgentID {
						rc.CriteriaChecked++
						break
					}
				}
			}
		}
		rc.NotReviewed = append(rc.NotReviewed, toolErrors(events)...)
		for _, row := range declared {
			if row.Kind == domain.ReviewItemNotReviewed {
				rc.NotReviewed = append(rc.NotReviewed, domain.ReviewNotReviewed{
					Scope: row.Scope, Reason: row.Reason, Kind: "declared", Source: "declared",
					FileURL: domain.ReviewFileURL(in.Task.PRURL, row.Scope, 0),
				})
			}
		}
		cov.Reviewers = append(cov.Reviewers, rc)
	}

	if isCode && diffKnown {
		for _, p := range reviewable {
			if !covered[p] {
				cov.NobodyReviewed = append(cov.NobodyReviewed, domain.UncoveredFile{
					Path: p, Lines: files[p].lines(), FileURL: domain.ReviewFileURL(in.Task.PRURL, p, 0),
				})
			}
		}
	}
	r.Coverage = cov
}
