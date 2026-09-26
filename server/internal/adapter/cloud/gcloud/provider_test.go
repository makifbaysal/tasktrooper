package gcloud_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cloud/gcloud"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// newTestProvider builds a Provider wired to the fakes; runURL may be "" when
// a test never issues a Cloud Run call.
func newTestProvider(t *testing.T, tokenURL, runURL string) *gcloud.Provider {
	t.Helper()
	p := gcloud.NewProvider()
	p.SetTokenURL(tokenURL)
	if runURL != "" {
		p.SetRunBaseURL(runURL)
	}
	return p
}

func testCredential(t *testing.T, accountID uuid.UUID, tokenURL string) domain.CloudCredential {
	t.Helper()
	return domain.CloudCredential{
		AccountID: accountID,
		Provider:  domain.CloudGCP,
		Fields:    map[string]string{"service_account_json": serviceAccountJSON(t, tokenURL)},
	}
}

func TestProviderKindIsGCP(t *testing.T) {
	if got := gcloud.NewProvider().Kind(); got != domain.CloudGCP {
		t.Fatalf("Kind() = %q, want %q", got, domain.CloudGCP)
	}
}

func TestVerifyReturnsProjectAndClientEmail(t *testing.T) {
	tok := newTokenServer(t)
	p := newTestProvider(t, tok.URL, "http://unused")
	cred := testCredential(t, uuid.New(), tok.URL)

	meta, err := p.Verify(context.Background(), cred)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if meta["project_id"] != "demo-project" {
		t.Fatalf("project_id = %q", meta["project_id"])
	}
	if meta["client_email"] != "tasktrooper@demo-project.iam.gserviceaccount.com" {
		t.Fatalf("client_email = %q", meta["client_email"])
	}
}

func TestVerifyWrapsARejectedAssertionAsCloudAuth(t *testing.T) {
	tok := newTokenServer(t)
	tok.status = http.StatusBadRequest
	p := newTestProvider(t, tok.URL, "http://unused")
	cred := testCredential(t, uuid.New(), tok.URL)

	_, err := p.Verify(context.Background(), cred)
	if !errors.Is(err, port.ErrCloudAuth) {
		t.Fatalf("err = %v, want it to wrap port.ErrCloudAuth", err)
	}
}

func TestVerifyRejectsAMissingServiceAccountJSON(t *testing.T) {
	p := gcloud.NewProvider()
	_, err := p.Verify(context.Background(), domain.CloudCredential{AccountID: uuid.New(), Provider: domain.CloudGCP})
	if !errors.Is(err, port.ErrCloudAuth) {
		t.Fatalf("err = %v, want it to wrap port.ErrCloudAuth", err)
	}
}

// TestClientsAreCachedPerAccountAndSecret is the point of the whole cache:
// two calls against the same credential must mint the JWT assertion once,
// not once per call.
func TestClientsAreCachedPerAccountAndSecret(t *testing.T) {
	tok := newTokenServer(t)
	p := newTestProvider(t, tok.URL, "http://unused")
	cred := testCredential(t, uuid.New(), tok.URL)

	if _, err := p.Verify(context.Background(), cred); err != nil {
		t.Fatalf("first Verify: %v", err)
	}
	if _, err := p.Verify(context.Background(), cred); err != nil {
		t.Fatalf("second Verify: %v", err)
	}
	if tok.count() != 1 {
		t.Fatalf("token exchanges = %d, want 1 — the client should be cached and its token reused", tok.count())
	}
}

func TestClientsAreNotSharedAcrossDifferentSecrets(t *testing.T) {
	tok := newTokenServer(t)
	p := newTestProvider(t, tok.URL, "http://unused")
	accountID := uuid.New()

	if _, err := p.Verify(context.Background(), testCredential(t, accountID, tok.URL)); err != nil {
		t.Fatalf("Verify A: %v", err)
	}
	if _, err := p.Verify(context.Background(), testCredential(t, accountID, tok.URL)); err != nil {
		t.Fatalf("Verify B: %v", err)
	}
	if tok.count() != 2 {
		t.Fatalf("token exchanges = %d, want 2 — a different key file must mint its own token", tok.count())
	}
}

func TestListResourcesListsServicesAndJobs(t *testing.T) {
	tok := newTokenServer(t)
	run := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/locations/-/services"):
			_, _ = w.Write([]byte(`{"services":[
				{"name":"projects/demo-project/locations/europe-west1/services/api","uri":"https://api.run.app","labels":{"team":"backend"},"template":{"containers":[{"image":"gcr.io/demo/api:v1"}]}}
			]}`))
		case strings.HasSuffix(r.URL.Path, "/locations"):
			_, _ = w.Write([]byte(`{"locations":[{"locationId":"europe-west1"},{"locationId":"us-central1"}]}`))
		case strings.Contains(r.URL.Path, "/locations/europe-west1/jobs"):
			_, _ = w.Write([]byte(`{"jobs":[{"name":"projects/demo-project/locations/europe-west1/jobs/nightly","labels":{"team":"data"},"template":{"template":{"containers":[{"image":"gcr.io/demo/nightly:v2"}]}}}]}`))
		case strings.Contains(r.URL.Path, "/locations/us-central1/jobs"):
			_, _ = w.Write([]byte(`{"jobs":[]}`))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer run.Close()

	p := newTestProvider(t, tok.URL, run.URL)
	cred := testCredential(t, uuid.New(), tok.URL)

	resources, err := p.ListResources(context.Background(), cred)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("got %d resources, want 2 (one service, one job)", len(resources))
	}

	// Sorted by Ref.ID: ".../jobs/nightly" precedes ".../services/api".
	job, svc := resources[0], resources[1]

	if job.Ref.Kind != domain.CloudResourceCloudRunJob {
		t.Fatalf("job kind = %q", job.Ref.Kind)
	}
	if job.Ref.Name != "nightly" || job.Ref.Region != "europe-west1" {
		t.Fatalf("job ref = %+v", job.Ref)
	}
	if job.Ref.Extra["project_id"] != "demo-project" {
		t.Fatalf("job extra = %+v", job.Ref.Extra)
	}
	if job.Labels["team"] != "data" || job.Labels["image"] != "gcr.io/demo/nightly:v2" {
		t.Fatalf("job labels = %+v", job.Labels)
	}

	if svc.Ref.Kind != domain.CloudResourceCloudRunService {
		t.Fatalf("service kind = %q", svc.Ref.Kind)
	}
	if svc.Ref.Name != "api" || svc.Ref.Region != "europe-west1" {
		t.Fatalf("service ref = %+v", svc.Ref)
	}
	if svc.URL != "https://api.run.app" {
		t.Fatalf("service url = %q", svc.URL)
	}
	if svc.Labels["team"] != "backend" || svc.Labels["image"] != "gcr.io/demo/api:v1" {
		t.Fatalf("service labels = %+v", svc.Labels)
	}
	for _, res := range resources {
		if res.Provider != domain.CloudGCP {
			t.Fatalf("provider = %q, want gcp", res.Provider)
		}
	}
}

// TestListResourcesFallsBackForServicesWhenWildcardRejected proves the
// services listing still enumerates every region when the aggregated call
// is refused, exactly like Client.ListCloudRunServices does — jobs never try
// the wildcard at all.
func TestListResourcesFallsBackForServicesWhenWildcardRejected(t *testing.T) {
	tok := newTokenServer(t)
	var mu sync.Mutex
	queried := map[string]bool{}
	run := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/locations/-/services"):
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":400,"message":"Location '-' is not a valid location"}}`))
		case strings.HasSuffix(r.URL.Path, "/locations"):
			_, _ = w.Write([]byte(`{"locations":[{"locationId":"europe-west1"},{"locationId":"us-central1"}]}`))
		case strings.HasSuffix(r.URL.Path, "/jobs"):
			_, _ = w.Write([]byte(`{"jobs":[]}`))
		case strings.Contains(r.URL.Path, "/services"):
			parts := strings.Split(r.URL.Path, "/")
			loc := parts[len(parts)-2]
			mu.Lock()
			queried[loc] = true
			mu.Unlock()
			_, _ = w.Write([]byte(`{"services":[{"name":"projects/demo-project/locations/` + loc + `/services/svc-` + loc + `","template":{"containers":[{"image":"img"}]}}]}`))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer run.Close()

	p := newTestProvider(t, tok.URL, run.URL)
	cred := testCredential(t, uuid.New(), tok.URL)

	resources, err := p.ListResources(context.Background(), cred)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if !queried["europe-west1"] || !queried["us-central1"] {
		t.Fatalf("fallback did not query every location: %v", queried)
	}
	services := 0
	for _, r := range resources {
		if r.Ref.Kind == domain.CloudResourceCloudRunService {
			services++
		}
	}
	if services != 2 {
		t.Fatalf("got %d services, want 2 (one per location)", services)
	}
}

func TestResourceServiceMapsStatusFactsConsoleURLAndLatestDeployment(t *testing.T) {
	tok := newTokenServer(t)
	run := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/revisions") {
			if got := r.URL.Query().Get("pageSize"); got != "1" {
				t.Errorf("revisions pageSize = %q, want 1", got)
			}
			_, _ = w.Write([]byte(`{"revisions":[
				{"name":"projects/demo-project/locations/us-central1/services/api/revisions/api-00009-xyz","createTime":"2026-09-20T10:00:00Z",
				 "labels":{"commit-sha":"abc123"},
				 "conditions":[{"type":"Ready","state":"CONDITION_SUCCEEDED","lastTransitionTime":"2026-09-20T10:01:00Z"}]}
			]}`))
			return
		}
		_, _ = w.Write([]byte(`{
			"name":"projects/demo-project/locations/us-central1/services/api",
			"uri":"https://api-abc.a.run.app",
			"labels":{"team":"backend"},
			"ingress":"INGRESS_TRAFFIC_ALL",
			"latestReadyRevision":"projects/demo-project/locations/us-central1/services/api/revisions/api-00009-xyz",
			"template":{"containers":[{"image":"gcr.io/demo/api:v9"}],"scaling":{"minInstanceCount":1,"maxInstanceCount":5}},
			"trafficStatuses":[{"revision":"projects/demo-project/locations/us-central1/services/api/revisions/api-00009-xyz","percent":100}],
			"conditions":[{"type":"Ready","state":"CONDITION_SUCCEEDED","message":"all good","lastTransitionTime":"2026-09-20T10:01:00Z"}],
			"terminalCondition":{"type":"Ready","state":"CONDITION_SUCCEEDED"}
		}`))
	}))
	defer run.Close()

	p := newTestProvider(t, tok.URL, run.URL)
	cred := testCredential(t, uuid.New(), tok.URL)
	ref := domain.CloudResourceRef{
		Kind:   domain.CloudResourceCloudRunService,
		ID:     "projects/demo-project/locations/us-central1/services/api",
		Name:   "api",
		Region: "us-central1",
		Extra:  map[string]string{"project_id": "demo-project"},
	}

	detail, err := p.Resource(context.Background(), cred, ref)
	if err != nil {
		t.Fatalf("Resource: %v", err)
	}

	if detail.Status != domain.CloudStatusHealthy {
		t.Fatalf("status = %q", detail.Status)
	}
	if detail.StatusDetail != "all good" {
		t.Fatalf("status detail = %q", detail.StatusDetail)
	}
	if detail.Revision != "api-00009-xyz" {
		t.Fatalf("revision = %q", detail.Revision)
	}
	wantConsole := "https://console.cloud.google.com/run/detail/us-central1/api/metrics?project=demo-project"
	if detail.ConsoleURL != wantConsole {
		t.Fatalf("console url = %q, want %q", detail.ConsoleURL, wantConsole)
	}

	facts := map[string]string{}
	for _, f := range detail.Facts {
		facts[f.Label] = f.Value
	}
	if facts["Region"] != "us-central1" {
		t.Fatalf("facts = %+v", detail.Facts)
	}
	if facts["Image"] != "gcr.io/demo/api:v9" {
		t.Fatalf("facts = %+v", detail.Facts)
	}
	if facts["Min instances"] != "1" || facts["Max instances"] != "5" {
		t.Fatalf("facts = %+v", detail.Facts)
	}
	if facts["Ingress"] != "INGRESS_TRAFFIC_ALL" {
		t.Fatalf("facts = %+v", detail.Facts)
	}
	if facts["Traffic"] != "api-00009-xyz: 100%" {
		t.Fatalf("facts = %+v", detail.Facts)
	}

	if detail.LatestDeployment == nil {
		t.Fatal("LatestDeployment is nil")
	}
	if detail.LatestDeployment.ID != "api-00009-xyz" {
		t.Fatalf("latest deployment id = %q", detail.LatestDeployment.ID)
	}
	if detail.LatestDeployment.CommitSHA != "abc123" {
		t.Fatalf("commit sha = %q", detail.LatestDeployment.CommitSHA)
	}
	if detail.LatestDeployment.Status != domain.CloudDeployReady {
		t.Fatalf("latest deployment status = %q", detail.LatestDeployment.Status)
	}

	if detail.Labels["team"] != "backend" || detail.Labels["image"] != "gcr.io/demo/api:v9" {
		t.Fatalf("labels = %+v", detail.Labels)
	}
}

func TestResourceServiceStatusMapping(t *testing.T) {
	tests := []struct {
		name  string
		state string
		want  domain.CloudResourceStatus
	}{
		{"succeeded", "CONDITION_SUCCEEDED", domain.CloudStatusHealthy},
		{"reconciling", "CONDITION_RECONCILING", domain.CloudStatusDeploying},
		{"failed", "CONDITION_FAILED", domain.CloudStatusFailed},
		{"pending", "CONDITION_PENDING", domain.CloudStatusDeploying},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tok := newTokenServer(t)
			run := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "/revisions") {
					_, _ = w.Write([]byte(`{"revisions":[]}`))
					return
				}
				_, _ = w.Write([]byte(`{"name":"projects/demo-project/locations/us-central1/services/api","terminalCondition":{"type":"Ready","state":"` + tt.state + `"}}`))
			}))
			defer run.Close()

			p := newTestProvider(t, tok.URL, run.URL)
			cred := testCredential(t, uuid.New(), tok.URL)
			ref := domain.CloudResourceRef{Kind: domain.CloudResourceCloudRunService, ID: "projects/demo-project/locations/us-central1/services/api", Name: "api", Region: "us-central1"}

			detail, err := p.Resource(context.Background(), cred, ref)
			if err != nil {
				t.Fatalf("Resource: %v", err)
			}
			if detail.Status != tt.want {
				t.Fatalf("status = %q, want %q", detail.Status, tt.want)
			}
		})
	}
}

func TestResourceJobUsesLastExecution(t *testing.T) {
	tok := newTokenServer(t)
	run := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/executions") {
			t.Errorf("path = %q, want the executions endpoint", r.URL.Path)
		}
		if got := r.URL.Query().Get("pageSize"); got != "1" {
			t.Errorf("pageSize = %q, want 1", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"executions":[
			{"name":"projects/demo-project/locations/us-central1/jobs/nightly/executions/nightly-ab12c","createTime":"2026-09-20T02:00:00Z","completionTime":"2026-09-20T02:05:00Z",
			 "labels":{"commit-sha":"deadbee"},
			 "conditions":[{"type":"Completed","state":"CONDITION_SUCCEEDED","message":"done"}],
			 "template":{"containers":[{"image":"gcr.io/demo/nightly:v3"}]}}
		]}`))
	}))
	defer run.Close()

	p := newTestProvider(t, tok.URL, run.URL)
	cred := testCredential(t, uuid.New(), tok.URL)
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceCloudRunJob, ID: "projects/demo-project/locations/us-central1/jobs/nightly", Name: "nightly", Region: "us-central1", Extra: map[string]string{"project_id": "demo-project"}}

	detail, err := p.Resource(context.Background(), cred, ref)
	if err != nil {
		t.Fatalf("Resource: %v", err)
	}
	if detail.Status != domain.CloudStatusHealthy {
		t.Fatalf("status = %q", detail.Status)
	}
	if detail.LatestDeployment == nil {
		t.Fatal("LatestDeployment is nil")
	}
	if detail.LatestDeployment.ID != "nightly-ab12c" {
		t.Fatalf("id = %q", detail.LatestDeployment.ID)
	}
	if detail.LatestDeployment.CommitSHA != "deadbee" {
		t.Fatalf("commit sha = %q", detail.LatestDeployment.CommitSHA)
	}
	if detail.LatestDeployment.ReadyAt == nil {
		t.Fatal("ReadyAt is nil")
	}
	consoleURL := "https://console.cloud.google.com/run/jobs/details/us-central1/nightly/executions?project=demo-project"
	if detail.ConsoleURL != consoleURL {
		t.Fatalf("console url = %q, want %q", detail.ConsoleURL, consoleURL)
	}
}

func TestResourceJobWithNoExecutionsIsHonestAboutIt(t *testing.T) {
	tok := newTokenServer(t)
	run := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"executions":[]}`))
	}))
	defer run.Close()

	p := newTestProvider(t, tok.URL, run.URL)
	cred := testCredential(t, uuid.New(), tok.URL)
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceCloudRunJob, ID: "projects/demo-project/locations/us-central1/jobs/nightly", Name: "nightly", Region: "us-central1"}

	detail, err := p.Resource(context.Background(), cred, ref)
	if err != nil {
		t.Fatalf("Resource: %v", err)
	}
	if detail.LatestDeployment != nil {
		t.Fatal("LatestDeployment should be nil with no executions")
	}
	if detail.StatusDetail != "no executions yet" {
		t.Fatalf("status detail = %q", detail.StatusDetail)
	}
}

func TestResourceRejectsAnUnsupportedKind(t *testing.T) {
	tok := newTokenServer(t)
	p := newTestProvider(t, tok.URL, "http://unused")
	cred := testCredential(t, uuid.New(), tok.URL)
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceGKEWorkload, ID: "x", Name: "x"}

	_, err := p.Resource(context.Background(), cred, ref)
	if !errors.Is(err, port.ErrUnsupported) {
		t.Fatalf("err = %v, want port.ErrUnsupported", err)
	}
}

func TestResourceReportsA404AsNotFound(t *testing.T) {
	tok := newTokenServer(t)
	run := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer run.Close()

	p := newTestProvider(t, tok.URL, run.URL)
	cred := testCredential(t, uuid.New(), tok.URL)
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceCloudRunService, ID: "projects/demo-project/locations/us-central1/services/gone", Name: "gone", Region: "us-central1"}

	_, err := p.Resource(context.Background(), cred, ref)
	if !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("err = %v, want it to wrap port.ErrNotFound", err)
	}
}

func TestResourceReportsA403AsCloudAuth(t *testing.T) {
	tok := newTokenServer(t)
	run := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer run.Close()

	p := newTestProvider(t, tok.URL, run.URL)
	cred := testCredential(t, uuid.New(), tok.URL)
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceCloudRunService, ID: "projects/demo-project/locations/us-central1/services/x", Name: "x", Region: "us-central1"}

	_, err := p.Resource(context.Background(), cred, ref)
	if !errors.Is(err, port.ErrCloudAuth) {
		t.Fatalf("err = %v, want it to wrap port.ErrCloudAuth", err)
	}
}

func TestDeploymentsListsServiceRevisionsNewestFirst(t *testing.T) {
	tok := newTokenServer(t)
	run := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("pageSize"); got != "5" {
			t.Errorf("pageSize = %q, want 5", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"revisions":[
			{"name":"projects/demo-project/locations/us-central1/services/api/revisions/api-00010","createTime":"2026-09-21T00:00:00Z","annotations":{"gcb-build-id":"build-77"},"conditions":[{"type":"Ready","state":"CONDITION_FAILED","lastTransitionTime":"2026-09-21T00:01:00Z"}]},
			{"name":"projects/demo-project/locations/us-central1/services/api/revisions/api-00009","createTime":"2026-09-20T00:00:00Z","labels":{"commit-sha":"abc123"},"conditions":[{"type":"Ready","state":"CONDITION_SUCCEEDED","lastTransitionTime":"2026-09-20T00:01:00Z"}]}
		]}`))
	}))
	defer run.Close()

	p := newTestProvider(t, tok.URL, run.URL)
	cred := testCredential(t, uuid.New(), tok.URL)
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceCloudRunService, ID: "projects/demo-project/locations/us-central1/services/api", Name: "api", Region: "us-central1", Extra: map[string]string{"project_id": "demo-project"}}

	deployments, err := p.Deployments(context.Background(), cred, ref, "", 5)
	if err != nil {
		t.Fatalf("Deployments: %v", err)
	}
	if len(deployments) != 2 {
		t.Fatalf("got %d deployments, want 2", len(deployments))
	}
	if deployments[0].ID != "api-00010" || deployments[0].Status != domain.CloudDeployError {
		t.Fatalf("first = %+v", deployments[0])
	}
	if deployments[0].CommitSHA != "build-77" {
		t.Fatalf("commit sha from annotation = %q", deployments[0].CommitSHA)
	}
	if deployments[1].ID != "api-00009" || deployments[1].Status != domain.CloudDeployReady {
		t.Fatalf("second = %+v", deployments[1])
	}
	if deployments[1].CommitSHA != "abc123" {
		t.Fatalf("commit sha from label = %q", deployments[1].CommitSHA)
	}
	if deployments[1].Environment != domain.EnvironmentProduction {
		t.Fatalf("environment = %q", deployments[1].Environment)
	}
	if deployments[1].ReadyAt == nil {
		t.Fatal("ReadyAt not set from the condition's lastTransitionTime")
	}
}

func TestDeploymentsListsJobExecutionsAndDetectsCancellation(t *testing.T) {
	tok := newTokenServer(t)
	run := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"executions":[
			{"name":"projects/demo-project/locations/us-central1/jobs/nightly/executions/nightly-1","createTime":"2026-09-21T00:00:00Z","cancelledCount":1,"conditions":[{"type":"Completed","state":"CONDITION_FAILED"}]}
		]}`))
	}))
	defer run.Close()

	p := newTestProvider(t, tok.URL, run.URL)
	cred := testCredential(t, uuid.New(), tok.URL)
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceCloudRunJob, ID: "projects/demo-project/locations/us-central1/jobs/nightly", Name: "nightly", Region: "us-central1"}

	deployments, err := p.Deployments(context.Background(), cred, ref, "", 10)
	if err != nil {
		t.Fatalf("Deployments: %v", err)
	}
	if len(deployments) != 1 {
		t.Fatalf("got %d, want 1", len(deployments))
	}
	if deployments[0].Status != domain.CloudDeployCanceled {
		t.Fatalf("status = %q, want canceled", deployments[0].Status)
	}
}
