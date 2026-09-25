package projectmodel

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func workflowBasename(workflow string) string {
	return path.Base(workflow)
}

// briefBudget is the character ceiling the rendered markdown must stay under;
// an agent's context is the scarce resource this exists to protect.
const briefBudget = 8000

// BriefScope narrows Brief to one component, one area, or (both nil/empty)
// every active component of the repository.
type BriefScope struct {
	ComponentID *uuid.UUID
	Area        string
}

// Brief renders the on-demand repository overview get_project_brief returns:
// what it is, what each in-scope component runs, and what it talks to. It
// never fails on a broken link — only a store error does.
func (s *Service) Brief(ctx context.Context, repoID uuid.UUID, scope BriefScope) (string, error) {
	repo, err := s.repos.Get(ctx, repoID)
	if err != nil {
		return "", fmt.Errorf("brief: %w", err)
	}
	all, err := s.store.ListComponents(ctx, repoID)
	if err != nil {
		return "", fmt.Errorf("brief: %w", err)
	}
	active := activeComponents(all)
	if len(active) == 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "# %s\n", repo.Name)
		if repo.Description != "" {
			fmt.Fprintf(&b, "%s\n", repo.Description)
		}
		b.WriteString("(The project model has not been scanned yet.)")
		return b.String(), nil
	}

	inScope, err := s.briefScopeComponents(ctx, repoID, all, active, scope)
	if err != nil {
		return "", err
	}
	sort.Slice(inScope, func(i, j int) bool { return inScope[i].Path < inScope[j].Path })

	checks, err := s.store.ListChecks(ctx, repoID)
	if err != nil {
		return "", fmt.Errorf("brief: %w", err)
	}
	links, err := s.store.ListLinks(ctx, repoID)
	if err != nil {
		return "", fmt.Errorf("brief: %w", err)
	}
	incoming, err := s.store.ListIncomingLinks(ctx, repoID)
	if err != nil {
		return "", fmt.Errorf("brief: %w", err)
	}
	environments, err := s.listEnvironments(ctx, repoID)
	if err != nil {
		return "", fmt.Errorf("brief: %w", err)
	}

	header := s.briefHeader(repo, active, inScope)
	if gitBlock := s.briefGitBlock(ctx, repo.ID); gitBlock != "" {
		header += "\n## Git\n" + gitBlock
	}
	if docs := briefReferenceDocs(repo.Docs, domain.RepositoryDocs{}); docs != "" {
		header += "\n## Reference docs\n" + docs
	}
	blocks := make([]string, 0, len(inScope))
	for _, c := range inScope {
		blocks = append(blocks, s.briefComponentBlock(ctx, repo, c, checks, links, incoming, environments))
	}

	return truncateBriefBlocks(header, blocks), nil
}

func activeComponents(components []domain.Component) []domain.Component {
	out := make([]domain.Component, 0, len(components))
	for _, c := range components {
		if c.Status == domain.ComponentStatusActive {
			out = append(out, c)
		}
	}
	return out
}

func (s *Service) briefScopeComponents(ctx context.Context, repoID uuid.UUID, all, active []domain.Component, scope BriefScope) ([]domain.Component, error) {
	if scope.ComponentID != nil {
		for _, c := range all {
			if c.ID == *scope.ComponentID {
				return []domain.Component{c}, nil
			}
		}
		comp, err := s.store.GetComponent(ctx, *scope.ComponentID)
		if err != nil {
			return nil, fmt.Errorf("brief: %w", err)
		}
		if comp.RepositoryID != repoID {
			return nil, fmt.Errorf("%w: component belongs to a different repository", ErrInvalidInput)
		}
		return []domain.Component{comp}, nil
	}
	if scope.Area != "" {
		return resolveAreaComponents(active, scope.Area), nil
	}
	return active, nil
}

// resolveAreaComponents is the one matching rule Brief and ComponentsForArea
// both use: an active component is in the area when its role or the role's
// legacy repo kind names it; an area nothing matches falls back to every
// active component rather than returning an empty scope.
func resolveAreaComponents(active []domain.Component, area string) []domain.Component {
	var matched []domain.Component
	for _, c := range active {
		role := c.Role.Get()
		if string(role) == area || role.LegacyRepoKind() == area {
			matched = append(matched, c)
		}
	}
	if len(matched) == 0 {
		return active
	}
	return matched
}

func (s *Service) briefHeader(repo domain.Repository, active, inScope []domain.Component) string {
	var b strings.Builder
	if len(inScope) == 1 {
		c := inScope[0]
		if c.Path == "." || c.Path == "" {
			fmt.Fprintf(&b, "# %s (%s)\n", repo.Name, c.Role.Get())
		} else {
			fmt.Fprintf(&b, "# %s — %s (%s)\n", repo.Name, c.Path, c.Role.Get())
		}
	} else {
		fmt.Fprintf(&b, "# %s\n", repo.Name)
	}

	if len(active) <= 1 {
		b.WriteString("Single repository.")
	} else {
		parts := make([]string, 0, len(active))
		for _, c := range active {
			parts = append(parts, fmt.Sprintf("%s (%s)", c.DisplayName(), c.Role.Get()))
		}
		fmt.Fprintf(&b, "Monorepo with %d components: %s.", len(active), strings.Join(parts, ", "))
	}
	if repo.Description != "" {
		fmt.Fprintf(&b, " %s", repo.Description)
	}
	b.WriteString("\n")
	return b.String()
}

// briefGitBlock reads the latest succeeded scan's git conventions straight
// off the stored ScanResult — nothing here is re-derived or persisted
// separately, so it goes stale exactly when the scan does.
func (s *Service) briefGitBlock(ctx context.Context, repoID uuid.UUID) string {
	latest, err := s.store.LatestScan(ctx, repoID)
	if err != nil || latest.Status != domain.ScanSucceeded {
		return ""
	}
	scan, err := s.store.GetScan(ctx, latest.ID)
	if err != nil || scan.Result == nil {
		return ""
	}
	return renderGitFacts(scan.Result.Git)
}

func renderGitFacts(g domain.ScanGit) string {
	var b strings.Builder
	if g.DefaultBranch != "" {
		fmt.Fprintf(&b, "- Default branch: %s\n", g.DefaultBranch)
	}
	if line := branchNamingLine(g); line != "" {
		b.WriteString(line)
	}
	if g.MergeStyle != "" {
		fmt.Fprintf(&b, "- Merge style: %s\n", g.MergeStyle)
	}
	if g.DirectToMain && g.DefaultBranch != "" {
		fmt.Fprintf(&b, "- History lands directly on %s\n", g.DefaultBranch)
	}
	if g.CommitStyle != "" {
		fmt.Fprintf(&b, "- Commit style: %s\n", g.CommitStyle)
	}
	if len(g.Hotspots) > 0 {
		top := g.Hotspots
		if len(top) > 5 {
			top = top[:5]
		}
		parts := make([]string, 0, len(top))
		for _, h := range top {
			parts = append(parts, fmt.Sprintf("%s (%d)", h.Path, h.Commits))
		}
		fmt.Fprintf(&b, "- Hotspots: %s\n", strings.Join(parts, ", "))
	}
	return b.String()
}

// branchNamingLine turns discovery's "<prefix>/* prefix — N of M recent
// branches" sentence into a template line an agent can act on directly
// instead of parsing prose.
func branchNamingLine(g domain.ScanGit) string {
	if g.BranchPattern == "" {
		return ""
	}
	prefix := g.BranchPattern
	if i := strings.Index(prefix, " — "); i >= 0 {
		prefix = prefix[:i]
	}
	label := prefix
	if trimmed, ok := strings.CutSuffix(prefix, "/* prefix"); ok {
		label = trimmed + "/<slug>"
	}
	return fmt.Sprintf("- Branch naming: %s (from %d recent branches)\n", label, len(g.BranchSamples))
}

func (s *Service) briefComponentBlock(ctx context.Context, repo domain.Repository, c domain.Component, checks []domain.ComponentCheck, links, incoming []domain.ComponentLink, environments []domain.ComponentEnvironment) string {
	var b strings.Builder
	heading := c.Path
	if heading == "." || heading == "" {
		heading = repo.Name
	}
	fmt.Fprintf(&b, "\n## %s (%s)\n", heading, c.Role.Get())

	if stack := briefStackLine(c.Stack.Get()); stack != "" {
		b.WriteString(stack + "\n")
	}

	if cmds := briefCommandLines(c); cmds != "" {
		where := "inside " + c.Path
		if c.Path == "." || c.Path == "" {
			where = "at the repository root"
		}
		fmt.Fprintf(&b, "Commands (run %s):\n%s", where, cmds)
	}

	if runsOn := briefRunsOnLines(environments, c.ID); runsOn != "" {
		b.WriteString("Runs on:\n" + runsOn)
	}

	componentChecks := checksForComponent(checks, c.ID)
	if required := briefRequiredCheckLines(componentChecks); required != "" {
		b.WriteString("Before handing off, run what CI runs:\n" + required)
	}
	if other := briefInformativeCheckNames(componentChecks); other != "" {
		fmt.Fprintf(&b, "Other CI checks: %s (informative)\n", other)
	}

	if talks := s.briefTalksTo(ctx, links, c.ID); talks != "" {
		b.WriteString("Talks to:\n" + talks)
	}
	if calledBy := s.briefCalledBy(ctx, incoming, c.ID); calledBy != "" {
		b.WriteString("Called by:\n" + calledBy)
	}

	if docs := briefReferenceDocs(domain.RepositoryDocs{}, c.Docs); docs != "" {
		b.WriteString("### Reference docs\n" + docs)
	}

	return b.String()
}

func briefStackLine(stack domain.ComponentStack) string {
	var parts []string
	if summary := stack.Summary(6); summary != "" {
		parts = append(parts, "Stack: "+summary)
	}
	if stack.Runtime != nil && stack.Runtime.Name != "" {
		parts = append(parts, "runtime "+stack.Runtime.Name)
	}
	if stack.PackageManager != "" {
		parts = append(parts, "package manager "+stack.PackageManager)
	}
	if stack.Container != "" {
		parts = append(parts, "container "+stack.Container)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "; ")
}

func briefCommandLines(c domain.Component) string {
	var b strings.Builder
	for _, cmd := range c.Commands {
		value := cmd.Command.Get()
		if value == "" {
			continue
		}
		fmt.Fprintf(&b, "- %s: `%s`\n", cmd.Purpose, value)
	}
	return b.String()
}

// deployEnvironmentPriority orders a component's environments the same way
// the Deploy & Runtime tab does (production first), regardless of what order
// the store or a fake test reader happens to return them in.
func deployEnvironmentPriority(env domain.DeployEnvironment) int {
	switch env {
	case domain.EnvironmentProduction:
		return 0
	case domain.EnvironmentStaging:
		return 1
	case domain.EnvironmentPreview:
		return 2
	case domain.EnvironmentDevelopment:
		return 3
	}
	return 4
}

// briefRunsOnLines lists a component's confirmed environments and, when any
// is bound to a cloud resource, points the agent at the runtime tools instead
// of leaving it to guess whether logs are even reachable.
func briefRunsOnLines(environments []domain.ComponentEnvironment, componentID uuid.UUID) string {
	var mine []domain.ComponentEnvironment
	for _, e := range environments {
		if e.ComponentID == componentID && e.Status == domain.LinkConfirmed {
			mine = append(mine, e)
		}
	}
	if len(mine) == 0 {
		return ""
	}
	sort.Slice(mine, func(i, j int) bool {
		return deployEnvironmentPriority(mine[i].Environment) < deployEnvironmentPriority(mine[j].Environment)
	})

	var b strings.Builder
	anyBound := false
	for _, e := range mine {
		if e.Bound() {
			anyBound = true
		}
		label := strings.TrimSpace(string(e.Provider))
		if e.Resource != nil && e.Resource.Name != "" {
			label = strings.TrimSpace(label + " " + e.Resource.Name)
		}
		if label == "" {
			fmt.Fprintf(&b, "- %s: %s\n", e.Environment, e.URL)
		} else {
			fmt.Fprintf(&b, "- %s: %s — %s\n", e.Environment, label, e.URL)
		}
	}
	if anyBound {
		b.WriteString("Logs and errors: query_runtime_logs, list_runtime_errors\n")
	}
	return b.String()
}

func checksForComponent(checks []domain.ComponentCheck, componentID uuid.UUID) []domain.ComponentCheck {
	var out []domain.ComponentCheck
	for _, chk := range checks {
		if chk.ComponentID == componentID {
			out = append(out, chk)
		}
	}
	return out
}

func checkLabel(chk domain.ComponentCheck) string {
	name := chk.JobName
	if name == "" {
		name = chk.JobKey
	}
	if chk.Workflow == "" {
		return name
	}
	return workflowBasename(chk.Workflow) + " › " + name
}

func briefRequiredCheckLines(checks []domain.ComponentCheck) string {
	var b strings.Builder
	for _, chk := range checks {
		if !chk.Required() {
			continue
		}
		cmds := chk.LocalCommands.Get()
		if len(cmds) == 0 {
			continue
		}
		fmt.Fprintf(&b, "- %s → `%s`\n", checkLabel(chk), domain.JoinLocalCommands(cmds))
	}
	return b.String()
}

func briefInformativeCheckNames(checks []domain.ComponentCheck) string {
	var names []string
	for _, chk := range checks {
		if chk.Status != domain.ModelStatusActive || chk.Missing || chk.Gate.Get() != domain.CheckGateInfo {
			continue
		}
		names = append(names, checkLabel(chk))
	}
	return strings.Join(names, ", ")
}

// briefTalksToEntry accumulates every confirmed link this component has to
// one target label; a merge can leave several links pointing at the same
// resource, so they render as one line instead of one per link.
type briefTalksToEntry struct {
	protocols []domain.LinkProtocol
	envVars   []string
}

func (s *Service) briefTalksTo(ctx context.Context, links []domain.ComponentLink, componentID uuid.UUID) string {
	byLabel := map[string]*briefTalksToEntry{}
	var order []string
	for _, l := range links {
		if l.FromComponentID != componentID || l.Status != domain.LinkConfirmed {
			continue
		}
		label := s.briefLinkTargetLabel(ctx, l)
		if label == "" {
			continue
		}
		entry, ok := byLabel[label]
		if !ok {
			entry = &briefTalksToEntry{}
			byLabel[label] = entry
			order = append(order, label)
		}
		if !containsProtocol(entry.protocols, l.Protocol) {
			entry.protocols = append(entry.protocols, l.Protocol)
		}
		for _, ev := range l.EnvVars {
			if !containsString(entry.envVars, ev) {
				entry.envVars = append(entry.envVars, ev)
			}
		}
	}

	var b strings.Builder
	for _, label := range order {
		entry := byLabel[label]
		protocols := make([]string, len(entry.protocols))
		for i, p := range entry.protocols {
			protocols[i] = string(p)
		}
		extra := ""
		if len(entry.envVars) > 0 {
			extra = ", env " + strings.Join(entry.envVars, ", ")
		}
		fmt.Fprintf(&b, "- %s (%s%s)\n", label, strings.Join(protocols, ", "), extra)
	}
	return b.String()
}

func (s *Service) briefLinkTargetLabel(ctx context.Context, l domain.ComponentLink) string {
	switch {
	case l.ToResourceID != nil:
		res, err := s.store.GetResource(ctx, *l.ToResourceID)
		if err != nil {
			return l.Hint
		}
		if res.Name != "" {
			return res.Name
		}
		return res.Vendor
	case l.ToComponentID != nil:
		comp, err := s.store.GetComponent(ctx, *l.ToComponentID)
		if err != nil {
			return l.Hint
		}
		repo, err := s.repos.Get(ctx, comp.RepositoryID)
		if err != nil {
			return l.Hint
		}
		return repo.Name + "/" + comp.Path
	default:
		return l.Hint
	}
}

func (s *Service) briefCalledBy(ctx context.Context, incoming []domain.ComponentLink, componentID uuid.UUID) string {
	var b strings.Builder
	for _, l := range incoming {
		if l.ToComponentID == nil || *l.ToComponentID != componentID || l.Status != domain.LinkConfirmed {
			continue
		}
		comp, err := s.store.GetComponent(ctx, l.FromComponentID)
		if err != nil {
			continue
		}
		repo, err := s.repos.Get(ctx, comp.RepositoryID)
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "- %s/%s (%s)\n", repo.Name, comp.Path, l.Protocol)
	}
	return b.String()
}

func briefReferenceDocs(repoDocs, componentDocs domain.RepositoryDocs) string {
	type doc struct {
		label string
		path  string
	}
	pick := func(component, repo string) string {
		if component != "" {
			return component
		}
		return repo
	}
	docs := []doc{
		{"coding standards", pick(componentDocs.CodingStandards, repoDocs.CodingStandards)},
		{"test standards", pick(componentDocs.TestStandards, repoDocs.TestStandards)},
		{"architecture", pick(componentDocs.Architecture, repoDocs.Architecture)},
		{"local run", pick(componentDocs.LocalRun, repoDocs.LocalRun)},
	}
	var b strings.Builder
	for _, d := range docs {
		if d.path == "" {
			continue
		}
		fmt.Fprintf(&b, "- %s: %s\n", d.label, d.path)
	}
	return b.String()
}

func truncateBriefBlocks(header string, blocks []string) string {
	total := header
	for _, blk := range blocks {
		total += blk
	}
	if len(total) <= briefBudget {
		return total
	}
	kept := append([]string(nil), blocks...)
	for len(kept) > 0 && len(header)+joinedLen(kept) > briefBudget {
		kept = kept[:len(kept)-1]
	}
	out := header
	for _, blk := range kept {
		out += blk
	}
	return out
}

func joinedLen(blocks []string) int {
	n := 0
	for _, b := range blocks {
		n += len(b)
	}
	return n
}

func (s *Service) RequiredCommands(ctx context.Context, repoID uuid.UUID, componentIDs []uuid.UUID) ([]domain.LocalCommand, error) {
	checks, err := s.store.ListChecks(ctx, repoID)
	if err != nil {
		return nil, fmt.Errorf("required commands: %w", err)
	}

	scope := map[uuid.UUID]bool{}
	for _, id := range componentIDs {
		scope[id] = true
	}
	if len(scope) == 0 {
		components, err := s.store.ListComponents(ctx, repoID)
		if err != nil {
			return nil, fmt.Errorf("required commands: %w", err)
		}
		for _, c := range components {
			if c.Status == domain.ComponentStatusActive {
				scope[c.ID] = true
			}
		}
	}

	seen := map[string]bool{}
	var out []domain.LocalCommand
	for _, chk := range checks {
		if !chk.Required() || !scope[chk.ComponentID] {
			continue
		}
		for _, cmd := range chk.LocalCommands.Get() {
			key := cmd.Dir + "\x00" + strings.Join(cmd.Argv, "\x00")
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, cmd)
		}
	}
	return out, nil
}

func (s *Service) ComponentsForPaths(ctx context.Context, repoID uuid.UUID, paths []string) ([]uuid.UUID, error) {
	components, err := s.store.ListComponents(ctx, repoID)
	if err != nil {
		return nil, fmt.Errorf("components for paths: %w", err)
	}
	seen := map[uuid.UUID]bool{}
	var out []uuid.UUID
	for _, p := range paths {
		comp, ok := domain.OwningComponent(components, p)
		if !ok || seen[comp.ID] {
			continue
		}
		seen[comp.ID] = true
		out = append(out, comp.ID)
	}
	return out, nil
}

func (s *Service) ComponentsForArea(ctx context.Context, repoID uuid.UUID, area string) ([]uuid.UUID, error) {
	components, err := s.store.ListComponents(ctx, repoID)
	if err != nil {
		return nil, fmt.Errorf("components for area: %w", err)
	}
	matched := resolveAreaComponents(activeComponents(components), area)
	out := make([]uuid.UUID, 0, len(matched))
	for _, c := range matched {
		out = append(out, c.ID)
	}
	return out, nil
}
