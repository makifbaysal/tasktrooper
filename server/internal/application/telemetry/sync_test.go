package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type fakeIncidents struct {
	stored []domain.Incident
	ingest []domain.IncidentInput
	failOn string
}

func (f *fakeIncidents) Ingest(_ context.Context, in domain.IncidentInput) (domain.Incident, error) {
	f.ingest = append(f.ingest, in)
	if f.failOn != "" && strings.HasSuffix(in.Fingerprint, f.failOn) {
		return domain.Incident{}, fmt.Errorf("boom")
	}
	if in.Resolved {
		for i := range f.stored {
			if f.stored[i].Fingerprint == in.Fingerprint {
				f.stored[i].Status = domain.IncidentStatusResolved
			}
		}
		return domain.Incident{}, nil
	}
	inc := domain.Incident{ID: uuid.New(), RepositoryID: in.RepositoryID, Env: in.Env, Source: in.Source,
		Fingerprint: in.Fingerprint, Title: in.Title, Severity: in.Severity, Status: domain.IncidentStatusOpen, Payload: in.Payload}
	f.stored = append(f.stored, inc)
	return inc, nil
}

func (f *fakeIncidents) List(_ context.Context, filter domain.IncidentFilter) ([]domain.Incident, error) {
	var out []domain.Incident
	for _, i := range f.stored {
		if filter.Source == "" || i.Source == filter.Source {
			out = append(out, i)
		}
	}
	return out, nil
}

type fakeRepos struct{ repos []domain.Repository }

func (f fakeRepos) List(context.Context) ([]domain.Repository, error) { return f.repos, nil }

type syncFixture struct {
	sync      *IncidentSync
	incidents *fakeIncidents
	client    *fakeClient
	open      string
	byID      map[string]string
	repoAPI   domain.Repository
	repoWeb   domain.Repository
}

func newSyncFixture(t *testing.T) *syncFixture {
	t.Helper()
	store := &fakeStore{baseURL: "http://t.example", apiKey: "k"}
	client := &fakeClient{}
	f := &syncFixture{
		incidents: &fakeIncidents{}, client: client, open: `[]`, byID: map[string]string{},
		repoAPI: domain.Repository{ID: uuid.New(), Name: "orders-api", RootPath: "/work/orders-api"},
		repoWeb: domain.Repository{ID: uuid.New(), Name: "storefront", RootPath: "/work/storefront"},
	}
	client.respond = func(c call) (port.TelemetryResponse, error) {
		body := f.open
		if strings.HasPrefix(c.path, "incidents/") {
			b, ok := f.byID[strings.TrimPrefix(c.path, "incidents/")]
			if !ok {
				return port.TelemetryResponse{Status: 404, Body: []byte(`{"error":"nope"}`)}, nil
			}
			body = b
		}
		return port.TelemetryResponse{Status: 200, ContentType: "application/json", Body: []byte(body)}, nil
	}
	svc := NewService(Deps{Store: store, Client: client})
	f.sync = NewIncidentSync(svc, f.incidents, fakeRepos{repos: []domain.Repository{f.repoWeb, f.repoAPI}})
	return f
}

const firingOrders = `{"id":"42","monitorId":"7","title":"orders-api error rate above 5%","severity":"critical","state":"firing",
"startedAt":1760000000000,"value":"0.12","service":"orders-api","endpoint":"POST /orders",
"rootCause":{"summary":"deploy v1.4.2 started the 500s","confidence":0.81}}`

func TestFiringIncidentIsIngestedIntoTheMatchingRepository(t *testing.T) {
	f := newSyncFixture(t)
	f.open = "[" + firingOrders + "]"

	f.sync.RunOnce(context.Background())

	require.Len(t, f.incidents.ingest, 1)
	in := f.incidents.ingest[0]
	assert.Equal(t, f.repoAPI.ID, in.RepositoryID)
	assert.Equal(t, domain.IncidentSourceTelemetry, in.Source)
	assert.Equal(t, domain.IncidentSeverityCritical, in.Severity)
	assert.Equal(t, domain.DeployEnvProd, in.Env)
	assert.Equal(t, "orders-api error rate above 5%", in.Title)
	assert.True(t, strings.HasPrefix(in.Fingerprint, "telemetry:"))
	assert.True(t, strings.HasSuffix(in.Fingerprint, ":42"))
	assert.Contains(t, in.Detail, "Service: orders-api · POST /orders")
	assert.Contains(t, in.Detail, "Root cause (telemetry RCA): deploy v1.4.2 started the 500s (81% confidence)")
	assert.NotContains(t, in.Detail, "No repository is named")
	assert.Equal(t, "42", in.Payload["telemetry_incident_id"])
	assert.Equal(t, "orders-api", in.Payload["service"])

	assert.Equal(t, "open", f.client.calls[0].query.Get("state"))
}

func TestAnIncidentIsIngestedOnceNoMatterHowOftenItIsPolled(t *testing.T) {
	f := newSyncFixture(t)
	f.open = "[" + firingOrders + "]"
	for i := 0; i < 3; i++ {
		f.sync.RunOnce(context.Background())
	}
	assert.Len(t, f.incidents.ingest, 1)
}

func TestAHumanResolvedIncidentIsNotReopenedWhileTelemetryStillFiresIt(t *testing.T) {
	f := newSyncFixture(t)
	f.open = "[" + firingOrders + "]"
	f.sync.RunOnce(context.Background())
	f.incidents.stored[0].Status = domain.IncidentStatusResolved
	f.incidents.ingest = nil

	f.sync.RunOnce(context.Background())
	assert.Empty(t, f.incidents.ingest)
}

func TestPendingQuietAndAcknowledgedHandling(t *testing.T) {
	f := newSyncFixture(t)
	f.open = `[
	 {"id":"1","title":"pending one","severity":"warning","state":"pending","service":"orders-api"},
	 {"id":"2","title":"quiet one","severity":"warning","state":"firing","quiet":true,"service":"orders-api"},
	 {"id":"3","title":"acked one","severity":"warning","state":"acknowledged","service":"orders-api"}]`
	f.sync.RunOnce(context.Background())
	require.Len(t, f.incidents.ingest, 1)
	assert.Equal(t, "acked one", f.incidents.ingest[0].Title)
	assert.Equal(t, domain.IncidentSeverityHigh, f.incidents.ingest[0].Severity, "a warning still opens a task")
}

func TestAnUnknownServiceGoesToTheFirstRepositoryAndSaysSo(t *testing.T) {
	f := newSyncFixture(t)
	f.open = `[{"id":"5","title":"mystery","severity":"critical","state":"firing","service":"billing-worker"}]`
	f.sync.RunOnce(context.Background())
	require.Len(t, f.incidents.ingest, 1)
	assert.Equal(t, f.repoWeb.ID, f.incidents.ingest[0].RepositoryID)
	assert.Contains(t, f.incidents.ingest[0].Detail, `No repository is named after the service "billing-worker"`)
}

func TestNothingHappensWithoutRepositories(t *testing.T) {
	f := newSyncFixture(t)
	f.sync.repos = fakeRepos{}
	f.open = "[" + firingOrders + "]"
	f.sync.RunOnce(context.Background())
	assert.Empty(t, f.incidents.ingest)
}

func TestRecoveryResolvesTheTaskTrooperIncident(t *testing.T) {
	f := newSyncFixture(t)
	f.open = "[" + firingOrders + "]"
	f.sync.RunOnce(context.Background())
	require.Len(t, f.incidents.stored, 1)

	f.open = `[]`
	f.byID["42"] = `{"id":"42","title":"orders-api error rate above 5%","state":"resolved","service":"orders-api"}`
	f.incidents.ingest = nil
	f.sync.RunOnce(context.Background())

	require.Len(t, f.incidents.ingest, 1)
	assert.True(t, f.incidents.ingest[0].Resolved)
	assert.Equal(t, f.incidents.stored[0].Fingerprint, f.incidents.ingest[0].Fingerprint)
	assert.Equal(t, domain.IncidentStatusResolved, f.incidents.stored[0].Status)

	f.incidents.ingest = nil
	f.sync.RunOnce(context.Background())
	assert.Empty(t, f.incidents.ingest, "a closed incident is not asked about again")
}

func TestAnIncidentMissingFromAFullListButStillFiringIsNotClosed(t *testing.T) {
	f := newSyncFixture(t)
	f.open = "[" + firingOrders + "]"
	f.sync.RunOnce(context.Background())

	f.open = `[]`
	f.byID["42"] = firingOrders
	f.incidents.ingest = nil
	f.sync.RunOnce(context.Background())
	assert.Empty(t, f.incidents.ingest)
}

func TestAnIncidentTelemetryForgotAboutIsClosed(t *testing.T) {
	f := newSyncFixture(t)
	f.open = "[" + firingOrders + "]"
	f.sync.RunOnce(context.Background())
	f.open = `[]`
	f.incidents.ingest = nil
	f.sync.RunOnce(context.Background())
	require.Len(t, f.incidents.ingest, 1)
	assert.True(t, f.incidents.ingest[0].Resolved)
}

func TestSyncSkipsQuietlyWhenNotConfiguredOrDown(t *testing.T) {
	f := newSyncFixture(t)
	f.sync.svc.store.(*fakeStore).apiKey = ""
	f.sync.RunOnce(context.Background())
	assert.Empty(t, f.client.calls)

	f.sync.svc.store.(*fakeStore).apiKey = "k"
	f.client.respond = func(call) (port.TelemetryResponse, error) {
		return port.TelemetryResponse{}, fmt.Errorf("%w: down", port.ErrTelemetryUnreachable)
	}
	f.sync.RunOnce(context.Background())
	assert.Empty(t, f.incidents.ingest)
}

func TestAFailingIngestDoesNotStopTheOthers(t *testing.T) {
	f := newSyncFixture(t)
	f.incidents.failOn = ":1"
	f.open = `[{"id":"1","title":"a","state":"firing","service":"orders-api"},{"id":"2","title":"b","state":"firing","service":"orders-api"}]`
	f.sync.RunOnce(context.Background())
	assert.Len(t, f.incidents.ingest, 2)
}

func TestNumericIdsAreAccepted(t *testing.T) {
	f := newSyncFixture(t)
	f.open = `[{"id":9,"monitorId":3,"title":"n","state":"firing","service":"orders-api"}]`
	f.sync.RunOnce(context.Background())
	require.Len(t, f.incidents.ingest, 1)
	assert.Equal(t, "9", f.incidents.ingest[0].Payload["telemetry_incident_id"])
}

func TestFingerprintsDifferPerTelemetryServer(t *testing.T) {
	assert.NotEqual(t, fingerprint(baseScope("http://a.example"), "1"), fingerprint(baseScope("http://b.example"), "1"))
	assert.Equal(t, baseScope("http://A.example"), baseScope("http://a.example"))
}

func TestPickRepository(t *testing.T) {
	api := domain.Repository{ID: uuid.New(), Name: "Orders API", RootPath: "/x/y"}
	mono := domain.Repository{ID: uuid.New(), Name: "platform", RootPath: "/x/platform",
		SubProjects: []domain.RepoSubProject{{Path: "services/checkout-svc"}, {Path: "."}}}
	byFolder := domain.Repository{ID: uuid.New(), Name: "x", RootPath: "/x/ledger"}
	repos := []domain.Repository{mono, api, byFolder}

	got, matched, ok := PickRepository("orders-api", repos)
	assert.True(t, ok && matched)
	assert.Equal(t, api.ID, got.ID)

	got, matched, _ = PickRepository("checkout-svc", repos)
	assert.True(t, matched)
	assert.Equal(t, mono.ID, got.ID)

	got, matched, _ = PickRepository("Ledger", repos)
	assert.True(t, matched)
	assert.Equal(t, byFolder.ID, got.ID)

	got, matched, ok = PickRepository("", repos)
	assert.True(t, ok)
	assert.False(t, matched)
	assert.Equal(t, mono.ID, got.ID)

	_, _, ok = PickRepository("x", nil)
	assert.False(t, ok)
}

func TestIncidentInputPayloadIsJSONSerializable(t *testing.T) {
	var r remoteIncident
	require.NoError(t, json.Unmarshal([]byte(firingOrders), &r))
	in := incidentInput("http://t.example", "abc", r, uuid.New(), true)
	_, err := json.Marshal(in.Payload)
	require.NoError(t, err)
	assert.Equal(t, "7", in.Payload["monitor_id"])
	_, hasKPI := in.Payload["kpi_id"]
	assert.False(t, hasKPI)
}

var _ = url.Values{}
