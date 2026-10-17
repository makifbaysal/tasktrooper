package signals

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const (
	defaultQueuesBudget = 20 * time.Second
	queueSignalPrefix   = "queue."
)

// WithQueuesBudget bounds a whole queues read; a table still unread then
// answers a timeout.
func WithQueuesBudget(d time.Duration) Option {
	return func(s *Service) {
		if d > 0 {
			s.queuesBudget = d
		}
	}
}

// Queues answers the queues view of a component environment: one table per
// distinct thing its confirmed queue.* bindings read. A binding whose recipe
// declares tables.queues reads that table with the params the table uses,
// so two bindings agreeing on those share one; a binding of a broker kind
// (one with the queues capability) hands its own query to the kind's Table.
// Any other binding has no table. Each table stands alone: one that cannot
// be read carries its error. Read at most s.concurrency queries at once, each
// within the query timeout and all within the queues budget, once for every
// request asking at the same time; answers are kept 30 s per cache scope,
// component, environment, and version of the bindings, sources and recipes
// they were read through. A broker kind's query is grouped by what it reads,
// whatever it measures (domain.BrokerTarget): that kind's table ignores the
// metric.
func (s *Service) Queues(ctx context.Context, componentID uuid.UUID, env domain.DeployEnvironment) (domain.ComponentQueues, error) {
	if !env.Valid() {
		return domain.ComponentQueues{}, fmt.Errorf("unknown environment %q: %w", env, ErrInvalidInput)
	}
	if _, err := s.components.GetComponent(ctx, componentID); err != nil {
		return domain.ComponentQueues{}, err
	}
	bindings, err := s.bindings.ListSignalBindings(ctx, componentID, env)
	if err != nil {
		return domain.ComponentQueues{}, err
	}
	l, err := s.lookup(ctx)
	if err != nil {
		return domain.ComponentQueues{}, err
	}
	targets := s.queueTargets(l, bindings)
	scope := s.cacheScope(ctx)
	key := scope + "\x00" + componentID.String() + "\x00" + string(env) + "\x00" + l.version() + "\x00" + targetsKey(targets)
	if out, ok := s.queuesCache.get(key, s.now()); ok {
		return out, nil
	}
	return shared(ctx, &s.queuesFlight, s.queuesCache, scope, key, s.queuesCache.generation(scope), s.now,
		func(ctx context.Context) domain.ComponentQueues {
			return s.readQueues(ctx, l, componentID, env, targets)
		})
}

func (s *Service) queueTargets(l lookup, bindings []domain.SignalBinding) []*tableTarget {
	order := map[string]int{}
	for i, sig := range s.signals {
		order[sig.ID] = i
	}
	var confirmed []domain.SignalBinding
	for _, b := range bindings {
		if b.Status == domain.SignalBindingConfirmed && strings.HasPrefix(b.Signal, queueSignalPrefix) {
			confirmed = append(confirmed, b)
		}
	}
	slices.SortStableFunc(confirmed, func(a, b domain.SignalBinding) int { return order[a.Signal] - order[b.Signal] })

	var out []*tableTarget
	byKey := map[string]*tableTarget{}
	for _, b := range confirmed {
		t := newTableTarget(l, b)
		r, hasRecipe := l.recipes[b.RecipeID]
		_, hasTable := r.Tables[domain.RecipeTableQueues]
		var key string
		switch {
		case !t.known:
			key = "gone\x00" + b.DataSourceID.String()
		case b.Query == "" && hasRecipe && hasTable && r.SourceKind == t.ds.Kind:
			t.useRecipeTable(r, domain.RecipeTableQueues, b)
			key = t.recipeKey()
		case slices.Contains(t.ds.Capabilities, domain.CapabilityQueues):
			t.recipe = r
			t.params = maps.Clone(b.Params)
			if t.params == nil {
				t.params = map[string]string{}
			}
			key = "broker\x00" + b.DataSourceID.String() + "\x00" + b.RecipeID + "\x00" + canonicalParams(t.params) + "\x00" + b.Query
			if len(needsParams(l, b)) == 0 {
				if q, err := renderIn(l, b, displayStep, headlineQuantile); err == nil {
					if target, ok := domain.BrokerTarget(t.ds.Kind, q); ok {
						key = "broker\x00" + b.DataSourceID.String() + "\x00" + target
					}
				}
			}
		default:
			continue
		}
		if _, seen := byKey[key]; seen {
			continue
		}
		byKey[key] = t
		out = append(out, t)
		if !t.known || len(t.missing) > 0 {
			continue
		}
		if t.table != nil {
			t.renderTable()
			continue
		}
		if missing := needsParams(l, b); len(missing) > 0 {
			t.missing = missing
			continue
		}
		q, err := renderIn(l, b, displayStep, headlineQuantile)
		if err != nil {
			t.renderErr = err
			continue
		}
		t.queries[domain.TableValueKey] = q
	}
	return out
}

func targetsKey(targets []*tableTarget) string {
	h := sha256.New()
	for _, t := range targets {
		fmt.Fprintf(h, "%s\x00%s\x00%s\x00", t.first.ID, t.first.UpdatedAt.Format(time.RFC3339Nano), t.name)
		for _, col := range slices.Sorted(maps.Keys(t.queries)) {
			fmt.Fprintf(h, "%s\x00%s\x00", col, t.queries[col])
		}
	}
	return hex.EncodeToString(h.Sum(nil)[:16])
}

func (s *Service) readQueues(ctx context.Context, l lookup, componentID uuid.UUID, env domain.DeployEnvironment, targets []*tableTarget) domain.ComponentQueues {
	at := s.now()
	out := domain.ComponentQueues{ComponentID: componentID, Environment: env, ReadAt: at, Queues: make([]domain.QueueTable, len(targets))}
	reads := s.readTargets(ctx, l, targets, at, s.queuesBudget, "a queues read", false)
	for i, t := range targets {
		q := s.queueHeader(t)
		if r := reads[i]; r.err != nil {
			q.Error = r.err
		} else if r.columns != nil || r.rows != nil {
			q.Columns, q.Rows, q.Truncated = r.columns, r.rows, r.truncated
			if r.keyColumns != nil {
				q.KeyColumns = r.keyColumns
			}
		}
		out.Queues[i] = q
	}
	return out
}

func (s *Service) queueHeader(t *tableTarget) domain.QueueTable {
	q := domain.QueueTable{
		SignalBindingID: t.first.ID, Signal: t.first.Signal, DataSourceID: t.first.DataSourceID, RecipeID: t.first.RecipeID,
		MessagingSystem: t.recipe.AppliesTo.MessagingSystem, Params: maps.Clone(t.params),
		Columns: []domain.TableColumn{}, Rows: []map[string]any{},
	}
	if t.known {
		q.DataSourceName = t.ds.Name
		if q.MessagingSystem == "" && slices.Contains(t.ds.Capabilities, domain.CapabilityQueues) {
			q.MessagingSystem = string(t.ds.Kind)
		}
	}
	if t.table != nil {
		q.Columns = slices.Clone(t.table.Columns)
		q.KeyColumns = slices.Clone(t.table.KeyLabels)
	}
	return q
}

// joinTable puts each value column's answer onto the row its key labels'
// values name, in key order (numbers as numbers: partition 2 before 10). A
// result row carries its value under domain.TableValueKey, or under the
// column's own key where the kind answers columns of its own. More rows than
// MaxTableRows keep the largest of the first value column; unranked, those
// stay in key order.
func joinTable(t domain.RecipeTable, answers map[string]domain.TableResult, ranked bool) ([]domain.TableColumn, []map[string]any, bool) {
	rows := map[string]map[string]any{}
	keys := map[string][]string{}
	truncated := false
	values := t.ValueColumns()
	for _, c := range values {
		res := answers[c.Key]
		truncated = truncated || res.Truncated
		for _, r := range res.Rows {
			id := make([]string, len(t.KeyLabels))
			for i, k := range t.KeyLabels {
				id[i] = cellString(r[k])
			}
			rk := strings.Join(id, "\x00")
			row, ok := rows[rk]
			if !ok {
				row = map[string]any{}
				for i, k := range t.KeyLabels {
					row[k] = id[i]
				}
				for _, v := range values {
					row[v.Key] = nil
				}
				rows[rk], keys[rk] = row, id
			}
			if v, ok := r[domain.TableValueKey]; ok {
				row[c.Key] = v
			} else if v, ok := r[c.Key]; ok {
				row[c.Key] = v
			}
		}
	}
	ids := slices.SortedFunc(maps.Keys(rows), func(a, b string) int { return compareKeys(keys[a], keys[b]) })
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		out = append(out, rows[id])
	}
	if len(out) > datasource.MaxTableRows && len(values) > 0 {
		datasource.RankByValue(out, values[0].Key)
		out, truncated = out[:datasource.MaxTableRows], true
		if !ranked {
			slices.SortStableFunc(out, func(a, b map[string]any) int { return compareKeys(rowKey(t, a), rowKey(t, b)) })
		}
	}
	return slices.Clone(t.Columns), out, truncated
}

func rowKey(t domain.RecipeTable, row map[string]any) []string {
	id := make([]string, len(t.KeyLabels))
	for i, k := range t.KeyLabels {
		id[i] = cellString(row[k])
	}
	return id
}

func cellString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	}
	return fmt.Sprint(v)
}

func compareKeys(a, b []string) int {
	for i := range min(len(a), len(b)) {
		if c := compareCell(a[i], b[i]); c != 0 {
			return c
		}
	}
	return len(a) - len(b)
}

func compareCell(a, b string) int {
	fa, errA := strconv.ParseFloat(a, 64)
	fb, errB := strconv.ParseFloat(b, 64)
	switch {
	case errA == nil && errB == nil && fa != fb:
		if fa < fb {
			return -1
		}
		return 1
	case errA == nil && errB != nil:
		return -1
	case errA != nil && errB == nil:
		return 1
	}
	return strings.Compare(a, b)
}
