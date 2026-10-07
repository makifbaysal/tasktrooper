package appstore

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// appsPager serves /v1/apps as a cursor-linked sequence of pages, with
// links.next pointing at a host that is NOT the test server — the same shape
// ASC returns, and the same trap: a client that followed the link's host would
// never reach this handler (and would hand a live ASC token to that host).
type appsPager struct {
	pages   []string
	cursors []string
	queries []string
}

func (p *appsPager) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/apps" {
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
			return
		}
		cursor := r.URL.Query().Get("cursor")
		p.cursors = append(p.cursors, cursor)
		p.queries = append(p.queries, r.URL.RawQuery)
		idx := 0
		if cursor != "" {
			n, err := strconv.Atoi(cursor)
			if err != nil {
				t.Errorf("cursor = %q, want an index the fixture handed out", cursor)
				return
			}
			idx = n
		}
		if idx >= len(p.pages) {
			t.Errorf("cursor %q is past the last fixture page", cursor)
			return
		}
		links := "{}"
		if idx+1 < len(p.pages) {
			links = fmt.Sprintf(`{"next":"https://appstore.invalid/v1/apps?cursor=%d"}`, idx+1)
		}
		_, _ = fmt.Fprintf(w, `{"data":%s,"links":%s}`, p.pages[idx], links)
	}
}

// An account's app list outgrows one ASC page, and the picker this feeds binds
// a repository to an app: a truncated list silently hides apps a person is
// looking for, so the walk must follow the cursor to the end.
func TestListAppsFollowsPagingCursor(t *testing.T) {
	pager := &appsPager{pages: []string{
		`[{"id":"A1","attributes":{"name":"First","bundleId":"com.example.first"}}]`,
		`[{"id":"A2","attributes":{"name":"Second","bundleId":"com.example.second"}}]`,
	}}
	c, _ := newTestClient(t, pager.handler(t))

	apps, err := c.ListApps(context.Background())
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	want := []port.StoreAppRef{
		{StoreAppID: "A1", Identifier: "com.example.first", Name: "First"},
		{StoreAppID: "A2", Identifier: "com.example.second", Name: "Second"},
	}
	if !reflect.DeepEqual(apps, want) {
		t.Errorf("apps = %+v, want %+v", apps, want)
	}
	if !reflect.DeepEqual(pager.cursors, []string{"", "1"}) {
		t.Errorf("cursors requested = %v, want [\"\" \"1\"]", pager.cursors)
	}
	if !strings.Contains(pager.queries[0], "include=appStoreVersions") {
		t.Errorf("first query = %q, want it to include appStoreVersions", pager.queries[0])
	}
	if !strings.Contains(pager.queries[0], "limit[appStoreVersions]=50") {
		t.Errorf("first query = %q, want an explicit limit[appStoreVersions] — left unset, ASC truncates the relationship on its own terms", pager.queries[0])
	}
}

// tracksFixture answers the endpoints Tracks walks. Any route left empty is
// reported as an unexpected call, so a test only wires up what it is about.
type tracksFixture struct {
	betaGroups        string
	builds            string
	preReleaseVersion string
	betaReview        string
	betaTesters       string
	appStoreVersions  string
	phasedRelease     string
	versionBuild      string

	buildsQuery map[string]string
}

func (f *tracksFixture) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		body := ""
		optional := false
		switch {
		case r.URL.Path == "/v1/apps/APP1/betaGroups":
			body = f.betaGroups
		case r.URL.Path == "/v1/builds":
			f.buildsQuery = map[string]string{}
			for k, v := range r.URL.Query() {
				f.buildsQuery[k] = v[0]
			}
			body = f.builds
		case r.URL.Path == "/v1/builds/B1/preReleaseVersion":
			body = f.preReleaseVersion
		case r.URL.Path == "/v1/builds/B1/betaAppReviewSubmission":
			body = f.betaReview
		case r.URL.Path == "/v1/betaTesters":
			body = f.betaTesters
		case r.URL.Path == "/v1/apps/APP1/appStoreVersions":
			body = f.appStoreVersions
		case r.URL.Path == "/v1/appStoreVersions/VER1/appStoreVersionPhasedRelease":
			body, optional = f.phasedRelease, true
		case r.URL.Path == "/v1/appStoreVersions/VER1/build":
			body, optional = f.versionBuild, true
		}
		if body == "" {
			if !optional {
				t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
			}
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[{"detail":"not found"}]}`))
			return
		}
		_, _ = w.Write([]byte(body))
	}
}

// An app with no TestFlight groups and no App Store version is a normal state
// for a freshly created app record — every channel is empty, and none of that
// is an error.
func TestTracksReportsEmptyChannelsWithoutError(t *testing.T) {
	f := &tracksFixture{
		betaGroups:       `{"data":[]}`,
		appStoreVersions: `{"data":[]}`,
	}
	c, _ := newTestClient(t, f.handler(t))

	tracks, err := c.Tracks(context.Background(), "APP1")
	if err != nil {
		t.Fatalf("Tracks: %v", err)
	}
	for _, ch := range []string{domain.StoreChannelInternal, domain.StoreChannelExternal, domain.StoreChannelProduction} {
		rel, _ := tracks.Channel(ch)
		if rel.HasRelease {
			t.Errorf("%s HasRelease = true, want false", ch)
		}
		if rel.Status != domain.TrackStatusNone {
			t.Errorf("%s Status = %q, want %q", ch, rel.Status, domain.TrackStatusNone)
		}
	}
}

// The regression this pins: build.attributes.version is the BUILD number, and
// the marketing version lives on preReleaseVersion. Swapping them puts "142"
// in front of a person where "1.4.0" belongs.
func TestTracksInternalUsesMarketingVersionNotBuildNumber(t *testing.T) {
	f := &tracksFixture{
		betaGroups:        `{"data":[{"id":"G1","attributes":{"name":"Team","isInternalGroup":true}}]}`,
		builds:            `{"data":[{"id":"B1","attributes":{"version":"142","uploadedDate":"2026-08-30T11:22:33.000+0000"}}]}`,
		preReleaseVersion: `{"data":{"id":"PRV1","attributes":{"version":"1.4.0"}}}`,
		betaTesters:       `{"data":[],"meta":{"paging":{"total":12,"limit":1}}}`,
		appStoreVersions:  `{"data":[]}`,
	}
	c, _ := newTestClient(t, f.handler(t))

	tracks, err := c.Tracks(context.Background(), "APP1")
	if err != nil {
		t.Fatalf("Tracks: %v", err)
	}
	got := tracks.Internal
	if !got.HasRelease {
		t.Fatal("Internal.HasRelease = false, want true")
	}
	if got.Version != "1.4.0" {
		t.Errorf("Internal.Version = %q, want 1.4.0 (the marketing version)", got.Version)
	}
	if got.Build != "142" {
		t.Errorf("Internal.Build = %q, want 142 (the build number)", got.Build)
	}
	if got.Status != domain.TrackStatusLive {
		t.Errorf("Internal.Status = %q, want %q", got.Status, domain.TrackStatusLive)
	}
	if got.Audience != "12 internal testers" {
		t.Errorf("Internal.Audience = %q, want \"12 internal testers\"", got.Audience)
	}
	if got.UpdatedAt == nil {
		t.Error("Internal.UpdatedAt = nil, want the build's uploadedDate")
	}
	if f.buildsQuery["filter[betaGroups]"] != "G1" {
		t.Errorf("builds filter[betaGroups] = %q, want G1 — without it an internal-only build shows on external too", f.buildsQuery["filter[betaGroups]"])
	}
	if f.buildsQuery["sort"] != "-uploadedDate" {
		t.Errorf("builds sort = %q, want -uploadedDate", f.buildsQuery["sort"])
	}
	if tracks.External.HasRelease {
		t.Error("External.HasRelease = true, want false — there is no external group")
	}
}

// External adds Beta App Review on top, and its audience falls back to the
// group count when the tester total is unreadable: a decorative label must not
// turn a readable track into an error.
func TestTracksExternalReportsReviewAndFallsBackToGroupCount(t *testing.T) {
	f := &tracksFixture{
		betaGroups:        `{"data":[{"id":"G1","attributes":{"name":"Public Beta","isInternalGroup":false}}]}`,
		builds:            `{"data":[{"id":"B1","attributes":{"version":"77","uploadedDate":"2026-08-30T11:22:33.000+0000"}}]}`,
		preReleaseVersion: `{"data":{"id":"PRV1","attributes":{"version":"2.0.0"}}}`,
		betaReview:        `{"data":{"id":"SUB1","attributes":{"betaReviewState":"WAITING_FOR_REVIEW"}}}`,
		betaTesters:       `{"data":[]}`,
		appStoreVersions:  `{"data":[]}`,
	}
	c, _ := newTestClient(t, f.handler(t))

	tracks, err := c.Tracks(context.Background(), "APP1")
	if err != nil {
		t.Fatalf("Tracks: %v", err)
	}
	got := tracks.External
	if got.Status != domain.TrackStatusInReview {
		t.Errorf("External.Status = %q, want %q", got.Status, domain.TrackStatusInReview)
	}
	if got.Version != "2.0.0" || got.Build != "77" {
		t.Errorf("External version/build = %q/%q, want 2.0.0/77", got.Version, got.Build)
	}
	if got.Audience != "1 group" {
		t.Errorf("External.Audience = %q, want \"1 group\"", got.Audience)
	}
	if tracks.Internal.HasRelease {
		t.Error("Internal.HasRelease = true, want false — the only group is external")
	}
}

// appStoreState reads READY_FOR_SALE from the moment a phased release starts,
// so on its own it cannot tell "everyone has it" from "10% of users have it".
// A running phased release is a rollout, and ASC reports only which day it is
// on — the percentage comes from Apple's fixed schedule, where day 4 is 10%.
func TestTracksProductionActivePhasedReleaseIsRollingOut(t *testing.T) {
	f := &tracksFixture{
		betaGroups:       `{"data":[]}`,
		appStoreVersions: `{"data":[{"id":"VER1","attributes":{"versionString":"3.1.0","appStoreState":"READY_FOR_SALE","createdDate":"2026-08-20T09:00:00.000+0000"}}]}`,
		phasedRelease:    `{"data":{"id":"PR1","attributes":{"phasedReleaseState":"ACTIVE","currentDayNumber":4}}}`,
		versionBuild:     `{"data":{"id":"B9","attributes":{"version":"412"}}}`,
	}
	c, _ := newTestClient(t, f.handler(t))

	tracks, err := c.Tracks(context.Background(), "APP1")
	if err != nil {
		t.Fatalf("Tracks: %v", err)
	}
	got := tracks.Production
	if got.Version != "3.1.0" {
		t.Errorf("Production.Version = %q, want 3.1.0", got.Version)
	}
	if got.Build != "412" {
		t.Errorf("Production.Build = %q, want 412 — the column sits beside Play's versionCode", got.Build)
	}
	if got.Status != domain.TrackStatusRollingOut {
		t.Errorf("Production.Status = %q, want %q", got.Status, domain.TrackStatusRollingOut)
	}
	if got.UserFraction != 0.10 {
		t.Errorf("Production.UserFraction = %v, want 0.10 (phased release day 4)", got.UserFraction)
	}
	if got.UpdatedAt == nil {
		t.Error("Production.UpdatedAt = nil, want the version's createdDate")
	}
}

// A paused phased release is stopped in place, holding whatever share it had
// reached — the same thing Play calls a halted rollout.
func TestTracksProductionPausedPhasedReleaseIsHalted(t *testing.T) {
	f := &tracksFixture{
		betaGroups:       `{"data":[]}`,
		appStoreVersions: `{"data":[{"id":"VER1","attributes":{"versionString":"3.1.0","appStoreState":"READY_FOR_SALE"}}]}`,
		phasedRelease:    `{"data":{"id":"PR1","attributes":{"phasedReleaseState":"PAUSED","currentDayNumber":6}}}`,
		versionBuild:     `{"data":{"id":"B9","attributes":{"version":"412"}}}`,
	}
	c, _ := newTestClient(t, f.handler(t))

	tracks, err := c.Tracks(context.Background(), "APP1")
	if err != nil {
		t.Fatalf("Tracks: %v", err)
	}
	got := tracks.Production
	if got.Status != domain.TrackStatusHalted {
		t.Errorf("Production.Status = %q, want %q", got.Status, domain.TrackStatusHalted)
	}
	if got.UserFraction != 0.50 {
		t.Errorf("Production.UserFraction = %v, want 0.50 (paused on day 6)", got.UserFraction)
	}
}

// A completed phased release is simply out to everyone: the status falls back
// to the raw appStoreState and no fraction is reported.
func TestTracksProductionCompletePhasedReleaseIsLive(t *testing.T) {
	f := &tracksFixture{
		betaGroups:       `{"data":[]}`,
		appStoreVersions: `{"data":[{"id":"VER1","attributes":{"versionString":"3.1.0","appStoreState":"READY_FOR_SALE"}}]}`,
		phasedRelease:    `{"data":{"id":"PR1","attributes":{"phasedReleaseState":"COMPLETE","currentDayNumber":7}}}`,
		versionBuild:     `{"data":{"id":"B9","attributes":{"version":"412"}}}`,
	}
	c, _ := newTestClient(t, f.handler(t))

	tracks, err := c.Tracks(context.Background(), "APP1")
	if err != nil {
		t.Fatalf("Tracks: %v", err)
	}
	got := tracks.Production
	if got.Status != domain.TrackStatusLive {
		t.Errorf("Production.Status = %q, want %q", got.Status, domain.TrackStatusLive)
	}
	if got.UserFraction != 0 {
		t.Errorf("Production.UserFraction = %v, want 0", got.UserFraction)
	}
}

// A version with no phased release resource is released to everyone at once;
// ASC answers that relationship with a 404, which is not a failure. Neither is
// an unreadable build: the channel still reports, with that cell blank.
func TestTracksProductionSurvivesMissingPhasedReleaseAndBuild(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/apps/APP1/betaGroups":
			_, _ = w.Write([]byte(`{"data":[]}`))
		case "/v1/apps/APP1/appStoreVersions":
			_, _ = w.Write([]byte(`{"data":[{"id":"VER1","attributes":{"versionString":"1.0.0","appStoreState":"WAITING_FOR_REVIEW"}}]}`))
		case "/v1/appStoreVersions/VER1/appStoreVersionPhasedRelease", "/v1/appStoreVersions/VER1/build":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[{"detail":"not found"}]}`))
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
	})

	tracks, err := c.Tracks(context.Background(), "APP1")
	if err != nil {
		t.Fatalf("Tracks: %v", err)
	}
	if tracks.Production.UserFraction != 0 {
		t.Errorf("UserFraction = %v, want 0", tracks.Production.UserFraction)
	}
	if tracks.Production.Build != "" {
		t.Errorf("Build = %q, want it left blank when ASC will not say", tracks.Production.Build)
	}
	if tracks.Production.Status != domain.TrackStatusInReview {
		t.Errorf("Status = %q, want %q", tracks.Production.Status, domain.TrackStatusInReview)
	}
}

// The whole reason ListApps asks for ?include=appStoreVersions: Apple returns
// the related versions once per page in the top-level `included` array, so one
// request per page fills every app's State. The newest of the linked versions
// wins, and 1.10.0 beats 1.9.0 — a lexicographic pick would get that backwards.
func TestListAppsFillsStateFromIncludedVersions(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/apps" {
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
			return
		}
		_, _ = w.Write([]byte(`{"data":[
			{"id":"A1","attributes":{"name":"First","bundleId":"com.example.first"},
			 "relationships":{"appStoreVersions":{"data":[
				{"type":"appStoreVersions","id":"V1"},
				{"type":"appStoreVersions","id":"V2"}]}}},
			{"id":"A2","attributes":{"name":"Second","bundleId":"com.example.second"}}
		],"included":[
			{"type":"appStoreVersions","id":"V1","attributes":{"versionString":"1.9.0","appStoreState":"REJECTED"}},
			{"type":"appStoreVersions","id":"V2","attributes":{"versionString":"1.10.0","appStoreState":"READY_FOR_SALE"}}
		]}`))
	})

	apps, err := c.ListApps(context.Background())
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	want := []port.StoreAppRef{
		{StoreAppID: "A1", Identifier: "com.example.first", Name: "First", State: "READY_FOR_SALE"},
		{StoreAppID: "A2", Identifier: "com.example.second", Name: "Second"},
	}
	if !reflect.DeepEqual(apps, want) {
		t.Errorf("apps = %+v, want %+v", apps, want)
	}
}

// Linkage the response did not actually include leaves State empty. On a
// picker a guessed state is indistinguishable from a known one, so there is no
// honest fallback here.
func TestListAppsLeavesStateEmptyWhenIncludedIsAbsent(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/apps" {
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
			return
		}
		_, _ = w.Write([]byte(`{"data":[
			{"id":"A1","attributes":{"name":"First","bundleId":"com.example.first"},
			 "relationships":{"appStoreVersions":{"data":[{"type":"appStoreVersions","id":"V1"}]}}}
		]}`))
	})

	apps, err := c.ListApps(context.Background())
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	if len(apps) != 1 {
		t.Fatalf("apps = %+v, want one row", apps)
	}
	if apps[0].State != "" {
		t.Errorf("State = %q, want it empty — nothing in the response says", apps[0].State)
	}
}

func TestNormalizeAppStoreState(t *testing.T) {
	cases := map[string]string{
		"READY_FOR_SALE":            domain.TrackStatusLive,
		"WAITING_FOR_REVIEW":        domain.TrackStatusInReview,
		"IN_REVIEW":                 domain.TrackStatusInReview,
		"PENDING_APPLE_RELEASE":     domain.TrackStatusInReview,
		"PREPARE_FOR_SUBMISSION":    domain.TrackStatusDraft,
		"DEVELOPER_REJECTED":        domain.TrackStatusDraft,
		"PENDING_DEVELOPER_RELEASE": domain.TrackStatusDraft,
	}
	for raw, want := range cases {
		if got := normalizeAppStoreState(raw); got != want {
			t.Errorf("normalizeAppStoreState(%q) = %q, want %q", raw, got, want)
		}
	}
}

// Promotion is forward and one step at a time. Anything else must be refused
// before a single ASC call goes out — a "promotion" that skipped external
// would push an unreviewed build straight at App Store review.
func TestPromoteChannelRejectsIllegalPairs(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("an illegal promotion reached ASC: %s %s", r.Method, r.URL.Path)
	})

	pairs := [][2]string{
		{domain.StoreChannelInternal, domain.StoreChannelProduction},
		{domain.StoreChannelExternal, domain.StoreChannelInternal},
		{domain.StoreChannelProduction, domain.StoreChannelExternal},
		{domain.StoreChannelProduction, domain.StoreChannelProduction},
		{domain.StoreChannelInternal, domain.StoreChannelInternal},
		{"beta", domain.StoreChannelProduction},
	}
	for _, p := range pairs {
		err := c.PromoteChannel(context.Background(), "APP1", p[0], p[1])
		if err == nil {
			t.Errorf("PromoteChannel(%q, %q) = nil, want an error", p[0], p[1])
		}
	}
}

func TestPromoteChannelInternalToExternalAddsBuildToExternalGroups(t *testing.T) {
	var addedTo []string
	var addedBuild string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/apps/APP1/betaGroups":
			_, _ = w.Write([]byte(`{"data":[
				{"id":"G1","attributes":{"name":"Team","isInternalGroup":true}},
				{"id":"G2","attributes":{"name":"Public","isInternalGroup":false}}
			]}`))
		case r.URL.Path == "/v1/builds":
			if got := r.URL.Query().Get("filter[betaGroups]"); got != "G1" {
				t.Errorf("promoted from filter[betaGroups] = %q, want G1 (the internal group)", got)
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"B1","attributes":{"version":"9"}}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/betaGroups/G2/relationships/builds":
			addedTo = append(addedTo, "G2")
			var body struct {
				Data []struct {
					ID string `json:"id"`
				} `json:"data"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decoding relationship body: %v", err)
			}
			if len(body.Data) == 1 {
				addedBuild = body.Data[0].ID
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
	})

	if err := c.PromoteChannel(context.Background(), "APP1", domain.StoreChannelInternal, domain.StoreChannelExternal); err != nil {
		t.Fatalf("PromoteChannel: %v", err)
	}
	if !reflect.DeepEqual(addedTo, []string{"G2"}) {
		t.Errorf("groups the build was added to = %v, want [G2]", addedTo)
	}
	if addedBuild != "B1" {
		t.Errorf("added build = %q, want B1", addedBuild)
	}
}

// external -> production submits the build's MARKETING version, which is the
// only string ASC's appStoreVersions collection keys on; submitting the build
// number would create a bogus version. The reused version gets the same build
// PATCH as a fresh one: an editable version left over from an earlier attempt
// carries whatever build it was given then, or none at all.
func TestPromoteChannelExternalToProductionSubmitsMarketingVersion(t *testing.T) {
	submitted := ""
	createdVersion := false
	var calls []string
	attachedBuild := ""
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/apps/APP1/betaGroups":
			_, _ = w.Write([]byte(`{"data":[{"id":"G2","attributes":{"name":"Public","isInternalGroup":false}}]}`))
		case r.URL.Path == "/v1/builds":
			_, _ = w.Write([]byte(`{"data":[{"id":"B1","attributes":{"version":"300"}}]}`))
		case r.URL.Path == "/v1/builds/B1/preReleaseVersion":
			_, _ = w.Write([]byte(`{"data":{"id":"PRV1","attributes":{"version":"3.1.0"}}}`))
		case r.URL.Path == "/v1/apps/APP1/appStoreVersions":
			_, _ = w.Write([]byte(`{"data":[{"id":"VER1","attributes":{"versionString":"3.1.0","appStoreState":"PREPARE_FOR_SUBMISSION"}}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/appStoreVersions":
			createdVersion = true
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"data":{"id":"NEW"}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/v1/appStoreVersions/VER1/relationships/build":
			calls = append(calls, "attach")
			attachedBuild = patchedBuildID(t, r)
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/appStoreVersionSubmissions":
			calls = append(calls, "submit")
			submitted = postedVersionID(t, r)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"data":{"id":"SUB1"}}`))
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
	})

	if err := c.PromoteChannel(context.Background(), "APP1", domain.StoreChannelExternal, domain.StoreChannelProduction); err != nil {
		t.Fatalf("PromoteChannel: %v", err)
	}
	if createdVersion {
		t.Error("created a duplicate appStoreVersion instead of reusing the editable 3.1.0")
	}
	if submitted != "VER1" {
		t.Errorf("submitted version = %q, want VER1", submitted)
	}
	if !reflect.DeepEqual(calls, []string{"attach", "submit"}) {
		t.Errorf("calls = %v, want [attach submit] — the build goes on before the submission, not after", calls)
	}
	if attachedBuild != "B1" {
		t.Errorf("attached build = %q, want B1", attachedBuild)
	}
}

// Nothing on the source channel is a refusal, not a silent no-op: the caller
// asked for a promotion that cannot happen.
func TestPromoteChannelRefusesWhenSourceChannelIsEmpty(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/apps/APP1/betaGroups":
			_, _ = w.Write([]byte(`{"data":[
				{"id":"G1","attributes":{"isInternalGroup":true}},
				{"id":"G2","attributes":{"isInternalGroup":false}}
			]}`))
		case "/v1/builds":
			_, _ = w.Write([]byte(`{"data":[]}`))
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
	})

	if err := c.PromoteChannel(context.Background(), "APP1", domain.StoreChannelInternal, domain.StoreChannelExternal); err == nil {
		t.Fatal("expected an error when the internal channel holds no build, got nil")
	}
}

func TestPhasedFractionFollowsApplesSchedule(t *testing.T) {
	want := map[int]float64{0: 0, 1: 0.01, 2: 0.02, 3: 0.05, 4: 0.10, 5: 0.20, 6: 0.50, 7: 1, 9: 1}
	for day, f := range want {
		if got := phasedFraction(day); got != f {
			t.Errorf("phasedFraction(%d) = %v, want %v", day, got, f)
		}
	}
}

// patchedBuildID pulls the build id out of a to-one relationship PATCH body,
// which is the only way a version's build can be set.
func patchedBuildID(t *testing.T, r *http.Request) string {
	t.Helper()
	var body struct {
		Data struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatalf("decoding %s body: %v", r.URL.Path, err)
	}
	if body.Data.Type != "builds" {
		t.Errorf("%s body data.type = %q, want builds", r.URL.Path, body.Data.Type)
	}
	return body.Data.ID
}

// The promotion that never worked: external carries build 300 for 3.1.0 and no
// appStoreVersion for 3.1.0 exists yet, so one is created — and ASC refuses an
// appStoreVersionSubmissions POST for a version with no build attached. The
// build has to be PATCHed on first, or the promotion 409s and leaves behind a
// draft version nothing can submit.
func TestPromoteChannelExternalToProductionAttachesBuildBeforeSubmitting(t *testing.T) {
	var calls []string
	attachedBuild := ""
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/apps/APP1/betaGroups":
			_, _ = w.Write([]byte(`{"data":[{"id":"G2","attributes":{"isInternalGroup":false}}]}`))
		case r.URL.Path == "/v1/builds":
			_, _ = w.Write([]byte(`{"data":[{"id":"B300","attributes":{"version":"300"}}]}`))
		case r.URL.Path == "/v1/builds/B300/preReleaseVersion":
			_, _ = w.Write([]byte(`{"data":{"id":"PRV1","attributes":{"version":"3.1.0"}}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/apps/APP1/appStoreVersions":
			_, _ = w.Write([]byte(`{"data":[]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/appStoreVersions":
			calls = append(calls, "create")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"data":{"id":"VERNEW"}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/v1/appStoreVersions/VERNEW/relationships/build":
			calls = append(calls, "attach")
			attachedBuild = patchedBuildID(t, r)
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/appStoreVersionSubmissions":
			calls = append(calls, "submit")
			if got := postedVersionID(t, r); got != "VERNEW" {
				t.Errorf("submitted version = %q, want VERNEW", got)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"data":{"id":"SUB1"}}`))
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
	})

	if err := c.PromoteChannel(context.Background(), "APP1", domain.StoreChannelExternal, domain.StoreChannelProduction); err != nil {
		t.Fatalf("PromoteChannel: %v", err)
	}
	if !reflect.DeepEqual(calls, []string{"create", "attach", "submit"}) {
		t.Errorf("calls = %v, want [create attach submit] — the build must be on the version before it is submitted", calls)
	}
	if attachedBuild != "B300" {
		t.Errorf("attached build = %q, want B300 (the build on the external channel)", attachedBuild)
	}
}

// A build that will not attach is a promotion that cannot succeed. Submitting
// anyway just moves the same failure to Apple's side, where it reads as an
// opaque entity error instead of naming the build and version.
func TestPromoteChannelStopsWhenTheBuildWillNotAttach(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/apps/APP1/betaGroups":
			_, _ = w.Write([]byte(`{"data":[{"id":"G2","attributes":{"isInternalGroup":false}}]}`))
		case r.URL.Path == "/v1/builds":
			_, _ = w.Write([]byte(`{"data":[{"id":"B300","attributes":{"version":"300"}}]}`))
		case r.URL.Path == "/v1/builds/B300/preReleaseVersion":
			_, _ = w.Write([]byte(`{"data":{"id":"PRV1","attributes":{"version":"3.1.0"}}}`))
		case r.URL.Path == "/v1/apps/APP1/appStoreVersions":
			_, _ = w.Write([]byte(`{"data":[{"id":"VER1","attributes":{"versionString":"3.1.0","appStoreState":"PREPARE_FOR_SUBMISSION"}}]}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/v1/appStoreVersions/VER1/relationships/build":
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"errors":[{"code":"ENTITY_ERROR","detail":"build is not processed"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/appStoreVersionSubmissions":
			t.Error("submitted a version whose build could not be attached")
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
	})

	err := c.PromoteChannel(context.Background(), "APP1", domain.StoreChannelExternal, domain.StoreChannelProduction)
	if err == nil {
		t.Fatal("PromoteChannel = nil, want the attach failure surfaced")
	}
	if !strings.Contains(err.Error(), "B300") || !strings.Contains(err.Error(), "VER1") {
		t.Errorf("error = %v, want it to name the build and the version it could not be attached to", err)
	}
}

// This system mints an appStoreVersion per merged PR, so a draft numbered above
// the live version is the normal state, not the exception. Reading the highest
// versionString reports that draft, and the panel then shows no live version,
// no user_fraction, and the draft's build — during an incident that reads as
// "nothing is out in production".
func TestTracksProductionPrefersLiveVersionOverHigherDraft(t *testing.T) {
	f := &tracksFixture{
		betaGroups: `{"data":[]}`,
		appStoreVersions: `{"data":[
			{"id":"VER1","attributes":{"versionString":"1.4.0","appStoreState":"READY_FOR_SALE","createdDate":"2026-08-20T09:00:00.000+0000"}},
			{"id":"VER2","attributes":{"versionString":"1.5.0","appStoreState":"PREPARE_FOR_SUBMISSION"}}
		]}`,
		versionBuild: `{"data":{"id":"B9","attributes":{"version":"412"}}}`,
	}
	c, _ := newTestClient(t, f.handler(t))

	tracks, err := c.Tracks(context.Background(), "APP1")
	if err != nil {
		t.Fatalf("Tracks: %v", err)
	}
	got := tracks.Production
	if got.Version != "1.4.0" {
		t.Errorf("Production.Version = %q, want 1.4.0 — the live version, not the higher-numbered draft", got.Version)
	}
	if got.Status != domain.TrackStatusLive {
		t.Errorf("Production.Status = %q, want %q", got.Status, domain.TrackStatusLive)
	}
	if got.Build != "412" {
		t.Errorf("Production.Build = %q, want 412 (the live version's build)", got.Build)
	}
	if got.UpdatedAt == nil {
		t.Error("Production.UpdatedAt = nil, want the live version's createdDate")
	}
}

// A version Apple is holding is still the production story, and it outranks a
// draft numbered above it.
func TestTracksProductionPrefersPendingDeveloperReleaseOverHigherDraft(t *testing.T) {
	f := &tracksFixture{
		betaGroups: `{"data":[]}`,
		appStoreVersions: `{"data":[
			{"id":"VER2","attributes":{"versionString":"2.1.0","appStoreState":"PREPARE_FOR_SUBMISSION"}},
			{"id":"VER1","attributes":{"versionString":"2.0.0","appStoreState":"PENDING_DEVELOPER_RELEASE"}}
		]}`,
		versionBuild: `{"data":{"id":"B9","attributes":{"version":"88"}}}`,
	}
	c, _ := newTestClient(t, f.handler(t))

	tracks, err := c.Tracks(context.Background(), "APP1")
	if err != nil {
		t.Fatalf("Tracks: %v", err)
	}
	if tracks.Production.Version != "2.0.0" {
		t.Errorf("Production.Version = %q, want 2.0.0 (the version pending developer release)", tracks.Production.Version)
	}
}

// With nothing shipping, the draft is all there is — preferring live states
// must not turn an app that has only ever been prepared into an empty channel.
func TestTracksProductionFallsBackToDraftWhenNothingIsShipping(t *testing.T) {
	f := &tracksFixture{
		betaGroups: `{"data":[]}`,
		appStoreVersions: `{"data":[
			{"id":"VER0","attributes":{"versionString":"0.9.0","appStoreState":"DEVELOPER_REJECTED"}},
			{"id":"VER1","attributes":{"versionString":"1.0.0","appStoreState":"PREPARE_FOR_SUBMISSION"}}
		]}`,
		versionBuild: `{"data":{"id":"B1","attributes":{"version":"7"}}}`,
	}
	c, _ := newTestClient(t, f.handler(t))

	tracks, err := c.Tracks(context.Background(), "APP1")
	if err != nil {
		t.Fatalf("Tracks: %v", err)
	}
	got := tracks.Production
	if !got.HasRelease {
		t.Fatal("Production.HasRelease = false, want the draft reported")
	}
	if got.Version != "1.0.0" {
		t.Errorf("Production.Version = %q, want 1.0.0", got.Version)
	}
	if got.Status != domain.TrackStatusDraft {
		t.Errorf("Production.Status = %q, want %q", got.Status, domain.TrackStatusDraft)
	}
}

// ASC caps the versions it inlines per app and documents no ordering for the
// relationship, so on a long-lived app the rows that come back are an arbitrary
// slice of its history and their newest can be months stale. paging.total above
// what was listed is the only signal of that — no badge beats a wrong one.
func TestListAppsLeavesStateEmptyWhenIncludedVersionsAreTruncated(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/apps" {
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
			return
		}
		_, _ = w.Write([]byte(`{"data":[
			{"id":"A1","attributes":{"name":"First","bundleId":"com.example.first"},
			 "relationships":{"appStoreVersions":{
				"data":[{"type":"appStoreVersions","id":"V1"}],
				"meta":{"paging":{"total":140,"limit":50}}}}},
			{"id":"A2","attributes":{"name":"Second","bundleId":"com.example.second"},
			 "relationships":{"appStoreVersions":{
				"data":[{"type":"appStoreVersions","id":"V2"}],
				"meta":{"paging":{"total":1,"limit":50}}}}}
		],"included":[
			{"type":"appStoreVersions","id":"V1","attributes":{"versionString":"1.1.0","appStoreState":"PREPARE_FOR_SUBMISSION"}},
			{"type":"appStoreVersions","id":"V2","attributes":{"versionString":"4.0.0","appStoreState":"IN_REVIEW"}}
		]}`))
	})

	apps, err := c.ListApps(context.Background())
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	want := []port.StoreAppRef{
		{StoreAppID: "A1", Identifier: "com.example.first", Name: "First"},
		{StoreAppID: "A2", Identifier: "com.example.second", Name: "Second", State: "IN_REVIEW"},
	}
	if !reflect.DeepEqual(apps, want) {
		t.Errorf("apps = %+v, want %+v — a truncated relationship must not produce a badge", apps, want)
	}
}

// A version in review is not on production — users still have the older one.
// Reporting the submission is the same failure as reporting the draft, one step
// smaller: the card says "Production" while hiding what people are running.
func TestTracksProductionPrefersLiveVersionOverVersionInReview(t *testing.T) {
	f := &tracksFixture{
		betaGroups: `{"data":[]}`,
		appStoreVersions: `{"data":[
			{"id":"VER2","attributes":{"versionString":"1.5.0","appStoreState":"IN_REVIEW"}},
			{"id":"VER1","attributes":{"versionString":"1.4.0","appStoreState":"READY_FOR_SALE","createdDate":"2026-08-20T09:00:00.000+0000"}}
		]}`,
		versionBuild: `{"data":{"id":"B9","attributes":{"version":"412"}}}`,
	}
	c, _ := newTestClient(t, f.handler(t))

	tracks, err := c.Tracks(context.Background(), "APP1")
	if err != nil {
		t.Fatalf("Tracks: %v", err)
	}
	got := tracks.Production
	if got.Version != "1.4.0" {
		t.Errorf("Production.Version = %q, want 1.4.0 — the version users have, not the one Apple is still reviewing", got.Version)
	}
	if got.Status != domain.TrackStatusLive {
		t.Errorf("Production.Status = %q, want %q", got.Status, domain.TrackStatusLive)
	}
	if got.Build != "412" {
		t.Errorf("Production.Build = %q, want 412 (the live version's build)", got.Build)
	}
}

// With nothing ever released, the version in review IS the production story and
// outranks the draft behind it — the second tier must not be dead code.
func TestTracksProductionReportsVersionInReviewWhenNothingIsLive(t *testing.T) {
	f := &tracksFixture{
		betaGroups: `{"data":[]}`,
		appStoreVersions: `{"data":[
			{"id":"VER2","attributes":{"versionString":"1.1.0","appStoreState":"PREPARE_FOR_SUBMISSION"}},
			{"id":"VER1","attributes":{"versionString":"1.0.0","appStoreState":"WAITING_FOR_REVIEW"}}
		]}`,
		versionBuild: `{"data":{"id":"B1","attributes":{"version":"3"}}}`,
	}
	c, _ := newTestClient(t, f.handler(t))

	tracks, err := c.Tracks(context.Background(), "APP1")
	if err != nil {
		t.Fatalf("Tracks: %v", err)
	}
	got := tracks.Production
	if got.Version != "1.0.0" {
		t.Errorf("Production.Version = %q, want 1.0.0 — nothing is live, so the submission is the story", got.Version)
	}
	if got.Status != domain.TrackStatusInReview {
		t.Errorf("Production.Status = %q, want %q", got.Status, domain.TrackStatusInReview)
	}
}
