package datasource_test

import (
	"context"
	"errors"
	"math"
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

type tableKind struct {
	*mocks.DataSourceKindAdapter
	*mocks.TableQuerier
}

type tableFixture struct {
	svc     *datasource.Service
	store   *mocks.DataSourceStore
	table   *mocks.TableQuerier
	plainDS domain.DataSource
	ds      domain.DataSource
	now     time.Time
}

func newTableFixture(t *testing.T) tableFixture {
	f := tableFixture{store: mocks.NewDataSourceStore(t), table: mocks.NewTableQuerier(t), now: time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)}
	kind := mocks.NewDataSourceKindAdapter(t)
	kind.On("Kind").Return(domain.DataSourceKindPrometheus).Maybe()
	kind.On("Schema").Return(testSchema()).Maybe()
	plain := mocks.NewDataSourceKindAdapter(t)
	plain.On("Kind").Return(plainKind).Maybe()
	plain.On("Schema").Return(domain.DataSourceSchema{}).Maybe()
	policy := urlguard.HumanConfigured()
	svc, err := datasource.NewService(datasource.Deps{
		Store: f.store, Kinds: []port.DataSourceKindAdapter{tableKind{kind, f.table}, plain}, URLPolicy: &policy,
	})
	require.NoError(t, err)
	svc.SetClock(func() time.Time { return f.now })
	f.svc = svc
	f.ds = domain.DataSource{ID: uuid.New(), Kind: domain.DataSourceKindPrometheus}
	f.plainDS = domain.DataSource{ID: uuid.New(), Kind: plainKind}
	return f
}

func TestBatchTableCapsTheRowsAndDropsNumbersJSONCannotCarry(t *testing.T) {
	f := newTableFixture(t)
	f.store.On("DataSourceSecrets", mock.Anything, f.ds.ID).Return(map[string]string{"token": "s3cret"}, nil).Once()
	rows := make([]map[string]any, datasource.MaxTableRows+1)
	for i := range rows {
		rows[i] = map[string]any{"queue": "q", "value": float64(i)}
	}
	rows[0]["value"] = math.NaN()
	rows[1]["value"] = math.Inf(1)
	f.table.On("Table", mock.Anything, f.ds, map[string]string{"token": "s3cret"}, domain.TableQuery{Query: "x", At: f.now}).
		Return(domain.TableResult{Rows: rows}, nil).Once()

	res, err := f.svc.Batch(f.ds).Table(context.Background(), f.ds.ID, domain.TableQuery{Query: " x "})

	require.NoError(t, err)
	assert.True(t, res.Truncated)
	assert.Len(t, res.Rows, datasource.MaxTableRows)
	assert.Nil(t, res.Rows[0]["value"], "+Inf ranks first, and JSON cannot carry it")
	assert.Equal(t, float64(datasource.MaxTableRows), res.Rows[1]["value"], "the cut keeps the largest values")
	assert.Equal(t, 2.0, res.Rows[len(res.Rows)-1]["value"], "a NaN, no number at all, is what goes")
	assert.Equal(t, []domain.TableColumn{}, res.Columns)
}

func TestBatchTableKeepsTheSourcesOrderWhenNothingIsCut(t *testing.T) {
	f := newTableFixture(t)
	f.store.On("DataSourceSecrets", mock.Anything, f.ds.ID).Return(map[string]string{}, nil).Once()
	rows := []map[string]any{{"queue": "a", "value": 1.0}, {"queue": "b", "value": 9.0}}
	f.table.On("Table", mock.Anything, f.ds, mock.Anything, mock.Anything).Return(domain.TableResult{Rows: rows}, nil).Once()

	res, err := f.svc.Batch(f.ds).Table(context.Background(), f.ds.ID, domain.TableQuery{Query: "x"})

	require.NoError(t, err)
	assert.Equal(t, []map[string]any{{"queue": "a", "value": 1.0}, {"queue": "b", "value": 9.0}}, res.Rows)
}

func TestBatchTableRedactsTheSourcesErrorAndRefusesAKindWithoutTables(t *testing.T) {
	f := newTableFixture(t)
	f.store.On("DataSourceSecrets", mock.Anything, f.ds.ID).Return(map[string]string{"token": "s3cret"}, nil).Once()
	f.store.On("DataSourceSecrets", mock.Anything, f.plainDS.ID).Return(map[string]string{}, nil).Once()
	f.table.On("Table", mock.Anything, f.ds, mock.Anything, mock.Anything).Return(domain.TableResult{}, errors.New("refused s3cret")).Once()
	b := f.svc.Batch(f.ds, f.plainDS)

	_, err := b.Table(context.Background(), f.ds.ID, domain.TableQuery{Query: "x"})
	require.ErrorIs(t, err, datasource.ErrUpstream)
	assert.NotContains(t, err.Error(), "s3cret")

	_, err = b.Table(context.Background(), f.plainDS.ID, domain.TableQuery{Query: "x"})
	require.ErrorIs(t, err, datasource.ErrUnsupported)

	_, err = b.Table(context.Background(), f.ds.ID, domain.TableQuery{Query: "  "})
	assert.Equal(t, datasource.FieldErrors{"query": datasource.FieldRequired}, err)
}
