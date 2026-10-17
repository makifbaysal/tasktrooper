package datasource_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

type logTraceKind struct {
	*mocks.DataSourceKindAdapter
	*mocks.LogsQuerier
	*mocks.TracesQuerier
	*mocks.LabelValuesLister
}

type logsFixture struct {
	svc     *datasource.Service
	store   *mocks.DataSourceStore
	logs    *mocks.LogsQuerier
	traces  *mocks.TracesQuerier
	values  *mocks.LabelValuesLister
	ds      domain.DataSource
	plainDS domain.DataSource
	secrets map[string]string
}

func newLogsFixture(t *testing.T) logsFixture {
	f := logsFixture{
		store: mocks.NewDataSourceStore(t), logs: mocks.NewLogsQuerier(t), traces: mocks.NewTracesQuerier(t), values: mocks.NewLabelValuesLister(t),
		secrets: map[string]string{"token": "s3cret-token"},
	}
	kind := mocks.NewDataSourceKindAdapter(t)
	kind.On("Kind").Return(domain.DataSourceKindLoki).Maybe()
	kind.On("Schema").Return(testSchema()).Maybe()
	plain := mocks.NewDataSourceKindAdapter(t)
	plain.On("Kind").Return(plainKind).Maybe()
	plain.On("Schema").Return(domain.DataSourceSchema{}).Maybe()
	policy := urlguard.HumanConfigured()
	svc, err := datasource.NewService(datasource.Deps{
		Store: f.store, Kinds: []port.DataSourceKindAdapter{logTraceKind{kind, f.logs, f.traces, f.values}, plain}, URLPolicy: &policy,
	})
	require.NoError(t, err)
	f.svc = svc
	f.ds = domain.DataSource{ID: uuid.New(), Kind: domain.DataSourceKindLoki}
	f.plainDS = domain.DataSource{ID: uuid.New(), Kind: plainKind}
	f.store.On("DataSourceSecrets", mock.Anything, f.ds.ID).Return(f.secrets, nil).Maybe()
	f.store.On("DataSourceSecrets", mock.Anything, f.plainDS.ID).Return(map[string]string{}, nil).Maybe()
	return f
}

var (
	logSince = time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	logUntil = logSince.Add(time.Hour)
)

func TestBatchLogsDefaultsTheLimitAndCutsAnOverfullPage(t *testing.T) {
	f := newLogsFixture(t)
	entries := make([]domain.RuntimeLogEntry, datasource.DefaultLogLimit+5)
	f.logs.On("Logs", mock.Anything, f.ds, f.secrets, domain.LogQuery{Query: `{app="x"}`, Since: logSince, Until: logUntil, Limit: datasource.DefaultLogLimit}).
		Return(domain.RuntimeLogPage{Entries: entries}, nil).Once()

	page, err := f.svc.Batch(f.ds).Logs(context.Background(), f.ds.ID, domain.LogQuery{Query: ` {app="x"} `, Since: logSince, Until: logUntil})

	require.NoError(t, err)
	assert.Len(t, page.Entries, datasource.DefaultLogLimit)
	assert.True(t, page.Truncated)
}

func TestBatchLogsRefusesWhatCannotBeReadAndRedactsTheSourcesError(t *testing.T) {
	f := newLogsFixture(t)
	b := f.svc.Batch(f.ds, f.plainDS)
	tests := []struct {
		name string
		q    domain.LogQuery
		want datasource.FieldErrors
	}{
		{name: "no query", q: domain.LogQuery{Since: logSince, Until: logUntil}, want: datasource.FieldErrors{"query": datasource.FieldRequired}},
		{name: "backwards window", q: domain.LogQuery{Query: "q", Since: logUntil, Until: logSince}, want: datasource.FieldErrors{"since": datasource.FieldInvalid}},
		{name: "negative limit", q: domain.LogQuery{Query: "q", Since: logSince, Until: logUntil, Limit: -1}, want: datasource.FieldErrors{"limit": datasource.FieldInvalid}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := b.Logs(context.Background(), f.ds.ID, tt.q)
			assert.Equal(t, tt.want, err)
		})
	}

	_, err := b.Logs(context.Background(), f.plainDS.ID, domain.LogQuery{Query: "q", Since: logSince, Until: logUntil})
	assert.ErrorIs(t, err, datasource.ErrUnsupported)

	f.logs.On("Logs", mock.Anything, f.ds, mock.Anything, mock.Anything).Return(domain.RuntimeLogPage{}, errors.New("Loki: rejected token s3cret-token")).Once()
	_, err = b.Logs(context.Background(), f.ds.ID, domain.LogQuery{Query: "q", Since: logSince, Until: logUntil})
	require.ErrorIs(t, err, datasource.ErrUpstream)
	assert.NotContains(t, err.Error(), "s3cret-token")

	f.logs.On("Logs", mock.Anything, f.ds, mock.Anything, mock.Anything).Return(domain.RuntimeLogPage{}, fmt.Errorf("%w: parse error", port.ErrDataSourceQuery)).Once()
	_, err = b.Logs(context.Background(), f.ds.ID, domain.LogQuery{Query: "q", Since: logSince, Until: logUntil})
	assert.ErrorIs(t, err, port.ErrDataSourceQuery)
}

func TestBatchTraceKeepsNotFoundAndNeverSendsAMalformedID(t *testing.T) {
	f := newLogsFixture(t)
	b := f.svc.Batch(f.ds)

	_, err := b.Trace(context.Background(), f.ds.ID, "../x")
	assert.ErrorIs(t, err, datasource.ErrInvalidInput)

	f.traces.On("Trace", mock.Anything, f.ds, f.secrets, "abc").Return(domain.Trace{}, fmt.Errorf("gone: %w", port.ErrNotFound)).Once()
	_, err = b.Trace(context.Background(), f.ds.ID, "ABC")
	assert.ErrorIs(t, err, port.ErrNotFound)
	assert.NotErrorIs(t, err, datasource.ErrUpstream)

	f.traces.On("Trace", mock.Anything, f.ds, f.secrets, "def").Return(domain.Trace{Spans: []domain.Span{{ID: "1"}}}, nil).Once()
	tr, err := b.Trace(context.Background(), f.ds.ID, "def")
	require.NoError(t, err)
	assert.Equal(t, "def", tr.ID)
	assert.Equal(t, map[string]string{}, tr.Spans[0].Attributes)
}

func TestBatchSearchTracesBoundsTheWindowAndTheLimit(t *testing.T) {
	f := newLogsFixture(t)
	b := f.svc.Batch(f.ds)

	_, err := b.SearchTraces(context.Background(), f.ds.ID, domain.TraceQuery{Query: "{ }", Since: logSince.Add(-8 * 24 * time.Hour), Until: logUntil})
	assert.Equal(t, datasource.FieldErrors{"since": datasource.FieldTooLong}, err)

	f.traces.On("SearchTraces", mock.Anything, f.ds, f.secrets, domain.TraceQuery{Query: "{ }", Since: logSince, Until: logUntil, Limit: datasource.MaxTraceLimit}).
		Return(make([]domain.TraceSummary, datasource.MaxTraceLimit+1), nil).Once()
	got, err := b.SearchTraces(context.Background(), f.ds.ID, domain.TraceQuery{Query: "{ }", Since: logSince, Until: logUntil, Limit: 1000})
	require.NoError(t, err)
	assert.Len(t, got, datasource.MaxTraceLimit)
}

func TestTheServiceSaysWhichKindsDiscoverAndListLabelValues(t *testing.T) {
	f := newLogsFixture(t)
	assert.True(t, f.svc.CanListLabelValues(domain.DataSourceKindLoki))
	assert.False(t, f.svc.CanListLabelValues(plainKind))
	assert.False(t, f.svc.CanDiscover(domain.DataSourceKindLoki), "the fixture's kind has no Discoverer")

	f.values.On("LabelValues", mock.Anything, f.ds, f.secrets, "service_name").Return([]string{"api"}, nil).Once()
	values, err := f.svc.Batch(f.ds).LabelValues(context.Background(), f.ds.ID, "service_name")
	require.NoError(t, err)
	assert.Equal(t, []string{"api"}, values)
}
