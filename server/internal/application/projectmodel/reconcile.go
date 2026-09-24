package projectmodel

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// reconcileSummary is what runScan needs to compose its finished-scan event
// and the ProjectScan.ReviewCount column.
type reconcileSummary struct {
	Components  int
	Checks      int
	Links       int
	ReviewCount int
}

// reconcileScan loads the repository's current model, ensures every
// resource a detected link points at exists, plans the update with the pure
// planReconcile, and applies it atomically.
func (s *Service) reconcileScan(ctx context.Context, repo domain.Repository, scanID uuid.UUID, trigger domain.ScanTrigger, result domain.ScanResult) (reconcileSummary, error) {
	existingComponents, err := s.store.ListComponents(ctx, repo.ID)
	if err != nil {
		return reconcileSummary{}, fmt.Errorf("list components: %w", err)
	}
	existingChecks, err := s.store.ListChecks(ctx, repo.ID)
	if err != nil {
		return reconcileSummary{}, fmt.Errorf("list checks: %w", err)
	}
	existingLinks, err := s.store.ListLinks(ctx, repo.ID)
	if err != nil {
		return reconcileSummary{}, fmt.Errorf("list links: %w", err)
	}
	allComponents, err := s.store.ListAllComponents(ctx)
	if err != nil {
		return reconcileSummary{}, fmt.Errorf("list all components: %w", err)
	}
	repos, err := s.repos.List(ctx)
	if err != nil {
		return reconcileSummary{}, fmt.Errorf("list repositories: %w", err)
	}

	resources, err := s.ensureResources(ctx, repo.ID, result.Links)
	if err != nil {
		return reconcileSummary{}, fmt.Errorf("ensure resources: %w", err)
	}

	in := planInput{
		repositoryID:       repo.ID,
		scanID:             scanID,
		now:                s.now(),
		result:             result,
		existingComponents: existingComponents,
		existingChecks:     existingChecks,
		existingLinks:      existingLinks,
		resources:          resources,
		matchCandidates:    buildMatchCandidates(repo, allComponents, repos),
		// flagNewRows: a later scan (not the first import, not a migrate replay)
		// on a repository that already had a model adds rows the human hasn't
		// seen yet, so they're queued for review instead of landing silently.
		flagNewRows: trigger != domain.ScanTriggerImport && trigger != domain.ScanTriggerMigrate && len(existingComponents) > 0,
	}
	out := planReconcile(in)

	if err := s.store.ApplyReconcile(ctx, port.ModelReconcile{
		RepositoryID:     repo.ID,
		SaveComponents:   out.saveComponents,
		DeleteComponents: out.deleteComponents,
		SaveChecks:       out.saveChecks,
		DeleteChecks:     out.deleteChecks,
		SaveLinks:        out.saveLinks,
		DeleteLinks:      out.deleteLinks,
	}); err != nil {
		return reconcileSummary{}, fmt.Errorf("apply reconcile: %w", err)
	}

	return reconcileSummary{
		Components: len(out.finalComponents),
		Checks:     len(out.finalChecks),
		Links:      len(out.finalLinks),
		// Environments this scan's own MatchScan will bind are not visible yet:
		// MatchScan runs after reconcileScan returns (see runScan), so the
		// review count here reflects environments as of the previous scan.
		ReviewCount: len(reviewItems(out.finalComponents, out.finalChecks, out.finalLinks, nil)),
	}, nil
}

// buildMatchCandidates is every other repository's active, non-dismissed
// component, tagged with whether its repository shares a project with repo.
func buildMatchCandidates(repo domain.Repository, allComponents []domain.Component, repos []domain.Repository) []matchCandidate {
	repoByID := make(map[uuid.UUID]domain.Repository, len(repos))
	for _, r := range repos {
		repoByID[r.ID] = r
	}
	sourceProjects := make(map[uuid.UUID]bool, len(repo.ProjectIDs))
	for _, id := range repo.ProjectIDs {
		sourceProjects[id] = true
	}

	out := make([]matchCandidate, 0, len(allComponents))
	for _, c := range allComponents {
		if c.RepositoryID == repo.ID || c.Status == domain.ComponentStatusDismissed {
			continue
		}
		r, ok := repoByID[c.RepositoryID]
		if !ok {
			continue
		}
		shares := false
		for _, id := range r.ProjectIDs {
			if sourceProjects[id] {
				shares = true
				break
			}
		}
		out = append(out, matchCandidate{
			Component:      c,
			RepositoryID:   r.ID,
			RepositoryName: r.Name,
			SharesProject:  shares,
		})
	}
	return out
}

type planInput struct {
	repositoryID uuid.UUID
	scanID       uuid.UUID
	now          time.Time
	result       domain.ScanResult

	existingComponents []domain.Component
	existingChecks     []domain.ComponentCheck
	existingLinks      []domain.ComponentLink

	// resources maps a resource-kind detected link's (componentPath|signalKey)
	// to the SystemResource id ensureResources already stored.
	resources map[string]uuid.UUID

	matchCandidates []matchCandidate

	// flagNewRows marks a component or required check this reconcile CREATES
	// (not merges into an existing row) with NeedsReview, so it queues for the
	// human instead of landing silently.
	flagNewRows bool
}

type planOutput struct {
	saveComponents   []domain.Component
	deleteComponents []uuid.UUID
	saveChecks       []domain.ComponentCheck
	deleteChecks     []uuid.UUID
	saveLinks        []domain.ComponentLink
	deleteLinks      []uuid.UUID

	// final* is the repository's whole model after this reconcile: existing
	// untouched rows plus every saved row, minus every deleted one. review.go
	// and the scan summary read these instead of round-tripping the store.
	finalComponents []domain.Component
	finalChecks     []domain.ComponentCheck
	finalLinks      []domain.ComponentLink
}

// planReconcile is pure: same input always produces the same output, so it
// is exercised directly by table tests instead of through the store.
func planReconcile(in planInput) planOutput {
	components := reconcileComponents(in)
	componentByPath := make(map[string]domain.Component, len(components.byID))
	for _, c := range components.byID {
		componentByPath[c.Path] = c
	}

	checks := reconcileChecks(in, componentByPath)
	links := reconcileLinks(in, componentByPath)

	deletedComponents := toSet(components.deleteIDs)
	checks.saveChecks, checks.deleteChecks, checks.finalChecks = dropForDeletedComponents(
		checks.saveChecks, checks.deleteChecks, checks.finalChecks, deletedComponents, checkComponentID)
	links.saveLinks, links.deleteLinks, links.finalLinks = dropForDeletedComponents(
		links.saveLinks, links.deleteLinks, links.finalLinks, deletedComponents, linkFromComponentID)

	return planOutput{
		saveComponents:   components.save,
		deleteComponents: components.deleteIDs,
		saveChecks:       checks.saveChecks,
		deleteChecks:     checks.deleteChecks,
		saveLinks:        links.saveLinks,
		deleteLinks:      links.deleteLinks,
		finalComponents:  components.final,
		finalChecks:      checks.finalChecks,
		finalLinks:       links.finalLinks,
	}
}

func toSet(ids []uuid.UUID) map[uuid.UUID]bool {
	out := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}

func checkComponentID(c domain.ComponentCheck) uuid.UUID   { return c.ComponentID }
func linkFromComponentID(l domain.ComponentLink) uuid.UUID { return l.FromComponentID }

// dropForDeletedComponents excludes rows whose owning component was just
// deleted from save/final: the DB's ON DELETE CASCADE removes them once the
// component delete lands, so writing or counting them here would be wrong.
func dropForDeletedComponents[T any](save []T, deleteIDs []uuid.UUID, final []T, deletedComponents map[uuid.UUID]bool, ownerID func(T) uuid.UUID) ([]T, []uuid.UUID, []T) {
	filteredSave := save[:0:0]
	for _, row := range save {
		if !deletedComponents[ownerID(row)] {
			filteredSave = append(filteredSave, row)
		}
	}
	filteredFinal := final[:0:0]
	for _, row := range final {
		if !deletedComponents[ownerID(row)] {
			filteredFinal = append(filteredFinal, row)
		}
	}
	return filteredSave, deleteIDs, filteredFinal
}

// --- components ---

type componentPlan struct {
	save      []domain.Component
	deleteIDs []uuid.UUID
	final     []domain.Component
	byID      map[uuid.UUID]domain.Component
}

func reconcileComponents(in planInput) componentPlan {
	existingByPath := make(map[string]domain.Component, len(in.existingComponents))
	for _, c := range in.existingComponents {
		existingByPath[c.Path] = c
	}
	detectedByPath := make(map[string]domain.DetectedComponent, len(in.result.Components))
	for _, d := range in.result.Components {
		detectedByPath[d.Path] = d
	}

	seen := make(map[string]bool, len(existingByPath)+len(detectedByPath))
	paths := make([]string, 0, len(existingByPath)+len(detectedByPath))
	for _, d := range in.result.Components {
		if !seen[d.Path] {
			seen[d.Path] = true
			paths = append(paths, d.Path)
		}
	}
	existingPaths := make([]string, 0, len(existingByPath))
	for p := range existingByPath {
		existingPaths = append(existingPaths, p)
	}
	sort.Strings(existingPaths)
	for _, p := range existingPaths {
		if !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}

	plan := componentPlan{byID: map[uuid.UUID]domain.Component{}}
	for _, p := range paths {
		existing, hasExisting := existingByPath[p]
		detected, hasDetected := detectedByPath[p]
		switch {
		case hasExisting && hasDetected:
			updated := mergeComponent(existing, detected, in.scanID)
			plan.save = append(plan.save, updated)
			plan.final = append(plan.final, updated)
			plan.byID[updated.ID] = updated
		case hasDetected:
			created := newComponent(in.repositoryID, detected, in.scanID, in.now, in.flagNewRows)
			plan.save = append(plan.save, created)
			plan.final = append(plan.final, created)
			plan.byID[created.ID] = created
		default:
			if keepUndetectedComponent(existing) {
				plan.final = append(plan.final, existing)
				plan.byID[existing.ID] = existing
			} else {
				plan.deleteIDs = append(plan.deleteIDs, existing.ID)
			}
		}
	}
	return plan
}

func mergeComponent(existing domain.Component, detected domain.DetectedComponent, scanID uuid.UUID) domain.Component {
	out := existing

	name := detected.Name
	out.Name = existing.Name.WithDetected(domain.Fact[string]{Detected: &name})

	role := detected.Role
	out.Role = existing.Role.WithDetected(domain.Fact[domain.ComponentRole]{
		Detected:   &role,
		Confidence: detected.RoleConfidence,
		Evidence:   detected.RoleEvidence,
	})

	stack := detected.Stack
	stack.PackageName = detected.PackageName
	stack.DevPort = detected.DevPort
	out.Stack = existing.Stack.WithDetected(domain.Fact[domain.ComponentStack]{Detected: &stack})

	out.Mobile = mergeMobile(existing.Mobile, detected.Mobile)
	out.Commands = mergeCommands(existing.Commands, detected.Commands)
	out.LastScanID = &scanID
	return out
}

func newComponent(repositoryID uuid.UUID, detected domain.DetectedComponent, scanID uuid.UUID, now time.Time, needsReview bool) domain.Component {
	name := detected.Name
	role := detected.Role
	stack := detected.Stack
	stack.PackageName = detected.PackageName
	stack.DevPort = detected.DevPort

	return domain.Component{
		ID:           uuid.New(),
		RepositoryID: repositoryID,
		Path:         detected.Path,
		Name:         domain.Fact[string]{Detected: &name},
		Role:         domain.Fact[domain.ComponentRole]{Detected: &role, Confidence: detected.RoleConfidence, Evidence: detected.RoleEvidence},
		Stack:        domain.Fact[domain.ComponentStack]{Detected: &stack},
		Commands:     mergeCommands(nil, detected.Commands),
		Mobile:       mergeMobile(nil, detected.Mobile),
		Status:       domain.ComponentStatusActive,
		NeedsReview:  needsReview,
		LastScanID:   &scanID,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

func mergeMobile(existing *domain.Fact[domain.MobileFacts], detected *domain.MobileFacts) *domain.Fact[domain.MobileFacts] {
	if existing == nil && detected == nil {
		return nil
	}
	next := domain.Fact[domain.MobileFacts]{Detected: detected}
	if existing != nil {
		next = existing.WithDetected(next)
	}
	if next.Detected == nil && next.Override == nil {
		return nil
	}
	return &next
}

// keepUndetectedComponent decides whether a component a rescan no longer
// found still belongs in the model: anything the human touched survives.
func keepUndetectedComponent(c domain.Component) bool {
	if c.ManuallyAdded || c.Status == domain.ComponentStatusDismissed {
		return true
	}
	if c.Name.Overridden() || c.Role.Overridden() || c.Stack.Overridden() {
		return true
	}
	for _, cmd := range c.Commands {
		if cmd.Command.Overridden() {
			return true
		}
	}
	return !docsIsZero(c.Docs) || !gatesIsZero(c.Gates)
}

func docsIsZero(d domain.RepositoryDocs) bool {
	return d.CodingStandards == "" && d.TestStandards == "" && d.Architecture == "" && d.LocalRun == ""
}

func gatesIsZero(g domain.ComponentGates) bool {
	return g.CoverageEnabled == nil && g.CoverageThreshold == nil && g.MutationEnabled == nil && g.MutationThreshold == nil
}

// mergeCommands keeps every override, replaces every detected half, and
// drops a purpose that is neither detected this scan nor overridden.
func mergeCommands(existing []domain.ComponentCommand, detected []domain.DetectedCommand) []domain.ComponentCommand {
	existingByPurpose := make(map[domain.CommandPurpose]domain.ComponentCommand, len(existing))
	for _, c := range existing {
		existingByPurpose[c.Purpose] = c
	}
	detectedByPurpose := make(map[domain.CommandPurpose]domain.DetectedCommand, len(detected))
	for _, d := range detected {
		detectedByPurpose[d.Purpose] = d
	}

	var out []domain.ComponentCommand
	for _, purpose := range domain.AllCommandPurposes() {
		ec, hasExisting := existingByPurpose[purpose]
		dc, hasDetected := detectedByPurpose[purpose]
		switch {
		case hasDetected:
			cmd := dc.Command
			next := domain.Fact[string]{Detected: &cmd}
			if hasExisting {
				next = ec.Command.WithDetected(next)
			}
			out = append(out, domain.ComponentCommand{Purpose: purpose, Command: next})
		case hasExisting && ec.Command.Overridden():
			out = append(out, domain.ComponentCommand{Purpose: purpose, Command: ec.Command.WithDetected(domain.Fact[string]{})})
		}
	}
	return out
}

// --- checks ---

type checkPlan struct {
	saveChecks   []domain.ComponentCheck
	deleteChecks []uuid.UUID
	finalChecks  []domain.ComponentCheck
}

type checkKey struct {
	componentID uuid.UUID
	workflow    string
	jobKey      string
}

func reconcileChecks(in planInput, componentByPath map[string]domain.Component) checkPlan {
	plan := checkPlan{}

	existingByKey := make(map[checkKey]domain.ComponentCheck, len(in.existingChecks))
	for _, c := range in.existingChecks {
		if c.Source == domain.CheckSourceManual {
			plan.finalChecks = append(plan.finalChecks, c)
			continue
		}
		existingByKey[checkKey{c.ComponentID, c.Workflow, c.JobKey}] = c
	}

	var order []checkKey
	detectedByKey := make(map[checkKey]domain.DetectedCheck)
	for _, dc := range in.result.Checks {
		comp, ok := componentByPath[dc.ComponentPath]
		if !ok || comp.Status == domain.ComponentStatusDismissed {
			continue
		}
		key := checkKey{comp.ID, dc.Workflow, dc.JobKey}
		if _, dup := detectedByKey[key]; !dup {
			order = append(order, key)
		}
		detectedByKey[key] = dc
	}

	for _, key := range order {
		dc := detectedByKey[key]
		gate := domain.DefaultCheckGate(dc.Purpose, len(dc.LocalCommands) > 0)
		if existing, ok := existingByKey[key]; ok {
			updated := mergeCheck(existing, dc, gate)
			plan.saveChecks = append(plan.saveChecks, updated)
			plan.finalChecks = append(plan.finalChecks, updated)
			delete(existingByKey, key)
		} else {
			created := newCheck(in.repositoryID, key.componentID, dc, gate, in.flagNewRows && gate == domain.CheckGateRequired)
			plan.saveChecks = append(plan.saveChecks, created)
			plan.finalChecks = append(plan.finalChecks, created)
		}
	}

	remaining := make([]domain.ComponentCheck, 0, len(existingByKey))
	for _, c := range existingByKey {
		remaining = append(remaining, c)
	}
	sort.Slice(remaining, func(i, j int) bool { return remaining[i].ID.String() < remaining[j].ID.String() })
	for _, c := range remaining {
		overridden := c.Purpose.Overridden() || c.Gate.Overridden() || c.LocalCommands.Overridden()
		if overridden || c.Status == domain.ModelStatusDismissed {
			c.Missing = true
			plan.saveChecks = append(plan.saveChecks, c)
			plan.finalChecks = append(plan.finalChecks, c)
		} else {
			plan.deleteChecks = append(plan.deleteChecks, c.ID)
		}
	}

	return plan
}

func mergeCheck(existing domain.ComponentCheck, dc domain.DetectedCheck, gate domain.CheckGate) domain.ComponentCheck {
	out := existing
	purpose := dc.Purpose
	out.Purpose = existing.Purpose.WithDetected(domain.Fact[domain.CheckPurpose]{Detected: &purpose, Confidence: dc.Confidence})
	out.Gate = existing.Gate.WithDetected(domain.Fact[domain.CheckGate]{Detected: &gate})
	cmds := dc.LocalCommands
	out.LocalCommands = existing.LocalCommands.WithDetected(domain.Fact[[]domain.LocalCommand]{Detected: &cmds})
	out.WorkflowName = dc.WorkflowName
	out.JobName = dc.JobName
	out.Environment = dc.Environment
	out.Triggers = dc.Triggers
	out.PathFilters = dc.PathFilters
	out.Steps = dc.Steps
	out.Dispatchable = dc.Dispatchable
	out.Missing = false
	return out
}

func newCheck(repositoryID, componentID uuid.UUID, dc domain.DetectedCheck, gate domain.CheckGate, needsReview bool) domain.ComponentCheck {
	purpose := dc.Purpose
	cmds := dc.LocalCommands
	return domain.ComponentCheck{
		ID:            uuid.New(),
		RepositoryID:  repositoryID,
		ComponentID:   componentID,
		Source:        domain.CheckSourceCI,
		Workflow:      dc.Workflow,
		WorkflowName:  dc.WorkflowName,
		JobKey:        dc.JobKey,
		JobName:       dc.JobName,
		Purpose:       domain.Fact[domain.CheckPurpose]{Detected: &purpose, Confidence: dc.Confidence},
		Environment:   dc.Environment,
		Triggers:      dc.Triggers,
		PathFilters:   dc.PathFilters,
		Steps:         dc.Steps,
		LocalCommands: domain.Fact[[]domain.LocalCommand]{Detected: &cmds},
		Gate:          domain.Fact[domain.CheckGate]{Detected: &gate},
		Dispatchable:  dc.Dispatchable,
		Status:        domain.ModelStatusActive,
		NeedsReview:   needsReview,
	}
}

// --- links ---

type linkPlan struct {
	saveLinks   []domain.ComponentLink
	deleteLinks []uuid.UUID
	finalLinks  []domain.ComponentLink
}

type linkKey struct {
	fromComponentID uuid.UUID
	signalKey       string
}

func reconcileLinks(in planInput, componentByPath map[string]domain.Component) linkPlan {
	plan := linkPlan{}

	existingByKey := make(map[linkKey]domain.ComponentLink, len(in.existingLinks))
	for _, l := range in.existingLinks {
		if l.Source == domain.LinkSourceUser {
			plan.finalLinks = append(plan.finalLinks, l)
			continue
		}
		existingByKey[linkKey{l.FromComponentID, l.SignalKey}] = l
	}

	var order []linkKey
	detectedByKey := make(map[linkKey]domain.DetectedLink)
	for _, dl := range in.result.Links {
		comp, ok := componentByPath[dl.ComponentPath]
		if !ok || comp.Status == domain.ComponentStatusDismissed {
			continue
		}
		if dl.Target.Kind == domain.LinkTargetComponent && dl.Target.ComponentPath == dl.ComponentPath {
			continue // a component never links to itself
		}
		key := linkKey{comp.ID, dl.SignalKey}
		if _, dup := detectedByKey[key]; !dup {
			order = append(order, key)
		}
		detectedByKey[key] = dl
	}

	for _, key := range order {
		dl := detectedByKey[key]
		if existing, ok := existingByKey[key]; ok {
			updated := mergeLink(existing, dl, in, componentByPath)
			plan.saveLinks = append(plan.saveLinks, updated)
			plan.finalLinks = append(plan.finalLinks, updated)
			delete(existingByKey, key)
			continue
		}
		if created, ok := newLink(in, key.fromComponentID, dl, componentByPath); ok {
			plan.saveLinks = append(plan.saveLinks, created)
			plan.finalLinks = append(plan.finalLinks, created)
		}
	}

	remaining := make([]domain.ComponentLink, 0, len(existingByKey))
	for _, l := range existingByKey {
		remaining = append(remaining, l)
	}
	sort.Slice(remaining, func(i, j int) bool { return remaining[i].ID.String() < remaining[j].ID.String() })
	for _, l := range remaining {
		if l.Status == domain.LinkDismissed {
			l.Missing = true
			plan.saveLinks = append(plan.saveLinks, l)
			plan.finalLinks = append(plan.finalLinks, l)
		} else {
			plan.deleteLinks = append(plan.deleteLinks, l.ID)
		}
	}

	return plan
}

type linkResolution struct {
	toComponentID *uuid.UUID
	toResourceID  *uuid.UUID
	reason        string
	hint          string
	confidence    domain.Confidence
	targetHost    string
	targetPort    int
}

// resolveLinkTarget follows the target-kind ladder: a resource signal wires
// the id ensureResources already minted; a same-repo component signal
// resolves by path; only a scanner-unresolved signal falls to the
// cross-repository matchers.
func resolveLinkTarget(in planInput, fromComponentID uuid.UUID, dl domain.DetectedLink, componentByPath map[string]domain.Component) linkResolution {
	res := linkResolution{confidence: dl.Confidence}
	switch dl.Target.Kind {
	case domain.LinkTargetResource:
		if id, ok := in.resources[dl.ComponentPath+"|"+dl.SignalKey]; ok {
			res.toResourceID = &id
		}
	case domain.LinkTargetComponent:
		if c, ok := componentByPath[dl.Target.ComponentPath]; ok && c.Status != domain.ComponentStatusDismissed && c.ID != fromComponentID {
			id := c.ID
			res.toComponentID = &id
		}
	case domain.LinkTargetUnresolved:
		// TargetHost/Port are kept regardless of whether this reconcile itself
		// resolves the link, so a later Relink can match it against an
		// environment bound after this scan.
		res.targetHost = dl.Target.URLHost
		res.targetPort = dl.Target.Port
		if m, ok := matchLink(dl.Target, in.matchCandidates); ok {
			id := m.Candidate.Component.ID
			res.toComponentID = &id
			res.reason = m.Reason
			res.confidence = m.Confidence
		}
	}
	if res.toComponentID == nil && res.toResourceID == nil {
		res.hint = linkHint(dl.Target)
	}
	return res
}

func linkHint(t domain.LinkTarget) string {
	hint := t.ServiceHint
	hostport := t.URLHost
	if t.Port != 0 {
		hostport = fmt.Sprintf("%s:%d", hostport, t.Port)
	}
	if hostport != "" {
		hint = fmt.Sprintf("%s (%s)", hint, hostport)
	}
	return hint
}

// newLinkStatus maps a link's confidence to its initial status; drop tells
// the caller a brand new suggestion this weak is not worth creating at all.
func newLinkStatus(c domain.Confidence) (status domain.LinkStatus, autoConfirmed, drop bool) {
	switch c {
	case domain.ConfidenceExact:
		return domain.LinkConfirmed, true, false
	case domain.ConfidenceHigh:
		return domain.LinkConfirmed, false, false
	case domain.ConfidenceMedium:
		return domain.LinkSuggested, false, false
	default:
		return "", false, true
	}
}

func newLink(in planInput, fromComponentID uuid.UUID, dl domain.DetectedLink, componentByPath map[string]domain.Component) (domain.ComponentLink, bool) {
	resolved := resolveLinkTarget(in, fromComponentID, dl, componentByPath)
	status, autoConfirmed, drop := newLinkStatus(resolved.confidence)
	if drop {
		return domain.ComponentLink{}, false
	}
	return domain.ComponentLink{
		ID:              uuid.New(),
		RepositoryID:    in.repositoryID,
		FromComponentID: fromComponentID,
		ToComponentID:   resolved.toComponentID,
		ToResourceID:    resolved.toResourceID,
		Protocol:        dl.Protocol,
		Detail:          dl.Detail,
		EnvVars:         dl.EnvVars,
		Evidence:        dl.Evidence,
		Confidence:      resolved.confidence,
		Reason:          resolved.reason,
		Hint:            resolved.hint,
		Status:          status,
		Source:          domain.LinkSourceScan,
		AutoConfirmed:   autoConfirmed,
		SignalKey:       dl.SignalKey,
		TargetHost:      resolved.targetHost,
		TargetPort:      resolved.targetPort,
		CreatedAt:       in.now,
		UpdatedAt:       in.now,
	}, true
}

// linkOpenForRematch is the "still asking the human to look" test shared by
// mergeLink (re-resolving against this scan's own candidates) and Relink
// (re-resolving against environments bound after the scan): suggested, or
// confirmed with nothing to point at. A dismissed or a confirmed-and-resolved
// row is the human's final word and is never touched again.
func linkOpenForRematch(l domain.ComponentLink) bool {
	return l.Status == domain.LinkSuggested || (l.Status == domain.LinkConfirmed && !l.Resolved())
}

// mergeLink always refreshes the scan-observed fields; it only re-resolves
// the target when the existing row is still asking the human to look
// (suggested, or confirmed with nothing to point at) — a dismissed row never
// changes status, and a confirmed-and-resolved one keeps the human's target.
func mergeLink(existing domain.ComponentLink, dl domain.DetectedLink, in planInput, componentByPath map[string]domain.Component) domain.ComponentLink {
	out := existing
	out.Evidence = dl.Evidence
	out.EnvVars = dl.EnvVars
	out.Protocol = dl.Protocol
	out.Detail = dl.Detail
	out.Missing = false

	if existing.Status == domain.LinkDismissed {
		return out
	}
	if !linkOpenForRematch(existing) {
		return out
	}

	resolved := resolveLinkTarget(in, existing.FromComponentID, dl, componentByPath)
	status, autoConfirmed, drop := newLinkStatus(resolved.confidence)
	if drop {
		status, autoConfirmed = domain.LinkSuggested, false
	}
	out.TargetHost = resolved.targetHost
	out.TargetPort = resolved.targetPort
	out.ToComponentID = resolved.toComponentID
	out.ToResourceID = resolved.toResourceID
	out.Confidence = resolved.confidence
	out.Reason = resolved.reason
	out.Hint = resolved.hint
	out.Status = status
	out.AutoConfirmed = autoConfirmed
	return out
}
