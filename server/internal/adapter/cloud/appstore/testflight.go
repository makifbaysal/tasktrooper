package appstore

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

var _ port.TestFlightClient = (*Client)(nil)

// ascBuildSequencePages bounds the newest-first walk LatestBuildSequence does.
// Build numbers only grow, so the highest one sits among the newest uploads; a
// full walk of an app with years of CI builds would cost dozens of requests for
// nothing.
const ascBuildSequencePages = 5

const ascWhatsNewMaxRunes = 4000

func (c *Client) LatestBuildSequence(ctx context.Context, appID string) (int64, error) {
	path := "/v1/builds?filter[app]=" + url.QueryEscape(appID) + "&sort=-uploadedDate&limit=200&fields[builds]=version"
	var latest int64
	for page := 0; path != "" && page < ascBuildSequencePages; page++ {
		var body jsonAPIPage[buildRow]
		if err := c.do(ctx, http.MethodGet, path, nil, &body); err != nil {
			return 0, fmt.Errorf("appstore: listing builds of app %s: %w", appID, err)
		}
		for _, b := range body.Data {
			if n, ok := domain.LeadingBuildInteger(b.Attributes.Version); ok && n > latest {
				latest = n
			}
		}
		next, err := nextPagePath(body.Links.Next)
		if err != nil {
			return 0, err
		}
		path = next
	}
	return latest, nil
}

type buildLookupPage struct {
	Data []struct {
		ID         string `json:"id"`
		Attributes struct {
			Version                 string `json:"version"`
			ProcessingState         string `json:"processingState"`
			Expired                 bool   `json:"expired"`
			UsesNonExemptEncryption *bool  `json:"usesNonExemptEncryption"`
			UploadedDate            string `json:"uploadedDate"`
		} `json:"attributes"`
		Relationships struct {
			PreReleaseVersion struct {
				Data *relationshipRef `json:"data"`
			} `json:"preReleaseVersion"`
		} `json:"relationships"`
	} `json:"data"`
	Included []struct {
		Type       string `json:"type"`
		ID         string `json:"id"`
		Attributes struct {
			Version string `json:"version"`
		} `json:"attributes"`
	} `json:"included"`
}

type relationshipRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// FindBuild picks the newest upload when several match: CFBundleVersion is
// unique only within a version train, so the same number can exist under two
// marketing versions.
func (c *Client) FindBuild(ctx context.Context, appID, buildNumber string) (port.TestFlightBuild, bool, error) {
	path := "/v1/builds?filter[app]=" + url.QueryEscape(appID) +
		"&filter[version]=" + url.QueryEscape(buildNumber) +
		"&include=preReleaseVersion,buildBetaDetail&limit=10"
	var page buildLookupPage
	if err := c.do(ctx, http.MethodGet, path, nil, &page); err != nil {
		return port.TestFlightBuild{}, false, fmt.Errorf("appstore: looking up build %s of app %s: %w", buildNumber, appID, err)
	}

	marketing := make(map[string]string, len(page.Included))
	for _, inc := range page.Included {
		if inc.Type == "preReleaseVersions" {
			marketing[inc.ID] = inc.Attributes.Version
		}
	}

	var best port.TestFlightBuild
	found := false
	for _, row := range page.Data {
		var uploaded time.Time
		if row.Attributes.UploadedDate != "" {
			t, err := parseASCTime(row.Attributes.UploadedDate)
			if err != nil {
				return port.TestFlightBuild{}, false, err
			}
			uploaded = t
		}
		if found && !uploaded.After(best.UploadedAt) {
			continue
		}
		b := port.TestFlightBuild{
			ID:                      row.ID,
			BuildNumber:             row.Attributes.Version,
			ProcessingState:         row.Attributes.ProcessingState,
			Expired:                 row.Attributes.Expired,
			UsesNonExemptEncryption: row.Attributes.UsesNonExemptEncryption,
			UploadedAt:              uploaded,
		}
		if ref := row.Relationships.PreReleaseVersion.Data; ref != nil {
			b.MarketingVersion = marketing[ref.ID]
		}
		best, found = b, true
	}
	return best, found, nil
}

type betaLocalizationRow struct {
	ID         string `json:"id"`
	Attributes struct {
		Locale string `json:"locale"`
	} `json:"attributes"`
}

func (c *Client) SetWhatToTest(ctx context.Context, buildID, locale, text string) error {
	text = truncateRunes(text, ascWhatsNewMaxRunes)
	locs, err := listAll[betaLocalizationRow](ctx, c, "/v1/builds/"+url.PathEscape(buildID)+"/betaBuildLocalizations")
	if err != nil {
		return fmt.Errorf("appstore: listing what-to-test localizations of build %s: %w", buildID, err)
	}

	for _, loc := range locs {
		if !strings.EqualFold(loc.Attributes.Locale, locale) {
			continue
		}
		body := map[string]any{
			"data": map[string]any{
				"type":       "betaBuildLocalizations",
				"id":         loc.ID,
				"attributes": map[string]any{"whatsNew": text},
			},
		}
		if err := c.do(ctx, http.MethodPatch, "/v1/betaBuildLocalizations/"+url.PathEscape(loc.ID), body, nil); err != nil {
			return fmt.Errorf("appstore: updating what to test of build %s (%s): %w", buildID, locale, err)
		}
		return nil
	}

	body := map[string]any{
		"data": map[string]any{
			"type": "betaBuildLocalizations",
			"attributes": map[string]any{
				"locale":   locale,
				"whatsNew": text,
			},
			"relationships": map[string]any{
				"build": map[string]any{
					"data": relationshipRef{Type: "builds", ID: buildID},
				},
			},
		},
	}
	if err := c.do(ctx, http.MethodPost, "/v1/betaBuildLocalizations", body, nil); err != nil {
		return fmt.Errorf("appstore: creating what to test of build %s (%s): %w", buildID, locale, err)
	}
	return nil
}

func truncateRunes(s string, n int) string {
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}

func (c *Client) DeclareEncryption(ctx context.Context, buildID string, usesNonExemptEncryption bool) error {
	body := map[string]any{
		"data": map[string]any{
			"type":       "builds",
			"id":         buildID,
			"attributes": map[string]any{"usesNonExemptEncryption": usesNonExemptEncryption},
		},
	}
	if err := c.do(ctx, http.MethodPatch, "/v1/builds/"+url.PathEscape(buildID), body, nil); err != nil {
		return fmt.Errorf("appstore: declaring export compliance of build %s: %w", buildID, err)
	}
	return nil
}

type betaGroupRow struct {
	ID         string `json:"id"`
	Attributes struct {
		Name                 string `json:"name"`
		IsInternalGroup      bool   `json:"isInternalGroup"`
		HasAccessToAllBuilds bool   `json:"hasAccessToAllBuilds"`
		PublicLinkEnabled    bool   `json:"publicLinkEnabled"`
		PublicLink           string `json:"publicLink"`
	} `json:"attributes"`
}

func (g betaGroupRow) toPort(testerCount int) port.BetaGroup {
	out := port.BetaGroup{
		ID:          g.ID,
		Name:        g.Attributes.Name,
		Internal:    g.Attributes.IsInternalGroup,
		AllBuilds:   g.Attributes.HasAccessToAllBuilds,
		TesterCount: testerCount,
	}
	if g.Attributes.PublicLinkEnabled {
		out.PublicLink = g.Attributes.PublicLink
	}
	return out
}

func (c *Client) BetaGroups(ctx context.Context, appID string) ([]port.BetaGroup, error) {
	rows, err := listAll[betaGroupRow](ctx, c, "/v1/apps/"+url.PathEscape(appID)+"/betaGroups?limit=200")
	if err != nil {
		return nil, fmt.Errorf("appstore: listing beta groups of app %s: %w", appID, err)
	}
	groups := make([]port.BetaGroup, 0, len(rows))
	for _, row := range rows {
		groups = append(groups, row.toPort(c.groupTesterCount(ctx, row.ID)))
	}
	slices.SortStableFunc(groups, func(a, b port.BetaGroup) int {
		if a.Internal != b.Internal {
			if a.Internal {
				return -1
			}
			return 1
		}
		return cmp.Compare(a.Name, b.Name)
	})
	return groups, nil
}

func (c *Client) groupTesterCount(ctx context.Context, groupID string) int {
	var resp struct {
		Meta struct {
			Paging struct {
				Total *int `json:"total"`
			} `json:"paging"`
		} `json:"meta"`
	}
	path := "/v1/betaGroups/" + url.PathEscape(groupID) + "/betaTesters?limit=1"
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil || resp.Meta.Paging.Total == nil {
		return -1
	}
	return *resp.Meta.Paging.Total
}

func (c *Client) CreateBetaGroup(ctx context.Context, appID, name string, internal bool) (port.BetaGroup, error) {
	body := map[string]any{
		"data": map[string]any{
			"type": "betaGroups",
			"attributes": map[string]any{
				"name":            name,
				"isInternalGroup": internal,
			},
			"relationships": map[string]any{
				"app": map[string]any{
					"data": relationshipRef{Type: "apps", ID: appID},
				},
			},
		},
	}
	var resp struct {
		Data betaGroupRow `json:"data"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/betaGroups", body, &resp); err != nil {
		return port.BetaGroup{}, fmt.Errorf("appstore: creating beta group %q for app %s: %w", name, appID, err)
	}
	return resp.Data.toPort(0), nil
}

func (c *Client) BuildGroupIDs(ctx context.Context, buildID string) ([]string, error) {
	refs, err := listAll[relationshipRef](ctx, c, "/v1/builds/"+url.PathEscape(buildID)+"/relationships/betaGroups")
	if err != nil {
		return nil, fmt.Errorf("appstore: listing beta groups of build %s: %w", buildID, err)
	}
	ids := make([]string, 0, len(refs))
	for _, ref := range refs {
		ids = append(ids, ref.ID)
	}
	return ids, nil
}

func relationshipList(typ string, ids []string) map[string]any {
	refs := make([]relationshipRef, 0, len(ids))
	for _, id := range ids {
		refs = append(refs, relationshipRef{Type: typ, ID: id})
	}
	return map[string]any{"data": refs}
}

func (c *Client) AddBuildToGroups(ctx context.Context, buildID string, groupIDs []string) error {
	if len(groupIDs) == 0 {
		return nil
	}
	path := "/v1/builds/" + url.PathEscape(buildID) + "/relationships/betaGroups"
	if err := c.do(ctx, http.MethodPost, path, relationshipList("betaGroups", groupIDs), nil); err != nil {
		return fmt.Errorf("appstore: adding build %s to beta groups: %w", buildID, err)
	}
	return nil
}

func (c *Client) RemoveBuildFromGroups(ctx context.Context, buildID string, groupIDs []string) error {
	if len(groupIDs) == 0 {
		return nil
	}
	path := "/v1/builds/" + url.PathEscape(buildID) + "/relationships/betaGroups"
	if err := c.do(ctx, http.MethodDelete, path, relationshipList("betaGroups", groupIDs), nil); err != nil {
		return fmt.Errorf("appstore: removing build %s from beta groups: %w", buildID, err)
	}
	return nil
}

// SubmitForBetaReview settles a 409 by asking for the build's own review
// state rather than reading the error text: a conflict can also be about
// another build, and Apple's wording is not a contract.
func (c *Client) SubmitForBetaReview(ctx context.Context, buildID string) error {
	body := map[string]any{
		"data": map[string]any{
			"type": "betaAppReviewSubmissions",
			"relationships": map[string]any{
				"build": map[string]any{
					"data": relationshipRef{Type: "builds", ID: buildID},
				},
			},
		},
	}
	err := c.do(ctx, http.MethodPost, "/v1/betaAppReviewSubmissions", body, nil)
	if err == nil {
		return nil
	}
	if isConflict(err) {
		if state, stateErr := c.BetaReviewState(ctx, buildID); stateErr == nil && state != "" {
			return nil
		}
	}
	return fmt.Errorf("appstore: submitting build %s for beta review: %w", buildID, err)
}

func (c *Client) BetaReviewState(ctx context.Context, buildID string) (string, error) {
	var resp struct {
		Data *struct {
			Attributes struct {
				BetaReviewState string `json:"betaReviewState"`
			} `json:"attributes"`
		} `json:"data"`
	}
	path := "/v1/builds/" + url.PathEscape(buildID) + "/betaAppReviewSubmission"
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		if isNotFound(err) {
			return "", nil
		}
		return "", fmt.Errorf("appstore: reading beta review state of build %s: %w", buildID, err)
	}
	if resp.Data == nil {
		return "", nil
	}
	return resp.Data.Attributes.BetaReviewState, nil
}

type betaTesterRow struct {
	ID         string `json:"id"`
	Attributes struct {
		Email      string `json:"email"`
		FirstName  string `json:"firstName"`
		LastName   string `json:"lastName"`
		State      string `json:"state"`
		InviteType string `json:"inviteType"`
	} `json:"attributes"`
}

func (t betaTesterRow) toPort() port.BetaTester {
	state := t.Attributes.State
	if state == "" {
		state = t.Attributes.InviteType
	}
	return port.BetaTester{
		ID:        t.ID,
		Email:     t.Attributes.Email,
		FirstName: t.Attributes.FirstName,
		LastName:  t.Attributes.LastName,
		State:     state,
	}
}

func (c *Client) GroupTesters(ctx context.Context, groupID string) ([]port.BetaTester, error) {
	rows, err := listAll[betaTesterRow](ctx, c, "/v1/betaGroups/"+url.PathEscape(groupID)+"/betaTesters?limit=200")
	if err != nil {
		return nil, fmt.Errorf("appstore: listing testers of beta group %s: %w", groupID, err)
	}
	testers := make([]port.BetaTester, 0, len(rows))
	for _, row := range rows {
		testers = append(testers, row.toPort())
	}
	return testers, nil
}

// AddTester links the existing tester on a 409: App Store Connect answers that
// when the email already belongs to a tester of the team, in whatever group.
func (c *Client) AddTester(ctx context.Context, groupID, email, firstName, lastName string) (port.BetaTester, error) {
	body := map[string]any{
		"data": map[string]any{
			"type": "betaTesters",
			"attributes": map[string]any{
				"email":     email,
				"firstName": firstName,
				"lastName":  lastName,
			},
			"relationships": map[string]any{
				"betaGroups": relationshipList("betaGroups", []string{groupID}),
			},
		},
	}
	var resp struct {
		Data betaTesterRow `json:"data"`
	}
	err := c.do(ctx, http.MethodPost, "/v1/betaTesters", body, &resp)
	if err == nil {
		return resp.Data.toPort(), nil
	}
	if !isConflict(err) {
		return port.BetaTester{}, fmt.Errorf("appstore: inviting %s to beta group %s: %w", email, groupID, err)
	}

	var existing jsonAPIPage[betaTesterRow]
	if lookupErr := c.do(ctx, http.MethodGet, "/v1/betaTesters?filter[email]="+url.QueryEscape(email)+"&limit=1", nil, &existing); lookupErr != nil {
		return port.BetaTester{}, fmt.Errorf("appstore: looking up existing tester %s: %w", email, lookupErr)
	}
	if len(existing.Data) == 0 {
		return port.BetaTester{}, fmt.Errorf("appstore: inviting %s to beta group %s: %w", email, groupID, err)
	}
	tester := existing.Data[0]
	path := "/v1/betaGroups/" + url.PathEscape(groupID) + "/relationships/betaTesters"
	if err := c.do(ctx, http.MethodPost, path, relationshipList("betaTesters", []string{tester.ID}), nil); err != nil {
		return port.BetaTester{}, fmt.Errorf("appstore: adding existing tester %s to beta group %s: %w", email, groupID, err)
	}
	return tester.toPort(), nil
}

func (c *Client) RemoveTester(ctx context.Context, groupID, testerID string) error {
	path := "/v1/betaGroups/" + url.PathEscape(groupID) + "/relationships/betaTesters"
	if err := c.do(ctx, http.MethodDelete, path, relationshipList("betaTesters", []string{testerID}), nil); err != nil {
		return fmt.Errorf("appstore: removing tester %s from beta group %s: %w", testerID, groupID, err)
	}
	return nil
}

func isConflict(err error) bool {
	var apiErr *apiError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusConflict
}
