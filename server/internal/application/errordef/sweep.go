package errordef

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

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
	// A window is read at most once per window/windowReadsPerWindow, and
	// never more often than minWindowPace: a seven-day count does not move
	// in two minutes, and each read costs the provider a query.
	windowReadsPerWindow = 12
	minWindowPace        = 2 * time.Minute
	// maxReadsPerSweep bounds the inbox reads one sweep makes; the due
	// windows that read longest ago go first, so none waits forever.
	maxReadsPerSweep = 8
	// componentsPerRead keeps one read within the environments the inbox
	// reads at once (opsquery.MaxErrorEnvironments), so it is not truncated.
	componentsPerRead = 50
	// One broad pattern can match many groups at once; the rest wait for
	// later sweeps rather than flooding the board.
	maxTasksPerSweep      = 5
	maxTasksPerDefinition = 1
	// staleClaim: a group claimed this long ago that opened no task was
	// claimed by a process that died before it could; it is claimed again.
	staleClaim = 10 * time.Minute

	taskSampleLines = 40
	taskSampleChars = 4000
	taskMessageLen  = 1000
	titleMessageLen = 80
	detailMessages  = 5
	detailLineLen   = 200
	minFence        = 3

	orphanSweepLimit = 500
)

// orphanResolveNote is the incident timeline's entry for an incident whose
// definition no longer acts.
const orphanResolveNote = "the error definition that opened it no longer exists or no longer opens incidents"

// sweptEnvironment: definitions carry no environment of their own, and only
// production failures are worth an incident or a task.
const sweptEnvironment = domain.EnvironmentProduction

// IncidentCloser is what resolving the incidents of definitions that no
// longer act needs; an IncidentIngester that is one too (prodops.Service)
// gets it.
type IncidentCloser interface {
	List(ctx context.Context, filter domain.IncidentFilter) ([]domain.Incident, error)
	Resolve(ctx context.Context, incidentID uuid.UUID, note string) (domain.Incident, error)
}

// SetScope keys the sweep's in-process pacing by scope(ctx) as well, so a
// process sweeping several tenants paces each on its own. Without it there is
// one scope. Call it before Start.
func (s *Service) SetScope(scope func(ctx context.Context) string) { s.scope = scope }

func (s *Service) scopeOf(ctx context.Context) string {
	if s.scope == nil {
		return ""
	}
	return s.scope(ctx)
}

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
// definition against production's error groups, and resolves the incidents
// of definitions that no longer act. A group counts toward a definition only
// when that definition acts on it (see actingOn), so a more specific ignore
// definition silences a broader alert. Each sweep reads the definitions once
// and judges by that one read.
func (s *Service) SweepOnce(ctx context.Context) {
	if s.errors == nil {
		return
	}
	s.sweepMu.Lock()
	defer s.sweepMu.Unlock()
	all, err := s.store.ListErrorDefinitions(ctx, domain.ErrorDefinitionFilter{
		Statuses: []domain.ErrorDefinitionStatus{domain.ErrorDefinitionActive},
	})
	if err != nil {
		log.Warn().Err(err).Msg("errordef: listing active definitions failed")
		return
	}
	byRepo := map[uuid.UUID][]domain.ErrorDefinition{}
	var acting []domain.ErrorDefinition
	for _, d := range all {
		byRepo[d.RepositoryID] = append(byRepo[d.RepositoryID], d)
		if d.Action.Swept() {
			acting = append(acting, d)
		}
	}
	s.resolveOrphans(ctx, acting)
	if len(acting) == 0 {
		return
	}
	units := s.dueUnits(ctx, acting, s.scopes(ctx, acting))
	budget, reads := maxTasksPerSweep, 0
	for _, u := range units {
		if reads > 0 && reads+len(u.chunks) > maxReadsPerSweep {
			break
		}
		reads += len(u.chunks)
		s.markRead(u.key, s.now())
		s.sweepUnit(ctx, u, byRepo[u.repo], &budget)
	}
}

// sweepUnit is one window of one repository: the definitions judged over it
// and the reads, at most componentsPerRead components each, that cover them.
type sweepUnit struct {
	key    string
	window time.Duration
	repo   uuid.UUID
	defs   []domain.ErrorDefinition
	scopes map[uuid.UUID][]uuid.UUID
	chunks [][]uuid.UUID
	last   time.Time
}

// dueUnits answers the units whose window is due, the one read longest ago
// (or never) first.
func (s *Service) dueUnits(ctx context.Context, defs []domain.ErrorDefinition, scopes map[uuid.UUID][]uuid.UUID) []sweepUnit {
	type unitKey struct {
		window time.Duration
		repo   uuid.UUID
	}
	byKey := map[unitKey]*sweepUnit{}
	var keys []unitKey
	for _, d := range defs {
		if len(scopes[d.ID]) == 0 {
			continue
		}
		w := taskWindow
		if d.Action.Thresholded() {
			w = threshold(d).Window()
		}
		k := unitKey{window: w, repo: d.RepositoryID}
		u, ok := byKey[k]
		if !ok {
			u = &sweepUnit{window: w, repo: d.RepositoryID, scopes: map[uuid.UUID][]uuid.UUID{}}
			byKey[k] = u
			keys = append(keys, k)
		}
		u.defs = append(u.defs, d)
		u.scopes[d.ID] = scopes[d.ID]
	}
	scope := s.scopeOf(ctx)
	now := s.now()
	var out []sweepUnit
	for _, k := range keys {
		u := byKey[k]
		u.key = scope + "\x00" + k.repo.String() + "\x00" + k.window.String()
		u.last = s.lastRead(u.key)
		if !u.last.IsZero() && now.Sub(u.last) < pace(k.window) {
			continue
		}
		var comps []uuid.UUID
		for _, d := range u.defs {
			for _, c := range u.scopes[d.ID] {
				if !slices.Contains(comps, c) {
					comps = append(comps, c)
				}
			}
		}
		for len(comps) > 0 {
			n := min(len(comps), componentsPerRead)
			u.chunks = append(u.chunks, comps[:n])
			comps = comps[n:]
		}
		out = append(out, *u)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].last.Equal(out[j].last) {
			return out[i].last.Before(out[j].last)
		}
		return out[i].window < out[j].window
	})
	return out
}

func pace(window time.Duration) time.Duration {
	return max(window/windowReadsPerWindow, minWindowPace)
}

func (s *Service) lastRead(key string) time.Time {
	s.paceMu.Lock()
	defer s.paceMu.Unlock()
	return s.read[key]
}

func (s *Service) markRead(key string, at time.Time) {
	s.paceMu.Lock()
	defer s.paceMu.Unlock()
	if len(s.read) > paceSweepAbove {
		for k, last := range s.read {
			if at.Sub(last) > maxThresholdWindow {
				delete(s.read, k)
			}
		}
	}
	s.read[key] = at
}

const paceSweepAbove = 1024

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

// sweepUnit reads the unit's window and judges its definitions. A component
// whose read failed, came back unavailable or was cut short counts as
// partial for the definitions covering it, and only for those.
func (s *Service) sweepUnit(ctx context.Context, u sweepUnit, repoDefs []domain.ErrorDefinition, budget *int) {
	var groups []domain.OpsErrorGroup
	partialComp := map[uuid.UUID]bool{}
	for _, chunk := range u.chunks {
		res, err := s.errors.Errors(ctx, opsquery.ErrorsQuery{
			Environment:  sweptEnvironment,
			Since:        s.now().Add(-u.window),
			ComponentIDs: chunk,
		})
		if err != nil {
			log.Warn().Err(err).Dur("window", u.window).Str("repository_id", u.repo.String()).Msg("errordef: reading production errors failed")
			for _, c := range chunk {
				partialComp[c] = true
			}
			continue
		}
		for _, g := range res.Groups {
			if g.RepositoryID == u.repo {
				groups = append(groups, g)
			}
		}
		for _, c := range res.Unavailable {
			partialComp[c.ComponentID] = true
		}
		if res.Truncated {
			for _, c := range chunk {
				partialComp[c] = true
			}
		}
	}
	picks := make([]acting, len(groups))
	for i, g := range groups {
		picks[i] = s.actingOn(repoDefs, g)
	}
	for _, d := range u.defs {
		scope := u.scopes[d.ID]
		class := classOf(d.Action)
		var matched []domain.OpsErrorGroup
		for i, g := range groups {
			if !slices.Contains(scope, g.ComponentID) {
				continue
			}
			if pick := picks[i].of(class); pick != nil && pick.ID == d.ID {
				matched = append(matched, g)
			}
		}
		partial := false
		for _, c := range scope {
			partial = partial || partialComp[c]
		}
		if d.Action == domain.ErrorActionTask {
			s.openTasks(ctx, d, matched, budget)
		} else {
			s.judge(ctx, d, matched, partial)
		}
	}
}

// judge moves a thresholded definition between quiet and firing. A read
// that missed some of its components may fire it, never quiet it. A
// repository-wide definition counts one error-tracker issue once, however
// many of its components bind the same source to it.
func (s *Service) judge(ctx context.Context, d domain.ErrorDefinition, matched []domain.OpsErrorGroup, partial bool) {
	if d.ComponentID == nil {
		matched = distinctIssues(matched)
	}
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

// distinctIssues keeps one group per issue: an error tracker's issue is its
// data source and fingerprint, a cloud environment's group is its own.
func distinctIssues(groups []domain.OpsErrorGroup) []domain.OpsErrorGroup {
	type issue struct {
		source      uuid.UUID
		environment uuid.UUID
		fingerprint string
	}
	at := map[issue]int{}
	out := make([]domain.OpsErrorGroup, 0, len(groups))
	for _, g := range groups {
		k := issue{fingerprint: g.Fingerprint}
		if g.DataSourceID != nil {
			k.source = *g.DataSourceID
		} else {
			k.environment = g.EnvironmentID
		}
		if i, seen := at[k]; seen {
			if g.Count > out[i].Count {
				out[i] = g
			}
			continue
		}
		at[k] = len(out)
		out = append(out, g)
	}
	return out
}

// resolveOrphans resolves the live incidents of definitions that no longer
// open one: deleted (by hand, or with their component), dismissed, or moved
// to an action that opens none.
func (s *Service) resolveOrphans(ctx context.Context, acting []domain.ErrorDefinition) {
	closer, ok := s.incidents.(IncidentCloser)
	if !ok || ctx.Err() != nil {
		return
	}
	opens := make(map[uuid.UUID]bool, len(acting))
	for _, d := range acting {
		if d.Action.Thresholded() {
			opens[d.ID] = true
		}
	}
	live, err := closer.List(ctx, domain.IncidentFilter{
		Source: domain.IncidentSourceErrorDefinition, Statuses: domain.LiveIncidentStatuses, Limit: orphanSweepLimit,
	})
	if err != nil {
		log.Warn().Err(err).Msg("errordef: listing error definition incidents failed")
		return
	}
	for _, in := range live {
		id, ok := domain.ErrorDefinitionOfFingerprint(in.Fingerprint)
		if !ok || in.Source != domain.IncidentSourceErrorDefinition || opens[id] {
			continue
		}
		if _, err := closer.Resolve(ctx, in.ID, orphanResolveNote); err != nil {
			log.Warn().Err(err).Str("incident_id", in.ID.String()).Msg("errordef: resolving an orphaned definition's incident failed")
		}
	}
}

func incidentInput(d domain.ErrorDefinition, matched []domain.OpsErrorGroup, count int, t domain.ErrorAlertThreshold) domain.IncidentInput {
	sort.SliceStable(matched, func(i, j int) bool { return matched[i].Count > matched[j].Count })
	fingerprints := make([]string, 0, len(matched))
	var detail []string
	for i, g := range matched {
		fingerprints = append(fingerprints, g.Fingerprint)
		if i < detailMessages {
			detail = append(detail, fmt.Sprintf("%d× %s", g.Count, detailLine(g.Message)))
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

// detailLine is a message as one short line of plain text: production
// error text lands in the incident others (and agents) read, so it carries
// no line breaks, control characters or markup fences of its own.
func detailLine(message string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.TrimSpace(message) {
		switch {
		case unicode.IsSpace(r) || unicode.IsControl(r):
			space = true
			continue
		case r == '`':
			r = '\''
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	return domain.TruncateHead(b.String(), detailLineLen)
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
		}, s.now().Add(-staleClaim))
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
	message := domain.TruncateHead(strings.TrimSpace(g.Message), taskMessageLen)
	description := errorTaskKey.Render(errorTaskInput{
		Fence:       fenceFor(message, sample),
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
		Message:     message,
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

// fenceFor is a backtick fence longer than any backtick run in texts, so
// nothing read from production can close the block it is quoted in.
func fenceFor(texts ...string) string {
	longest := 0
	for _, t := range texts {
		run := 0
		for _, r := range t {
			if r == '`' {
				run++
				longest = max(longest, run)
				continue
			}
			run = 0
		}
	}
	return strings.Repeat("`", max(minFence, longest+1))
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
