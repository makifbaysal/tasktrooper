package googleplay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

const draftAppRefusal = `{"error":{"code":400,"message":"Only releases with status draft may be created on draft app.","status":"INVALID_ARGUMENT"}}`

func writeAAB(t *testing.T, content []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "app-release.aab")
	if err := os.WriteFile(p, content, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return p
}

func checkMediaUpload(t *testing.T, r *http.Request, want []byte) {
	t.Helper()
	if got := r.URL.Query().Get("uploadType"); got != "media" {
		t.Errorf("uploadType = %q, want media", got)
	}
	if got := r.Header.Get("Content-Type"); got != "application/octet-stream" {
		t.Errorf("Content-Type = %q, want application/octet-stream", got)
	}
	if got := r.Header.Get("Authorization"); got != "Bearer fake-access-token" {
		t.Errorf("Authorization = %q, want the publisher bearer token", got)
	}
	if r.ContentLength != int64(len(want)) {
		t.Errorf("Content-Length = %d, want %d", r.ContentLength, len(want))
	}
	body, _ := io.ReadAll(r.Body)
	if !bytes.Equal(body, want) {
		t.Errorf("uploaded body = %q, want the file's bytes %q", body, want)
	}
}

func TestLatestVersionCodeIsTheHighestAcrossBundlesAPKsAndTracks(t *testing.T) {
	cases := []struct {
		name                  string
		bundles, apks, tracks string
		want                  int64
	}{
		{
			name:    "bundle",
			bundles: `{"bundles":[{"versionCode":41,"sha256":"a"},{"versionCode":77,"sha256":"b"}]}`,
			apks:    `{"apks":[{"versionCode":12}]}`,
			tracks:  `{"tracks":[{"track":"internal","releases":[{"status":"completed","versionCodes":["50"]}]}]}`,
			want:    77,
		},
		{
			name:    "apk",
			bundles: `{"bundles":[{"versionCode":41}]}`,
			apks:    `{"apks":[{"versionCode":90},{"versionCode":12}]}`,
			tracks:  `{"tracks":[{"track":"internal","releases":[{"status":"completed","versionCodes":["50"]}]}]}`,
			want:    90,
		},
		{
			name:    "track",
			bundles: `{"bundles":[{"versionCode":41}]}`,
			apks:    `{}`,
			tracks:  `{"tracks":[{"track":"production","releases":[{"status":"completed","versionCodes":["garbage","120"]}]},{"track":"beta"}]}`,
			want:    120,
		},
		{name: "nothing uploaded", bundles: `{}`, apks: `{}`, tracks: `{}`, want: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var calls []string
			const edit = "/androidpublisher/v3/applications/com.x/edits/E1"
			c, _ := newTestClient(t, tokenHandlerOK(t), func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				calls = append(calls, r.Method+" "+r.URL.Path)
				mu.Unlock()
				switch r.Method + " " + r.URL.Path {
				case "POST /androidpublisher/v3/applications/com.x/edits":
					_, _ = w.Write([]byte(`{"id":"E1"}`))
				case "GET " + edit + "/bundles":
					_, _ = w.Write([]byte(tc.bundles))
				case "GET " + edit + "/apks":
					_, _ = w.Write([]byte(tc.apks))
				case "GET " + edit + "/tracks":
					_, _ = w.Write([]byte(tc.tracks))
				case "DELETE " + edit:
					w.WriteHeader(http.StatusOK)
				default:
					t.Errorf("unexpected api call %s %s", r.Method, r.URL.Path)
				}
			})

			got, err := c.LatestVersionCode(context.Background(), "com.x")
			if err != nil {
				t.Fatalf("LatestVersionCode: %v", err)
			}
			if got != tc.want {
				t.Errorf("LatestVersionCode = %d, want %d", got, tc.want)
			}
			mu.Lock()
			defer mu.Unlock()
			if strings.Count(strings.Join(calls, "\n"), "POST /androidpublisher/v3/applications/com.x/edits") != 1 {
				t.Errorf("want exactly one edit opened:\n%s", strings.Join(calls, "\n"))
			}
			if !slices.Contains(calls, "DELETE "+edit) {
				t.Errorf("read-only edit was not discarded:\n%s", strings.Join(calls, "\n"))
			}
		})
	}
}

func TestUploadInternalSharingStreamsTheBundle(t *testing.T) {
	content := []byte("PK\x03\x04 not really a bundle")
	c, _ := newTestClient(t, tokenHandlerOK(t), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/upload/androidpublisher/v3/applications/com.x/internalappsharing/bundle" {
			t.Errorf("unexpected api call %s %s", r.Method, r.URL.Path)
			return
		}
		checkMediaUpload(t, r, content)
		_, _ = w.Write([]byte(`{"downloadUrl":"https://play.google.com/apps/test/com.x/42","certificateFingerprint":"AB:CD","sha256":"deadbeef"}`))
	})

	link, err := c.UploadInternalSharing(context.Background(), "com.x", writeAAB(t, content))
	if err != nil {
		t.Fatalf("UploadInternalSharing: %v", err)
	}
	if link.DownloadURL != "https://play.google.com/apps/test/com.x/42" || link.CertificateFingerprint != "AB:CD" || link.SHA256 != "deadbeef" {
		t.Errorf("link = %+v", link)
	}
}

func TestUploadInternalSharingSurfacesPlayErrors(t *testing.T) {
	c, _ := newTestClient(t, tokenHandlerOK(t), func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":403,"message":"Internal app sharing is not enabled"}}`))
	})

	_, err := c.UploadInternalSharing(context.Background(), "com.x", writeAAB(t, []byte("aab")))
	if err == nil || !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "not enabled") {
		t.Fatalf("err = %v, want the status and Play's message", err)
	}
}

// The upload client is a copy: lengthening its timeout must not lengthen the
// one every JSON call runs on.
func TestMediaUploadsDoNotRunOnTheJSONTimeout(t *testing.T) {
	c, _ := newTestClient(t, tokenHandlerOK(t), func(http.ResponseWriter, *http.Request) {})
	before := c.httpClient.Timeout
	if got := c.mediaHTTPClient().Timeout; got != mediaUploadTimeout {
		t.Errorf("media upload timeout = %v, want %v", got, mediaUploadTimeout)
	}
	if c.httpClient.Timeout != before {
		t.Errorf("JSON client timeout changed to %v", c.httpClient.Timeout)
	}
}

func TestListTracksKeepsPlayOrderAndParsesVersionCodes(t *testing.T) {
	var calls []string
	c, _ := newTestClient(t, tokenHandlerOK(t), tracksHandler(t, "E1", `{"tracks":[
		{"track":"production","releases":[{"name":"1.3.0","status":"inProgress","userFraction":0.25,"versionCodes":["30"]},{"name":"1.2.0","status":"completed","versionCodes":["29"]}]},
		{"track":"internal","releases":[{"name":"1.5.0","status":"completed","versionCodes":["50","bogus","51"]}]},
		{"track":"qa-team"}
	]}`, &calls))

	tracks, err := c.ListTracks(context.Background(), "com.x")
	if err != nil {
		t.Fatalf("ListTracks: %v", err)
	}
	var names []string
	for _, tr := range tracks {
		names = append(names, tr.Name)
	}
	if want := []string{"production", "internal", "qa-team"}; !slices.Equal(names, want) {
		t.Fatalf("track order = %v, want %v", names, want)
	}
	prod := tracks[0].Releases
	if len(prod) != 2 || prod[0].Name != "1.3.0" || prod[0].Status != "inProgress" || prod[0].UserFraction != 0.25 || !slices.Equal(prod[0].VersionCodes, []int64{30}) {
		t.Errorf("production releases = %+v", prod)
	}
	if got := tracks[1].Releases[0].VersionCodes; !slices.Equal(got, []int64{50, 51}) {
		t.Errorf("internal versionCodes = %v, want [50 51] with the unparsable one skipped", got)
	}
	if len(tracks[2].Releases) != 0 {
		t.Errorf("empty track releases = %+v", tracks[2].Releases)
	}
	if !slices.Contains(calls, "DELETE /androidpublisher/v3/applications/com.x/edits/E1") {
		t.Errorf("read-only edit was not discarded:\n%s", strings.Join(calls, "\n"))
	}
}

// releaseStub is a Play stub for the edit ReleaseToTrack runs against the
// alpha track. Each edits.insert opens the next edit, E1, E2, …; failPut and
// failCommit answer that edit's track update or commit with a 400 carrying the
// given body.
type releaseStub struct {
	t           *testing.T
	bundlesJSON string
	uploadVC    int64
	aab         []byte
	failPut     map[string]string
	failCommit  map[string]string

	mu      sync.Mutex
	edits   int
	calls   []string
	uploads int
	puts    []map[string]any
}

func (s *releaseStub) handler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, r.Method+" "+r.URL.Path)
	const app = "/androidpublisher/v3/applications/com.x"
	id := "E" + strconv.Itoa(s.edits)
	edit := app + "/edits/" + id
	switch r.Method + " " + r.URL.Path {
	case "POST " + app + "/edits":
		s.edits++
		_, _ = fmt.Fprintf(w, `{"id":"E%d"}`, s.edits)
	case "GET " + edit + "/bundles":
		_, _ = w.Write([]byte(s.bundlesJSON))
	case "POST /upload" + edit + "/bundles":
		checkMediaUpload(s.t, r, s.aab)
		s.uploads++
		_, _ = fmt.Fprintf(w, `{"versionCode":%d,"sha1":"x","sha256":"y"}`, s.uploadVC)
	case "PUT " + edit + "/tracks/alpha":
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			s.t.Errorf("decoding track update: %v", err)
		}
		s.puts = append(s.puts, body)
		if msg, ok := s.failPut[id]; ok {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(msg))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	case "POST " + edit + ":commit":
		if msg, ok := s.failCommit[id]; ok {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(msg))
			return
		}
		_, _ = w.Write([]byte(`{"id":"` + id + `"}`))
	case "DELETE " + edit:
		w.WriteHeader(http.StatusOK)
	default:
		s.t.Errorf("unexpected api call %s %s", r.Method, r.URL.Path)
	}
}

func (s *releaseStub) client(t *testing.T) *Client {
	t.Helper()
	c, _ := newTestClient(t, tokenHandlerOK(t), s.handler)
	return c
}

func (s *releaseStub) called(call string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Contains(s.calls, call)
}

func (s *releaseStub) log() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Join(s.calls, "\n")
}

const (
	commitE1 = "POST /androidpublisher/v3/applications/com.x/edits/E1:commit"
	commitE2 = "POST /androidpublisher/v3/applications/com.x/edits/E2:commit"
	deleteE1 = "DELETE /androidpublisher/v3/applications/com.x/edits/E1"
)

func TestReleaseToTrackUploadsABundlePlayDoesNotHold(t *testing.T) {
	s := &releaseStub{t: t, bundlesJSON: `{"bundles":[{"versionCode":41}]}`, uploadVC: 42, aab: []byte("aab-42")}
	c := s.client(t)

	if err := c.ReleaseToTrack(context.Background(), "com.x", "alpha", writeAAB(t, s.aab), 42, "1.5.0 (42)", "Fixes login"); err != nil {
		t.Fatalf("ReleaseToTrack: %v", err)
	}
	if s.uploads != 1 {
		t.Fatalf("uploads = %d, want 1:\n%s", s.uploads, s.log())
	}
	if len(s.puts) != 1 {
		t.Fatalf("track updates = %d, want 1", len(s.puts))
	}
	got, _ := json.Marshal(s.puts[0])
	want := `{"releases":[{"name":"1.5.0 (42)","releaseNotes":[{"language":"en-US","text":"Fixes login"}],"status":"completed","versionCodes":["42"]}],"track":"alpha"}`
	if string(got) != want {
		t.Errorf("track update =\n%s\nwant\n%s", got, want)
	}
	if !s.called(commitE1) || s.called(deleteE1) {
		t.Errorf("edit was not committed:\n%s", s.log())
	}
}

func TestReleaseToTrackSkipsTheUploadWhenPlayHoldsTheBundle(t *testing.T) {
	s := &releaseStub{t: t, bundlesJSON: `{"bundles":[{"versionCode":41},{"versionCode":42}]}`}
	c := s.client(t)

	if err := c.ReleaseToTrack(context.Background(), "com.x", "alpha", "", 42, "1.5.0 (42)", ""); err != nil {
		t.Fatalf("ReleaseToTrack: %v", err)
	}
	if s.uploads != 0 {
		t.Errorf("uploaded a bundle Play already holds:\n%s", s.log())
	}
	if !s.called(commitE1) {
		t.Errorf("edit was not committed:\n%s", s.log())
	}
}

func TestReleaseToTrackRejectsABundleWithTheWrongVersionCode(t *testing.T) {
	s := &releaseStub{t: t, bundlesJSON: `{}`, uploadVC: 43, aab: []byte("aab-43")}
	c := s.client(t)

	err := c.ReleaseToTrack(context.Background(), "com.x", "alpha", writeAAB(t, s.aab), 42, "1.5.0 (42)", "")
	if err == nil || !strings.Contains(err.Error(), "43") || !strings.Contains(err.Error(), "42") {
		t.Fatalf("err = %v, want a versionCode mismatch naming both codes", err)
	}
	if len(s.puts) != 0 || s.called(commitE1) {
		t.Errorf("mismatched bundle reached a track or a commit:\n%s", s.log())
	}
	if !s.called(deleteE1) {
		t.Errorf("edit holding the stray bundle was not discarded:\n%s", s.log())
	}
}

func TestReleaseToTrackWithoutALocalCopyFails(t *testing.T) {
	s := &releaseStub{t: t, bundlesJSON: `{"bundles":[{"versionCode":41}]}`}
	c := s.client(t)

	err := c.ReleaseToTrack(context.Background(), "com.x", "alpha", "", 42, "1.5.0 (42)", "")
	if err == nil || !strings.Contains(err.Error(), "no local copy") {
		t.Fatalf("err = %v, want a not-on-Play, no-local-copy failure", err)
	}
	if s.called(commitE1) || !s.called(deleteE1) {
		t.Errorf("edit was not discarded:\n%s", s.log())
	}
}

func TestReleaseToTrackRetriesADraftAppAsADraft(t *testing.T) {
	for _, at := range []string{"track update", "commit"} {
		t.Run(at, func(t *testing.T) {
			s := &releaseStub{t: t, bundlesJSON: `{"bundles":[{"versionCode":42}]}`}
			if at == "commit" {
				s.failCommit = map[string]string{"E1": draftAppRefusal}
			} else {
				s.failPut = map[string]string{"E1": draftAppRefusal}
			}
			c := s.client(t)

			if err := c.ReleaseToTrack(context.Background(), "com.x", "alpha", "", 42, "1.0.0 (42)", ""); err != nil {
				t.Fatalf("ReleaseToTrack: %v", err)
			}
			if len(s.puts) != 2 {
				t.Fatalf("track updates = %d, want 2:\n%s", len(s.puts), s.log())
			}
			if got := firstRelease(t, s.puts[0])["status"]; got != "completed" {
				t.Errorf("first attempt status = %v, want completed", got)
			}
			if got := firstRelease(t, s.puts[1])["status"]; got != "draft" {
				t.Errorf("retry status = %v, want draft", got)
			}
			if !s.called(commitE2) {
				t.Errorf("draft retry was not committed:\n%s", s.log())
			}
		})
	}
}

func TestReleaseToTrackRetriesAsDraftOnlyOnce(t *testing.T) {
	s := &releaseStub{t: t, bundlesJSON: `{"bundles":[{"versionCode":42}]}`, failCommit: map[string]string{"E1": draftAppRefusal, "E2": draftAppRefusal}}
	c := s.client(t)

	if err := c.ReleaseToTrack(context.Background(), "com.x", "alpha", "", 42, "1.0.0 (42)", ""); err == nil {
		t.Fatal("err = nil, want the second refusal")
	}
	if s.edits != 2 {
		t.Errorf("edits opened = %d, want 2", s.edits)
	}
}

func TestReleaseToTrackDoesNotRetryOtherRefusals(t *testing.T) {
	s := &releaseStub{t: t, bundlesJSON: `{"bundles":[{"versionCode":42}]}`, failCommit: map[string]string{"E1": `{"error":{"code":400,"message":"Version code 42 has already been used."}}`}}
	c := s.client(t)

	err := c.ReleaseToTrack(context.Background(), "com.x", "alpha", "", 42, "1.0.0 (42)", "")
	if err == nil || !strings.Contains(err.Error(), "already been used") {
		t.Fatalf("err = %v, want Play's refusal", err)
	}
	if s.edits != 1 {
		t.Errorf("edits opened = %d, want 1 — only the draft-app refusal earns a retry", s.edits)
	}
}

func TestReleaseToTrackReleaseNotes(t *testing.T) {
	t.Run("empty notes are omitted", func(t *testing.T) {
		s := &releaseStub{t: t, bundlesJSON: `{"bundles":[{"versionCode":42}]}`}
		if err := s.client(t).ReleaseToTrack(context.Background(), "com.x", "alpha", "", 42, "1.0.0 (42)", ""); err != nil {
			t.Fatalf("ReleaseToTrack: %v", err)
		}
		if _, present := firstRelease(t, s.puts[0])["releaseNotes"]; present {
			t.Errorf("release carried empty release notes: %v", s.puts[0])
		}
	})
	t.Run("long notes are cut to Play's 500 characters", func(t *testing.T) {
		s := &releaseStub{t: t, bundlesJSON: `{"bundles":[{"versionCode":42}]}`}
		if err := s.client(t).ReleaseToTrack(context.Background(), "com.x", "alpha", "", 42, "1.0.0 (42)", strings.Repeat("ğ", 600)); err != nil {
			t.Fatalf("ReleaseToTrack: %v", err)
		}
		notes, _ := firstRelease(t, s.puts[0])["releaseNotes"].([]any)
		if len(notes) != 1 {
			t.Fatalf("releaseNotes = %v", notes)
		}
		text, _ := notes[0].(map[string]any)["text"].(string)
		if n := utf8.RuneCountInString(text); n != 500 || !utf8.ValidString(text) {
			t.Errorf("notes text is %d runes (valid UTF-8: %v), want 500", n, utf8.ValidString(text))
		}
	})
}
