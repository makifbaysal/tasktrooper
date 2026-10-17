// Package errordef keeps error definitions: what one kind of runtime error
// means (a bug, an expected outcome, a failing dependency, …) and what a
// matching error group sets off — hidden from the Errors inbox, an incident
// once it recurs past a threshold, or a board task for the component's
// developer. Matching happens on read: every Errors inbox answer is
// annotated with the definition each group matches, and a background sweep
// carries out the alert, incident and task actions.
package errordef

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/application/opsquery"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	maxNameLen          = 200
	maxRationaleLen     = 4000
	maxEvidence         = 20
	maxEvidenceLen      = 500
	maxFingerprintLen   = 500
	maxExceptionTypeLen = 200
	maxEndpointLen      = 500
	maxAttributes       = 20
	maxAttributeKeyLen  = 100
	maxAttributeValLen  = 500
	maxProposals        = 50
	// MaxDefinitionsPerRepository bounds a repository's active and suggested
	// definitions: every one is matched against every group of every read.
	MaxDefinitionsPerRepository = 200

	minThresholdWindow = time.Minute
	maxThresholdWindow = 7 * 24 * time.Hour
	maxThresholdCount  = 1_000_000

	definitionCacheTTL = 30 * time.Second
)

// DefaultAlertThreshold and DefaultIncidentThreshold apply to a definition
// that sets none.
var (
	DefaultAlertThreshold    = domain.ErrorAlertThreshold{Count: 10, WindowSeconds: 3600}
	DefaultIncidentThreshold = domain.ErrorAlertThreshold{Count: 1, WindowSeconds: 3600}
)

type Components interface {
	GetComponent(ctx context.Context, id uuid.UUID) (domain.Component, error)
	ListComponents(ctx context.Context, repositoryID uuid.UUID) ([]domain.Component, error)
}

type Repositories interface {
	Get(ctx context.Context, id uuid.UUID) (domain.Repository, error)
}

// ErrorsReader is opsquery.Service.
type ErrorsReader interface {
	Errors(ctx context.Context, q opsquery.ErrorsQuery) (domain.OpsErrors, error)
}

// IncidentIngester is prodops.Service.
type IncidentIngester interface {
	Ingest(ctx context.Context, in domain.IncidentInput) (domain.Incident, error)
}

type TaskBoard interface {
	CreateTask(ctx context.Context, repositoryID uuid.UUID, req domain.CreateBoardTaskRequest) (domain.BoardTask, error)
}

type Roles interface {
	RoleByKey(ctx context.Context, key string) (domain.AgentRole, error)
	AgentForRole(ctx context.Context, roleID uuid.UUID, area string) (*uuid.UUID, error)
}

type DefectTypes interface {
	DefectTaskType(ctx context.Context) (domain.TaskType, error)
}

// Deps: Errors, Incidents, Tasks and Roles serve the sweep only; without
// Errors no sweep runs, without Incidents the alert and incident actions do
// nothing, without Tasks the task action does nothing.
type Deps struct {
	Store        port.ErrorDefinitionStore
	Components   Components
	Repositories Repositories
	Errors       ErrorsReader
	Incidents    IncidentIngester
	Tasks        TaskBoard
	Roles        Roles
	DefectTypes  DefectTypes
}

type Service struct {
	store      port.ErrorDefinitionStore
	components Components
	repos      Repositories
	errors     ErrorsReader
	incidents  IncidentIngester
	tasks      TaskBoard
	roles      Roles
	defects    DefectTypes

	regexes *regexCache
	now     func() time.Time

	cacheMu sync.Mutex
	cache   map[uuid.UUID]cachedDefinitions

	interval time.Duration
	runMu    sync.Mutex
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	sweepMu  sync.Mutex

	scope  func(ctx context.Context) string
	paceMu sync.Mutex
	read   map[string]time.Time
}

type cachedDefinitions struct {
	at   time.Time
	defs []domain.ErrorDefinition
}

type Option func(*Service)

func WithClock(now func() time.Time) Option { return func(s *Service) { s.now = now } }

// WithInterval is how often the sweep reads production errors.
func WithInterval(d time.Duration) Option {
	return func(s *Service) {
		if d > 0 {
			s.interval = d
		}
	}
}

func NewService(d Deps, opts ...Option) (*Service, error) {
	if d.Store == nil || d.Components == nil {
		return nil, errors.New("errordef: store and components are required")
	}
	s := &Service{
		store:      d.Store,
		components: d.Components,
		repos:      d.Repositories,
		errors:     d.Errors,
		incidents:  d.Incidents,
		tasks:      d.Tasks,
		roles:      d.Roles,
		defects:    d.DefectTypes,
		regexes:    newRegexCache(),
		now:        func() time.Time { return time.Now().UTC() },
		cache:      map[uuid.UUID]cachedDefinitions{},
		interval:   DefaultSweepInterval,
		read:       map[string]time.Time{},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// DefinitionRequest is the body of POST and PATCH /v1/error-definitions: an
// absent field keeps what the definition had; component_id "" or
// alert_threshold null clear theirs. Match replaces the whole match.
type DefinitionRequest struct {
	RepositoryID   *string                                     `json:"repository_id"`
	ComponentID    *string                                     `json:"component_id"`
	Name           *string                                     `json:"name"`
	Match          *domain.ErrorMatch                          `json:"match"`
	Category       *string                                     `json:"category"`
	Severity       *string                                     `json:"severity"`
	Action         *string                                     `json:"action"`
	AlertThreshold domain.Nullable[domain.ErrorAlertThreshold] `json:"alert_threshold"`
	Rationale      *string                                     `json:"rationale"`
	Evidence       *[]string                                   `json:"evidence"`
}

type ListFilter struct {
	RepositoryID *uuid.UUID
	ComponentID  *uuid.UUID
	Status       domain.ErrorDefinitionStatus
}

func (s *Service) List(ctx context.Context, f ListFilter) ([]domain.ErrorDefinition, error) {
	filter := domain.ErrorDefinitionFilter{ComponentID: f.ComponentID}
	if f.RepositoryID != nil {
		filter.RepositoryIDs = []uuid.UUID{*f.RepositoryID}
	}
	if f.Status != "" {
		if !domain.ValidErrorDefinitionStatus(f.Status) {
			return nil, FieldErrors{"status": datasource.FieldInvalid}
		}
		filter.Statuses = []domain.ErrorDefinitionStatus{f.Status}
	}
	return s.store.ListErrorDefinitions(ctx, filter)
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (domain.ErrorDefinition, error) {
	return s.store.GetErrorDefinition(ctx, id)
}

// Create stores a person's definition: source human, status active.
func (s *Service) Create(ctx context.Context, req DefinitionRequest) (domain.ErrorDefinition, error) {
	d, err := s.apply(ctx, domain.ErrorDefinition{Evidence: []string{}}, req, true)
	if err != nil {
		return domain.ErrorDefinition{}, err
	}
	held, err := s.store.ListErrorDefinitions(ctx, domain.ErrorDefinitionFilter{
		RepositoryIDs: []uuid.UUID{d.RepositoryID},
		Statuses:      []domain.ErrorDefinitionStatus{domain.ErrorDefinitionActive, domain.ErrorDefinitionSuggested},
	})
	if err != nil {
		return domain.ErrorDefinition{}, err
	}
	if len(held) >= MaxDefinitionsPerRepository {
		return domain.ErrorDefinition{}, ErrTooManyDefinitions
	}
	d.Source, d.Status = domain.ErrorDefinitionHuman, domain.ErrorDefinitionActive
	out, err := s.store.CreateErrorDefinition(ctx, d)
	if err != nil {
		return domain.ErrorDefinition{}, err
	}
	s.forget(out.RepositoryID)
	return out, nil
}

// Update never moves a definition to another repository.
func (s *Service) Update(ctx context.Context, id uuid.UUID, req DefinitionRequest) (domain.ErrorDefinition, error) {
	cur, err := s.store.GetErrorDefinition(ctx, id)
	if err != nil {
		return domain.ErrorDefinition{}, err
	}
	if req.RepositoryID != nil && strings.TrimSpace(*req.RepositoryID) != cur.RepositoryID.String() {
		return domain.ErrorDefinition{}, FieldErrors{"repository_id": datasource.FieldNotPermitted}
	}
	req.RepositoryID = nil
	d, err := s.apply(ctx, cur, req, false)
	if err != nil {
		return domain.ErrorDefinition{}, err
	}
	out, err := s.store.UpdateErrorDefinition(ctx, d)
	if err != nil {
		return domain.ErrorDefinition{}, err
	}
	s.forget(out.RepositoryID)
	if !out.Action.Thresholded() {
		s.quiet(ctx, cur)
	}
	return out, nil
}

// Delete resolves the incident the definition's firing alert or incident
// action holds open: no sweep will judge it again.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	cur, err := s.store.GetErrorDefinition(ctx, id)
	if err != nil {
		return err
	}
	if err := s.store.DeleteErrorDefinition(ctx, id); err != nil {
		return err
	}
	s.forget(cur.RepositoryID)
	s.quiet(ctx, cur)
	return nil
}

// quiet resolves the incident of a definition that was firing and no longer
// opens one. A resolution that does not go through is left to the sweep's
// orphan pass.
func (s *Service) quiet(ctx context.Context, was domain.ErrorDefinition) {
	if was.FiringSince == nil || !was.Action.Thresholded() || s.incidents == nil {
		return
	}
	in := incidentInput(was, nil, 0, threshold(was))
	in.Resolved = true
	if _, err := s.incidents.Ingest(ctx, in); err != nil {
		log.Warn().Err(err).Str("definition_id", was.ID.String()).Msg("errordef: resolving the incident of a definition that stopped acting failed")
	}
}

// Accept turns a suggestion into an active definition; accepting an active
// one is no change.
func (s *Service) Accept(ctx context.Context, id uuid.UUID) (domain.ErrorDefinition, error) {
	return s.decide(ctx, id, domain.ErrorDefinitionActive)
}

// Dismiss keeps the suggestion as dismissed, so the same match is never
// suggested again; dismissing a dismissed one is no change.
func (s *Service) Dismiss(ctx context.Context, id uuid.UUID) (domain.ErrorDefinition, error) {
	return s.decide(ctx, id, domain.ErrorDefinitionDismissed)
}

func (s *Service) decide(ctx context.Context, id uuid.UUID, to domain.ErrorDefinitionStatus) (domain.ErrorDefinition, error) {
	cur, err := s.store.GetErrorDefinition(ctx, id)
	if err != nil {
		return domain.ErrorDefinition{}, err
	}
	if cur.Status == to {
		return cur, nil
	}
	if cur.Status != domain.ErrorDefinitionSuggested {
		return domain.ErrorDefinition{}, ErrNotSuggested
	}
	out, err := s.store.SetErrorDefinitionStatus(ctx, id, to)
	if err != nil {
		return domain.ErrorDefinition{}, err
	}
	s.forget(out.RepositoryID)
	return out, nil
}

// Proposal is one definition the release engineer proposes.
type Proposal struct {
	DefinitionRequest
	Source domain.ErrorDefinitionSource
}

// ProposalOutcome: a proposal whose component and match equal an existing
// definition's, in any status, is not stored again; Existing names it.
type ProposalOutcome struct {
	Name     string                  `json:"name"`
	Stored   *domain.ErrorDefinition `json:"-"`
	Existing *uuid.UUID              `json:"existing_id,omitempty"`
	Fields   FieldErrors             `json:"fields,omitempty"`
}

// Propose stores each valid proposal as a suggestion of its source (ai or
// code). One invalid proposal is reported and the others still stand.
func (s *Service) Propose(ctx context.Context, repositoryID uuid.UUID, proposals []Proposal) ([]ProposalOutcome, error) {
	if len(proposals) > maxProposals {
		return nil, FieldErrors{"definitions": datasource.FieldTooLong}
	}
	existing, err := s.store.ListErrorDefinitions(ctx, domain.ErrorDefinitionFilter{RepositoryIDs: []uuid.UUID{repositoryID}})
	if err != nil {
		return nil, err
	}
	repo := repositoryID.String()
	out := make([]ProposalOutcome, 0, len(proposals))
	stored := false
	held := 0
	for _, e := range existing {
		if e.Status != domain.ErrorDefinitionDismissed {
			held++
		}
	}
	for _, p := range proposals {
		p.RepositoryID = &repo
		outcome := ProposalOutcome{}
		if p.Name != nil {
			outcome.Name = strings.TrimSpace(*p.Name)
		}
		d, err := s.apply(ctx, domain.ErrorDefinition{Evidence: []string{}}, p.DefinitionRequest, true)
		var fe FieldErrors
		switch {
		case errors.As(err, &fe):
			outcome.Fields = fe
			out = append(out, outcome)
			continue
		case err != nil:
			return nil, err
		}
		if dup := sameDefinition(existing, d); dup != nil {
			outcome.Existing = &dup.ID
			out = append(out, outcome)
			continue
		}
		if held >= MaxDefinitionsPerRepository {
			outcome.Fields = FieldErrors{"definitions": datasource.FieldTooLong}
			out = append(out, outcome)
			continue
		}
		d.Source, d.Status = p.Source, domain.ErrorDefinitionSuggested
		if d.Source != domain.ErrorDefinitionCode {
			d.Source = domain.ErrorDefinitionAI
		}
		saved, err := s.store.CreateErrorDefinition(ctx, d)
		if errors.Is(err, port.ErrErrorDefinitionExists) {
			if existing, err = s.store.ListErrorDefinitions(ctx, domain.ErrorDefinitionFilter{RepositoryIDs: []uuid.UUID{repositoryID}}); err != nil {
				return nil, err
			}
			if dup := sameDefinition(existing, d); dup != nil {
				outcome.Existing = &dup.ID
			}
			out = append(out, outcome)
			continue
		}
		if err != nil {
			return nil, err
		}
		existing = append(existing, saved)
		held++
		outcome.Stored = &saved
		out = append(out, outcome)
		stored = true
	}
	if stored {
		s.forget(repositoryID)
	}
	return out, nil
}

func sameDefinition(all []domain.ErrorDefinition, d domain.ErrorDefinition) *domain.ErrorDefinition {
	for i := range all {
		e := &all[i]
		if sameComponent(e.ComponentID, d.ComponentID) && sameMatch(e.Match, d.Match) {
			return e
		}
	}
	return nil
}

func sameComponent(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func sameMatch(a, b domain.ErrorMatch) bool {
	if a.Fingerprint != b.Fingerprint || a.MessageRegex != b.MessageRegex || a.ExceptionType != b.ExceptionType ||
		a.Endpoint != b.Endpoint || a.Status != b.Status || len(a.Attributes) != len(b.Attributes) {
		return false
	}
	for k, v := range a.Attributes {
		if bv, ok := b.Attributes[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

// apply sets req onto base and validates the result, reporting every
// problem as a field error.
func (s *Service) apply(ctx context.Context, base domain.ErrorDefinition, req DefinitionRequest, create bool) (domain.ErrorDefinition, error) {
	d := base
	fe := FieldErrors{}
	if req.RepositoryID != nil {
		id, err := uuid.Parse(strings.TrimSpace(*req.RepositoryID))
		if err != nil {
			fe["repository_id"] = datasource.FieldInvalid
		} else {
			d.RepositoryID = id
		}
	}
	if create && d.RepositoryID == uuid.Nil {
		if _, bad := fe["repository_id"]; !bad {
			fe["repository_id"] = datasource.FieldRequired
		}
	}
	if req.ComponentID != nil {
		raw := strings.TrimSpace(*req.ComponentID)
		if raw == "" {
			d.ComponentID = nil
		} else if id, err := uuid.Parse(raw); err != nil {
			fe["component_id"] = datasource.FieldInvalid
		} else {
			d.ComponentID = &id
		}
	}
	if req.Name != nil {
		d.Name = strings.TrimSpace(*req.Name)
	}
	switch {
	case d.Name == "":
		fe["name"] = datasource.FieldRequired
	case len(d.Name) > maxNameLen:
		fe["name"] = datasource.FieldTooLong
	}
	if req.Match != nil {
		d.Match = normalizeMatch(*req.Match)
	}
	validateMatch(d.Match, fe)
	if req.Category != nil {
		d.Category = domain.ErrorCategory(strings.TrimSpace(*req.Category))
	}
	if d.Category == "" {
		fe["category"] = datasource.FieldRequired
	} else if !domain.ValidErrorCategory(d.Category) {
		fe["category"] = datasource.FieldInvalid
	}
	if req.Severity != nil {
		d.Severity = domain.IncidentSeverity(strings.TrimSpace(*req.Severity))
	}
	if d.Severity == "" {
		d.Severity = domain.IncidentSeverityMedium
	}
	if d.Severity.Rank() == 0 {
		fe["severity"] = datasource.FieldInvalid
	}
	if req.Action != nil {
		d.Action = domain.ErrorAction(strings.TrimSpace(*req.Action))
	}
	if d.Action == "" {
		d.Action = domain.ErrorActionNone
	}
	if !domain.ValidErrorAction(d.Action) {
		fe["action"] = datasource.FieldInvalid
	}
	if req.AlertThreshold.Present {
		d.AlertThreshold = req.AlertThreshold.Value
	}
	if d.AlertThreshold != nil {
		validateThreshold(*d.AlertThreshold, d.Action, fe)
	}
	if req.Rationale != nil {
		d.Rationale = strings.TrimSpace(*req.Rationale)
	}
	if len(d.Rationale) > maxRationaleLen {
		fe["rationale"] = datasource.FieldTooLong
	}
	if req.Evidence != nil {
		d.Evidence = []string{}
		for _, e := range *req.Evidence {
			if e = strings.TrimSpace(e); e != "" {
				d.Evidence = append(d.Evidence, e)
			}
		}
	}
	if len(d.Evidence) > maxEvidence {
		fe["evidence"] = datasource.FieldTooLong
	}
	for _, e := range d.Evidence {
		if len(e) > maxEvidenceLen {
			fe["evidence"] = datasource.FieldTooLong
		}
	}
	if err := s.validateComponent(ctx, d, fe); err != nil {
		return domain.ErrorDefinition{}, err
	}
	if len(fe) > 0 {
		return domain.ErrorDefinition{}, fe
	}
	return d, nil
}

func (s *Service) validateComponent(ctx context.Context, d domain.ErrorDefinition, fe FieldErrors) error {
	if d.ComponentID == nil {
		return nil
	}
	if _, bad := fe["component_id"]; bad {
		return nil
	}
	comp, err := s.components.GetComponent(ctx, *d.ComponentID)
	switch {
	case errors.Is(err, port.ErrNotFound):
		fe["component_id"] = datasource.FieldInvalid
	case err != nil:
		return err
	case d.RepositoryID != uuid.Nil && comp.RepositoryID != d.RepositoryID:
		fe["component_id"] = datasource.FieldInvalidOption
	}
	return nil
}

func normalizeMatch(m domain.ErrorMatch) domain.ErrorMatch {
	m.Fingerprint = strings.TrimSpace(m.Fingerprint)
	m.MessageRegex = strings.TrimSpace(m.MessageRegex)
	m.ExceptionType = strings.TrimSpace(m.ExceptionType)
	m.Endpoint = strings.TrimSpace(m.Endpoint)
	if len(m.Attributes) == 0 {
		m.Attributes = nil
		return m
	}
	attrs := make(map[string]string, len(m.Attributes))
	for k, v := range m.Attributes {
		if k = strings.TrimSpace(k); k != "" {
			attrs[k] = strings.TrimSpace(v)
		}
	}
	m.Attributes = attrs
	return m
}

func validateMatch(m domain.ErrorMatch, fe FieldErrors) {
	if m.Empty() {
		fe["match"] = datasource.FieldRequired
		return
	}
	if len(m.Fingerprint) > maxFingerprintLen {
		fe["match.fingerprint"] = datasource.FieldTooLong
	}
	if m.MessageRegex != "" {
		if _, err := compileBounded(m.MessageRegex); err != nil {
			if errors.Is(err, errRegexTooLong) || errors.Is(err, errRegexTooLarge) {
				fe["match.message_regex"] = datasource.FieldTooLong
			} else {
				fe["match.message_regex"] = datasource.FieldInvalid
			}
		}
	}
	if len(m.ExceptionType) > maxExceptionTypeLen {
		fe["match.exception_type"] = datasource.FieldTooLong
	}
	if len(m.Endpoint) > maxEndpointLen {
		fe["match.endpoint"] = datasource.FieldTooLong
	}
	if m.Status != 0 && (m.Status < 100 || m.Status > 599) {
		fe["match.status"] = datasource.FieldInvalid
	}
	if len(m.Attributes) > maxAttributes {
		fe["match.attributes"] = datasource.FieldTooLong
	}
	for k, v := range m.Attributes {
		if len(k) > maxAttributeKeyLen || len(v) > maxAttributeValLen {
			fe["match.attributes."+k] = datasource.FieldTooLong
		}
	}
}

func validateThreshold(t domain.ErrorAlertThreshold, action domain.ErrorAction, fe FieldErrors) {
	if !action.Thresholded() {
		fe["alert_threshold"] = datasource.FieldInvalidOption
		return
	}
	if t.Count < 1 || t.Count > maxThresholdCount {
		fe["alert_threshold.count"] = datasource.FieldInvalid
	}
	if w := t.Window(); w < minThresholdWindow || w > maxThresholdWindow {
		fe["alert_threshold.window_seconds"] = datasource.FieldInvalid
	}
}

// threshold is what the sweep judges a definition by.
func threshold(d domain.ErrorDefinition) domain.ErrorAlertThreshold {
	if d.AlertThreshold != nil {
		return *d.AlertThreshold
	}
	if d.Action == domain.ErrorActionIncident {
		return DefaultIncidentThreshold
	}
	return DefaultAlertThreshold
}

// active answers the repository's active definitions, from a cache a write
// through this service clears.
func (s *Service) active(ctx context.Context, repositoryIDs []uuid.UUID) (map[uuid.UUID][]domain.ErrorDefinition, error) {
	now := s.now()
	out := make(map[uuid.UUID][]domain.ErrorDefinition, len(repositoryIDs))
	var missing []uuid.UUID
	s.cacheMu.Lock()
	for _, id := range repositoryIDs {
		if c, ok := s.cache[id]; ok && now.Sub(c.at) < definitionCacheTTL {
			out[id] = c.defs
			continue
		}
		missing = append(missing, id)
	}
	s.cacheMu.Unlock()
	if len(missing) == 0 {
		return out, nil
	}
	defs, err := s.store.ListErrorDefinitions(ctx, domain.ErrorDefinitionFilter{
		RepositoryIDs: missing,
		Statuses:      []domain.ErrorDefinitionStatus{domain.ErrorDefinitionActive},
	})
	if err != nil {
		return nil, fmt.Errorf("read error definitions: %w", err)
	}
	fresh := make(map[uuid.UUID][]domain.ErrorDefinition, len(missing))
	for _, id := range missing {
		fresh[id] = []domain.ErrorDefinition{}
	}
	for _, d := range defs {
		fresh[d.RepositoryID] = append(fresh[d.RepositoryID], d)
	}
	s.cacheMu.Lock()
	for id, list := range fresh {
		s.cache[id] = cachedDefinitions{at: now, defs: list}
		out[id] = list
	}
	s.cacheMu.Unlock()
	return out, nil
}

func (s *Service) forget(repositoryID uuid.UUID) {
	s.cacheMu.Lock()
	delete(s.cache, repositoryID)
	s.cacheMu.Unlock()
}
