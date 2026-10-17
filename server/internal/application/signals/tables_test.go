package signals_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/application/signals"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

const (
	webRecipe   = "svc-web"
	crashRecipe = "svc-crashes"
	apmRecipe   = "svc-apm"
	pagesQuery  = `sum by (path) (svc_page_views{site="shop"})`
)

var pageColumns = []domain.TableColumn{
	{Key: "path", Label: "Path"}, {Key: "views", Label: "Views", Unit: "views"}, {Key: "visitors", Label: "Visitors"},
}

// tableRecipes read every table from one Prometheus source, so a test reads
// what it defines: a named-column pages table whose two value columns share
// one query, crashes and signatures tables, and an endpoints table whose
// param has a default.
func tableRecipes() []domain.Recipe {
	return []domain.Recipe{
		{
			ID: webRecipe, SourceKind: domain.DataSourceKindPrometheus, Params: []string{"site"},
			Signals: map[string]string{
				"web.page_views": `sum(rate(svc_page_views{site="$site"}[$__rate]))`,
				"web.sessions":   `sum(svc_sessions{site="$site"})`,
			},
			Tables: map[string]domain.RecipeTable{domain.RecipeTablePages: {
				Columns: pageColumns, KeyLabels: []string{"path"},
				Queries: map[string]string{
					"views":    `sum by (path) (svc_page_views{site="$site"})`,
					"visitors": `sum by (path) (svc_page_views{site="$site"})`,
				},
			}},
		},
		{
			ID: crashRecipe, SourceKind: domain.DataSourceKindPrometheus, Params: []string{"app"},
			Signals: map[string]string{
				"mobile.crash_rate":       `sum(svc_crash_rate{app="$app"})`,
				"mobile.crash_free_users": `sum(svc_crash_free{app="$app"})`,
			},
			Tables: map[string]domain.RecipeTable{
				domain.RecipeTableCrashes: {
					Columns:   []domain.TableColumn{{Key: "issue", Label: "Issue"}, {Key: "users", Label: "Users"}},
					KeyLabels: []string{"issue"},
					Queries:   map[string]string{"users": `sum by (issue) (svc_crash_users{app="$app"})`},
				},
				domain.RecipeTableSignatures: {
					Columns:   []domain.TableColumn{{Key: "issue", Label: "Signature"}, {Key: "weight", Label: "Weight"}},
					KeyLabels: []string{"issue"},
					Queries:   map[string]string{"weight": `sum by (issue) (svc_hang_weight{app="$app"})`},
				},
			},
		},
		{
			ID: apmRecipe, SourceKind: domain.DataSourceKindPrometheus, Params: []string{"service", "prefix"},
			Defaults: map[string]string{"prefix": "http"},
			Signals:  map[string]string{"http.requests": `sum(rate(svc_requests{service="$service", prefix="$prefix"}[$__rate]))`},
			Tables: map[string]domain.RecipeTable{domain.RecipeTableEndpoints: {
				Columns: []domain.TableColumn{
					{Key: "route", Label: "Endpoint"}, {Key: "rpm", Label: "Requests"}, {Key: "p95_ms", Label: "p95"},
				},
				KeyLabels: []string{"route"},
				Queries: map[string]string{
					"rpm":    `sum by (route) (svc_rpm{service="$service", prefix="$prefix"})`,
					"p95_ms": `sum by (route) (svc_p95{service="$service", prefix="$prefix"})`,
				},
			}},
		},
	}
}

type tablesFixture struct {
	now      time.Time
	table    *mocks.TableQuerier
	bindings *mocks.SignalBindingStore
	source   domain.DataSource
	comp     domain.Component
	svc      *signals.Service
}

func newTablesFixture(t *testing.T) *tablesFixture {
	f := &tablesFixture{now: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)}
	store := mocks.NewDataSourceStore(t)
	recipes := mocks.NewObservabilityRecipeStore(t)
	kind := mocks.NewDataSourceKindAdapter(t)
	f.table = mocks.NewTableQuerier(t)
	f.bindings = mocks.NewSignalBindingStore(t)
	comps := mocks.NewComponentStore(t)
	kind.On("Kind").Return(domain.DataSourceKindPrometheus).Maybe()
	kind.On("Schema").Return(domain.DataSourceSchema{Label: "Prometheus"}).Maybe()
	kind.On("Capabilities").Return([]domain.Capability{domain.CapabilityMetrics}).Maybe()
	recipes.On("ListUserRecipes", mock.Anything).Return(tableRecipes(), nil).Maybe()
	f.source = domain.DataSource{ID: uuid.New(), Kind: domain.DataSourceKindPrometheus, Name: "prom", UpdatedAt: f.now.Add(-time.Hour)}
	store.On("ListDataSources", mock.Anything).Return([]domain.DataSource{f.source}, nil).Maybe()
	store.On("GetDataSource", mock.Anything, f.source.ID).Return(f.source, nil).Maybe()
	store.On("DataSourceSecrets", mock.Anything, f.source.ID).Return(map[string]string{}, nil).Maybe()
	f.comp = domain.Component{ID: uuid.New(), RepositoryID: uuid.New(), Status: domain.ComponentStatusActive}
	comps.On("GetComponent", mock.Anything, f.comp.ID).Return(f.comp, nil).Maybe()

	policy := urlguard.HumanConfigured()
	sources, err := datasource.NewService(datasource.Deps{
		Store: store, Recipes: recipes,
		Kinds:     []port.DataSourceKindAdapter{promKind{kind, mocks.NewMetricsQuerier(t), mocks.NewDiscoverer(t), f.table}},
		URLPolicy: &policy,
	})
	require.NoError(t, err)
	f.svc, err = signals.NewService(signals.Deps{Bindings: f.bindings, Sources: sources, Components: comps, Environments: mocks.NewEnvironmentStore(t)},
		signals.WithClock(func() time.Time { return f.now }), signals.WithQueryTimeout(2*time.Second))
	require.NoError(t, err)
	return f
}

func (f *tablesFixture) bound(signal, recipe string, params map[string]string) domain.SignalBinding {
	return domain.SignalBinding{
		ID: uuid.New(), ComponentID: f.comp.ID, Environment: domain.EnvironmentProduction, Signal: signal,
		DataSourceID: f.source.ID, RecipeID: recipe, Params: params, Status: domain.SignalBindingConfirmed,
		Source: domain.SignalBindingDetected, CreatedAt: f.now, UpdatedAt: f.now,
	}
}

func (f *tablesFixture) stored(list ...domain.SignalBinding) {
	f.bindings.On("ListSignalBindings", mock.Anything, f.comp.ID, domain.EnvironmentProduction).Return(list, nil)
}

func (f *tablesFixture) answers(query string, res domain.TableResult, err error) *mock.Call {
	return f.table.On("Table", mock.Anything, mock.Anything, mock.Anything, mock.MatchedBy(func(q domain.TableQuery) bool {
		return q.Query == query && q.At.Equal(f.now)
	})).Return(res, err)
}

func TestWebReadsANamedColumnTableOnceAndKeepsTheSourcesOrder(t *testing.T) {
	f := newTablesFixture(t)
	views := f.bound("web.page_views", webRecipe, map[string]string{"site": "shop"})
	sessions := f.bound("web.sessions", webRecipe, map[string]string{"site": "shop"})
	f.stored(sessions, views)
	f.answers(pagesQuery, domain.TableResult{Rows: []map[string]any{
		{"path": "/checkout", "views": 90.0, "visitors": 40.0},
		{"path": "/", "views": 300.0, "visitors": 120.0},
	}, Truncated: true}, nil).Once()

	got, err := f.svc.Web(context.Background(), f.comp.ID, domain.EnvironmentProduction, domain.RecipeTablePages)

	require.NoError(t, err)
	assert.Equal(t, f.comp.ID, got.ComponentID)
	require.Len(t, got.Tables, 1, "two bindings agreeing on the table's params share it")
	tb := got.Tables[0]
	assert.Equal(t, views.ID, tb.SignalBindingID, "the first binding in catalog order names the table")
	assert.Equal(t, domain.RecipeTablePages, tb.Table)
	assert.Equal(t, webRecipe, tb.RecipeID)
	assert.Equal(t, "prom", tb.DataSourceName)
	assert.Equal(t, map[string]string{"site": "shop"}, tb.Params)
	assert.Nil(t, tb.Error)
	assert.Equal(t, pageColumns, tb.Columns)
	assert.Equal(t, []map[string]any{
		{"path": "/checkout", "views": 90.0, "visitors": 40.0},
		{"path": "/", "views": 300.0, "visitors": 120.0},
	}, tb.Rows, "one query fills every column, in the order the source ranked them")
	assert.True(t, tb.Truncated)

	none, err := f.svc.Web(context.Background(), f.comp.ID, domain.EnvironmentProduction, domain.RecipeTableReferrers)
	require.NoError(t, err)
	assert.Empty(t, none.Tables, "the recipe declares no referrers table")

	_, err = f.svc.Web(context.Background(), f.comp.ID, domain.EnvironmentProduction, domain.RecipeTableQueues)
	assert.ErrorIs(t, err, signals.ErrInvalidInput)
}

func TestCrashesAnswersBothMobileTablesAndEachStandsAlone(t *testing.T) {
	f := newTablesFixture(t)
	f.stored(
		f.bound("mobile.crash_rate", crashRecipe, map[string]string{"app": "ios"}),
		f.bound("mobile.crash_free_users", crashRecipe, map[string]string{"app": "ios"}),
		f.bound("web.page_views", webRecipe, map[string]string{"site": "shop"}),
	)
	f.answers(`sum by (issue) (svc_crash_users{app="ios"})`, domain.TableResult{Rows: []map[string]any{
		{"issue": "A-1", "value": 3.0}, {"issue": "A-2", "value": 9.0},
	}}, nil).Once()
	f.answers(`sum by (issue) (svc_hang_weight{app="ios"})`, domain.TableResult{}, errors.New("connection reset")).Once()

	got, err := f.svc.Crashes(context.Background(), f.comp.ID, domain.EnvironmentProduction)

	require.NoError(t, err)
	require.Len(t, got.Tables, 2)
	assert.Equal(t, domain.RecipeTableCrashes, got.Tables[0].Table)
	assert.Nil(t, got.Tables[0].Error)
	assert.Equal(t, []map[string]any{{"issue": "A-2", "users": 9.0}, {"issue": "A-1", "users": 3.0}}, got.Tables[0].Rows,
		"one value per row in no order of the source's own (a PromQL vector): ranked by it")
	assert.Equal(t, domain.RecipeTableSignatures, got.Tables[1].Table)
	require.NotNil(t, got.Tables[1].Error)
	assert.Equal(t, domain.SignalErrUpstream, got.Tables[1].Error.Code)
	assert.Empty(t, got.Tables[1].Rows)
}

func TestEndpointsFillsAParamFromTheRecipeDefaultAndRanksByTheFirstValue(t *testing.T) {
	f := newTablesFixture(t)
	f.stored(f.bound("http.requests", apmRecipe, map[string]string{"service": "api"}))
	f.answers(`sum by (route) (svc_rpm{service="api", prefix="http"})`, domain.TableResult{Rows: []map[string]any{
		{"route": "/a", "value": 5.0}, {"route": "/b", "value": 50.0}, {"route": "/c", "value": nil},
	}}, nil).Once()
	f.answers(`sum by (route) (svc_p95{service="api", prefix="http"})`, domain.TableResult{Rows: []map[string]any{
		{"route": "/a", "value": 12.0},
	}}, nil).Once()

	got, err := f.svc.Endpoints(context.Background(), f.comp.ID, domain.EnvironmentProduction)

	require.NoError(t, err)
	require.Len(t, got.Tables, 1)
	tb := got.Tables[0]
	assert.Nil(t, tb.Error)
	assert.Equal(t, map[string]string{"service": "api", "prefix": "http"}, tb.Params)
	assert.Equal(t, []map[string]any{
		{"route": "/b", "rpm": 50.0, "p95_ms": nil},
		{"route": "/a", "rpm": 5.0, "p95_ms": 12.0},
		{"route": "/c", "rpm": nil, "p95_ms": nil},
	}, tb.Rows)

	again, err := f.svc.Endpoints(context.Background(), f.comp.ID, domain.EnvironmentProduction)
	require.NoError(t, err)
	assert.Equal(t, got, again, "read once within the cache window")
}

func TestEndpointsSaysWhichParamATableStillNeeds(t *testing.T) {
	f := newTablesFixture(t)
	f.stored(f.bound("http.requests", apmRecipe, map[string]string{"service": " "}))

	got, err := f.svc.Endpoints(context.Background(), f.comp.ID, domain.EnvironmentProduction)

	require.NoError(t, err)
	require.Len(t, got.Tables, 1)
	require.NotNil(t, got.Tables[0].Error)
	assert.Equal(t, domain.SignalErrNeedsParams, got.Tables[0].Error.Code)
	assert.Equal(t, "no value for service", got.Tables[0].Error.Message, "a param with a default is never missing")
}

func TestPutTakesAParamLeftOutWhenTheRecipeHasADefaultForIt(t *testing.T) {
	f := newTablesFixture(t)
	f.bindings.On("PutConfirmedSignalBinding", mock.Anything, mock.MatchedBy(func(b domain.SignalBinding) bool {
		_, set := b.Params["prefix"]
		return b.RecipeID == apmRecipe && b.Params["service"] == "api" && !set
	})).Return(func(_ context.Context, b domain.SignalBinding) (domain.SignalBinding, error) {
		b.ID, b.Status = uuid.New(), domain.SignalBindingConfirmed
		return b, nil
	}).Once()

	got, err := f.svc.Put(context.Background(), f.comp.ID, domain.EnvironmentProduction, "http.requests", signals.BindingRequest{
		DataSourceID: f.source.ID.String(), RecipeID: apmRecipe, Params: map[string]string{"service": "api"},
	})

	require.NoError(t, err)
	assert.Equal(t, `sum(rate(svc_requests{service="api", prefix="http"}[120s]))`, got.RenderedQuery)
	assert.Equal(t, []string{}, got.NeedsParams)

	_, err = f.svc.Put(context.Background(), f.comp.ID, domain.EnvironmentProduction, "http.requests", signals.BindingRequest{
		DataSourceID: f.source.ID.String(), RecipeID: apmRecipe, Params: map[string]string{"prefix": "grpc"},
	})
	var fe datasource.FieldErrors
	require.ErrorAs(t, err, &fe)
	assert.Equal(t, datasource.FieldErrors{"params.service": datasource.FieldRequired}, fe, "a param with no default is still required")
}
