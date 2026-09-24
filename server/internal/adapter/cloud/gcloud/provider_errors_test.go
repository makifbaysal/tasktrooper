package gcloud_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func TestErrorsMapsGroupStatsAndBuildsTheRequest(t *testing.T) {
	tok := newTokenServer(t)
	since := time.Now().Add(-2 * time.Hour)
	firstSeen := since.Add(30 * time.Minute).UTC().Format(time.RFC3339)
	lastSeen := since.Add(time.Hour).UTC().Format(time.RFC3339)

	var gotPath string
	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errorGroupStats":[
			{"group":{"groupId":"CIDp1a2b3c"},"count":"42","firstSeenTime":"` + firstSeen + `","lastSeenTime":"` + lastSeen + `",
			 "representative":{"message":"panic: nil pointer\ngoroutine 1 [running]:\nmain.foo()"}}
		]}`))
	}))
	defer errSrv.Close()

	p := newTestProvider(t, tok.URL, "http://unused")
	p.SetErrorReportingBaseURL(errSrv.URL)
	cred := testCredential(t, uuid.New(), tok.URL)
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceCloudRunService, Name: "api", Region: "us-central1", Extra: map[string]string{"project_id": "demo-project"}}

	groups, err := p.Errors(context.Background(), cred, ref, since)
	if err != nil {
		t.Fatalf("Errors: %v", err)
	}

	if !strings.Contains(gotPath, "/v1beta1/projects/demo-project/groupStats") {
		t.Fatalf("path = %q", gotPath)
	}
	if !strings.Contains(gotPath, "serviceFilter.service=api") {
		t.Fatalf("path = %q", gotPath)
	}
	if !strings.Contains(gotPath, "timeRange.period=PERIOD_6_HOURS") {
		t.Fatalf("path = %q, want PERIOD_6_HOURS for a 2h window", gotPath)
	}
	if !strings.Contains(gotPath, "order=COUNT_DESC") || !strings.Contains(gotPath, "pageSize=50") {
		t.Fatalf("path = %q", gotPath)
	}

	if len(groups) != 1 {
		t.Fatalf("got %d groups, want 1", len(groups))
	}
	g := groups[0]
	if g.Fingerprint != "CIDp1a2b3c" {
		t.Fatalf("fingerprint = %q", g.Fingerprint)
	}
	if g.Count != 42 {
		t.Fatalf("count = %d", g.Count)
	}
	if g.Message != "panic: nil pointer" {
		t.Fatalf("message = %q", g.Message)
	}
	if !strings.Contains(g.Sample, "goroutine 1") {
		t.Fatalf("sample = %q", g.Sample)
	}
	if g.Source != "Error Reporting" {
		t.Fatalf("source = %q", g.Source)
	}
	if !g.New {
		t.Fatal("New should be true — first seen falls inside the queried window")
	}
	wantURL := "https://console.cloud.google.com/errors/detail/CIDp1a2b3c?project=demo-project"
	if g.ExternalURL != wantURL {
		t.Fatalf("external url = %q, want %q", g.ExternalURL, wantURL)
	}
}

func TestErrorsMarksAGroupOlderThanTheWindowAsNotNew(t *testing.T) {
	tok := newTokenServer(t)
	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errorGroupStats":[
			{"group":{"groupId":"old"},"count":"3","firstSeenTime":"2020-01-01T00:00:00Z","lastSeenTime":"2026-09-20T10:00:00Z","representative":{"message":"stale"}}
		]}`))
	}))
	defer errSrv.Close()

	p := newTestProvider(t, tok.URL, "http://unused")
	p.SetErrorReportingBaseURL(errSrv.URL)
	cred := testCredential(t, uuid.New(), tok.URL)
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceCloudRunService, Name: "api", Region: "us-central1", Extra: map[string]string{"project_id": "demo-project"}}

	groups, err := p.Errors(context.Background(), cred, ref, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("Errors: %v", err)
	}
	if len(groups) != 1 || groups[0].New {
		t.Fatalf("groups = %+v, want New=false for a group first seen long before the window", groups)
	}
}

func TestErrorReportingPeriodPicksTheSmallestCoveringWindow(t *testing.T) {
	tests := []struct {
		name  string
		since time.Duration
		want  string
	}{
		{"30 minutes", 30 * time.Minute, "PERIOD_1_HOUR"},
		{"3 hours", 3 * time.Hour, "PERIOD_6_HOURS"},
		{"12 hours", 12 * time.Hour, "PERIOD_1_DAY"},
		{"3 days", 3 * 24 * time.Hour, "PERIOD_1_WEEK"},
		{"45 days", 45 * 24 * time.Hour, "PERIOD_30_DAYS"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tok := newTokenServer(t)
			var gotPath string
			errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.RequestURI()
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"errorGroupStats":[]}`))
			}))
			defer errSrv.Close()

			p := newTestProvider(t, tok.URL, "http://unused")
			p.SetErrorReportingBaseURL(errSrv.URL)
			cred := testCredential(t, uuid.New(), tok.URL)
			ref := domain.CloudResourceRef{Kind: domain.CloudResourceCloudRunService, Name: "api", Region: "us-central1", Extra: map[string]string{"project_id": "demo-project"}}

			if _, err := p.Errors(context.Background(), cred, ref, time.Now().Add(-tt.since)); err != nil {
				t.Fatalf("Errors: %v", err)
			}
			if !strings.Contains(gotPath, "timeRange.period="+tt.want) {
				t.Fatalf("path = %q, want period %s", gotPath, tt.want)
			}
		})
	}
}

func TestErrorsReturnsUnsupportedWhenErrorReportingIsDisabled(t *testing.T) {
	tok := newTokenServer(t)
	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":403,"status":"PERMISSION_DENIED","message":"Error Reporting API has not been used, or it is SERVICE_DISABLED"}}`))
	}))
	defer errSrv.Close()

	p := newTestProvider(t, tok.URL, "http://unused")
	p.SetErrorReportingBaseURL(errSrv.URL)
	cred := testCredential(t, uuid.New(), tok.URL)
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceCloudRunService, Name: "api", Region: "us-central1", Extra: map[string]string{"project_id": "demo-project"}}

	_, err := p.Errors(context.Background(), cred, ref, time.Now().Add(-time.Hour))
	if !errors.Is(err, port.ErrUnsupported) {
		t.Fatalf("err = %v, want port.ErrUnsupported", err)
	}
}

func TestErrorsReturnsUnsupportedOn404(t *testing.T) {
	tok := newTokenServer(t)
	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer errSrv.Close()

	p := newTestProvider(t, tok.URL, "http://unused")
	p.SetErrorReportingBaseURL(errSrv.URL)
	cred := testCredential(t, uuid.New(), tok.URL)
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceCloudRunService, Name: "api", Region: "us-central1", Extra: map[string]string{"project_id": "demo-project"}}

	_, err := p.Errors(context.Background(), cred, ref, time.Now().Add(-time.Hour))
	if !errors.Is(err, port.ErrUnsupported) {
		t.Fatalf("err = %v, want port.ErrUnsupported", err)
	}
}

func TestErrorsMapsAPlain403AsCloudAuth(t *testing.T) {
	tok := newTokenServer(t)
	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":403,"message":"Permission denied"}}`))
	}))
	defer errSrv.Close()

	p := newTestProvider(t, tok.URL, "http://unused")
	p.SetErrorReportingBaseURL(errSrv.URL)
	cred := testCredential(t, uuid.New(), tok.URL)
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceCloudRunService, Name: "api", Region: "us-central1", Extra: map[string]string{"project_id": "demo-project"}}

	_, err := p.Errors(context.Background(), cred, ref, time.Now().Add(-time.Hour))
	if !errors.Is(err, port.ErrCloudAuth) {
		t.Fatalf("err = %v, want it to wrap port.ErrCloudAuth", err)
	}
}
