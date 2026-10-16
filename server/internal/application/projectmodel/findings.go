package projectmodel

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// TaskCreator is repository.Service.CreateTask behind an interface this
// package owns, the same seam DeployMatcher uses to keep application packages
// from importing one another.
type TaskCreator interface {
	CreateTask(ctx context.Context, repositoryID uuid.UUID, req domain.CreateBoardTaskRequest) (domain.BoardTask, error)
	GetTask(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.BoardTask, error)
}

const (
	maxFindingsPerTask    = 50
	maxFindingReasonLen   = 500
	maxFindingTitleLen    = 200
	resolvedFindingKeep   = 30 * 24 * time.Hour
	findingEvidenceInTask = domain.MaxFindingEvidence
)

// SetFindingStore wires code-health findings; nil (the default) leaves a
// scan's findings unsaved and every model without them.
func (s *Service) SetFindingStore(f port.FindingStore) { s.findings = f }

// SetTaskCreator wires the board for "open a task from these findings" and
// for the CI task a quality gate opens when it is switched on.
func (s *Service) SetTaskCreator(c TaskCreator) { s.tasks = c }

func (s *Service) listFindings(ctx context.Context, repositoryID uuid.UUID) ([]domain.ComponentFinding, error) {
	if s.findings == nil {
		return nil, nil
	}
	return s.findings.ListFindings(ctx, repositoryID)
}

// reconcileFindings runs after the model's own reconcile, so a component a
// scan just created already has the id its findings point at.
func (s *Service) reconcileFindings(ctx context.Context, repositoryID, scanID uuid.UUID, result domain.ScanResult) error {
	if s.findings == nil || !result.FindingsScanned {
		return nil
	}
	components, err := s.store.ListComponents(ctx, repositoryID)
	if err != nil {
		return fmt.Errorf("list components: %w", err)
	}
	existing, err := s.findings.ListFindings(ctx, repositoryID)
	if err != nil {
		return fmt.Errorf("list findings: %w", err)
	}
	save, del := planFindings(findingPlanInput{
		repositoryID: repositoryID,
		scanID:       scanID,
		now:          s.now(),
		detected:     result.Findings,
		partial:      result.FindingsPartial,
		components:   components,
		existing:     existing,
	})
	return s.findings.ApplyFindings(ctx, repositoryID, save, del)
}

type findingPlanInput struct {
	repositoryID uuid.UUID
	scanID       uuid.UUID
	now          time.Time
	detected     []domain.DetectedFinding
	partial      bool
	components   []domain.Component
	existing     []domain.ComponentFinding
}

func findingKey(componentID *uuid.UUID, rule string) string {
	if componentID == nil {
		return "\x00" + rule
	}
	return componentID.String() + "\x00" + rule
}

// planFindings diffs one scan's findings against the stored ones. A finding
// the scan reports again keeps its id, its task and — when a person accepted
// it or called it a false positive — that verdict. One the scan no longer
// reports is resolved; a resolved one comes back open if the problem does.
func planFindings(in findingPlanInput) (save []domain.ComponentFinding, del []uuid.UUID) {
	byPath := make(map[string]domain.Component, len(in.components))
	for _, c := range in.components {
		byPath[c.Path] = c
	}
	existing := make(map[string]domain.ComponentFinding, len(in.existing))
	for _, f := range in.existing {
		existing[findingKey(f.ComponentID, f.Rule)] = f
	}
	scanID := in.scanID
	seen := map[string]bool{}

	for _, d := range in.detected {
		var componentID *uuid.UUID
		if d.ComponentPath != "" {
			c, ok := byPath[d.ComponentPath]
			if !ok || c.Status == domain.ComponentStatusDismissed {
				continue
			}
			id := c.ID
			componentID = &id
		}
		key := findingKey(componentID, d.Rule)
		if seen[key] {
			continue
		}
		seen[key] = true

		f, ok := existing[key]
		if !ok {
			f = domain.ComponentFinding{ID: uuid.New(), RepositoryID: in.repositoryID, ComponentID: componentID, Rule: d.Rule, Status: domain.FindingOpen}
		}
		f.Category = d.Category
		f.Severity = d.Severity
		f.Occurrences = d.Occurrences
		f.Evidence = d.Evidence
		f.LastScanID = &scanID
		if f.Status == domain.FindingResolved {
			f.Status = domain.FindingOpen
			f.ResolvedAt = nil
		}
		f.Task = nil
		save = append(save, f)
	}

	for key, f := range existing {
		if seen[key] {
			continue
		}
		switch {
		case f.Status == domain.FindingOpen && !in.partial:
			resolvedAt := in.now
			f.Status = domain.FindingResolved
			f.ResolvedAt = &resolvedAt
			f.Task = nil
			save = append(save, f)
		case f.Status == domain.FindingResolved && f.TaskID == nil && f.ResolvedAt != nil && in.now.Sub(*f.ResolvedAt) > resolvedFindingKeep:
			del = append(del, f.ID)
		}
	}
	sort.Slice(del, func(i, j int) bool { return del[i].String() < del[j].String() })
	return save, del
}

// UpdateFinding records a person's verdict: reopen, accept the risk, or call
// it a false positive. A resolved finding is the scan's to reopen.
func (s *Service) UpdateFinding(ctx context.Context, id uuid.UUID, patch domain.FindingPatch) (domain.ComponentFinding, error) {
	if s.findings == nil {
		return domain.ComponentFinding{}, port.ErrNotFound
	}
	f, err := s.findings.GetFinding(ctx, id)
	if err != nil {
		return domain.ComponentFinding{}, err
	}
	if patch.Status != nil {
		if !patch.Status.ValidPatchStatus() {
			return domain.ComponentFinding{}, fmt.Errorf("%w: status must be open, accepted or false_positive", ErrInvalidInput)
		}
		if f.Status == domain.FindingResolved {
			return domain.ComponentFinding{}, fmt.Errorf("%w: a resolved finding reopens when a scan finds it again", ErrInvalidInput)
		}
		f.Status = *patch.Status
		if f.Status == domain.FindingOpen {
			f.StatusReason = ""
		}
	}
	if patch.Reason != nil {
		reason := strings.TrimSpace(*patch.Reason)
		if len([]rune(reason)) > maxFindingReasonLen {
			return domain.ComponentFinding{}, fmt.Errorf("%w: reason is longer than %d characters", ErrInvalidInput, maxFindingReasonLen)
		}
		if f.Status.Suppressed() {
			f.StatusReason = reason
		}
	}
	return s.findings.SaveFinding(ctx, f)
}

type findingTaskInput struct {
	Repository string
	Notes      string
	Findings   []findingTaskItem
}

type findingTaskItem struct {
	Rule        string
	Category    string
	Severity    string
	Component   string
	Occurrences int
	Locations   []string
	More        int
}

var findingTaskKey = prompt.Define("findings.task", findingTaskInput{
	Repository: "acme-web",
	Notes:      "Start with the API.",
	Findings: []findingTaskItem{{
		Rule: "console_logs_in_production", Category: "logging", Severity: "medium", Component: "web (apps/web)",
		Occurrences: 12, Locations: []string{"apps/web/src/App.tsx:14 — console.log(user)"}, More: 11,
	}},
})

// CreateFindingTask opens one board task covering the given findings and
// links each to it. Only open findings qualify: an accepted or
// false-positive one was already decided.
func (s *Service) CreateFindingTask(ctx context.Context, repositoryID uuid.UUID, req domain.FindingTaskRequest) (domain.FindingTaskResult, error) {
	if s.findings == nil || s.tasks == nil {
		return domain.FindingTaskResult{}, port.ErrNotFound
	}
	title := strings.TrimSpace(req.Title)
	switch {
	case len(req.FindingIDs) == 0:
		return domain.FindingTaskResult{}, fmt.Errorf("%w: finding_ids is empty", ErrInvalidInput)
	case len(req.FindingIDs) > maxFindingsPerTask:
		return domain.FindingTaskResult{}, fmt.Errorf("%w: at most %d findings per task", ErrInvalidInput, maxFindingsPerTask)
	case title == "":
		return domain.FindingTaskResult{}, fmt.Errorf("%w: title is required", ErrInvalidInput)
	case len([]rune(title)) > maxFindingTitleLen:
		return domain.FindingTaskResult{}, fmt.Errorf("%w: title is longer than %d characters", ErrInvalidInput, maxFindingTitleLen)
	}

	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return domain.FindingTaskResult{}, err
	}
	all, err := s.findings.ListFindings(ctx, repositoryID)
	if err != nil {
		return domain.FindingTaskResult{}, err
	}
	byID := make(map[uuid.UUID]domain.ComponentFinding, len(all))
	for _, f := range all {
		byID[f.ID] = f
	}
	var picked []domain.ComponentFinding
	seen := map[uuid.UUID]bool{}
	for _, id := range req.FindingIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		f, ok := byID[id]
		if !ok {
			return domain.FindingTaskResult{}, fmt.Errorf("%w: finding %s is not in this repository", ErrInvalidInput, id)
		}
		if f.Status != domain.FindingOpen {
			return domain.FindingTaskResult{}, fmt.Errorf("%w: finding %s is %s, not open", ErrInvalidInput, id, f.Status)
		}
		picked = append(picked, f)
	}
	sort.SliceStable(picked, func(i, j int) bool { return picked[i].Severity.Rank() < picked[j].Severity.Rank() })

	components, err := s.store.ListComponents(ctx, repositoryID)
	if err != nil {
		return domain.FindingTaskResult{}, err
	}
	byComponent := make(map[uuid.UUID]domain.Component, len(components))
	for _, c := range components {
		byComponent[c.ID] = c
	}

	input := findingTaskInput{Repository: repo.Name, Notes: strings.TrimSpace(req.Notes)}
	var componentID *uuid.UUID
	sameComponent := true
	for i, f := range picked {
		item := findingTaskItem{
			Rule: f.Rule, Category: string(f.Category), Severity: string(f.Severity), Occurrences: f.Occurrences,
		}
		if f.ComponentID != nil {
			if c, ok := byComponent[*f.ComponentID]; ok {
				item.Component = fmt.Sprintf("%s (%s)", c.DisplayName(), c.Path)
			}
		}
		for _, e := range f.Evidence {
			if len(item.Locations) >= findingEvidenceInTask {
				break
			}
			loc := e.String()
			if e.Note != "" {
				loc += " — " + e.Note
			}
			item.Locations = append(item.Locations, loc)
		}
		if f.Occurrences > len(item.Locations) {
			item.More = f.Occurrences - len(item.Locations)
		}
		input.Findings = append(input.Findings, item)

		if i == 0 {
			componentID = f.ComponentID
		} else if !sameUUID(componentID, f.ComponentID) {
			sameComponent = false
		}
	}
	if !sameComponent {
		componentID = nil
	}

	taskType := req.TaskType
	if taskType == "" {
		taskType = domain.TaskTypeTechnical
	}
	priority := req.Priority
	if priority == "" {
		priority = picked[0].Severity.TaskPriority()
	}
	createdBy := req.CreatedBy
	if createdBy == "" {
		createdBy = "user"
	}
	task, err := s.tasks.CreateTask(ctx, repositoryID, domain.CreateBoardTaskRequest{
		Title:       title,
		TaskType:    taskType,
		Description: findingTaskKey.Render(input),
		ComponentID: componentID,
		Priority:    priority,
		CreatedBy:   createdBy,
	})
	if err != nil {
		return domain.FindingTaskResult{}, fmt.Errorf("%w: create task: %v", ErrInvalidInput, err)
	}
	ids := make([]uuid.UUID, 0, len(picked))
	for _, f := range picked {
		ids = append(ids, f.ID)
	}
	if err := s.findings.LinkFindingsToTask(ctx, ids, task.ID); err != nil {
		return domain.FindingTaskResult{}, err
	}
	updated := make([]domain.ComponentFinding, 0, len(ids))
	for _, id := range ids {
		f, err := s.findings.GetFinding(ctx, id)
		if err != nil {
			return domain.FindingTaskResult{}, err
		}
		updated = append(updated, f)
	}
	return domain.FindingTaskResult{Task: task, Findings: updated}, nil
}

func sameUUID(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
