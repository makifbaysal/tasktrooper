package github

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type zipEntry struct {
	name    string
	body    string
	symlink bool
}

func buildZip(t *testing.T, entries ...zipEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		hdr := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if e.symlink {
			hdr.SetMode(os.ModeSymlink | 0o777)
		} else {
			hdr.SetMode(0o644)
		}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatalf("zip header %s: %v", e.name, err)
		}
		if _, err := w.Write([]byte(e.body)); err != nil {
			t.Fatalf("zip write %s: %v", e.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

// fakeActions answers every call MobileTestBuilder makes for acme/widget, and
// serves the artifact archive from a second server so the redirect crosses
// origins the way GitHub's 302 to blob storage does.
type fakeActions struct {
	t            *testing.T
	workflow     string
	identifier   string
	buildID      uuid.UUID
	conclusion   string
	sameTree     bool
	jobLog       string
	artifactZip  []byte
	missingPolls int

	mu            sync.Mutex
	api           *httptest.Server
	blob          *httptest.Server
	commitMessage string
	committed     bool
	dispatch      map[string]any
	dispatched    bool
	runsQueries   []url.Values
	runPolls      int
	apiAuth       []string
	blobAuth      []string
	blobHits      int
}

func (f *fakeActions) start() {
	f.blob = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.blobAuth = append(f.blobAuth, r.Header.Get("Authorization"))
		f.blobHits++
		f.mu.Unlock()
		if r.URL.Query().Get("sig") != "signed" {
			http.Error(w, "unsigned", http.StatusForbidden)
			return
		}
		_, _ = w.Write(f.artifactZip)
	}))
	f.t.Cleanup(f.blob.Close)

	f.api = httptest.NewServer(http.HandlerFunc(f.serveAPI))
	f.t.Cleanup(f.api.Close)
}

func (f *fakeActions) serveAPI(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := f.t
	f.apiAuth = append(f.apiAuth, r.Header.Get("Authorization"))
	decode := func(out any) {
		if err := json.NewDecoder(r.Body).Decode(out); err != nil {
			t.Errorf("decode %s: %v", r.URL.Path, err)
		}
	}
	runTitle := "tt-test-build " + f.buildID.String()
	runURL := "https://github.com/acme/widget/actions/runs/99"
	const repo = "/repos/acme/widget"
	switch {
	case r.URL.Path == repo+"/git/ref/heads/main":
		_, _ = w.Write([]byte(`{"object":{"sha":"tip-sha"}}`))
	case r.URL.Path == repo+"/git/commits/tip-sha" && r.Method == http.MethodGet:
		_, _ = w.Write([]byte(`{"tree":{"sha":"base-tree"}}`))
	case r.URL.Path == repo+"/git/blobs":
		_, _ = w.Write([]byte(`{"sha":"blob-sha"}`))
	case r.URL.Path == repo+"/git/trees":
		if f.sameTree {
			_, _ = w.Write([]byte(`{"sha":"base-tree"}`))
			return
		}
		_, _ = w.Write([]byte(`{"sha":"new-tree"}`))
	case r.URL.Path == repo+"/git/commits" && r.Method == http.MethodPost:
		var body struct {
			Message string `json:"message"`
		}
		decode(&body)
		f.commitMessage = body.Message
		f.committed = true
		_, _ = w.Write([]byte(`{"sha":"new-commit-sha"}`))
	case r.URL.Path == repo+"/git/refs/heads/main" && r.Method == http.MethodPatch:
		_, _ = w.Write([]byte(`{}`))
	case r.URL.Path == repo+"/actions/workflows/"+f.workflow+"/dispatches" && r.Method == http.MethodPost:
		decode(&f.dispatch)
		f.dispatched = true
		w.WriteHeader(http.StatusNoContent)
	case r.URL.Path == repo+"/actions/workflows/"+f.workflow+"/runs":
		f.runsQueries = append(f.runsQueries, r.URL.Query())
		if !f.dispatched || len(f.runsQueries) <= f.missingPolls {
			_, _ = w.Write([]byte(`{"workflow_runs":[]}`))
			return
		}
		_, _ = fmt.Fprintf(w, `{"workflow_runs":[
			{"id":98,"display_title":"tt-test-build someone-else","status":"queued","html_url":"https://github.com/acme/widget/actions/runs/98","created_at":"2026-10-08T10:00:01Z"},
			{"id":99,"display_title":%q,"status":"queued","html_url":%q,"created_at":"2026-10-08T10:00:00Z"}
		]}`, runTitle, runURL)
	case r.URL.Path == repo+"/actions/runs/99":
		f.runPolls++
		if f.runPolls < 2 {
			_, _ = fmt.Fprintf(w, `{"id":99,"status":"in_progress","html_url":%q}`, runURL)
			return
		}
		_, _ = fmt.Fprintf(w, `{"id":99,"status":"completed","conclusion":%q,"html_url":%q}`, f.conclusion, runURL)
	case r.URL.Path == repo+"/actions/runs/99/jobs":
		_, _ = w.Write([]byte(`{"jobs":[
			{"id":6,"name":"setup","status":"completed","conclusion":"success"},
			{"id":7,"name":"release","status":"completed","conclusion":"failure"}
		]}`))
	case r.URL.Path == repo+"/actions/jobs/7/logs":
		_, _ = w.Write([]byte(f.jobLog))
	case r.URL.Path == repo+"/actions/runs/99/artifacts":
		name := "mobile-release-" + f.identifier + "-" + f.buildID.String()
		_, _ = fmt.Fprintf(w, `{"artifacts":[
			{"id":4,"name":"something-else","archive_download_url":%q,"size_in_bytes":10},
			{"id":5,"name":%q,"archive_download_url":%q,"size_in_bytes":%d}
		]}`, f.api.URL+repo+"/actions/artifacts/4/zip", name, f.api.URL+repo+"/actions/artifacts/5/zip", len(f.artifactZip))
	case r.URL.Path == repo+"/actions/artifacts/5/zip":
		http.Redirect(w, r, f.blob.URL+"/blob/artifact.zip?sig=signed", http.StatusFound)
	default:
		t.Errorf("unexpected %s %s", r.Method, r.URL.String())
		http.Error(w, "unexpected", http.StatusNotFound)
	}
}

func (f *fakeActions) builder() *MobileTestBuilder {
	b := NewMobileTestBuilder(func(context.Context, domain.Repository) (string, string, string, string, error) {
		return "tok", "acme", "widget", "main", nil
	})
	b.SetBaseURL(f.api.URL)
	b.SetTimings(time.Millisecond, 2*time.Second, time.Millisecond, 5*time.Second)
	return b
}

func testBuildRequest(f *fakeActions, platform string, artifactDir string) (port.MobileBuildRequest, *[]string, *[]string) {
	var logs, runURLs []string
	req := port.MobileBuildRequest{
		BuildID:    f.buildID,
		Platform:   platform,
		Identifier: f.identifier,
		Env:        map[string]string{"BUILD_NUMBER": "42", "MOBILE_RELEASE_UPLOAD": "true"},
		Log:        func(line string) { logs = append(logs, line) },
		Files: []port.PipelineFile{
			{Path: ".github/workflows/" + f.workflow, Body: "name: mobile-release\n", Mode: 0o644},
			{Path: "scripts/release.sh", Body: "#!/usr/bin/env bash\n", Mode: 0o755},
		},
		Workflow:    f.workflow,
		Ref:         "feature-sha",
		ArtifactDir: artifactDir,
		OnRunURL:    func(u string) { runURLs = append(runURLs, u) },
	}
	return req, &logs, &runURLs
}

func TestMobileTestBuilderDispatchesExactlyTheDeclaredInputs(t *testing.T) {
	cases := []struct {
		platform string
		want     map[string]any
	}{
		{platform: "ios", want: map[string]any{"channel": "stage", "build_number": "42", "ref": "feature-sha"}},
		{platform: "android", want: map[string]any{"channel": "stage", "build_number": "42", "ref": "feature-sha", "upload": "false"}},
	}
	for _, tc := range cases {
		t.Run(tc.platform, func(t *testing.T) {
			f := &fakeActions{
				t:           t,
				workflow:    "mobile-release-widget.yml",
				identifier:  "com.acme.widget",
				buildID:     uuid.New(),
				conclusion:  "success",
				artifactZip: buildZip(t, zipEntry{name: "app-release.aab", body: "aab"}),
			}
			f.start()
			req, _, runURLs := testBuildRequest(f, tc.platform, t.TempDir())
			req.Workflow = ".github/workflows/" + f.workflow

			res, err := f.builder().Run(context.Background(), req)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}

			f.mu.Lock()
			defer f.mu.Unlock()
			tc.want["build_id"] = f.buildID.String()
			if f.dispatch["ref"] != "main" {
				t.Fatalf("dispatch ref = %v, want the default branch", f.dispatch["ref"])
			}
			inputs, _ := f.dispatch["inputs"].(map[string]any)
			if len(inputs) != len(tc.want) {
				t.Fatalf("inputs = %v, want exactly %v", inputs, tc.want)
			}
			for k, v := range tc.want {
				if inputs[k] != v {
					t.Fatalf("input %s = %#v, want %#v (inputs %v)", k, inputs[k], v, inputs)
				}
			}
			if !f.committed || f.commitMessage != "chore(release): generate the "+tc.platform+" release pipeline for com.acme.widget" {
				t.Fatalf("commit = %v %q", f.committed, f.commitMessage)
			}
			if len(*runURLs) != 1 || (*runURLs)[0] != "https://github.com/acme/widget/actions/runs/99" {
				t.Fatalf("OnRunURL calls = %v", *runURLs)
			}
			if res.RunURL != "https://github.com/acme/widget/actions/runs/99" {
				t.Fatalf("RunURL = %q", res.RunURL)
			}
			if tc.platform == "ios" && (res.Artifact != "" || f.blobHits != 0) {
				t.Fatalf("ios downloaded an artifact: %q, blob hits %d", res.Artifact, f.blobHits)
			}
		})
	}
}

func TestMobileTestBuilderMakesNoCommitWhenThePipelineIsUnchanged(t *testing.T) {
	f := &fakeActions{
		t:          t,
		workflow:   "mobile-release-widget.yml",
		identifier: "com.acme.widget",
		buildID:    uuid.New(),
		conclusion: "success",
		sameTree:   true,
	}
	f.start()
	req, _, _ := testBuildRequest(f, "ios", "")
	if _, err := f.builder().Run(context.Background(), req); err != nil {
		t.Fatalf("Run: %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.committed {
		t.Fatalf("an unchanged pipeline produced a commit")
	}
	if !f.dispatched {
		t.Fatalf("the workflow was not dispatched")
	}
}

func TestFindRunByTitleFiltersByCreatedAndMatchesDisplayTitle(t *testing.T) {
	buildID := uuid.New()
	f := &fakeActions{t: t, workflow: "mobile-release-widget.yml", buildID: buildID, dispatched: true}
	f.start()
	since := time.Date(2026, 10, 8, 9, 59, 0, 0, time.FixedZone("x", 3*3600))

	run, found, err := findRunByTitleAt(context.Background(), f.api.URL, "tok", "acme", "widget", f.workflow, "tt-test-build "+buildID.String(), since)
	if err != nil {
		t.Fatalf("findRunByTitle: %v", err)
	}
	if !found || run.ID != 99 || run.Status != "queued" || run.HTMLURL != "https://github.com/acme/widget/actions/runs/99" {
		t.Fatalf("run = %+v found=%v, want run 99 — the newer run 98 carries another title", run, found)
	}
	f.mu.Lock()
	q := f.runsQueries[0]
	f.mu.Unlock()
	if q.Get("event") != "workflow_dispatch" || q.Get("per_page") != "50" {
		t.Fatalf("query = %v", q)
	}
	if q.Get("created") != ">=2026-10-08T06:59:00Z" {
		t.Fatalf("created = %q, want the since time in UTC", q.Get("created"))
	}

	_, found, err = findRunByTitleAt(context.Background(), f.api.URL, "tok", "acme", "widget", f.workflow, "tt-test-build nobody", since)
	if err != nil || found {
		t.Fatalf("unknown title: found=%v err=%v", found, err)
	}
}

func TestMobileTestBuilderWaitsForTheRunToAppear(t *testing.T) {
	f := &fakeActions{
		t:            t,
		workflow:     "mobile-release-widget.yml",
		identifier:   "com.acme.widget",
		buildID:      uuid.New(),
		conclusion:   "success",
		missingPolls: 3,
	}
	f.start()
	req, logs, _ := testBuildRequest(f, "ios", "")
	if _, err := f.builder().Run(context.Background(), req); err != nil {
		t.Fatalf("Run: %v", err)
	}
	f.mu.Lock()
	polls := len(f.runsQueries)
	f.mu.Unlock()
	if polls != 4 {
		t.Fatalf("runs listed %d times, want 4", polls)
	}
	joined := strings.Join(*logs, "\n")
	for _, want := range []string{"run 99: queued", "run 99: in_progress", "run 99: completed (success)"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("log lacks %q:\n%s", want, joined)
		}
	}
}

func TestMobileTestBuilderFailureReturnsTheFailedJobsLogTail(t *testing.T) {
	var log strings.Builder
	for i := 1; i <= 200; i++ {
		fmt.Fprintf(&log, "2026-10-08T10:00:00.1234567Z line %d\r\n", i)
	}
	f := &fakeActions{
		t:          t,
		workflow:   "mobile-release-widget.yml",
		identifier: "com.acme.widget",
		buildID:    uuid.New(),
		conclusion: "failure",
		jobLog:     log.String(),
	}
	f.start()
	req, _, runURLs := testBuildRequest(f, "android", t.TempDir())

	res, err := f.builder().Run(context.Background(), req)
	if err == nil {
		t.Fatalf("a failed run returned no error")
	}
	msg := err.Error()
	for _, want := range []string{"failure", "https://github.com/acme/widget/actions/runs/99", `"release"`, "\nline 121\n", "\nline 200"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error lacks %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "line 120\n") || strings.Contains(msg, "2026-10-08T10:00:00") || strings.Contains(msg, "\r") {
		t.Fatalf("error carries more than the last 80 bare lines:\n%s", msg)
	}
	if res.RunURL != "https://github.com/acme/widget/actions/runs/99" || res.Artifact != "" {
		t.Fatalf("result = %+v", res)
	}
	if len(*runURLs) != 1 {
		t.Fatalf("OnRunURL calls = %v", *runURLs)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.blobHits != 0 {
		t.Fatalf("a failed run's artifact was downloaded")
	}
}

func TestMobileTestBuilderAndroidSuccessUnpacksTheLargestAAB(t *testing.T) {
	f := &fakeActions{
		t:          t,
		workflow:   "mobile-release-widget.yml",
		identifier: "com.acme.widget",
		buildID:    uuid.New(),
		conclusion: "success",
		artifactZip: buildZip(t,
			zipEntry{name: "build/outputs/bundle/release/app-release.aab", body: strings.Repeat("A", 4096)},
			zipEntry{name: "build/outputs/bundle/debug/app-debug.aab", body: "small"},
			zipEntry{name: "mapping.txt", body: "map"},
		),
	}
	f.start()
	dir := t.TempDir()
	req, _, _ := testBuildRequest(f, "android", dir)

	res, err := f.builder().Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := filepath.Join(dir, "build", "outputs", "bundle", "release", "app-release.aab")
	if res.Artifact != want {
		t.Fatalf("Artifact = %q, want %q", res.Artifact, want)
	}
	data, err := os.ReadFile(res.Artifact)
	if err != nil || len(data) != 4096 {
		t.Fatalf("artifact content: %d bytes, %v", len(data), err)
	}
	if _, err := os.Stat(filepath.Join(dir, "mapping.txt")); err != nil {
		t.Fatalf("mapping.txt not unpacked: %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.blobHits != 1 || f.blobAuth[0] != "" {
		t.Fatalf("blob saw %d requests, Authorization %q", f.blobHits, f.blobAuth)
	}
}

// Both servers listen on 127.0.0.1, so net/http's own rule — which compares
// host names without the port — would forward the token. keepAuthOnOrigin must
// not.
func TestDownloadRunArtifactDoesNotForwardAuthorizationToAnotherOrigin(t *testing.T) {
	f := &fakeActions{
		t:           t,
		workflow:    "mobile-release-widget.yml",
		identifier:  "com.acme.widget",
		buildID:     uuid.New(),
		artifactZip: buildZip(t, zipEntry{name: "app.aab", body: "aab"}),
	}
	f.start()
	files, err := downloadRunArtifactAt(context.Background(), f.api.URL, "secret-token", "acme", "widget", 99,
		"mobile-release-com.acme.widget-"+f.buildID.String(), t.TempDir())
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("files = %v", files)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.blobAuth) != 1 || f.blobAuth[0] != "" {
		t.Fatalf("blob host received Authorization %q", f.blobAuth)
	}
	for _, a := range f.apiAuth {
		if a != "Bearer secret-token" {
			t.Fatalf("API host received Authorization %q", a)
		}
	}
}

func TestKeepAuthOnOriginDropsTheHeaderOffOrigin(t *testing.T) {
	origin, _ := http.NewRequest(http.MethodGet, "https://api.github.com/repos/a/b/actions/artifacts/1/zip", nil)
	cases := []struct {
		target string
		keep   bool
	}{
		{"https://api.github.com/other", true},
		{"https://API.github.com/other", true},
		{"https://api.github.com:8443/other", false},
		{"http://api.github.com/other", false},
		{"https://blob.api.github.com/x", false},
		{"https://productionresultssa1.blob.core.windows.net/x?sig=1", false},
	}
	for _, tc := range cases {
		next, _ := http.NewRequest(http.MethodGet, tc.target, nil)
		next.Header.Set("Authorization", "Bearer tok")
		if err := keepAuthOnOrigin(next, []*http.Request{origin}); err != nil {
			t.Fatalf("%s: %v", tc.target, err)
		}
		if got := next.Header.Get("Authorization") != ""; got != tc.keep {
			t.Fatalf("%s: kept=%v, want %v", tc.target, got, tc.keep)
		}
	}
}

func TestExtractZipRejectsEntriesThatEscapeAndSkipsSymlinks(t *testing.T) {
	cases := []struct {
		name    string
		entries []zipEntry
		wantErr string
	}{
		{name: "parent", entries: []zipEntry{{name: "ok.txt", body: "x"}, {name: "../evil.txt", body: "x"}}, wantErr: "escapes"},
		{name: "nested parent", entries: []zipEntry{{name: "a/../../evil.txt", body: "x"}}, wantErr: "escapes"},
		{name: "backslash parent", entries: []zipEntry{{name: `..\evil.txt`, body: "x"}}, wantErr: "escapes"},
		{name: "absolute", entries: []zipEntry{{name: "/tmp/evil.txt", body: "x"}}, wantErr: "absolute"},
		{name: "drive", entries: []zipEntry{{name: "C:/evil.txt", body: "x"}}, wantErr: "absolute"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parent := t.TempDir()
			dest := filepath.Join(parent, "out")
			zipPath := filepath.Join(parent, "a.zip")
			if err := os.WriteFile(zipPath, buildZip(t, tc.entries...), 0o644); err != nil {
				t.Fatal(err)
			}
			files, err := extractZip(zipPath, dest)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want one mentioning %q (files %v)", err, tc.wantErr, files)
			}
			if _, err := os.Stat(filepath.Join(parent, "evil.txt")); !os.IsNotExist(err) {
				t.Fatalf("an escaping entry was written: %v", err)
			}
			if _, err := os.Stat(filepath.Join(dest, "ok.txt")); !os.IsNotExist(err) {
				t.Fatalf("entries were written before the archive was rejected: %v", err)
			}
		})
	}

	t.Run("symlink", func(t *testing.T) {
		parent := t.TempDir()
		dest := filepath.Join(parent, "out")
		zipPath := filepath.Join(parent, "a.zip")
		data := buildZip(t,
			zipEntry{name: "link", body: "/etc/passwd", symlink: true},
			zipEntry{name: "dir/", body: ""},
			zipEntry{name: "dir/app.aab", body: "aab"},
		)
		if err := os.WriteFile(zipPath, data, 0o644); err != nil {
			t.Fatal(err)
		}
		files, err := extractZip(zipPath, dest)
		if err != nil {
			t.Fatalf("extract: %v", err)
		}
		if len(files) != 1 || files[0] != filepath.Join(dest, "dir", "app.aab") {
			t.Fatalf("files = %v", files)
		}
		if _, err := os.Lstat(filepath.Join(dest, "link")); !os.IsNotExist(err) {
			t.Fatalf("the symlink entry was materialised: %v", err)
		}
	})
}

func TestDownloadRunArtifactThroughAndroidRunRejectsZipSlip(t *testing.T) {
	f := &fakeActions{
		t:           t,
		workflow:    "mobile-release-widget.yml",
		identifier:  "com.acme.widget",
		buildID:     uuid.New(),
		conclusion:  "success",
		artifactZip: buildZip(t, zipEntry{name: "app.aab", body: "aab"}, zipEntry{name: "../../escape.aab", body: "evil"}),
	}
	f.start()
	parent := t.TempDir()
	dir := filepath.Join(parent, "a", "b")
	req, _, _ := testBuildRequest(f, "android", dir)

	res, err := f.builder().Run(context.Background(), req)
	if err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("err = %v, want a zip-slip rejection", err)
	}
	if res.Artifact != "" {
		t.Fatalf("Artifact = %q after a rejected archive", res.Artifact)
	}
	if _, err := os.Stat(filepath.Join(parent, "escape.aab")); !os.IsNotExist(err) {
		t.Fatalf("the escaping entry was written: %v", err)
	}
}

func TestDispatchWorkflowInputsPostsRefAndInputs(t *testing.T) {
	var gotPath string
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	err := dispatchWorkflowInputsAt(context.Background(), srv.URL, "tok", "o", "r", "release.yml", "main", map[string]string{"channel": "stage"})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if gotPath != "/repos/o/r/actions/workflows/release.yml/dispatches" {
		t.Fatalf("path = %q", gotPath)
	}
	inputs, _ := got["inputs"].(map[string]any)
	if got["ref"] != "main" || len(got) != 2 || len(inputs) != 1 || inputs["channel"] != "stage" {
		t.Fatalf("body = %v", got)
	}
}
