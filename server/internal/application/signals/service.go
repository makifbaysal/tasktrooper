// Package signals binds the vendor-neutral observability signals to
// components: which data source (and recipe, or raw query) feeds one signal
// of one component environment. It detects suggestions from what the
// sources hold and where the environment runs, and reads those bindings back
// as time series for one component and as golden values across components.
package signals

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/singleflight"

	"github.com/makifbaysal/tasktrooper/catalog"
	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	defaultConcurrency       = 4
	defaultGoldenConcurrency = 8
	defaultQueryTimeout      = 20 * time.Second

	// displayStep is what rendered_query is shown at: a one-minute step.
	displayStep = time.Minute

	maxQueryLen = 10000
	maxParamLen = 1024
	maxParams   = 32

	quantilePlaceholder = "$__quantile"
	quantileLabel       = "quantile"
	headlineQuantile    = 0.95
)

// quantiles are what a quantile signal is read at when its query takes
// $__quantile, each answered as a series named by its label.
var quantiles = []variant{{"p50", 0.5}, {"p75", 0.75}, {"p95", headlineQuantile}, {"p99", 0.99}}

// variant is one read of a binding: a quantile and the series name it gives,
// or the zero value for a single read as written.
type variant struct {
	label string
	value float64
}

type Components interface {
	GetComponent(ctx context.Context, id uuid.UUID) (domain.Component, error)
	ListAllComponents(ctx context.Context) ([]domain.Component, error)
}

type Environments interface {
	ListEnvironments(ctx context.Context, repositoryID uuid.UUID) ([]domain.ComponentEnvironment, error)
}

type Deps struct {
	Bindings     port.SignalBindingStore
	Sources      *datasource.Service
	Components   Components
	Environments Environments
	// Links is optional; without it the detector does not know which brokers
	// a component talks to.
	Links Links
	// StoreApps is optional; without it the detector does not know which
	// store apps a mobile component's repository ships.
	StoreApps StoreApps
	// Catalog holds golden.yaml; defaults to the embedded catalog.ObservabilityFS().
	Catalog fs.FS
}

type Service struct {
	bindings     port.SignalBindingStore
	sources      *datasource.Service
	components   Components
	environments Environments
	links        Links
	storeApps    StoreApps

	signals     []domain.Signal
	signalIndex map[string]domain.Signal
	golden      map[domain.ComponentRole][]string

	now               func() time.Time
	concurrency       int
	goldenConcurrency int
	queryTimeout      time.Duration
	seriesBudget      time.Duration
	goldenBudget      time.Duration
	queuesBudget      time.Duration
	tablesBudget      time.Duration

	scope        func(ctx context.Context) string
	discovery    *discoveryCache
	goldenCache  *answerCache[domain.GoldenBoard]
	queuesCache  *answerCache[domain.ComponentQueues]
	tablesCache  *answerCache[domain.ComponentTables]
	goldenFlight singleflight.Group
	queuesFlight singleflight.Group
	tablesFlight singleflight.Group
}

type Option func(*Service)

func WithClock(now func() time.Time) Option { return func(s *Service) { s.now = now } }

func WithConcurrency(n int) Option {
	return func(s *Service) {
		if n > 0 {
			s.concurrency = n
		}
	}
}

func WithGoldenConcurrency(n int) Option {
	return func(s *Service) {
		if n > 0 {
			s.goldenConcurrency = n
		}
	}
}

// WithQueryTimeout bounds each query a series or golden read runs.
func WithQueryTimeout(d time.Duration) Option {
	return func(s *Service) {
		if d > 0 {
			s.queryTimeout = d
		}
	}
}

// NewService fails on a golden list that names a signal or role the catalog
// does not have: a build defect, not a runtime condition.
func NewService(d Deps, opts ...Option) (*Service, error) {
	if d.Bindings == nil || d.Sources == nil || d.Components == nil || d.Environments == nil {
		return nil, errors.New("signals: bindings, sources, components and environments are required")
	}
	s := &Service{
		bindings:          d.Bindings,
		sources:           d.Sources,
		components:        d.Components,
		environments:      d.Environments,
		signals:           d.Sources.Signals(),
		signalIndex:       map[string]domain.Signal{},
		now:               func() time.Time { return time.Now().UTC() },
		concurrency:       defaultConcurrency,
		goldenConcurrency: defaultGoldenConcurrency,
		queryTimeout:      defaultQueryTimeout,
		seriesBudget:      defaultSeriesBudget,
		goldenBudget:      defaultGoldenBudget,
		queuesBudget:      defaultQueuesBudget,
		tablesBudget:      defaultTablesBudget,
		links:             d.Links,
		storeApps:         d.StoreApps,
		discovery:         newDiscoveryCache(),
		goldenCache:       newAnswerCache[domain.GoldenBoard](goldenCacheTTL, goldenCacheSize),
		queuesCache:       newAnswerCache[domain.ComponentQueues](queuesCacheTTL, queuesCacheSize),
		tablesCache:       newAnswerCache[domain.ComponentTables](queuesCacheTTL, queuesCacheSize),
	}
	for _, sig := range s.signals {
		s.signalIndex[sig.ID] = sig
	}
	fsys := d.Catalog
	if fsys == nil {
		fsys = catalog.ObservabilityFS()
	}
	golden, err := loadGolden(fsys, s.signals)
	if err != nil {
		return nil, fmt.Errorf("signals: %w", err)
	}
	s.golden = golden
	for _, opt := range opts {
		opt(s)
	}
	d.Sources.OnChange(s.sourceChanged)
	return s, nil
}

// BindingRequest is the body of PUT …/signals/:signal and the target half of
// a preview: a source plus a recipe and its params, or a raw query, which
// wins when both are given.
type BindingRequest struct {
	DataSourceID string            `json:"data_source_id"`
	RecipeID     string            `json:"recipe_id"`
	Params       map[string]string `json:"params"`
	Query        string            `json:"query"`
}

func (s *Service) List(ctx context.Context, componentID uuid.UUID, env domain.DeployEnvironment) (domain.ComponentSignals, error) {
	if !env.Valid() {
		return domain.ComponentSignals{}, fmt.Errorf("unknown environment %q: %w", env, ErrInvalidInput)
	}
	comp, err := s.components.GetComponent(ctx, componentID)
	if err != nil {
		return domain.ComponentSignals{}, err
	}
	bindings, err := s.bindings.ListSignalBindings(ctx, componentID, env)
	if err != nil {
		return domain.ComponentSignals{}, err
	}
	l, err := s.lookup(ctx)
	if err != nil {
		return domain.ComponentSignals{}, err
	}
	return s.assemble(ctx, l, comp, env, bindings), nil
}

func (s *Service) assemble(ctx context.Context, l lookup, comp domain.Component, env domain.DeployEnvironment, bindings []domain.SignalBinding) domain.ComponentSignals {
	role := comp.Role.Get()
	bySignal := map[string][]domain.SignalBinding{}
	for _, b := range bindings {
		if b.Status != domain.SignalBindingDismissed {
			bySignal[b.Signal] = append(bySignal[b.Signal], b)
		}
	}
	relevant := s.relevant(role)
	golden := s.goldenSet(role)
	out := domain.ComponentSignals{ComponentID: comp.ID, Role: role, Environment: env, Signals: []domain.ComponentSignal{}, SourceOffers: []domain.DataSourceOffer{}}
	for _, sig := range s.signals {
		if !relevant[sig.ID] && len(bySignal[sig.ID]) == 0 {
			continue
		}
		cs := domain.ComponentSignal{
			Signal: sig.ID, Kind: sig.Kind, Unit: sig.Unit, Golden: golden[sig.ID],
			Status: domain.SignalStateUnbound, Suggestions: []domain.SignalBindingView{},
		}
		for _, b := range bySignal[sig.ID] {
			v := s.view(ctx, l, b)
			if b.Status == domain.SignalBindingConfirmed {
				cs.Binding = &v
			} else {
				cs.Suggestions = append(cs.Suggestions, v)
			}
		}
		switch {
		case cs.Binding != nil:
			cs.Status = domain.SignalStateBound
		case len(cs.Suggestions) > 0:
			cs.Status = domain.SignalStateSuggested
		}
		out.Signals = append(out.Signals, cs)
	}
	return out
}

// Put makes a person's choice the confirmed binding of the signal, replacing
// whatever was confirmed there.
func (s *Service) Put(ctx context.Context, componentID uuid.UUID, env domain.DeployEnvironment, signal string, req BindingRequest) (domain.SignalBindingView, error) {
	if !env.Valid() {
		return domain.SignalBindingView{}, fmt.Errorf("unknown environment %q: %w", env, ErrInvalidInput)
	}
	if _, err := s.components.GetComponent(ctx, componentID); err != nil {
		return domain.SignalBindingView{}, err
	}
	l, err := s.lookup(ctx)
	if err != nil {
		return domain.SignalBindingView{}, err
	}
	b, err := s.validateTarget(ctx, l, signal, req)
	if err != nil {
		return domain.SignalBindingView{}, err
	}
	b.ComponentID, b.Environment, b.Source = componentID, env, domain.SignalBindingHuman
	saved, err := s.bindings.PutConfirmedSignalBinding(ctx, b)
	if err != nil {
		return domain.SignalBindingView{}, err
	}
	s.dropAnswers(ctx)
	return s.view(ctx, l, saved), nil
}

// Confirm promotes a suggestion; one still missing a recipe param is refused
// with that param as a field error, since it could not be read.
func (s *Service) Confirm(ctx context.Context, id uuid.UUID) (domain.SignalBindingView, error) {
	b, err := s.bindings.GetSignalBinding(ctx, id)
	if err != nil {
		return domain.SignalBindingView{}, err
	}
	l, err := s.lookup(ctx)
	if err != nil {
		return domain.SignalBindingView{}, err
	}
	if missing := needsParams(l, b); len(missing) > 0 {
		fe := datasource.FieldErrors{}
		for _, p := range missing {
			fe["params."+p] = datasource.FieldRequired
		}
		return domain.SignalBindingView{}, fe
	}
	confirmed, err := s.bindings.ConfirmSignalBinding(ctx, id)
	if err != nil {
		return domain.SignalBindingView{}, err
	}
	s.dropAnswers(ctx)
	return s.view(ctx, l, confirmed), nil
}

// Dismiss dismisses only a suggestion, in one guarded write: a binding
// confirmed meanwhile is ErrConfirmed, one already dismissed is no change.
func (s *Service) Dismiss(ctx context.Context, id uuid.UUID) error {
	b, err := s.bindings.DismissSignalBinding(ctx, id)
	if err != nil {
		return err
	}
	if b.Status == domain.SignalBindingConfirmed {
		return ErrConfirmed
	}
	return nil
}

func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	if err := s.bindings.DeleteSignalBinding(ctx, id); err != nil {
		return err
	}
	s.dropAnswers(ctx)
	return nil
}

// lookup is what one request resolves bindings against: the sources by id
// and the recipes (catalog plus user) by id.
type lookup struct {
	sources map[uuid.UUID]domain.DataSource
	order   []domain.DataSource
	recipes map[string]domain.Recipe
}

func (s *Service) lookup(ctx context.Context) (lookup, error) {
	list, err := s.sources.List(ctx)
	if err != nil {
		return lookup{}, err
	}
	recipes, err := s.sources.Recipes(ctx)
	if err != nil {
		return lookup{}, err
	}
	l := lookup{sources: make(map[uuid.UUID]domain.DataSource, len(list)), order: list, recipes: make(map[string]domain.Recipe, len(recipes))}
	for _, ds := range list {
		l.sources[ds.ID] = ds
	}
	for _, r := range recipes {
		l.recipes[r.ID] = r
	}
	return l, nil
}

func (s *Service) view(ctx context.Context, l lookup, b domain.SignalBinding) domain.SignalBindingView {
	if b.Params == nil {
		b.Params = map[string]string{}
	}
	v := domain.SignalBindingView{SignalBinding: b, NeedsParams: needsParams(l, b)}
	if ds, ok := l.sources[b.DataSourceID]; ok {
		v.DataSourceName, v.DataSourceKind = ds.Name, ds.Kind
	}
	if len(v.NeedsParams) > 0 {
		return v
	}
	if q, err := renderIn(l, b, displayStep, headlineQuantile); err == nil {
		v.RenderedQuery = q
	}
	return v
}

// needsParams lists the recipe params a binding has no value for. A raw
// query needs none: it carries its own values.
func needsParams(l lookup, b domain.SignalBinding) []string {
	out := []string{}
	if b.Query != "" {
		return out
	}
	r, ok := l.recipes[b.RecipeID]
	if !ok {
		return out
	}
	for _, p := range r.Params {
		if strings.TrimSpace(b.Params[p]) == "" && strings.TrimSpace(r.Defaults[p]) == "" {
			out = append(out, p)
		}
	}
	return out
}

func template(l lookup, b domain.SignalBinding) string {
	if b.Query != "" {
		return b.Query
	}
	return l.recipes[b.RecipeID].Signals[b.Signal]
}

func takesQuantile(query string) bool { return strings.Contains(query, quantilePlaceholder) }

// relevant: a role's signals are those listing it, plus its golden ones.
func (s *Service) relevant(role domain.ComponentRole) map[string]bool {
	out := s.goldenSet(role)
	for _, sig := range s.signals {
		if slices.Contains(sig.Roles, role) {
			out[sig.ID] = true
		}
	}
	return out
}

// GoldenSignals is the role's golden list, headline first.
func (s *Service) GoldenSignals(role domain.ComponentRole) []string {
	return slices.Clone(s.golden[role])
}

func (s *Service) goldenSet(role domain.ComponentRole) map[string]bool {
	out := map[string]bool{}
	for _, id := range s.golden[role] {
		out[id] = true
	}
	return out
}

// validateTarget checks a binding request against the catalog and the
// source, reporting every problem as a field error like the data source
// routes do.
func (s *Service) validateTarget(ctx context.Context, l lookup, signal string, req BindingRequest) (domain.SignalBinding, error) {
	fe := datasource.FieldErrors{}
	if _, ok := s.signalIndex[signal]; !ok {
		fe["signal"] = datasource.FieldInvalid
	}
	var ds domain.DataSource
	dsFound := false
	rawID := strings.TrimSpace(req.DataSourceID)
	id, perr := uuid.Parse(rawID)
	switch {
	case rawID == "":
		fe["data_source_id"] = datasource.FieldRequired
	case perr != nil:
		fe["data_source_id"] = datasource.FieldInvalid
	default:
		ds, dsFound = l.sources[id]
		if !dsFound {
			fe["data_source_id"] = datasource.FieldInvalid
		}
	}

	query := strings.TrimSpace(req.Query)
	recipeID := strings.TrimSpace(req.RecipeID)
	if query == "" && recipeID == "" {
		fe["recipe_id"] = datasource.FieldRequired
	}
	if len(query) > maxQueryLen {
		fe["query"] = datasource.FieldTooLong
	}
	params := map[string]string{}
	for k, v := range req.Params {
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch {
		case k == "":
		case len(v) > maxParamLen:
			fe["params."+k] = datasource.FieldTooLong
		default:
			params[k] = v
		}
	}
	if len(params) > maxParams {
		fe["params"] = datasource.FieldTooLong
	}

	if recipeID != "" {
		r, ok := l.recipes[recipeID]
		switch {
		case !ok:
			fe["recipe_id"] = datasource.FieldInvalid
		case dsFound && r.SourceKind != ds.Kind:
			fe["recipe_id"] = datasource.FieldInvalidOption
		default:
			if _, mapped := r.Signals[signal]; !mapped {
				fe["recipe_id"] = datasource.FieldInvalidOption
			}
			if query == "" {
				for _, p := range r.Params {
					if params[p] == "" && strings.TrimSpace(r.Defaults[p]) == "" {
						fe["params."+p] = datasource.FieldRequired
					}
				}
				for k := range params {
					if !slices.Contains(r.Params, k) {
						fe["params."+k] = datasource.FieldUnknown
					}
				}
			}
		}
	}
	if len(fe) > 0 {
		return domain.SignalBinding{}, fe
	}
	b := domain.SignalBinding{Signal: signal, DataSourceID: id, RecipeID: recipeID, Params: params, Query: query}
	if query != "" {
		if _, err := renderIn(l, b, displayStep, headlineQuantile); err != nil {
			if errors.Is(err, datasource.ErrInvalidInput) {
				return domain.SignalBinding{}, datasource.FieldErrors{"query": datasource.FieldInvalid}
			}
			return domain.SignalBinding{}, err
		}
	}
	return b, nil
}
