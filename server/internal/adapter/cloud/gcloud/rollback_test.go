// Endpoints verified against the Cloud Run Admin API v2 reference on
// 2026-09-25:
//   - https://cloud.google.com/run/docs/reference/rest/v2/projects.locations.services/patch
//     (PATCH /v2/{service.name}?updateMask=traffic)
//   - https://cloud.google.com/run/docs/reference/rest/v2/projects.locations.services/get
//     (trafficStatuses / TrafficTarget.type — TRAFFIC_TARGET_ALLOCATION_TYPE_REVISION,
//     TRAFFIC_TARGET_ALLOCATION_TYPE_LATEST)
package gcloud_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const testServiceName = "projects/demo-project/locations/europe-west1/services/api"

func testServiceRef() domain.CloudResourceRef {
	return domain.CloudResourceRef{
		Kind:   domain.CloudResourceCloudRunService,
		ID:     testServiceName,
		Name:   "api",
		Region: "europe-west1",
		Extra:  map[string]string{"project_id": "demo-project"},
	}
}

func TestRollbackToPatchesTrafficToTheRevision(t *testing.T) {
	tok := newTokenServer(t)
	var gotMethod, gotPath string
	var gotBody map[string]any
	run := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		if r.URL.RawQuery != "updateMask=traffic" {
			t.Errorf("query = %q, want updateMask=traffic", r.URL.RawQuery)
		}
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer run.Close()

	p := newTestProvider(t, tok.URL, run.URL)
	cred := testCredential(t, uuid.New(), tok.URL)

	err := p.RollbackTo(context.Background(), cred, testServiceRef(), "api-00023-xyz")
	if err != nil {
		t.Fatalf("RollbackTo: %v", err)
	}
	if gotMethod != http.MethodPatch {
		t.Fatalf("method = %q", gotMethod)
	}
	if gotPath != "/v2/"+testServiceName {
		t.Fatalf("path = %q", gotPath)
	}
	traffic := gotBody["traffic"].([]any)[0].(map[string]any)
	if traffic["type"] != "TRAFFIC_TARGET_ALLOCATION_TYPE_REVISION" || traffic["revision"] != "api-00023-xyz" || traffic["percent"].(float64) != 100 {
		t.Fatalf("traffic = %+v", traffic)
	}
}

func TestRollbackToRefusesNonCloudRunServiceKinds(t *testing.T) {
	tok := newTokenServer(t)
	run := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("must not call the API for an unsupported resource kind")
	}))
	defer run.Close()

	p := newTestProvider(t, tok.URL, run.URL)
	cred := testCredential(t, uuid.New(), tok.URL)
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceCloudRunJob, ID: "projects/demo-project/locations/europe-west1/jobs/nightly"}

	err := p.RollbackTo(context.Background(), cred, ref, "exec-1")
	if err == nil || err != port.ErrUnsupported {
		t.Fatalf("err = %v, want port.ErrUnsupported", err)
	}
}

func TestRollbackToForbiddenNeedsRunServicesUpdate(t *testing.T) {
	tok := newTokenServer(t)
	run := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":403,"message":"Permission denied"}}`))
	}))
	defer run.Close()

	p := newTestProvider(t, tok.URL, run.URL)
	cred := testCredential(t, uuid.New(), tok.URL)

	err := p.RollbackTo(context.Background(), cred, testServiceRef(), "api-00023-xyz")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, port.ErrCloudWriteDenied) {
		t.Fatalf("err = %v, want it to wrap port.ErrCloudWriteDenied", err)
	}
}

func TestPromotePinsTheRevisionWhenItIsNotLatest(t *testing.T) {
	tok := newTokenServer(t)
	var gotBody map[string]any
	run := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"name":"` + testServiceName + `","latestReadyRevision":"api-00025-abc"}`))
		case http.MethodPatch:
			data, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(data, &gotBody)
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer run.Close()

	p := newTestProvider(t, tok.URL, run.URL)
	cred := testCredential(t, uuid.New(), tok.URL)

	err := p.Promote(context.Background(), cred, testServiceRef(), "api-00023-xyz")
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}
	traffic := gotBody["traffic"].([]any)[0].(map[string]any)
	if traffic["type"] != "TRAFFIC_TARGET_ALLOCATION_TYPE_REVISION" || traffic["revision"] != "api-00023-xyz" {
		t.Fatalf("traffic = %+v, want a revision-pinned target", traffic)
	}
}

func TestPromoteUsesLatestAllocationWhenDeploymentIsTheLatestReadyRevision(t *testing.T) {
	tok := newTokenServer(t)
	var gotBody map[string]any
	run := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"name":"` + testServiceName + `","latestReadyRevision":"api-00025-abc"}`))
		case http.MethodPatch:
			data, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(data, &gotBody)
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer run.Close()

	p := newTestProvider(t, tok.URL, run.URL)
	cred := testCredential(t, uuid.New(), tok.URL)

	err := p.Promote(context.Background(), cred, testServiceRef(), "api-00025-abc")
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}
	traffic := gotBody["traffic"].([]any)[0].(map[string]any)
	if traffic["type"] != "TRAFFIC_TARGET_ALLOCATION_TYPE_LATEST" {
		t.Fatalf("traffic = %+v, want the LATEST allocation", traffic)
	}
	if _, hasRevision := traffic["revision"]; hasRevision {
		t.Fatalf("traffic = %+v, must not name a revision for a LATEST allocation", traffic)
	}
}

func TestCurrentReturnsTheRevisionServingTheLargestShare(t *testing.T) {
	tok := newTokenServer(t)
	run := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/revisions"):
			_, _ = w.Write([]byte(`{"revisions":[
				{"name":"` + testServiceName + `/revisions/api-00025-abc","createTime":"2026-09-20T00:00:00Z","labels":{"commit-sha":"deadbeef"}},
				{"name":"` + testServiceName + `/revisions/api-00024-old","createTime":"2026-09-10T00:00:00Z"}
			]}`))
		case r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"name":"` + testServiceName + `","latestReadyRevision":"api-00025-abc","trafficStatuses":[
				{"type":"TRAFFIC_TARGET_ALLOCATION_TYPE_REVISION","revision":"api-00024-old","percent":20},
				{"type":"TRAFFIC_TARGET_ALLOCATION_TYPE_REVISION","revision":"api-00025-abc","percent":80}
			]}`))
		}
	}))
	defer run.Close()

	p := newTestProvider(t, tok.URL, run.URL)
	cred := testCredential(t, uuid.New(), tok.URL)

	d, err := p.Current(context.Background(), cred, testServiceRef())
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if d.ID != "api-00025-abc" {
		t.Fatalf("Current().ID = %q, want the 80%%-traffic revision", d.ID)
	}
	if d.CommitSHA != "deadbeef" {
		t.Fatalf("CommitSHA = %q", d.CommitSHA)
	}
}

func TestCurrentFallsBackToLatestReadyRevisionWithNoTrafficStatuses(t *testing.T) {
	tok := newTokenServer(t)
	run := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/revisions"):
			_, _ = w.Write([]byte(`{"revisions":[{"name":"` + testServiceName + `/revisions/api-00025-abc","createTime":"2026-09-20T00:00:00Z"}]}`))
		case r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"name":"` + testServiceName + `","latestReadyRevision":"api-00025-abc"}`))
		}
	}))
	defer run.Close()

	p := newTestProvider(t, tok.URL, run.URL)
	cred := testCredential(t, uuid.New(), tok.URL)

	d, err := p.Current(context.Background(), cred, testServiceRef())
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if d.ID != "api-00025-abc" {
		t.Fatalf("Current().ID = %q", d.ID)
	}
}

func TestCurrentRefusesNonCloudRunServiceKinds(t *testing.T) {
	tok := newTokenServer(t)
	run := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("must not call the API for an unsupported resource kind")
	}))
	defer run.Close()

	p := newTestProvider(t, tok.URL, run.URL)
	cred := testCredential(t, uuid.New(), tok.URL)
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceCloudRunJob, ID: "projects/demo-project/locations/europe-west1/jobs/nightly"}

	_, err := p.Current(context.Background(), cred, ref)
	if err == nil || err != port.ErrUnsupported {
		t.Fatalf("err = %v, want port.ErrUnsupported", err)
	}
}
