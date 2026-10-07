package googleplay

import (
	"context"
	"net/http"
	"testing"
)

func productionTrackServer(t *testing.T, track string, releases string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/androidpublisher/v3/applications/com.example.app/edits":
			_, _ = w.Write([]byte(`{"id":"e1"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/androidpublisher/v3/applications/com.example.app/edits/e1/tracks/"+track:
			_, _ = w.Write([]byte(`{"track":"` + track + `","releases":` + releases + `}`))
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected api call %s %s", r.Method, r.URL.Path)
		}
	}
}

func TestLiveVersionIsFalseForADraftOnlyProductionTrack(t *testing.T) {
	c, _ := newTestClient(t, tokenHandlerOK(t), productionTrackServer(t, "production",
		`[{"name":"1.0.0","status":"draft","versionCodes":["7"]}]`))

	_, live, err := c.LiveVersion(context.Background(), "com.example.app")
	if err != nil {
		t.Fatalf("LiveVersion: %v", err)
	}
	if live {
		t.Fatal("live = true, want false — a draft has reached nobody")
	}
}

func TestLiveVersionPicksTheNewestPublishedRelease(t *testing.T) {
	c, _ := newTestClient(t, tokenHandlerOK(t), productionTrackServer(t, "production", `[
		{"name":"1.0.0","status":"completed","versionCodes":["7"]},
		{"name":"1.1.0","status":"inProgress","userFraction":0.2,"versionCodes":["9"]},
		{"name":"1.2.0","status":"draft","versionCodes":["11"]}
	]`))

	version, live, err := c.LiveVersion(context.Background(), "com.example.app")
	if err != nil {
		t.Fatalf("LiveVersion: %v", err)
	}
	if !live || version != "1.1.0" {
		t.Fatalf("LiveVersion = %q live=%v, want 1.1.0 (rolling out) — the draft has not shipped", version, live)
	}
}

// A staged rollout keeps the old completed release next to the new one; the
// halted-rollout incident must see the new one wherever Play lists it.
func TestTrackInfoReadsTheNewestVersionCode(t *testing.T) {
	c, _ := newTestClient(t, tokenHandlerOK(t), productionTrackServer(t, "production", `[
		{"name":"1.0.0","status":"completed","versionCodes":["7"]},
		{"name":"1.1.0","status":"halted","userFraction":0.1,"versionCodes":["9"]}
	]`))

	info, err := c.TrackInfo(context.Background(), "com.example.app", "production")
	if err != nil {
		t.Fatalf("TrackInfo: %v", err)
	}
	if info.VersionName != "1.1.0" || info.Status != "halted" {
		t.Fatalf("TrackInfo = %+v, want the halted 1.1.0 release", info)
	}
}
