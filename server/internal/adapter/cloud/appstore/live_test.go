package appstore

import (
	"context"
	"net/http"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func versionsServer(t *testing.T, body string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/apps/app123/appStoreVersions" {
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
			return
		}
		if got := r.URL.Query().Get("filter[platform]"); got != "IOS" {
			t.Errorf("filter[platform] = %q, want IOS — a universal app record also lists macOS and visionOS versions", got)
		}
		_, _ = w.Write([]byte(body))
	}
}

// The bug this guards: a live app whose next version was drafted read as "not
// published", because only the newest version's state was checked.
func TestLiveVersionFindsTheVersionOnSaleBehindANewerDraft(t *testing.T) {
	c, _ := newTestClient(t, versionsServer(t, `{"data":[
		{"id":"v2","attributes":{"versionString":"2.1.0","appStoreState":"PREPARE_FOR_SUBMISSION","platform":"IOS"}},
		{"id":"v1","attributes":{"versionString":"2.0.0","appStoreState":"READY_FOR_SALE","platform":"IOS"}}
	]}`))

	info, found, err := c.LiveVersion(context.Background(), "app123")
	if err != nil {
		t.Fatalf("LiveVersion: %v", err)
	}
	if !found || info.Version != "2.0.0" {
		t.Fatalf("LiveVersion = %+v found=%v, want 2.0.0 on sale", info, found)
	}
}

func TestLiveVersionReadsTheNewAppVersionStateField(t *testing.T) {
	c, _ := newTestClient(t, versionsServer(t, `{"data":[
		{"id":"v1","attributes":{"versionString":"1.4.0","appVersionState":"READY_FOR_DISTRIBUTION","platform":"IOS"}}
	]}`))

	info, found, err := c.LiveVersion(context.Background(), "app123")
	if err != nil {
		t.Fatalf("LiveVersion: %v", err)
	}
	if !found || info.Version != "1.4.0" || info.State != stateReadyForSale {
		t.Fatalf("LiveVersion = %+v found=%v, want 1.4.0 read as READY_FOR_SALE", info, found)
	}
}

func TestLiveVersionIgnoresOtherPlatforms(t *testing.T) {
	c, _ := newTestClient(t, versionsServer(t, `{"data":[
		{"id":"v1","attributes":{"versionString":"5.0.0","appStoreState":"READY_FOR_SALE","platform":"MAC_OS"}},
		{"id":"v2","attributes":{"versionString":"1.0.0","appStoreState":"PREPARE_FOR_SUBMISSION","platform":"IOS"}}
	]}`))

	_, found, err := c.LiveVersion(context.Background(), "app123")
	if err != nil {
		t.Fatalf("LiveVersion: %v", err)
	}
	if found {
		t.Fatal("found = true, want false — only the macOS app is on sale")
	}
}

// An approved version waiting for its release used to outrank the one on sale,
// turning a live production channel into "draft".
func TestTracksProductionShowsTheLiveVersionAndQueuesTheApprovedOne(t *testing.T) {
	f := &tracksFixture{
		betaGroups: `{"data":[]}`,
		appStoreVersions: `{"data":[
			{"id":"VER2","attributes":{"versionString":"1.5.0","appStoreState":"PENDING_DEVELOPER_RELEASE"}},
			{"id":"VER1","attributes":{"versionString":"1.4.0","appStoreState":"READY_FOR_SALE"}},
			{"id":"VER0","attributes":{"versionString":"1.3.0","appStoreState":"REPLACED_WITH_NEW_VERSION"}}
		]}`,
		versionBuild: `{"data":{"id":"B9","attributes":{"version":"412"}}}`,
	}
	c, _ := newTestClient(t, f.handler(t))

	tracks, err := c.Tracks(context.Background(), "APP1")
	if err != nil {
		t.Fatalf("Tracks: %v", err)
	}
	got := tracks.Production
	if got.Version != "1.4.0" || got.Status != domain.TrackStatusLive {
		t.Errorf("Production = %s/%s, want 1.4.0/live", got.Version, got.Status)
	}
	if got.PendingVersion != "1.5.0" || got.PendingStatus != domain.TrackStatusDraft {
		t.Errorf("Pending = %s/%s, want 1.5.0/draft", got.PendingVersion, got.PendingStatus)
	}
}

// Only one version is ever on sale at a time, so seeing it among a truncated
// set of included versions is enough to call the app live.
func TestListAppsReportsLiveEvenWhenIncludedVersionsAreTruncated(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[
			{"id":"A1","attributes":{"name":"First","bundleId":"com.example.first"},
			 "relationships":{"appStoreVersions":{
				"data":[{"type":"appStoreVersions","id":"V1"},{"type":"appStoreVersions","id":"V2"}],
				"meta":{"paging":{"total":140,"limit":50}}}}}
		],"included":[
			{"type":"appStoreVersions","id":"V1","attributes":{"versionString":"1.1.0","appStoreState":"READY_FOR_SALE"}},
			{"type":"appStoreVersions","id":"V2","attributes":{"versionString":"1.2.0","appStoreState":"PREPARE_FOR_SUBMISSION"}}
		]}`))
	})

	apps, err := c.ListApps(context.Background())
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	if len(apps) != 1 || apps[0].State != stateReadyForSale {
		t.Fatalf("apps = %+v, want the app reported on sale", apps)
	}
}
