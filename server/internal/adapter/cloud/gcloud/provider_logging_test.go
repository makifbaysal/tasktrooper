package gcloud_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestLogsBuildsTheServiceFilterAndMapsEntries(t *testing.T) {
	tok := newTokenServer(t)
	var gotPath string
	var gotBody map[string]any
	logging := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"entries":[
				{"timestamp":"2026-09-20T10:00:00.123Z","severity":"ERROR","textPayload":"boom",
				 "resource":{"labels":{"revision_name":"api-00009-xyz"}},
				 "httpRequest":{"requestMethod":"GET","requestUrl":"https://api.run.app/health?x=1","status":500},
				 "trace":"projects/demo-project/traces/abc123",
				 "logName":"projects/demo-project/logs/run.googleapis.com%2Fstderr"}
			],
			"nextPageToken":"page-2"
		}`))
	}))
	defer logging.Close()

	p := newTestProvider(t, tok.URL, "http://unused")
	p.SetLoggingBaseURL(logging.URL)
	cred := testCredential(t, uuid.New(), tok.URL)
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceCloudRunService, Name: "api", Region: "us-central1", Extra: map[string]string{"project_id": "demo-project"}}

	q := domain.RuntimeLogQuery{
		Since:       time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC),
		Until:       time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC),
		MinSeverity: domain.LogError,
		Text:        `disk "full"`,
		Limit:       50,
	}

	page, err := p.Logs(context.Background(), cred, ref, q)
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}

	if gotPath != "/v2/entries:list" {
		t.Fatalf("path = %q", gotPath)
	}
	filter, _ := gotBody["filter"].(string)
	wantFilter := `resource.type="cloud_run_revision" AND resource.labels.service_name="api" AND resource.labels.location="us-central1" AND timestamp>="2026-09-20T00:00:00Z" AND timestamp<="2026-09-20T12:00:00Z" AND severity>=ERROR AND SEARCH("disk \"full\"")`
	if filter != wantFilter {
		t.Fatalf("filter = %q, want %q", filter, wantFilter)
	}
	if gotBody["orderBy"] != "timestamp desc" {
		t.Fatalf("orderBy = %v", gotBody["orderBy"])
	}
	resourceNames, ok := gotBody["resourceNames"].([]any)
	if !ok || len(resourceNames) != 1 || resourceNames[0] != "projects/demo-project" {
		t.Fatalf("resourceNames = %v", gotBody["resourceNames"])
	}
	if pageSize, ok := gotBody["pageSize"].(float64); !ok || pageSize != 50 {
		t.Fatalf("pageSize = %v", gotBody["pageSize"])
	}

	if page.NextCursor != "page-2" {
		t.Fatalf("next cursor = %q", page.NextCursor)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(page.Entries))
	}
	e := page.Entries[0]
	if e.Severity != domain.LogError {
		t.Fatalf("severity = %q", e.Severity)
	}
	if e.Message != "boom" {
		t.Fatalf("message = %q", e.Message)
	}
	if e.Source != "api-00009-xyz" {
		t.Fatalf("source = %q", e.Source)
	}
	if e.Method != "GET" || e.Path != "/health" || e.StatusCode != 500 {
		t.Fatalf("http request fields = %+v", e)
	}
	if e.TraceID != "abc123" {
		t.Fatalf("trace id = %q", e.TraceID)
	}
	if e.Fields["log_name"] != "run.googleapis.com/stderr" {
		t.Fatalf("fields = %+v", e.Fields)
	}
	if e.Timestamp.IsZero() {
		t.Fatal("timestamp not parsed")
	}
}

func TestLogsBuildsTheJobFilterWithNoLocationClause(t *testing.T) {
	tok := newTokenServer(t)
	var gotBody map[string]any
	logging := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"entries":[]}`))
	}))
	defer logging.Close()

	p := newTestProvider(t, tok.URL, "http://unused")
	p.SetLoggingBaseURL(logging.URL)
	cred := testCredential(t, uuid.New(), tok.URL)
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceCloudRunJob, Name: "nightly", Region: "us-central1", Extra: map[string]string{"project_id": "demo-project"}}

	if _, err := p.Logs(context.Background(), cred, ref, domain.RuntimeLogQuery{}); err != nil {
		t.Fatalf("Logs: %v", err)
	}
	want := `resource.type="cloud_run_job" AND resource.labels.job_name="nightly"`
	if gotBody["filter"] != want {
		t.Fatalf("filter = %v, want %q", gotBody["filter"], want)
	}
	if gotBody["pageSize"].(float64) != 200 {
		t.Fatalf("default page size = %v, want 200", gotBody["pageSize"])
	}
}

func TestLogsMapsJSONPayloadAndClampsPageSize(t *testing.T) {
	tok := newTokenServer(t)
	var gotBody map[string]any
	logging := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"entries":[
			{"timestamp":"2026-09-20T10:00:00Z","severity":"DEFAULT","jsonPayload":{"msg":"structured message","extra":"x"}}
		]}`))
	}))
	defer logging.Close()

	p := newTestProvider(t, tok.URL, "http://unused")
	p.SetLoggingBaseURL(logging.URL)
	cred := testCredential(t, uuid.New(), tok.URL)
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceCloudRunService, Name: "api", Region: "us-central1", Extra: map[string]string{"project_id": "demo-project"}}

	page, err := p.Logs(context.Background(), cred, ref, domain.RuntimeLogQuery{Limit: 5000})
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if gotBody["pageSize"].(float64) != 1000 {
		t.Fatalf("page size = %v, want clamped to 1000", gotBody["pageSize"])
	}
	if len(page.Entries) != 1 {
		t.Fatalf("got %d entries", len(page.Entries))
	}
	if page.Entries[0].Message != "structured message" {
		t.Fatalf("message = %q", page.Entries[0].Message)
	}
	if page.Entries[0].Severity != domain.LogDebug {
		t.Fatalf("severity = %q, want debug for DEFAULT", page.Entries[0].Severity)
	}
}

func TestLogsUsesACursorAsThePageToken(t *testing.T) {
	tok := newTokenServer(t)
	var gotBody map[string]any
	logging := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"entries":[]}`))
	}))
	defer logging.Close()

	p := newTestProvider(t, tok.URL, "http://unused")
	p.SetLoggingBaseURL(logging.URL)
	cred := testCredential(t, uuid.New(), tok.URL)
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceCloudRunService, Name: "api", Region: "us-central1", Extra: map[string]string{"project_id": "demo-project"}}

	if _, err := p.Logs(context.Background(), cred, ref, domain.RuntimeLogQuery{Cursor: "cursor-abc"}); err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if gotBody["pageToken"] != "cursor-abc" {
		t.Fatalf("pageToken = %v, want the query cursor forwarded", gotBody["pageToken"])
	}
}

// TestLogsRequestsTheReadOnlyLoggingScope decodes the minted JWT's own scope
// claim — the read-only + logging.read pair the spec calls for, distinct
// from the plain cloud-platform scope every other call uses.
func TestLogsRequestsTheReadOnlyLoggingScope(t *testing.T) {
	var gotScope string
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		parts := strings.Split(r.PostFormValue("assertion"), ".")
		if len(parts) == 3 {
			claimsJSON, _ := base64.RawURLEncoding.DecodeString(parts[1])
			var claims struct {
				Scope string `json:"scope"`
			}
			_ = json.Unmarshal(claimsJSON, &claims)
			gotScope = claims.Scope
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"ya29.log-token","expires_in":3600}`))
	}))
	defer tokenSrv.Close()

	logging := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer ya29.log-token" {
			t.Errorf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"entries":[]}`))
	}))
	defer logging.Close()

	p := newTestProvider(t, tokenSrv.URL, "http://unused")
	p.SetLoggingBaseURL(logging.URL)
	cred := testCredential(t, uuid.New(), tokenSrv.URL)
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceCloudRunService, Name: "api", Region: "us-central1", Extra: map[string]string{"project_id": "demo-project"}}

	if _, err := p.Logs(context.Background(), cred, ref, domain.RuntimeLogQuery{}); err != nil {
		t.Fatalf("Logs: %v", err)
	}
	want := "https://www.googleapis.com/auth/cloud-platform.read-only https://www.googleapis.com/auth/logging.read"
	if gotScope != want {
		t.Fatalf("scope = %q, want %q", gotScope, want)
	}
}
