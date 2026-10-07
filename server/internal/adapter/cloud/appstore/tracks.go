package appstore

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type appListingPage struct {
	Data []struct {
		ID         string `json:"id"`
		Attributes struct {
			Name     string `json:"name"`
			BundleID string `json:"bundleId"`
		} `json:"attributes"`
		Relationships struct {
			AppStoreVersions struct {
				Data []struct {
					ID string `json:"id"`
				} `json:"data"`
				Meta struct {
					Paging struct {
						Total *int `json:"total"`
					} `json:"paging"`
				} `json:"meta"`
			} `json:"appStoreVersions"`
		} `json:"relationships"`
	} `json:"data"`
	Included []struct {
		Type       string `json:"type"`
		ID         string `json:"id"`
		Attributes struct {
			VersionString   string `json:"versionString"`
			AppStoreState   string `json:"appStoreState"`
			AppVersionState string `json:"appVersionState"`
			Platform        string `json:"platform"`
		} `json:"attributes"`
	} `json:"included"`
	Links struct {
		Next string `json:"next"`
	} `json:"links"`
}

const ascIncludedVersionsLimit = 50

func (c *Client) ListApps(ctx context.Context) ([]port.StoreAppRef, error) {
	var refs []port.StoreAppRef
	path := "/v1/apps?include=appStoreVersions&fields[appStoreVersions]=versionString,appStoreState,appVersionState,platform" +
		"&limit[appStoreVersions]=" + strconv.Itoa(ascIncludedVersionsLimit)
	for page := 0; path != ""; page++ {
		if page >= ascMaxCollectionPages {
			return nil, fmt.Errorf("appstore: %s: still paging after %d pages, refusing to follow the cursor further", path, ascMaxCollectionPages)
		}
		var body appListingPage
		if err := c.do(ctx, http.MethodGet, path, nil, &body); err != nil {
			return nil, err
		}

		included := make(map[string]appStoreVersion, len(body.Included))
		for _, inc := range body.Included {
			if inc.Type != "appStoreVersions" {
				continue
			}
			var v appStoreVersion
			v.ID = inc.ID
			v.Attributes.VersionString = inc.Attributes.VersionString
			v.Attributes.AppStoreState = inc.Attributes.AppStoreState
			v.Attributes.AppVersionState = inc.Attributes.AppVersionState
			v.Attributes.Platform = inc.Attributes.Platform
			included[inc.ID] = v
		}

		for _, row := range body.Data {
			rel := row.Relationships.AppStoreVersions
			related := make([]appStoreVersion, 0, len(rel.Data))
			for _, ref := range rel.Data {
				if v, ok := included[ref.ID]; ok {
					related = append(related, v)
				}
			}
			related = iOSOnly(related)
			state := ""
			truncated := rel.Meta.Paging.Total != nil && *rel.Meta.Paging.Total > len(rel.Data)
			if live, found := liveVersion(related); found {
				state = live.state()
			} else if newest, found := newestVersion(related); found && !truncated {
				state = newest.state()
			}
			refs = append(refs, port.StoreAppRef{
				StoreAppID: row.ID,
				Identifier: row.Attributes.BundleID,
				Name:       row.Attributes.Name,
				State:      state,
			})
		}

		next, err := nextPagePath(body.Links.Next)
		if err != nil {
			return nil, err
		}
		path = next
	}
	return refs, nil
}

type betaGroup struct {
	ID         string `json:"id"`
	Attributes struct {
		IsInternalGroup bool `json:"isInternalGroup"`
	} `json:"attributes"`
}

type buildRow struct {
	ID         string `json:"id"`
	Attributes struct {
		Version      string `json:"version"`
		UploadedDate string `json:"uploadedDate"`
	} `json:"attributes"`
}

func (c *Client) Tracks(ctx context.Context, appID string) (domain.StoreTracks, error) {
	internalGroups, externalGroups, err := c.betaGroups(ctx, appID)
	if err != nil {
		return domain.StoreTracks{}, err
	}

	internal, err := c.betaTrack(ctx, appID, internalGroups, domain.StoreChannelInternal)
	if err != nil {
		return domain.StoreTracks{}, err
	}
	external, err := c.betaTrack(ctx, appID, externalGroups, domain.StoreChannelExternal)
	if err != nil {
		return domain.StoreTracks{}, err
	}
	production, err := c.productionTrack(ctx, appID)
	if err != nil {
		return domain.StoreTracks{}, err
	}
	return domain.StoreTracks{Internal: internal, External: external, Production: production}, nil
}

func (c *Client) betaGroups(ctx context.Context, appID string) (internal, external []betaGroup, err error) {
	groups, err := listAll[betaGroup](ctx, c, "/v1/apps/"+url.PathEscape(appID)+"/betaGroups")
	if err != nil {
		return nil, nil, err
	}
	for _, g := range groups {
		if g.Attributes.IsInternalGroup {
			internal = append(internal, g)
			continue
		}
		external = append(external, g)
	}
	return internal, external, nil
}

func emptyChannel() domain.TrackRelease {
	return domain.TrackRelease{Status: domain.TrackStatusNone}
}

func (c *Client) betaTrack(ctx context.Context, appID string, groups []betaGroup, channel string) (domain.TrackRelease, error) {
	if len(groups) == 0 {
		return emptyChannel(), nil
	}
	build, found, err := c.newestGroupBuild(ctx, appID, groups)
	if err != nil {
		return domain.TrackRelease{}, err
	}
	if !found {
		return emptyChannel(), nil
	}
	version, err := c.buildMarketingVersion(ctx, build.ID)
	if err != nil {
		return domain.TrackRelease{}, err
	}

	status := domain.TrackStatusLive
	if channel == domain.StoreChannelExternal {
		status, err = c.betaReviewStatus(ctx, build.ID)
		if err != nil {
			return domain.TrackRelease{}, err
		}
	}

	rel := domain.TrackRelease{
		HasRelease: true,
		Version:    version,
		Build:      build.Attributes.Version,
		Status:     status,
		Audience:   c.betaAudience(ctx, groups, channel),
	}
	if build.Attributes.UploadedDate != "" {
		uploaded, err := parseASCTime(build.Attributes.UploadedDate)
		if err != nil {
			return domain.TrackRelease{}, err
		}
		rel.UpdatedAt = &uploaded
	}
	return rel, nil
}

func (c *Client) newestGroupBuild(ctx context.Context, appID string, groups []betaGroup) (buildRow, bool, error) {
	if len(groups) == 0 {
		return buildRow{}, false, nil
	}
	limit := "1"
	if c.skipBuild != nil {
		limit = strconv.Itoa(skippedBuildsWindow)
	}
	path := "/v1/builds?filter[app]=" + url.QueryEscape(appID) +
		"&filter[betaGroups]=" + url.QueryEscape(groupIDs(groups)) +
		"&sort=-uploadedDate&limit=" + limit
	var page jsonAPIPage[buildRow]
	if err := c.do(ctx, http.MethodGet, path, nil, &page); err != nil {
		return buildRow{}, false, err
	}
	for _, b := range page.Data {
		if c.skipBuild != nil && c.skipBuild(b.Attributes.Version) {
			continue
		}
		return b, true, nil
	}
	return buildRow{}, false, nil
}

// skippedBuildsWindow bounds how far past the newest uploads the release
// channel looks for one that is not a test build.
const skippedBuildsWindow = 50

func groupIDs(groups []betaGroup) string {
	ids := make([]string, 0, len(groups))
	for _, g := range groups {
		ids = append(ids, g.ID)
	}
	return strings.Join(ids, ",")
}

func (c *Client) buildMarketingVersion(ctx context.Context, buildID string) (string, error) {
	var resp struct {
		Data struct {
			Attributes struct {
				Version string `json:"version"`
			} `json:"attributes"`
		} `json:"data"`
	}
	path := "/v1/builds/" + url.PathEscape(buildID) + "/preReleaseVersion"
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		if isNotFound(err) {
			return "", nil
		}
		return "", err
	}
	return resp.Data.Attributes.Version, nil
}

func (c *Client) betaReviewStatus(ctx context.Context, buildID string) (string, error) {
	var resp struct {
		Data struct {
			Attributes struct {
				BetaReviewState string `json:"betaReviewState"`
			} `json:"attributes"`
		} `json:"data"`
	}
	path := "/v1/builds/" + url.PathEscape(buildID) + "/betaAppReviewSubmission"
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		if isNotFound(err) {
			return domain.TrackStatusDraft, nil
		}
		return "", err
	}
	switch resp.Data.Attributes.BetaReviewState {
	case "WAITING_FOR_REVIEW", "IN_REVIEW":
		return domain.TrackStatusInReview, nil
	case "APPROVED":
		return domain.TrackStatusLive, nil
	}
	return domain.TrackStatusDraft, nil
}

func (c *Client) betaAudience(ctx context.Context, groups []betaGroup, channel string) string {
	if total, ok := c.betaTesterCount(ctx, groups); ok {
		return pluralize(total, channel+" tester")
	}
	return pluralize(len(groups), "group")
}

func (c *Client) betaTesterCount(ctx context.Context, groups []betaGroup) (int, bool) {
	var resp struct {
		Meta struct {
			Paging struct {
				Total *int `json:"total"`
			} `json:"paging"`
		} `json:"meta"`
	}
	path := "/v1/betaTesters?limit=1&filter[betaGroups]=" + url.QueryEscape(groupIDs(groups))
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return 0, false
	}
	if resp.Meta.Paging.Total == nil {
		return 0, false
	}
	return *resp.Meta.Paging.Total, true
}

func pluralize(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

var approvedAppStoreStates = map[string]bool{
	"PENDING_APPLE_RELEASE":     true,
	"PENDING_DEVELOPER_RELEASE": true,
}

var inReviewAppStoreStates = map[string]bool{
	"IN_REVIEW":          true,
	"WAITING_FOR_REVIEW": true,
}

// retiredAppStoreStates never describe what the channel holds next: a replaced
// version is history, and a removed one is not coming back on its own.
var retiredAppStoreStates = map[string]bool{
	stateReadyForSale:             true,
	"REPLACED_WITH_NEW_VERSION":   true,
	"REMOVED_FROM_SALE":           true,
	"DEVELOPER_REMOVED_FROM_SALE": true,
}

// productionVersion prefers what customers have over what is coming: the
// version on sale, then an approved one, then one in review, then the newest.
// Ranking "approved" with "on sale" made a live app's channel read as draft
// the moment its next version cleared review.
func productionVersion(versions []appStoreVersion) (appStoreVersion, bool) {
	if v, found := liveVersion(versions); found {
		return v, true
	}
	for _, tier := range []map[string]bool{approvedAppStoreStates, inReviewAppStoreStates} {
		matching := make([]appStoreVersion, 0, len(versions))
		for _, v := range versions {
			if tier[v.state()] {
				matching = append(matching, v)
			}
		}
		if v, found := newestVersion(matching); found {
			return v, true
		}
	}
	return newestVersion(versions)
}

// pendingVersion is the newest version above the shown one that is still on
// its way to customers.
func pendingVersion(versions []appStoreVersion, shown appStoreVersion) (appStoreVersion, bool) {
	ahead := make([]appStoreVersion, 0, len(versions))
	for _, v := range versions {
		if v.ID != shown.ID && !retiredAppStoreStates[v.state()] &&
			versionLess(shown.Attributes.VersionString, v.Attributes.VersionString) {
			ahead = append(ahead, v)
		}
	}
	return newestVersion(ahead)
}

func (c *Client) productionTrack(ctx context.Context, appID string) (domain.TrackRelease, error) {
	versions, err := c.listAppStoreVersions(ctx, appID)
	if err != nil {
		return domain.TrackRelease{}, err
	}
	version, found := productionVersion(versions)
	if !found {
		return emptyChannel(), nil
	}

	rel := domain.TrackRelease{
		HasRelease: true,
		Version:    version.Attributes.VersionString,
		Build:      c.versionBuildNumber(ctx, version.ID),
		Status:     normalizeAppStoreState(version.state()),
	}
	if pending, found := pendingVersion(versions, version); found {
		rel.PendingVersion = pending.Attributes.VersionString
		rel.PendingStatus = normalizeAppStoreState(pending.state())
	}
	if version.Attributes.CreatedDate != "" {
		created, err := parseASCTime(version.Attributes.CreatedDate)
		if err != nil {
			return domain.TrackRelease{}, err
		}
		rel.UpdatedAt = &created
	}

	phased, err := c.phasedRelease(ctx, version.ID)
	if err != nil {
		return domain.TrackRelease{}, err
	}

	switch phased.State {
	case "ACTIVE":
		rel.Status = domain.TrackStatusRollingOut
		rel.UserFraction = phasedFraction(phased.Day)
	case "PAUSED":
		rel.Status = domain.TrackStatusHalted
		rel.UserFraction = phasedFraction(phased.Day)
	}
	return rel, nil
}

func (c *Client) versionBuildNumber(ctx context.Context, versionID string) string {
	var resp struct {
		Data struct {
			Attributes struct {
				Version string `json:"version"`
			} `json:"attributes"`
		} `json:"data"`
	}
	path := "/v1/appStoreVersions/" + url.PathEscape(versionID) + "/build"
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return ""
	}
	return resp.Data.Attributes.Version
}

func normalizeAppStoreState(state string) string {
	switch state {
	case "READY_FOR_SALE":
		return domain.TrackStatusLive
	case "WAITING_FOR_REVIEW", "IN_REVIEW", "PENDING_APPLE_RELEASE":
		return domain.TrackStatusInReview
	case "PREPARE_FOR_SUBMISSION", "DEVELOPER_REJECTED", "PENDING_DEVELOPER_RELEASE":
		return domain.TrackStatusDraft
	}
	return domain.TrackStatusDraft
}

var phasedReleaseDayFractions = []float64{0.01, 0.02, 0.05, 0.10, 0.20, 0.50, 1.00}

type phasedReleaseInfo struct {
	State string
	Day   int
}

func (c *Client) phasedRelease(ctx context.Context, versionID string) (phasedReleaseInfo, error) {
	var resp struct {
		Data struct {
			Attributes struct {
				PhasedReleaseState string `json:"phasedReleaseState"`
				CurrentDayNumber   int    `json:"currentDayNumber"`
			} `json:"attributes"`
		} `json:"data"`
	}
	path := "/v1/appStoreVersions/" + url.PathEscape(versionID) + "/appStoreVersionPhasedRelease"
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		if isNotFound(err) {
			return phasedReleaseInfo{}, nil
		}
		return phasedReleaseInfo{}, err
	}
	return phasedReleaseInfo{
		State: resp.Data.Attributes.PhasedReleaseState,
		Day:   resp.Data.Attributes.CurrentDayNumber,
	}, nil
}

func phasedFraction(day int) float64 {
	if day < 1 {
		return 0
	}
	if day > len(phasedReleaseDayFractions) {
		return 1
	}
	return phasedReleaseDayFractions[day-1]
}

func (c *Client) PromoteChannel(ctx context.Context, appID, from, to string) error {
	if next, ok := domain.NextChannel(from); !ok || next != to {
		return fmt.Errorf("appstore: cannot promote %q -> %q: the only promotions are internal -> external and external -> production", from, to)
	}

	internalGroups, externalGroups, err := c.betaGroups(ctx, appID)
	if err != nil {
		return err
	}

	if from == domain.StoreChannelInternal {
		if len(externalGroups) == 0 {
			return fmt.Errorf("appstore: app %s has no external TestFlight group to promote into", appID)
		}
		build, found, err := c.newestGroupBuild(ctx, appID, internalGroups)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("appstore: app %s has no build on the internal channel to promote", appID)
		}

		body := map[string]any{
			"data": []map[string]any{
				{"type": "builds", "id": build.ID},
			},
		}
		for _, g := range externalGroups {
			path := "/v1/betaGroups/" + url.PathEscape(g.ID) + "/relationships/builds"
			if err := c.do(ctx, http.MethodPost, path, body, nil); err != nil {
				return err
			}
		}
		return nil
	}

	build, found, err := c.newestGroupBuild(ctx, appID, externalGroups)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("appstore: app %s has no build on the external channel to promote", appID)
	}
	version, err := c.buildMarketingVersion(ctx, build.ID)
	if err != nil {
		return err
	}
	if version == "" {
		return fmt.Errorf("appstore: build %s carries no marketing version to submit for review", build.ID)
	}
	return c.submitVersionForReview(ctx, appID, version, build.ID)
}

func isNotFound(err error) bool {
	var apiErr *apiError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound
}
