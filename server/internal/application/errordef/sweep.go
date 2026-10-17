package errordef

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/opsquery"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const (
	DefaultSweepInterval = 2 * time.Minute

	// taskWindow is how far back the task action looks for a group it has
	// not opened a task for yet.
	taskWindow = 24 * time.Hour
	// maxReadsPerSweep bounds the inbox reads one sweep makes: one per
	// distinct threshold window.
	maxReadsPerSweep = 8
	// One broad pattern can match many groups at once; the rest wait for
	// later sweeps rather than flooding the board.
	maxTasksPerSweep      = 5
	maxTasksPerDefinition = 1

	taskSampleLines = 40
	taskSampleChars = 4000
	taskMessageLen  = 1000
	titleMessageLen = 80
)

// sweptEnvironment: definitions carry no environment of their own, and only
// production failures are worth an incident or a task.
const sweptEnvironment = domain.EnvironmentProduction

// Start runs the sweep until ctx ends or Stop. Without an errors reader
// there is nothing to sweep.
func (s *Service) Start(ctx context.Context) {
	if s.errors == nil {
		return
	}
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.cancel != nil {
		return
	}
	ctx, s.cancel = context.WithCancel(ctx)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.SweepOnce(ctx)
			}
		}
	}()
}

func (s *Service) Stop() {
	s.runMu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.runMu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.wg.Wait()
}

// SweepOnce carries out the alert, incident and task actions of every active
// definition against production's error groups. A group counts toward a
// definition only when that definition is the one the inbox shows for it,
// so a more specific ignore definition silences a broader alert.
func (s *Service) SweepOnce(ctx context.Context) {
	if s.errors == nil {
		return
	}
	s.sweepMu.Lock()
	defer s.sweepMu.Unlock()
	defs, err := s.store.ListErrorDefinitions(ctx, domain.ErrorDefinitionFilter{
		Statuses: []domain.ErrorDefinitionStatus{domain.ErrorDefinitionActive},
		Actions:  []domain.ErrorAction{domain.ErrorActionAlert, domain.ErrorActionIncident, domain.ErrorActionTask},
	})
	if err != nil {
		log.Warn().Err(err).Msg("errordef: listing acting definitions failed")
		return
	}
	if len(defs) == 0 {
		return
	}
	scopes := s.scopes(ctx, defs)
	byWindow := map[time.Duration][]domain.ErrorDefinition{}
	for _, d := range defs {
		if len(scopes[d.ID]) == 0 {
			continue
		}
		w := taskWindow
		if d.Action.Thresholded() {
			w = threshold(d).Window()
		}
		byWindow[w] = append(byWindow[w], d)
	}
	windows := make([]time.Duration, 0, len(byWindow))
	for w := range byWindow {
		windows = append(windows, w)
	}
	slices.Sort(windows)
	if len(windows) > maxReadsPerSweep {
		log.Warn().Int("windows", len(windows)).Int("max", maxReadsPerSweep).
			Msg("errordef: more distinct threshold windows than one sweep reads; the longest wait")
		windows = windows[:maxReadsPerSweep]
	}
	budget := maxTasksPerSweep
	for _, w := range windows {
		s.sweepWindow(ctx, w, byWindow[w], scopes, &budget)
	}
}

// scopes is each definition's components: its own, or every active one of
// its repository.
func (s *Service) scopes(ctx context.Context, defs []domain.ErrorDefinition) map[uuid.UUID][]uuid.UUID {
	byRepo := map[uuid.UUID][]uuid.UUID{}
	out := make(map[uuid.UUID][]uuid.UUID, len(defs))
	for _, d := range defs {
		if d.ComponentID != nil {
			out[d.ID] = []uuid.UUID{*d.ComponentID}
			continue
		}
		comps, ok := byRepo[d.RepositoryID]
		if !ok {
			all, err := s.components.ListComponents(ctx, d.RepositoryID)
			if err != nil {
				log.Warn().Err(err).Str("repository_id", d.RepositoryID.String()).Msg("errordef: listing components failed")
			}
			for _, c := range all {
				if c.Status != domain.ComponentStatusDismissed {
					comps = append(comps, c.ID)
				}
			}
			byRepo[d.RepositoryID] = comps
		}
		out[d.ID] = comps
	}
	return out
}

func (s *Service) sweepWindow(ctx context.Context, window time.Duration, defs []domain.ErrorDefinition, scopes map[uuid.UUID][]uuid.UUID, budget *int) {
	var comps []uuid.UUID
	var repos []uuid.UUID
	for _, d := range defs {
		for _, c := range scopes[d.ID] {
			if !slices.Contains(comps, c) {
				comps = append(comps, c)
			}
		}
		if !slices.Contains(repos, d.RepositoryID) {
			repos = append(repos, d.RepositoryID)
		}
	}
	res, err := s.errors.Errors(ctx, opsquery.ErrorsQuery{
		Environment:  sweptEnvironment,
		Since:        s.now().Add(-window),
		ComponentIDs: comps,
	})
	if err != nil {
		log.Warn().Err(err).Dur("window", window).Msg("errordef: reading production errors failed")
		return
	}
	active, err := s.active(ctx, repos)
	if err != nil {
		log.Warn().Err(err).Msg("errordef: reading active definitions failed")
		return
	}
	unavailable := map[uuid.UUID]bool{}
	for _, u := range res.Unavailable {
		unavailable[u.ComponentID] = true
	}
	for _, d := range defs {
		scope := scopes[d.ID]
		var matched []domain.OpsErrorGroup
		for _, g := range res.Groups {
			if g.RepositoryID != d.RepositoryID || !slices.Contains(scope, g.ComponentID) {
				continue
			}
			if best := s.best(active[d.RepositoryID], g); best != nil && best.ID == d.ID {
				matched = append(matched, g)
			}
		}
		partial := res.Truncated
		for _, c := range scope {
			partial = partial || unavailable[c]
		}
		if d.Action == domain.ErrorActionTask {
			s.openTasks(ctx, d, matched, budget)
		} else {
			s.judge(ctx, d, matched, partial)
		}
	}
}

// judge moves a thresholded definition between quiet and firing. A read
// that missed some of its components may fire it, never quiet it.
func (s *Service) judge(ctx context.Context, d domain.ErrorDefinition, matched []domain.OpsErrorGroup, partial bool) {
	t := threshold(d)
	count := 0
	for _, g := range matched {
		count += g.Count
	}
	over := count >= t.Count
	switch {
	case over && d.FiringSince == nil:
		if s.incidents == nil {
			return
		}
		if _, err := s.incidents.Ingest(ctx, incidentInput(d, matched, count, t)); err != nil {
			log.Warn().Err(err).Str("definition_id", d.ID.String()).Msg("errordef: opening the incident failed")
			return
		}
		now := s.now()
		if err := s.store.SetErrorDefinitionFiring(ctx, d.ID, &now); err != nil {
			log.Warn().Err(err).Str("definition_id", d.ID.String()).Msg("errordef: recording firing failed")
		}
	case !over && d.FiringSince != nil && !partial:
		if d.Action == domain.ErrorActionAlert && s.incidents != nil {
			in := incidentInput(d, matched, count, t)
			in.Resolved = true
			if _, err := s.incidents.Ingest(ctx, in); err != nil {
				log.Warn().Err(err).Str("definition_id", d.ID.String()).Msg("errordef: resolving the incident failed")
				return
			}
		}
		if err := s.store.SetErrorDefinitionFiring(ctx, d.ID, nil); err != nil {
			log.Warn().Err(err).Str("definition_id", d.ID.String()).Msg("errordef: recording quiet failed")
		}
	}
}

func incidentInput(d domain.ErrorDefinition, matched []domain.OpsErrorGroup, count int, t domain.ErrorAlertThreshold) domain.IncidentInput {
	sort.SliceStable(matched, func(i, j int) bool { return matched[i].Count > matched[j].Count })
	fingerprints := make([]string, 0, len(matched))
	var detail []string
	for i, g := range matched {
		fingerprints = append(fingerprints, g.Fingerprint)
		if i < 5 {
			detail = append(detail, fmt.Sprintf("%d× %s", g.Count, domain.TruncateHead(g.Message, 300)))
		}
	}
	return domain.IncidentInput{
		RepositoryID: d.RepositoryID,
		Env:          domain.DeployEnvProd,
		Source:       domain.IncidentSourceErrorDefinition,
		Fingerprint:  domain.ErrorDefinitionFingerprint(d.ID),
		Title:        fmt.Sprintf("%s: %d in %s", d.Name, count, t.Window()),
		Detail:       strings.Join(detail, "\n"),
		Severity:     d.Severity,
		Payload: map[string]any{
			"definition_id":  d.ID.String(),
			"definition":     d.Name,
			"category":       string(d.Category),
			"count":          count,
			"threshold":      t.Count,
			"window_seconds": t.WindowSeconds,
			"fingerprints":   fingerprints,
		},
	}
}

func (s *Service) openTasks(ctx context.Context, d domain.ErrorDefinition, matched []domain.OpsErrorGroup, budget *int) {
	if s.tasks == nil {
		return
	}
	sort.SliceStable(matched, func(i, j int) bool { return matched[i].Count > matched[j].Count })
	opened := 0
	for _, g := range matched {
		if *budget <= 0 || opened >= maxTasksPerDefinition {
			return
		}
		envID := &g.EnvironmentID
		if g.EnvironmentID == uuid.Nil {
			envID = nil
		}
		claimed, err := s.store.ClaimErrorGroupLink(ctx, domain.ErrorGroupLink{
			DefinitionID: d.ID, Fingerprint: g.Fingerprint, ComponentID: g.ComponentID,
			EnvironmentID: envID, DataSourceID: g.DataSourceID,
		})
		if err != nil {
			log.Warn().Err(err).Str("definition_id", d.ID.String()).Msg("errordef: claiming an error group failed")
			return
		}
		if !claimed {
			continue
		}
		task, err := s.createTask(ctx, d, g)
		if err != nil {
			log.Warn().Err(err).Str("definition_id", d.ID.String()).Msg("errordef: opening the task failed")
			if rerr := s.store.ReleaseErrorGroupLink(ctx, d.ID, g.Fingerprint); rerr != nil {
				log.Warn().Err(rerr).Str("definition_id", d.ID.String()).Msg("errordef: releasing the error group failed")
			}
			continue
		}
		if err := s.store.SetErrorGroupLinkTask(ctx, d.ID, g.Fingerprint, task.ID); err != nil {
			log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("errordef: linking the task failed")
		}
		if err := s.store.SetErrorDefinitionTask(ctx, d.ID, task.ID); err != nil {
			log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("errordef: recording the task failed")
		}
		opened++
		*budget--
	}
}

func (s *Service) createTask(ctx context.Context, d domain.ErrorDefinition, g domain.OpsErrorGroup) (domain.BoardTask, error) {
	comp, err := s.components.GetComponent(ctx, g.ComponentID)
	if err != nil {
		return domain.BoardTask{}, fmt.Errorf("read component: %w", err)
	}
	sample, _ := headLines(g.Sample, taskSampleLines, taskSampleChars)
	description := errorTaskKey.Render(errorTaskInput{
		Definition:  d.Name,
		Category:    string(d.Category),
		Severity:    string(d.Severity),
		Rationale:   d.Rationale,
		Component:   comp.DisplayName(),
		Path:        comp.Path,
		Provider:    string(g.Provider),
		Fingerprint: g.Fingerprint,
		Count:       g.Count,
		FirstSeen:   g.FirstSeen.UTC().Format(time.RFC3339),
		LastSeen:    g.LastSeen.UTC().Format(time.RFC3339),
		Message:     domain.TruncateHead(strings.TrimSpace(g.Message), taskMessageLen),
		Sample:      sample,
		ExternalURL: g.ExternalURL,
	})
	title := strings.TrimSpace(errorTaskTitleKey.Render(errorTaskTitleInput{
		Definition: d.Name,
		Message:    domain.TruncateHead(firstLine(strings.TrimSpace(g.Message)), titleMessageLen),
	}))
	compID := g.ComponentID
	return s.tasks.CreateTask(ctx, d.RepositoryID, domain.CreateBoardTaskRequest{
		Title:           title,
		Description:     description,
		TaskType:        s.defectType(ctx),
		Priority:        priorityOf(d.Severity),
		Column:          domain.TaskColumnTodo,
		CreatedBy:       "system",
		ComponentID:     &compID,
		AssigneeAgentID: s.developer(ctx, comp),
	})
}

func (s *Service) defectType(ctx context.Context) domain.TaskType {
	if s.defects == nil {
		return ""
	}
	t, err := s.defects.DefectTaskType(ctx)
	if err != nil {
		return ""
	}
	return t
}

// developer is the agent holding the developer role for the component's
// area (or the area it falls back to); nil leaves the task for a person to
// assign.
func (s *Service) developer(ctx context.Context, comp domain.Component) *uuid.UUID {
	if s.roles == nil {
		return nil
	}
	role, err := s.roles.RoleByKey(ctx, domain.RoleKeyDeveloper)
	if err != nil {
		return nil
	}
	area := domain.ComponentArea(comp.Role.Get())
	for _, a := range append([]string{area}, domain.AreaFallbacks(area)...) {
		if id, err := s.roles.AgentForRole(ctx, role.ID, a); err == nil && id != nil {
			return id
		}
	}
	return nil
}

func priorityOf(sev domain.IncidentSeverity) domain.TaskPriority {
	switch sev {
	case domain.IncidentSeverityCritical:
		return domain.TaskPriorityCritical
	case domain.IncidentSeverityHigh:
		return domain.TaskPriorityHigh
	case domain.IncidentSeverityLow:
		return domain.TaskPriorityLow
	default:
		return domain.TaskPriorityMedium
	}
}

// headLines cuts s to its first n lines and at most max bytes; true when
// anything was cut.
func headLines(s string, n, max int) (string, bool) {
	s = strings.TrimSpace(s)
	cut := false
	if lines := strings.SplitN(s, "\n", n+1); len(lines) > n {
		s = strings.Join(lines[:n], "\n")
		cut = true
	}
	if len(s) > max {
		s = domain.TruncateHead(s, max)
		cut = true
	}
	return s, cut
}
