package signals

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const defaultTablesBudget = 20 * time.Second

// WithTablesBudget bounds a whole endpoints, web or crashes read; a table
// still unread then answers a timeout.
func WithTablesBudget(d time.Duration) Option {
	return func(s *Service) {
		if d > 0 {
			s.tablesBudget = d
		}
	}
}

// tableTarget is one table to read: first is the first binding, in catalog
// order, of those reading it, and queries are what to run (one per value
// column of a recipe table, or a broker kind's own query under
// domain.TableValueKey).
type tableTarget struct {
	first     domain.SignalBinding
	ds        domain.DataSource
	known     bool
	recipe    domain.Recipe
	name      string
	table     *domain.RecipeTable
	params    map[string]string
	queries   map[string]string
	missing   []string
	renderErr error
}

func newTableTarget(l lookup, b domain.SignalBinding) *tableTarget {
	t := &tableTarget{first: b, params: map[string]string{}, queries: map[string]string{}}
	t.ds, t.known = l.sources[b.DataSourceID]
	return t
}

// useRecipeTable points t at r's table name with the params b gives it (r's
// default where b leaves one empty); a param still empty is missing.
func (t *tableTarget) useRecipeTable(r domain.Recipe, name string, b domain.SignalBinding) {
	table := r.Tables[name]
	t.recipe, t.name, t.table = r, name, &table
	params := r.WithDefaults(b.Params)
	for _, p := range datasource.TableParams(r, name) {
		if v := strings.TrimSpace(params[p]); v != "" {
			t.params[p] = v
		} else {
			t.missing = append(t.missing, p)
		}
	}
}

func (t *tableTarget) recipeKey() string {
	return "recipe\x00" + t.ds.ID.String() + "\x00" + t.recipe.ID + "\x00" + t.name + "\x00" + canonicalParams(t.params)
}

func (t *tableTarget) renderTable() {
	for _, c := range t.table.ValueColumns() {
		q, err := datasource.RenderKindQuery(t.ds.Kind, t.table.Queries[c.Key], t.params, datasource.RenderOptions{Step: displayStep})
		if err != nil {
			t.renderErr = err
			return
		}
		t.queries[c.Key] = q
	}
}

// tableRead is one target's answer, or the error that kept it unread.
type tableRead struct {
	columns    []domain.TableColumn
	keyColumns []string
	rows       []map[string]any
	truncated  bool
	err        *domain.SignalReadError
}

// readTargets runs every distinct query of each readable target once, at
// most s.concurrency at once, each within the query timeout and all within
// budget (what names the read in a timeout's message), and joins each
// target's answers. Ranked: a recipe table whose value columns share one
// query keeps the order its source answered in (the most viewed page
// first), unless the source answers one value per row in no order of its
// own (an instant PromQL vector), and one joined from several is ordered by
// its first value column, largest first; unranked rows are in key order (a
// queue's partitions).
func (s *Service) readTargets(ctx context.Context, l lookup, targets []*tableTarget, at time.Time, budget time.Duration, what string, ranked bool) []tableRead {
	out := make([]tableRead, len(targets))
	type job struct {
		target int
		query  string
	}
	var jobs []job
	for i, t := range targets {
		switch {
		case !t.known:
			out[i].err = &domain.SignalReadError{Code: domain.SignalErrNotFound, Message: "the data source no longer exists"}
		case len(t.missing) > 0:
			out[i].err = &domain.SignalReadError{Code: domain.SignalErrNeedsParams, Message: "no value for " + strings.Join(t.missing, ", ")}
		case t.renderErr != nil:
			out[i].err = readError(t.renderErr, s.queryTimeout)
		default:
			seen := map[string]bool{}
			for _, col := range slices.Sorted(maps.Keys(t.queries)) {
				if q := t.queries[col]; !seen[q] {
					seen[q] = true
					jobs = append(jobs, job{target: i, query: q})
				}
			}
		}
	}

	rd := s.reader(l)
	bctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	results := fanOut(bctx, len(jobs), s.concurrency, s.queryTimeout,
		func(i int) uuid.UUID { return targets[jobs[i].target].ds.ID },
		func(ctx context.Context, i int) (domain.TableResult, error) {
			j := jobs[i]
			return rd.batch.Table(ctx, targets[j.target].ds.ID, domain.TableQuery{Query: j.query, At: at})
		})

	answers := make([]map[string]domain.TableResult, len(targets))
	for i, r := range results {
		j := jobs[i]
		if r.err != nil {
			if out[j.target].err == nil {
				out[j.target].err = s.tableJobError(r.err, r.late, budget, what)
			}
			continue
		}
		if answers[j.target] == nil {
			answers[j.target] = map[string]domain.TableResult{}
		}
		answers[j.target][j.query] = r.value
	}
	for i, t := range targets {
		if out[i].err != nil || answers[i] == nil {
			continue
		}
		byColumn := map[string]domain.TableResult{}
		for col, q := range t.queries {
			byColumn[col] = answers[i][q]
		}
		r := &out[i]
		switch {
		case t.table == nil:
			res := byColumn[domain.TableValueKey]
			r.columns, r.keyColumns, r.rows, r.truncated = res.Columns, res.KeyColumns, res.Rows, res.Truncated
		case ranked && len(answers[i]) == 1:
			res := answers[i][t.queries[t.table.ValueColumns()[0].Key]]
			r.columns, r.rows, r.truncated = projectTable(*t.table, res)
			if slices.ContainsFunc(res.Rows, func(row map[string]any) bool { _, ok := row[domain.TableValueKey]; return ok }) {
				rankRows(r.rows, t.table.ValueColumns()[0].Key)
			}
		default:
			r.columns, r.rows, r.truncated = joinTable(*t.table, byColumn, ranked)
			if ranked {
				rankRows(r.rows, t.table.ValueColumns()[0].Key)
			}
		}
	}
	return out
}

func (s *Service) tableJobError(err error, late bool, budget time.Duration, what string) *domain.SignalReadError {
	if late && errors.Is(err, context.DeadlineExceeded) {
		return &domain.SignalReadError{Code: domain.SignalErrTimeout, Message: fmt.Sprintf("not read within the %s %s may take", budget, what)}
	}
	return readError(err, s.queryTimeout)
}

// projectTable keeps one named-column answer's rows in the order the source
// gave them, each cut to the table's columns: a key label as text, a value
// column by its own key (or domain.TableValueKey for a table with one).
func projectTable(t domain.RecipeTable, res domain.TableResult) ([]domain.TableColumn, []map[string]any, bool) {
	values := t.ValueColumns()
	rows := make([]map[string]any, 0, min(len(res.Rows), datasource.MaxTableRows))
	for _, src := range res.Rows {
		if len(rows) == datasource.MaxTableRows {
			return slices.Clone(t.Columns), rows, true
		}
		row := map[string]any{}
		for _, k := range t.KeyLabels {
			row[k] = cellString(src[k])
		}
		for _, c := range values {
			v, ok := src[c.Key]
			if !ok && len(values) == 1 {
				v = src[domain.TableValueKey]
			}
			row[c.Key] = v
		}
		rows = append(rows, row)
	}
	return slices.Clone(t.Columns), rows, res.Truncated
}

// rankRows orders rows by one column, largest first; a row without a number
// there goes last, and ties keep their key order.
func rankRows(rows []map[string]any, column string) { datasource.RankByValue(rows, column) }

// tablesView is one of the component table reads: which confirmed bindings
// it reads tables through, and which recipe tables, in the order answered.
type tablesView struct {
	name    string
	what    string
	signals func(signal string) bool
	tables  []string
}

var (
	endpointsView = tablesView{
		name: "endpoints", what: "an endpoints read",
		signals: func(s string) bool { return strings.HasPrefix(s, "http.") },
		tables:  []string{domain.RecipeTableEndpoints},
	}
	crashesView = tablesView{
		name: "crashes", what: "a crashes read",
		signals: func(s string) bool { return strings.HasPrefix(s, "mobile.") || s == domain.SignalErrorsIssues },
		tables:  []string{domain.RecipeTableCrashes, domain.RecipeTableSignatures},
	}
)

func webView(table string) tablesView {
	return tablesView{
		name: "web\x00" + table, what: "a web read",
		signals: func(s string) bool { return strings.HasPrefix(s, "web.") },
		tables:  []string{table},
	}
}

// Endpoints answers the backend endpoints view of a component environment:
// the endpoints table (one row per route or transaction) of each distinct
// recipe its confirmed http.* bindings read through.
func (s *Service) Endpoints(ctx context.Context, componentID uuid.UUID, env domain.DeployEnvironment) (domain.ComponentTables, error) {
	return s.componentTables(ctx, componentID, env, endpointsView)
}

// Web answers one table of the frontend web view (pages, referrers,
// countries or devices) through the confirmed web.* bindings.
func (s *Service) Web(ctx context.Context, componentID uuid.UUID, env domain.DeployEnvironment, table string) (domain.ComponentTables, error) {
	if !slices.Contains(domain.WebTables(), table) {
		return domain.ComponentTables{}, fmt.Errorf("table %q is not one of %s: %w", table, strings.Join(domain.WebTables(), ", "), ErrInvalidInput)
	}
	return s.componentTables(ctx, componentID, env, webView(table))
}

// Crashes answers the mobile crashes view: the crashes and signatures tables
// of the recipes its confirmed mobile.* and errors.issues bindings read
// through.
func (s *Service) Crashes(ctx context.Context, componentID uuid.UUID, env domain.DeployEnvironment) (domain.ComponentTables, error) {
	return s.componentTables(ctx, componentID, env, crashesView)
}

// componentTables reads, for each confirmed binding of the view's signals
// whose recipe declares one of its tables, that table with the params the
// table uses, so bindings agreeing on those share one. A binding with a raw
// query, or of a recipe declaring none of them, has no table. Each table
// stands alone: one that cannot be read carries its error. Read as Queues is,
// within the tables budget and once for every request asking at the same
// time, and kept 30 s per cache scope, view, component, environment, and
// version of what they were read through.
func (s *Service) componentTables(ctx context.Context, componentID uuid.UUID, env domain.DeployEnvironment, view tablesView) (domain.ComponentTables, error) {
	if !env.Valid() {
		return domain.ComponentTables{}, fmt.Errorf("unknown environment %q: %w", env, ErrInvalidInput)
	}
	if _, err := s.components.GetComponent(ctx, componentID); err != nil {
		return domain.ComponentTables{}, err
	}
	bindings, err := s.bindings.ListSignalBindings(ctx, componentID, env)
	if err != nil {
		return domain.ComponentTables{}, err
	}
	l, err := s.lookup(ctx)
	if err != nil {
		return domain.ComponentTables{}, err
	}
	targets := s.viewTargets(l, bindings, view)
	scope := s.cacheScope(ctx)
	key := scope + "\x00" + view.name + "\x00" + componentID.String() + "\x00" + string(env) + "\x00" + l.version() + "\x00" + targetsKey(targets)
	if out, ok := s.tablesCache.get(key, s.now()); ok {
		return out, nil
	}
	return shared(ctx, &s.tablesFlight, s.tablesCache, scope, key, s.tablesCache.generation(scope), s.now,
		func(ctx context.Context) domain.ComponentTables {
			return s.readComponentTables(ctx, l, componentID, env, view, targets)
		})
}

func (s *Service) readComponentTables(ctx context.Context, l lookup, componentID uuid.UUID, env domain.DeployEnvironment, view tablesView, targets []*tableTarget) domain.ComponentTables {
	at := s.now()
	reads := s.readTargets(ctx, l, targets, at, s.tablesBudget, view.what, true)
	out := domain.ComponentTables{ComponentID: componentID, Environment: env, ReadAt: at, Tables: make([]domain.ComponentTable, len(targets))}
	for i, t := range targets {
		ct := domain.ComponentTable{
			SignalBindingID: t.first.ID, Signal: t.first.Signal, DataSourceID: t.first.DataSourceID, RecipeID: t.first.RecipeID, Table: t.name,
			Params: maps.Clone(t.params), Columns: slices.Clone(t.table.Columns), KeyColumns: slices.Clone(t.table.KeyLabels),
			Rows: []map[string]any{},
		}
		if t.known {
			ct.DataSourceName = t.ds.Name
		}
		if r := reads[i]; r.err != nil {
			ct.Error = r.err
		} else if r.columns != nil {
			ct.Columns, ct.Rows, ct.Truncated = r.columns, r.rows, r.truncated
		}
		out.Tables[i] = ct
	}
	return out
}

func (s *Service) viewTargets(l lookup, bindings []domain.SignalBinding, view tablesView) []*tableTarget {
	order := map[string]int{}
	for i, sig := range s.signals {
		order[sig.ID] = i
	}
	var confirmed []domain.SignalBinding
	for _, b := range bindings {
		if b.Status == domain.SignalBindingConfirmed && b.Query == "" && view.signals(b.Signal) {
			confirmed = append(confirmed, b)
		}
	}
	slices.SortStableFunc(confirmed, func(a, b domain.SignalBinding) int { return order[a.Signal] - order[b.Signal] })

	var out []*tableTarget
	seen := map[string]bool{}
	for _, b := range confirmed {
		r, ok := l.recipes[b.RecipeID]
		if !ok {
			continue
		}
		for _, name := range view.tables {
			if _, has := r.Tables[name]; !has {
				continue
			}
			t := newTableTarget(l, b)
			if t.known && r.SourceKind != t.ds.Kind {
				continue
			}
			t.useRecipeTable(r, name, b)
			key := t.recipeKey()
			if !t.known {
				key = "gone\x00" + b.DataSourceID.String() + "\x00" + r.ID + "\x00" + name
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, t)
			if t.known && len(t.missing) == 0 {
				t.renderTable()
			}
		}
	}
	return out
}
