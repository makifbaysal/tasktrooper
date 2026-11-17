package datasource_test

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type telemetryKind struct {
	reader port.TelemetryReader
}

func (k *telemetryKind) Kind() domain.DataSourceKind {
	return domain.DataSourceKindTaskTrooperTelemetry
}
func (k *telemetryKind) Schema() domain.DataSourceSchema {
	return domain.DataSourceSchema{Label: "TaskTrooper Telemetry", Fields: []domain.DataSourceField{}}
}
func (k *telemetryKind) Capabilities() []domain.Capability   { return nil }
func (k *telemetryKind) SetTelemetry(r port.TelemetryReader) { k.reader = r }
func (k *telemetryKind) Verify(ctx context.Context, _ domain.DataSource, _ map[string]string) (domain.DataSourceInfo, error) {
	if _, err := k.reader.Get(ctx, "services", nil); err != nil {
		return domain.DataSourceInfo{}, err
	}
	return domain.DataSourceInfo{Detail: "ok"}, nil
}

type fakeConnection struct{ configured bool }

func (c *fakeConnection) Configured(context.Context) (bool, error) { return c.configured, nil }
func (c *fakeConnection) Get(context.Context, string, url.Values) (port.TelemetryResponse, error) {
	if !c.configured {
		return port.TelemetryResponse{}, errors.New("telemetry is not configured")
	}
	return port.TelemetryResponse{Status: 200, Body: []byte(`[]`)}, nil
}

type memMarks struct{ deleted bool }

func (m *memMarks) TelemetrySourceDeleted(context.Context) (bool, error) { return m.deleted, nil }
func (m *memMarks) SetTelemetrySourceDeleted(_ context.Context, d bool) error {
	m.deleted = d
	return nil
}

func newTelemetryFixture(t *testing.T) (*datasource.Service, *memSources, *fakeConnection, *memMarks, *telemetryKind) {
	t.Helper()
	mem, conn, marks, kind := &memSources{}, &fakeConnection{}, &memMarks{}, &telemetryKind{}
	policy := urlguard.HumanConfigured()
	svc, err := datasource.NewService(datasource.Deps{Store: mem, Kinds: []port.DataSourceKindAdapter{kind}, URLPolicy: &policy})
	require.NoError(t, err)
	svc.SetTelemetry(conn, marks)
	return svc, mem, conn, marks, kind
}

func TestTheTelemetrySourceFollowsTheConnectionInSettings(t *testing.T) {
	svc, _, conn, _, kind := newTelemetryFixture(t)
	ctx := context.Background()
	require.NotNil(t, kind.reader, "the kind reads the connection it was handed")

	svc.SyncTelemetrySource(ctx)
	list, err := svc.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, list, "nothing is made before the connection is saved")

	conn.configured = true
	svc.SyncTelemetrySource(ctx)
	svc.SyncTelemetrySource(ctx)
	list, err = svc.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1, "made once")
	assert.Equal(t, "TaskTrooper Telemetry", list[0].Name)
	assert.Empty(t, list[0].Config)
	assert.Equal(t, domain.DataSourceOK, list[0].Status)

	conn.configured = false
	svc.SyncTelemetrySource(ctx)
	list, err = svc.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1, "clearing the connection keeps the source and its bindings")
	assert.Equal(t, domain.DataSourceError, list[0].Status, "and says it cannot read")
	assert.Contains(t, list[0].StatusDetail, "not configured")
}

func TestADeletedTelemetrySourceIsNotMadeAgainUntilAddedByHand(t *testing.T) {
	svc, _, conn, marks, _ := newTelemetryFixture(t)
	ctx := context.Background()
	conn.configured = true
	svc.SyncTelemetrySource(ctx)
	list, _ := svc.List(ctx)
	require.Len(t, list, 1)

	require.NoError(t, svc.Delete(ctx, list[0].ID))
	assert.True(t, marks.deleted)
	svc.SyncTelemetrySource(ctx)
	list, _ = svc.List(ctx)
	assert.Empty(t, list)

	made, err := svc.Create(ctx, domain.SaveDataSourceRequest{Kind: domain.DataSourceKindTaskTrooperTelemetry})
	require.NoError(t, err)
	assert.False(t, marks.deleted)
	assert.Equal(t, "TaskTrooper Telemetry", made.Name)

	_, err = svc.Create(ctx, domain.SaveDataSourceRequest{Kind: domain.DataSourceKindTaskTrooperTelemetry})
	var fe datasource.FieldErrors
	require.ErrorAs(t, err, &fe)
	assert.Equal(t, datasource.FieldDuplicate, fe["kind"], "one connection, one source")
}
