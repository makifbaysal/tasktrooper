package appstore

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// buildsPager serves /v1/builds as cursor-linked pages; with endless set it
// never stops handing out a next link.
type buildsPager struct {
	pages   []string
	endless bool
	queries []string
}

func (p *buildsPager) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/builds" {
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
			return
		}
		p.queries = append(p.queries, r.URL.RawQuery)
		idx := len(p.queries) - 1
		page := `[]`
		if idx < len(p.pages) {
			page = p.pages[idx]
		}
		links := "{}"
		if p.endless || idx+1 < len(p.pages) {
			links = fmt.Sprintf(`{"next":"https://appstore.invalid/v1/builds?cursor=%d"}`, idx+1)
		}
		_, _ = fmt.Fprintf(w, `{"data":%s,"links":%s}`, page, links)
	}
}

// Build numbers from an older CI scheme ("57") live next to the dotted
// "<sequence>.<task>.<attempt>" ones; only the leading integer orders them,
// and the highest may sit on any page.
func TestLatestBuildSequenceTakesTheHighestLeadingIntegerAcrossPages(t *testing.T) {
	pager := &buildsPager{pages: []string{
		`[{"id":"B1","attributes":{"version":"57"}},{"id":"B2","attributes":{"version":"9.1.1"}}]`,
		`[{"id":"B3","attributes":{"version":"412.54.2"}},{"id":"B4","attributes":{"version":"nightly"}}]`,
	}}
	c, _ := newTestClient(t, pager.handler(t))

	got, err := c.LatestBuildSequence(context.Background(), "APP1")
	if err != nil {
		t.Fatalf("LatestBuildSequence: %v", err)
	}
	if got != 412 {
		t.Errorf("LatestBuildSequence = %d, want 412", got)
	}
	if len(pager.queries) != 2 {
		t.Fatalf("requested %d pages, want 2", len(pager.queries))
	}
	for _, want := range []string{"filter[app]=APP1", "sort=-uploadedDate", "limit=200"} {
		if !strings.Contains(pager.queries[0], want) {
			t.Errorf("first query = %q, want it to carry %s", pager.queries[0], want)
		}
	}
}

func TestLatestBuildSequenceStopsPagingAtTheCap(t *testing.T) {
	pager := &buildsPager{
		pages:   []string{`[{"id":"B1","attributes":{"version":"7"}}]`},
		endless: true,
	}
	c, _ := newTestClient(t, pager.handler(t))

	got, err := c.LatestBuildSequence(context.Background(), "APP1")
	if err != nil {
		t.Fatalf("LatestBuildSequence: %v", err)
	}
	if got != 7 {
		t.Errorf("LatestBuildSequence = %d, want 7", got)
	}
	if len(pager.queries) != ascBuildSequencePages {
		t.Errorf("requested %d pages, want %d", len(pager.queries), ascBuildSequencePages)
	}
}

func TestLatestBuildSequenceIsZeroWithoutBuilds(t *testing.T) {
	c, _ := newTestClient(t, (&buildsPager{}).handler(t))

	got, err := c.LatestBuildSequence(context.Background(), "APP1")
	if err != nil {
		t.Fatalf("LatestBuildSequence: %v", err)
	}
	if got != 0 {
		t.Errorf("LatestBuildSequence = %d, want 0", got)
	}
}

// The same CFBundleVersion can exist in two version trains; the newest upload
// is the one just made. Its export compliance answer is still null, which must
// read as unanswered rather than as "no encryption".
func TestFindBuildMapsTheNewestMatch(t *testing.T) {
	var query url.Values
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/builds" {
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
			return
		}
		query = r.URL.Query()
		_, _ = w.Write([]byte(`{
			"data":[
				{"id":"B1","attributes":{"version":"412.54.2","processingState":"VALID","expired":true,"usesNonExemptEncryption":false,"uploadedDate":"2026-08-01T10:00:00.000+0000"},
				 "relationships":{"preReleaseVersion":{"data":{"type":"preReleaseVersions","id":"PRV0"}}}},
				{"id":"B2","attributes":{"version":"412.54.2","processingState":"PROCESSING","expired":false,"usesNonExemptEncryption":null,"uploadedDate":"2026-09-01T10:00:00.000+0000"},
				 "relationships":{"preReleaseVersion":{"data":{"type":"preReleaseVersions","id":"PRV1"}}}}
			],
			"included":[
				{"type":"preReleaseVersions","id":"PRV0","attributes":{"version":"1.3.0"}},
				{"type":"buildBetaDetails","id":"PRV1","attributes":{"internalBuildState":"PROCESSING"}},
				{"type":"preReleaseVersions","id":"PRV1","attributes":{"version":"1.4.0"}}
			]}`))
	})

	got, found, err := c.FindBuild(context.Background(), "APP1", "412.54.2")
	if err != nil {
		t.Fatalf("FindBuild: %v", err)
	}
	if !found {
		t.Fatal("found = false, want true")
	}
	want := port.TestFlightBuild{
		ID:               "B2",
		BuildNumber:      "412.54.2",
		MarketingVersion: "1.4.0",
		ProcessingState:  "PROCESSING",
		UploadedAt:       time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC),
	}
	if got.UsesNonExemptEncryption != nil {
		t.Errorf("UsesNonExemptEncryption = %v, want nil for an unanswered question", *got.UsesNonExemptEncryption)
	}
	if !got.UploadedAt.Equal(want.UploadedAt) {
		t.Errorf("UploadedAt = %v, want %v", got.UploadedAt, want.UploadedAt)
	}
	got.UploadedAt = want.UploadedAt
	if !reflect.DeepEqual(got, want) {
		t.Errorf("build = %+v, want %+v", got, want)
	}
	if query.Get("filter[version]") != "412.54.2" || query.Get("filter[app]") != "APP1" {
		t.Errorf("query = %v, want filter[app]=APP1 and filter[version]=412.54.2", query)
	}
	if !strings.Contains(query.Get("include"), "preReleaseVersion") {
		t.Errorf("include = %q, want preReleaseVersion", query.Get("include"))
	}
}

func TestFindBuildReportsAnUnregisteredUploadAsNotFound(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/builds" {
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
			return
		}
		_, _ = w.Write([]byte(`{"data":[]}`))
	})

	_, found, err := c.FindBuild(context.Background(), "APP1", "413.1.1")
	if err != nil {
		t.Fatalf("FindBuild: %v", err)
	}
	if found {
		t.Error("found = true, want false")
	}
}

type localizationBody struct {
	Data struct {
		Type       string `json:"type"`
		ID         string `json:"id"`
		Attributes struct {
			Locale   string `json:"locale"`
			WhatsNew string `json:"whatsNew"`
		} `json:"attributes"`
		Relationships struct {
			Build struct {
				Data relationshipRef `json:"data"`
			} `json:"build"`
		} `json:"relationships"`
	} `json:"data"`
}

func decodeJSONBody(t *testing.T, r *http.Request, into any) {
	t.Helper()
	if err := json.NewDecoder(r.Body).Decode(into); err != nil {
		t.Errorf("decoding %s %s body: %v", r.Method, r.URL.Path, err)
	}
}

func localizationsHandler(t *testing.T, existing string, onWrite func(r *http.Request)) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/builds/B1/betaBuildLocalizations":
			_, _ = w.Write([]byte(existing))
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/v1/betaBuildLocalizations/"),
			r.Method == http.MethodPost && r.URL.Path == "/v1/betaBuildLocalizations":
			onWrite(r)
			_, _ = w.Write([]byte(`{"data":{"id":"LNEW"}}`))
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
	}
}

func TestSetWhatToTestUpdatesTheExistingLocalization(t *testing.T) {
	var calls []string
	var body localizationBody
	c, _ := newTestClient(t, localizationsHandler(t,
		`{"data":[{"id":"L2","attributes":{"locale":"tr"}},{"id":"L1","attributes":{"locale":"en-US"}}]}`,
		func(r *http.Request) {
			calls = append(calls, r.Method+" "+r.URL.Path)
			decodeJSONBody(t, r, &body)
		}))

	if err := c.SetWhatToTest(context.Background(), "B1", "en-US", "T-54 · #2"); err != nil {
		t.Fatalf("SetWhatToTest: %v", err)
	}
	if !reflect.DeepEqual(calls, []string{"PATCH /v1/betaBuildLocalizations/L1"}) {
		t.Errorf("calls = %v, want only the PATCH of the en-US localization", calls)
	}
	if body.Data.Type != "betaBuildLocalizations" || body.Data.ID != "L1" || body.Data.Attributes.WhatsNew != "T-54 · #2" {
		t.Errorf("body = %+v, want type betaBuildLocalizations, id L1, whatsNew %q", body.Data, "T-54 · #2")
	}
}

// App Store Connect rejects a whatsNew over 4000 characters outright; a long
// task description must still land, cut on a character boundary.
func TestSetWhatToTestCreatesAMissingLocalizationWithinTheLimit(t *testing.T) {
	var calls []string
	var body localizationBody
	c, _ := newTestClient(t, localizationsHandler(t,
		`{"data":[{"id":"L2","attributes":{"locale":"tr"}}]}`,
		func(r *http.Request) {
			calls = append(calls, r.Method+" "+r.URL.Path)
			decodeJSONBody(t, r, &body)
		}))

	long := strings.Repeat("ç", ascWhatsNewMaxRunes+100)
	if err := c.SetWhatToTest(context.Background(), "B1", "en-US", long); err != nil {
		t.Fatalf("SetWhatToTest: %v", err)
	}
	if !reflect.DeepEqual(calls, []string{"POST /v1/betaBuildLocalizations"}) {
		t.Errorf("calls = %v, want only the POST", calls)
	}
	attrs := body.Data.Attributes
	if attrs.Locale != "en-US" {
		t.Errorf("locale = %q, want en-US", attrs.Locale)
	}
	if n := utf8.RuneCountInString(attrs.WhatsNew); n != ascWhatsNewMaxRunes || !utf8.ValidString(attrs.WhatsNew) {
		t.Errorf("whatsNew has %d runes (valid UTF-8: %v), want %d", n, utf8.ValidString(attrs.WhatsNew), ascWhatsNewMaxRunes)
	}
	if got := body.Data.Relationships.Build.Data; got != (relationshipRef{Type: "builds", ID: "B1"}) {
		t.Errorf("build relationship = %+v, want builds/B1", got)
	}
}

func linkageIDs(t *testing.T, r *http.Request, wantType string) []string {
	t.Helper()
	var body struct {
		Data []relationshipRef `json:"data"`
	}
	decodeJSONBody(t, r, &body)
	ids := make([]string, 0, len(body.Data))
	for _, ref := range body.Data {
		if ref.Type != wantType {
			t.Errorf("%s %s linkage type = %q, want %s", r.Method, r.URL.Path, ref.Type, wantType)
		}
		ids = append(ids, ref.ID)
	}
	return ids
}

func TestBuildGroupMembership(t *testing.T) {
	var added, removed []string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/builds/B1/relationships/betaGroups":
			_, _ = w.Write([]byte(`{"data":[{"type":"betaGroups","id":"G1"},{"type":"betaGroups","id":"G2"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/builds/B1/relationships/betaGroups":
			added = linkageIDs(t, r, "betaGroups")
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/builds/B1/relationships/betaGroups":
			removed = linkageIDs(t, r, "betaGroups")
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
	})
	ctx := context.Background()

	ids, err := c.BuildGroupIDs(ctx, "B1")
	if err != nil {
		t.Fatalf("BuildGroupIDs: %v", err)
	}
	if !reflect.DeepEqual(ids, []string{"G1", "G2"}) {
		t.Errorf("BuildGroupIDs = %v, want [G1 G2]", ids)
	}
	if err := c.AddBuildToGroups(ctx, "B1", []string{"G3", "G4"}); err != nil {
		t.Fatalf("AddBuildToGroups: %v", err)
	}
	if err := c.RemoveBuildFromGroups(ctx, "B1", []string{"G1"}); err != nil {
		t.Fatalf("RemoveBuildFromGroups: %v", err)
	}
	if err := c.AddBuildToGroups(ctx, "B1", nil); err != nil {
		t.Fatalf("AddBuildToGroups(nil): %v", err)
	}
	if !reflect.DeepEqual(added, []string{"G3", "G4"}) {
		t.Errorf("added = %v, want [G3 G4]", added)
	}
	if !reflect.DeepEqual(removed, []string{"G1"}) {
		t.Errorf("removed = %v, want [G1]", removed)
	}
}

// POST /v1/betaTesters refuses an email App Store Connect already holds; the
// person still has to end up in the group.
func TestAddTesterLinksAnExistingTesterOnConflict(t *testing.T) {
	var linked []string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/betaTesters":
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"errors":[{"status":"409","code":"ENTITY_ERROR.ATTRIBUTE.INVALID.DUPLICATE"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/betaTesters":
			if got := r.URL.Query().Get("filter[email]"); got != "ada+qa@example.com" {
				t.Errorf("filter[email] = %q, want ada+qa@example.com", got)
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"T9","attributes":{"email":"ada+qa@example.com","firstName":"Ada","lastName":"Lovelace","inviteType":"EMAIL"}}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/betaGroups/G1/relationships/betaTesters":
			linked = linkageIDs(t, r, "betaTesters")
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
	})

	got, err := c.AddTester(context.Background(), "G1", "ada+qa@example.com", "Ada", "Lovelace")
	if err != nil {
		t.Fatalf("AddTester: %v", err)
	}
	want := port.BetaTester{ID: "T9", Email: "ada+qa@example.com", FirstName: "Ada", LastName: "Lovelace", State: "EMAIL"}
	if got != want {
		t.Errorf("tester = %+v, want %+v", got, want)
	}
	if !reflect.DeepEqual(linked, []string{"T9"}) {
		t.Errorf("linked = %v, want [T9]", linked)
	}
}

func betaReviewHandler(t *testing.T, review string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/betaAppReviewSubmissions":
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"errors":[{"status":"409","code":"ENTITY_ERROR","detail":"The build has already been submitted."}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/builds/B1/betaAppReviewSubmission":
			if review == "" {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"errors":[{"status":"404"}]}`))
				return
			}
			_, _ = w.Write([]byte(review))
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
	}
}

func TestSubmitForBetaReviewToleratesAnAlreadySubmittedBuild(t *testing.T) {
	c, _ := newTestClient(t, betaReviewHandler(t, `{"data":{"id":"S1","attributes":{"betaReviewState":"WAITING_FOR_REVIEW"}}}`))

	if err := c.SubmitForBetaReview(context.Background(), "B1"); err != nil {
		t.Errorf("SubmitForBetaReview = %v, want nil for a build already in review", err)
	}
}

// A conflict about anything else — the build never reached review — is a
// failure, whatever its message says.
func TestSubmitForBetaReviewSurfacesAConflictForAnUnsubmittedBuild(t *testing.T) {
	c, _ := newTestClient(t, betaReviewHandler(t, ""))

	err := c.SubmitForBetaReview(context.Background(), "B1")
	if err == nil {
		t.Fatal("SubmitForBetaReview = nil, want the conflict surfaced")
	}
	if !isConflict(err) {
		t.Errorf("error = %v, want it to wrap the 409", err)
	}
}

func TestBetaReviewStateIsEmptyForANeverSubmittedBuild(t *testing.T) {
	c, _ := newTestClient(t, betaReviewHandler(t, ""))

	state, err := c.BetaReviewState(context.Background(), "B1")
	if err != nil {
		t.Fatalf("BetaReviewState: %v", err)
	}
	if state != "" {
		t.Errorf("BetaReviewState = %q, want empty", state)
	}
}

// A public link left behind after it was switched off is not a way in; and a
// tester count App Store Connect will not give must not hide the group.
func TestBetaGroupsMapsPublicLinksAndUnknownTesterCounts(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/apps/APP1/betaGroups":
			if got := r.URL.Query().Get("limit"); got != "200" {
				t.Errorf("limit = %q, want 200", got)
			}
			_, _ = w.Write([]byte(`{"data":[
				{"id":"GB","attributes":{"name":"Beta","isInternalGroup":false,"publicLinkEnabled":true,"publicLink":"https://testflight.apple.com/join/abc"}},
				{"id":"GA","attributes":{"name":"Alpha","isInternalGroup":false,"publicLinkEnabled":false,"publicLink":"https://testflight.apple.com/join/old"}},
				{"id":"GT","attributes":{"name":"Team","isInternalGroup":true,"hasAccessToAllBuilds":true,"publicLink":null}}
			]}`))
		case "/v1/betaGroups/GT/betaTesters":
			_, _ = w.Write([]byte(`{"data":[],"meta":{"paging":{"total":3,"limit":1}}}`))
		case "/v1/betaGroups/GA/betaTesters":
			_, _ = w.Write([]byte(`{"data":[],"meta":{"paging":{"limit":1}}}`))
		case "/v1/betaGroups/GB/betaTesters":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
	})

	got, err := c.BetaGroups(context.Background(), "APP1")
	if err != nil {
		t.Fatalf("BetaGroups: %v", err)
	}
	want := []port.BetaGroup{
		{ID: "GT", Name: "Team", Internal: true, AllBuilds: true, TesterCount: 3},
		{ID: "GA", Name: "Alpha", TesterCount: -1},
		{ID: "GB", Name: "Beta", PublicLink: "https://testflight.apple.com/join/abc", TesterCount: -1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("groups = %+v, want %+v", got, want)
	}
}
