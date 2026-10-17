package signals

import (
	"context"
	"fmt"
	"maps"
	"path"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const (
	// cloudAccountKey is the config field linking a cloud monitoring source
	// to the connected account whose environments it watches.
	cloudAccountKey = "cloud_account_id"

	// The label probe looks at the last hour only: what is scraped now, and
	// cheap on Mimir and Thanos.
	probeWindow = time.Hour
	probeStep   = 30 * time.Minute

	maxParamSetsPerRecipe   = 3
	maxSuggestionsPerSignal = 6
	minContainedNameLen     = 3
)

// Suggest runs the detector for one component environment and answers what
// List would afterwards. It offers (a) every recipe a source holds the
// detect metric of (on a log or trace source, a label, field or tag), with
// params taken from the label values that name the component in that
// environment, and (b) when the environment is bound
// to a cloud resource, every recipe for that resource kind on a source
// linked to the same cloud account, with params from the resource, and (c)
// on a broker source, the consumers whose names best name the component,
// through its kind's direct recipe, and (d) for a mobile component whose
// repository has a store app linked, the recipes for that store platform
// with params from the app (detectFromStoreApps; with no source of their
// kind, SourceOffers says which to create). Once the component's brokers are
// known (its links to queue resources), suggestions reading one of them come
// first; another system's stay on offer after them, since the links know only
// the libraries the discovery catalog names. A signal with a confirmed binding gets no suggestion,
// a dismissed one is never offered again, and only the detector's own
// pending suggestions are replaced: those of a source whose discovery, label
// probe or consumer listing failed this time are kept, and one offered again
// keeps its id.
func (s *Service) Suggest(ctx context.Context, componentID uuid.UUID, env domain.DeployEnvironment) (domain.ComponentSignals, error) {
	if !env.Valid() {
		return domain.ComponentSignals{}, fmt.Errorf("unknown environment %q: %w", env, ErrInvalidInput)
	}
	comp, err := s.components.GetComponent(ctx, componentID)
	if err != nil {
		return domain.ComponentSignals{}, err
	}
	existing, err := s.bindings.ListSignalBindings(ctx, componentID, env)
	if err != nil {
		return domain.ComponentSignals{}, err
	}
	l, err := s.lookup(ctx)
	if err != nil {
		return domain.ComponentSignals{}, err
	}
	envs, err := s.environments.ListEnvironments(ctx, comp.RepositoryID)
	if err != nil {
		return domain.ComponentSignals{}, err
	}
	var here []domain.ComponentEnvironment
	var bound *domain.ComponentEnvironment
	for _, e := range envs {
		if e.ComponentID != comp.ID || e.Environment != env {
			continue
		}
		here = append(here, e)
		if e.Status == domain.LinkConfirmed && e.Bound() {
			bound = &e
		}
	}
	role := comp.Role.Get()
	relevant := s.relevant(role)
	names := componentNames(comp, here)
	brokers := s.messagingSystems(ctx, comp)

	rd := s.reader(l)
	scope := s.cacheScope(ctx)
	var found []domain.SignalBinding
	var answered []uuid.UUID
	for _, ds := range l.order {
		got, ok := s.detectFromInventory(ctx, rd, scope, ds, relevant, names)
		found = append(found, got...)
		fromBroker, brokerOK := s.detectFromBroker(ctx, rd, ds, relevant, names)
		found = append(found, fromBroker...)
		if ok && brokerOK {
			answered = append(answered, ds.ID)
		}
	}
	if bound != nil {
		for _, ds := range l.order {
			if ds.Config[cloudAccountKey] == bound.AccountID.String() {
				found = append(found, detectFromResource(l, ds, relevant, *bound.Resource)...)
			}
		}
	}
	fromApps, offers := s.detectFromStoreApps(ctx, l, comp, relevant)
	found = append(fromApps, found...)
	rankByBrokers(l, found, brokers)
	if _, err := s.bindings.ReplaceDetectedSuggestions(ctx, comp.ID, env, answered, newSuggestions(found, existing)); err != nil {
		return domain.ComponentSignals{}, err
	}
	after, err := s.bindings.ListSignalBindings(ctx, comp.ID, env)
	if err != nil {
		return domain.ComponentSignals{}, err
	}
	out := s.assemble(ctx, l, comp, env, after)
	out.SourceOffers = offers
	return out, nil
}

// newSuggestions drops what must not be offered: a signal already bound, a
// target dismissed (or offered by a person) for that signal, a repeat.
func newSuggestions(found, existing []domain.SignalBinding) []domain.SignalBinding {
	bound := map[string]bool{}
	var kept []domain.SignalBinding
	for _, b := range existing {
		switch {
		case b.Status == domain.SignalBindingConfirmed:
			bound[b.Signal] = true
		case b.Status == domain.SignalBindingDismissed, b.Source != domain.SignalBindingDetected:
			kept = append(kept, b)
		}
	}
	perSignal := map[string]int{}
	out := []domain.SignalBinding{}
	for _, b := range found {
		if bound[b.Signal] || perSignal[b.Signal] >= maxSuggestionsPerSignal {
			continue
		}
		if slices.ContainsFunc(kept, func(o domain.SignalBinding) bool { return o.Signal == b.Signal && o.SameTarget(b) }) ||
			slices.ContainsFunc(out, func(o domain.SignalBinding) bool { return o.Signal == b.Signal && o.SameTarget(b) }) {
			continue
		}
		perSignal[b.Signal]++
		out = append(out, b)
	}
	return out
}

// detectFromInventory answers false when the source did not answer this
// time: its discovery, or a label probe, failed.
func (s *Service) detectFromInventory(ctx context.Context, rd *reader, scope string, ds domain.DataSource, relevant map[string]bool, names []string) ([]domain.SignalBinding, bool) {
	// A kind that lists nothing (the store kinds) has nothing to detect from,
	// and answers by not being asked.
	if !s.sources.CanDiscover(ds.Kind) {
		return nil, true
	}
	matches, ok := s.discover(ctx, rd, scope, ds)
	if !ok {
		return nil, false
	}
	ids := make([]string, 0, len(matches))
	for _, m := range matches {
		ids = append(ids, m.RecipeID)
	}
	sort.Strings(ids)
	answered := true
	var out []domain.SignalBinding
	for _, id := range slices.Compact(ids) {
		r, ok := rd.l.recipes[id]
		// A recipe for a cloud resource kind is offered from the environment's
		// binding, with params from the resource, never from the inventory.
		if !ok || r.SourceKind != ds.Kind || r.AppliesTo.ResourceKind != "" {
			continue
		}
		sigs := recipeSignals(r, relevant)
		if len(sigs) == 0 {
			continue
		}
		sets, probed := s.paramSets(ctx, rd, ds, r, names)
		answered = answered && probed
		for _, params := range sets {
			for _, sig := range sigs {
				out = append(out, suggestion(sig, ds.ID, r.ID, params))
			}
		}
	}
	return out, answered
}

func detectFromResource(l lookup, ds domain.DataSource, relevant map[string]bool, ref domain.CloudResourceRef) []domain.SignalBinding {
	ids := slices.Sorted(maps.Keys(l.recipes))
	var out []domain.SignalBinding
	for _, id := range ids {
		r := l.recipes[id]
		if r.SourceKind != ds.Kind || r.AppliesTo.ResourceKind == "" || r.AppliesTo.ResourceKind != ref.Kind {
			continue
		}
		params := r.ParamsFromResource(ref)
		for _, sig := range recipeSignals(r, relevant) {
			out = append(out, suggestion(sig, ds.ID, r.ID, params))
		}
	}
	return out
}

// detectFromBroker offers the consumers a broker source holds that best name
// the component (the label probe's score, at most maxParamSetsPerRecipe),
// each as the params of every recipe of the kind that takes exactly those
// params (its direct recipe), for the role's signals. False when the source
// did not answer.
func (s *Service) detectFromBroker(ctx context.Context, rd *reader, ds domain.DataSource, relevant map[string]bool, names []string) ([]domain.SignalBinding, bool) {
	if !s.sources.CanDiscoverQueues(ds.Kind) || len(names) == 0 {
		return nil, true
	}
	targets, err := rd.batch.DiscoverQueues(ctx, ds.ID)
	if err != nil {
		log.Debug().Err(err).Str("data_source", ds.ID.String()).Msg("signals: listing the broker's consumers failed; skipping the source")
		return nil, false
	}
	best := 0
	scores := make([]int, len(targets))
	for i, t := range targets {
		scores[i] = matchScore(t.Name, names)
		best = max(best, scores[i])
	}
	if best == 0 {
		return nil, true
	}
	var picked []domain.QueueTarget
	for i, t := range targets {
		if scores[i] == best {
			picked = append(picked, t)
		}
	}
	sort.SliceStable(picked, func(i, j int) bool { return picked[i].Name < picked[j].Name })
	if len(picked) > maxParamSetsPerRecipe {
		picked = picked[:maxParamSetsPerRecipe]
	}
	var out []domain.SignalBinding
	for _, t := range picked {
		for _, r := range directRecipes(rd.l, ds.Kind, t.Params) {
			for _, sig := range recipeSignals(r, relevant) {
				out = append(out, suggestion(sig, ds.ID, r.ID, t.Params))
			}
		}
	}
	return out, true
}

// directRecipes are the kind's recipes whose params are exactly the ones a
// discovered consumer is named by, in id order.
func directRecipes(l lookup, kind domain.DataSourceKind, params map[string]string) []domain.Recipe {
	if len(params) == 0 {
		return nil
	}
	want := slices.Sorted(maps.Keys(params))
	var out []domain.Recipe
	for _, id := range slices.Sorted(maps.Keys(l.recipes)) {
		r := l.recipes[id]
		if r.SourceKind == kind && r.AppliesTo.ResourceKind == "" && slices.Equal(slices.Sorted(slices.Values(r.Params)), want) {
			out = append(out, r)
		}
	}
	return out
}

func suggestion(signal string, dataSourceID uuid.UUID, recipeID string, params map[string]string) domain.SignalBinding {
	return domain.SignalBinding{
		Signal: signal, DataSourceID: dataSourceID, RecipeID: recipeID, Params: maps.Clone(params),
		Status: domain.SignalBindingSuggested, Source: domain.SignalBindingDetected,
	}
}

func recipeSignals(r domain.Recipe, relevant map[string]bool) []string {
	var out []string
	for _, id := range slices.Sorted(maps.Keys(r.Signals)) {
		if relevant[id] {
			out = append(out, id)
		}
	}
	return out
}

func (s *Service) discover(ctx context.Context, rd *reader, scope string, ds domain.DataSource) ([]domain.RecipeMatch, bool) {
	key := discoveryKey{scope: scope, id: ds.ID}
	if matches, ok := s.discovery.get(key, ds.UpdatedAt, s.now()); ok {
		return matches, true
	}
	d, err := rd.batch.Discover(ctx, ds.ID, "", 1)
	if err != nil {
		log.Debug().Err(err).Str("data_source", ds.ID.String()).Msg("signals: discovery failed; skipping the source")
		return nil, false
	}
	s.discovery.put(key, ds.UpdatedAt, s.now(), d.Recipes)
	return d.Recipes, true
}

// paramSets proposes values for a recipe's params: it groups the detect
// metric by the labels the params select on and keeps the label sets that
// best name the component. With nothing to go on it answers one empty set,
// a suggestion that still needs its params; false only when the probe
// failed.
func (s *Service) paramSets(ctx context.Context, rd *reader, ds domain.DataSource, r domain.Recipe, names []string) ([]map[string]string, bool) {
	none := []map[string]string{{}}
	labels := paramLabels(r)
	if len(labels) == 0 || r.Detect.Metric == "" || len(names) == 0 {
		return none, true
	}
	if s.sources.CanListLabelValues(ds.Kind) {
		return paramSetsFromValues(ctx, rd, ds, labels, names)
	}
	labelNames := slices.Sorted(maps.Values(labels))
	labelNames = slices.Compact(labelNames)
	now := s.now()
	res, err := rd.batch.Query(ctx, ds.ID, domain.SeriesQuery{
		Query: fmt.Sprintf("count by (%s) (%s)", strings.Join(labelNames, ", "), r.Detect.Metric),
		From:  now.Add(-probeWindow),
		To:    now,
		Step:  probeStep,
	})
	if err != nil {
		log.Debug().Err(err).Str("recipe", r.ID).Msg("signals: label probe failed; the suggestion needs its params")
		return none, false
	}
	type candidate struct {
		params map[string]string
		key    string
		score  int
	}
	var cands []candidate
	seen := map[string]bool{}
	for _, series := range res.Series {
		set := map[string]string{}
		score := 0
		for param, label := range labels {
			if v := series.Labels[label]; v != "" {
				set[param] = v
				score = max(score, matchScore(v, names))
			}
		}
		key := canonicalParams(set)
		if score == 0 || seen[key] {
			continue
		}
		seen[key] = true
		cands = append(cands, candidate{params: set, key: key, score: score})
	}
	if len(cands) == 0 {
		return none, true
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].score != cands[j].score {
			return cands[i].score > cands[j].score
		}
		return cands[i].key < cands[j].key
	})
	var out []map[string]string
	for _, c := range cands {
		if c.score < cands[0].score || len(out) == maxParamSetsPerRecipe {
			break
		}
		out = append(out, c.params)
	}
	return out, true
}

// paramLabels maps each declared param to the label (field, tag) it is
// compared to with equality in the recipe's queries, in the language of its
// kind: the first one found in signal order.
func paramLabels(r domain.Recipe) map[string]string {
	out := map[string]string{}
	for _, id := range slices.Sorted(maps.Keys(r.Signals)) {
		for _, f := range datasource.ParamFields(r.SourceKind, r.Signals[id]) {
			if _, done := out[f.Param]; !done && slices.Contains(r.Params, f.Param) {
				out[f.Param] = f.Field
			}
		}
	}
	return out
}

func canonicalParams(p map[string]string) string {
	var b strings.Builder
	for _, k := range slices.Sorted(maps.Keys(p)) {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(p[k])
		b.WriteByte(0)
	}
	return b.String()
}

// componentNames are what a label value naming the component would say: its
// name, its directory, its package name, the cloud resources it runs as.
func componentNames(c domain.Component, envs []domain.ComponentEnvironment) []string {
	var out []string
	add := func(n string) {
		n = strings.ToLower(strings.TrimSpace(n))
		if n != "" && !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	add(c.Name.Get())
	if c.Path != "" && c.Path != "." {
		add(path.Base(c.Path))
	}
	pkg := c.Stack.Get().PackageName
	add(pkg)
	if i := strings.LastIndex(pkg, "/"); i >= 0 {
		add(pkg[i+1:])
	}
	for _, e := range envs {
		if e.Resource != nil {
			add(e.Resource.Name)
		}
	}
	return out
}

// matchScore: 3 the value is a name (or ends in one after a "/" or ":"), 2
// the same ignoring case and punctuation, 1 it contains a name of at least
// three characters, 0 no relation.
func matchScore(value string, names []string) int {
	v := strings.ToLower(strings.TrimSpace(value))
	if v == "" {
		return 0
	}
	last := v
	if i := strings.LastIndexAny(v, "/:"); i >= 0 {
		last = v[i+1:]
	}
	nv, nl := squash(v), squash(last)
	best := 0
	for _, n := range names {
		nn := squash(n)
		switch {
		case v == n || last == n:
			return 3
		case nn != "" && (nv == nn || nl == nn):
			best = max(best, 2)
		case len(nn) >= minContainedNameLen && strings.Contains(nv, nn):
			best = max(best, 1)
		}
	}
	return best
}

func squash(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}
