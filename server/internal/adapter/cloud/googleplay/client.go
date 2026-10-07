package googleplay

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const defaultBaseURL = "https://androidpublisher.googleapis.com"


const defaultReportingBaseURL = "https://playdeveloperreporting.googleapis.com"

const defaultTokenURL = "https://oauth2.googleapis.com/token"

const tokenRefreshMargin = 30 * time.Second

type cachedToken struct {
	token string
	exp   time.Time
}

type Client struct {
	clientEmail string
	privKey     *rsa.PrivateKey

	baseURL          string
	reportingBaseURL string
	tokenURL         string
	httpClient       *http.Client

	mu     sync.Mutex
	tokens map[string]cachedToken
}

var _ port.GooglePlayClient = (*Client)(nil)

func New(cred domain.StoreCredential) (*Client, error) {
	raw := cred.Data["service_account_json"]
	if raw == "" {
		return nil, errors.New("googleplay: credential missing service_account_json")
	}
	clientEmail, priv, err := parseServiceAccountJSON(raw)
	if err != nil {
		return nil, err
	}
	return &Client{
		clientEmail:      clientEmail,
		privKey:          priv,
		baseURL:          defaultBaseURL,
		reportingBaseURL: defaultReportingBaseURL,
		tokenURL:         defaultTokenURL,
		httpClient:       &http.Client{Timeout: 30 * time.Second},
		tokens:           make(map[string]cachedToken, 2),
	}, nil
}

func (c *Client) SetBaseURL(u string) {
	c.baseURL = u
}

func (c *Client) SetReportingBaseURL(u string) {
	c.reportingBaseURL = u
}

func (c *Client) SetTokenURL(u string) {
	c.tokenURL = u
}

func (c *Client) bearerToken(ctx context.Context, scope string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cached, ok := c.tokens[scope]; ok && time.Now().Before(cached.exp.Add(-tokenRefreshMargin)) {
		return cached.token, nil
	}
	tok, exp, err := fetchAccessToken(ctx, c.httpClient, c.tokenURL, c.clientEmail, scope, c.privKey)
	if err != nil {
		return "", err
	}
	c.tokens[scope] = cachedToken{token: tok, exp: exp}
	return tok, nil
}

type apiError struct {
	Status int
	Body   string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("googleplay api: %d %s", e.Status, e.Body)
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	return c.doAt(ctx, c.baseURL, androidPublisherScope, method, path, body, out)
}

func (c *Client) doAt(ctx context.Context, baseURL, scope, method, path string, body, out any) error {
	tok, err := c.bearerToken(ctx, scope)
	if err != nil {
		return err
	}

	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet := domain.TruncateHead(string(data), 500)
		return &apiError{Status: resp.StatusCode, Body: snippet}
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

func (c *Client) ValidateAuth(ctx context.Context) error {
	_, err := c.bearerToken(ctx, androidPublisherScope)
	return err
}

var (
	errAppNotFound  = errors.New("googleplay: app not found")
	errAppForbidden = errors.New("googleplay: this credential cannot access the app")
)

func (c *Client) insertEdit(ctx context.Context, packageName string) (editID string, err error) {
	var resp struct {
		ID string `json:"id"`
	}
	path := "/androidpublisher/v3/applications/" + url.PathEscape(packageName) + "/edits"
	if err := c.do(ctx, http.MethodPost, path, nil, &resp); err != nil {
		var apiErr *apiError
		if errors.As(err, &apiErr) {
			switch apiErr.Status {
			case http.StatusNotFound:
				return "", fmt.Errorf("%w: %q", errAppNotFound, packageName)
			case http.StatusForbidden:
				return "", fmt.Errorf("%w: %q", errAppForbidden, packageName)
			}
		}
		return "", err
	}
	return resp.ID, nil
}

func (c *Client) deleteEdit(ctx context.Context, packageName, editID string) {
	path := "/androidpublisher/v3/applications/" + url.PathEscape(packageName) + "/edits/" + url.PathEscape(editID)
	if err := c.do(ctx, http.MethodDelete, path, nil, nil); err != nil {
		log.Warn().Err(err).Str("package", packageName).Msg("googleplay: discarding probe edit failed")
	}
}

func (c *Client) AppExists(ctx context.Context, packageName string) (bool, error) {
	editID, err := c.insertEdit(ctx, packageName)
	if err != nil {

		if errors.Is(err, errAppNotFound) || errors.Is(err, errAppForbidden) {
			return false, nil
		}
		return false, err
	}
	defer c.deleteEdit(ctx, packageName, editID)
	return true, nil
}

func (c *Client) TrackInfo(ctx context.Context, packageName, track string) (port.PlayTrackInfo, error) {
	editID, err := c.insertEdit(ctx, packageName)
	if err != nil {
		return port.PlayTrackInfo{}, err
	}
	defer c.deleteEdit(ctx, packageName, editID)

	releases, err := c.trackReleases(ctx, packageName, editID, track)
	if err != nil {
		return port.PlayTrackInfo{}, err
	}
	// A staged rollout leaves the previous completed release on the track next
	// to the new one, in no promised order; the newest versionCode is the one
	// the track is about.
	r, ok := newestRelease(releases)
	if !ok {
		return port.PlayTrackInfo{}, nil
	}
	return port.PlayTrackInfo{
		HasRelease:   true,
		VersionName:  r.Name,
		Status:       r.Status,
		UserFraction: r.UserFraction,
	}, nil
}

func (c *Client) trackReleases(ctx context.Context, packageName, editID, track string) ([]playRelease, error) {
	var resp struct {
		Releases []playRelease `json:"releases"`
	}
	path := "/androidpublisher/v3/applications/" + url.PathEscape(packageName) + "/edits/" + url.PathEscape(editID) + "/tracks/" + url.PathEscape(track)
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Releases, nil
}

// LiveVersion is the production release users can install. A production track
// holding only a draft has never reached anyone, so it is not live.
func (c *Client) LiveVersion(ctx context.Context, packageName string) (string, bool, error) {
	editID, err := c.insertEdit(ctx, packageName)
	if err != nil {
		return "", false, err
	}
	defer c.deleteEdit(ctx, packageName, editID)

	releases, err := c.trackReleases(ctx, packageName, editID, trackProduction)
	if err != nil {
		return "", false, err
	}
	published := make([]playRelease, 0, len(releases))
	for _, r := range releases {
		switch r.Status {
		case "completed", "inProgress", "halted":
			published = append(published, r)
		}
	}
	r, ok := newestRelease(published)
	if !ok {
		return "", false, nil
	}
	return r.Name, true, nil
}

func (c *Client) withEdit(ctx context.Context, packageName string, fn func(editID string) error) error {
	editID, err := c.insertEdit(ctx, packageName)
	if err != nil {
		return err
	}
	if err := fn(editID); err != nil {
		c.deleteEdit(ctx, packageName, editID)
		return err
	}
	path := "/androidpublisher/v3/applications/" + url.PathEscape(packageName) + "/edits/" + url.PathEscape(editID) + ":commit"
	return c.do(ctx, http.MethodPost, path, nil, nil)
}

func releasePayload(name string, versionCodes []string, fraction float64, halted bool) map[string]any {
	rel := map[string]any{"name": name, "versionCodes": versionCodes}
	switch {
	case halted:
		rel["status"] = "halted"
		if fraction > 0 && fraction < 1 {
			rel["userFraction"] = fraction
		}
	case fraction >= 1 || fraction <= 0:
		rel["status"] = "completed"
	default:
		rel["status"] = "inProgress"
		rel["userFraction"] = fraction
	}
	return map[string]any{"releases": []any{rel}}
}

type trackRelease struct {
	Name         string
	VersionCodes []string
	Status       string
	UserFraction float64
}

func (c *Client) getTrackRelease(ctx context.Context, packageName, editID, track string) (*trackRelease, error) {
	var resp struct {
		Releases []struct {
			Name         string   `json:"name"`
			Status       string   `json:"status"`
			UserFraction float64  `json:"userFraction"`
			VersionCodes []string `json:"versionCodes"`
		} `json:"releases"`
	}
	path := "/androidpublisher/v3/applications/" + url.PathEscape(packageName) + "/edits/" + url.PathEscape(editID) + "/tracks/" + url.PathEscape(track)
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	if len(resp.Releases) == 0 {
		return nil, nil
	}
	r := resp.Releases[0]
	return &trackRelease{Name: r.Name, VersionCodes: r.VersionCodes, Status: r.Status, UserFraction: r.UserFraction}, nil
}

func (c *Client) putTrack(ctx context.Context, packageName, editID, track string, payload map[string]any) error {
	path := "/androidpublisher/v3/applications/" + url.PathEscape(packageName) + "/edits/" + url.PathEscape(editID) + "/tracks/" + url.PathEscape(track)
	return c.do(ctx, http.MethodPut, path, payload, nil)
}

func (c *Client) PromoteTrack(ctx context.Context, packageName, fromTrack, toTrack string, userFraction float64) error {
	return c.withEdit(ctx, packageName, func(editID string) error {
		src, err := c.getTrackRelease(ctx, packageName, editID, fromTrack)
		if err != nil {
			return err
		}
		if src == nil {
			return fmt.Errorf("googleplay: track %q has no release to promote", fromTrack)
		}
		payload := releasePayload(src.Name, src.VersionCodes, userFraction, false)
		return c.putTrack(ctx, packageName, editID, toTrack, payload)
	})
}

func (c *Client) SetRolloutFraction(ctx context.Context, packageName, track string, userFraction float64) error {
	return c.withEdit(ctx, packageName, func(editID string) error {
		rel, err := c.getTrackRelease(ctx, packageName, editID, track)
		if err != nil {
			return err
		}
		if rel == nil {
			return fmt.Errorf("googleplay: track %q has no release", track)
		}
		payload := releasePayload(rel.Name, rel.VersionCodes, userFraction, false)
		return c.putTrack(ctx, packageName, editID, track, payload)
	})
}

func (c *Client) HaltRollout(ctx context.Context, packageName, track string) error {
	return c.withEdit(ctx, packageName, func(editID string) error {
		rel, err := c.getTrackRelease(ctx, packageName, editID, track)
		if err != nil {
			return err
		}
		if rel == nil {
			return fmt.Errorf("googleplay: track %q has no release to halt", track)
		}
		payload := releasePayload(rel.Name, rel.VersionCodes, rel.UserFraction, true)
		return c.putTrack(ctx, packageName, editID, track, payload)
	})
}

func (c *Client) ResumeRollout(ctx context.Context, packageName, track string) error {
	return c.withEdit(ctx, packageName, func(editID string) error {
		rel, err := c.getTrackRelease(ctx, packageName, editID, track)
		if err != nil {
			return err
		}
		if rel == nil {
			return fmt.Errorf("googleplay: track %q has no release to resume", track)
		}
		payload := releasePayload(rel.Name, rel.VersionCodes, rel.UserFraction, false)
		return c.putTrack(ctx, packageName, editID, track, payload)
	})
}

const (
	trackInternal   = "internal"
	trackAlpha      = "alpha"
	trackBeta       = "beta"
	trackProduction = "production"
)

const (
	audienceOpenTesting   = "Open testing"
	audienceClosedTesting = "Closed testing"
)

const listAppsPageSize = 100

const listAppsMaxPages = 50

func (c *Client) ListApps(ctx context.Context) ([]port.StoreAppRef, error) {
	if _, err := c.bearerToken(ctx, playReportingScope); err != nil {
		return nil, fmt.Errorf("googleplay: play developer reporting scope: %v: %w", err, port.ErrAppListingUnavailable)
	}

	var apps []port.StoreAppRef
	pageToken := ""
	for page := 0; ; page++ {
		if page >= listAppsMaxPages {
			return nil, fmt.Errorf("googleplay: apps:search: still paging after %d pages, refusing to follow the cursor further", listAppsMaxPages)
		}

		path := fmt.Sprintf("/v1beta1/apps:search?pageSize=%d", listAppsPageSize)
		if pageToken != "" {
			path += "&pageToken=" + url.QueryEscape(pageToken)
		}

		var resp struct {
			Apps []struct {
				PackageName string `json:"packageName"`
				DisplayName string `json:"displayName"`
			} `json:"apps"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := c.doAt(ctx, c.reportingBaseURL, playReportingScope, http.MethodGet, path, nil, &resp); err != nil {
			var apiErr *apiError
			if errors.As(err, &apiErr) && (apiErr.Status == http.StatusForbidden || apiErr.Status == http.StatusNotFound) {
				return nil, fmt.Errorf("googleplay: play developer reporting api: %w", port.ErrAppListingUnavailable)
			}
			return nil, err
		}

		for _, a := range resp.Apps {
			if a.PackageName == "" {
				continue
			}
			apps = append(apps, port.StoreAppRef{Identifier: a.PackageName, Name: a.DisplayName})
		}

		if resp.NextPageToken == "" || resp.NextPageToken == pageToken {
			return apps, nil
		}
		pageToken = resp.NextPageToken
	}
}

type playRelease struct {
	Name         string   `json:"name"`
	Status       string   `json:"status"`
	UserFraction float64  `json:"userFraction"`
	VersionCodes []string `json:"versionCodes"`
}

func (c *Client) Tracks(ctx context.Context, packageName string) (domain.StoreTracks, error) {
	editID, err := c.insertEdit(ctx, packageName)
	if err != nil {
		return domain.StoreTracks{}, err
	}
	defer c.deleteEdit(ctx, packageName, editID)

	var resp struct {
		Tracks []struct {
			Track    string        `json:"track"`
			Releases []playRelease `json:"releases"`
		} `json:"tracks"`
	}
	path := "/androidpublisher/v3/applications/" + url.PathEscape(packageName) + "/edits/" + url.PathEscape(editID) + "/tracks"
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return domain.StoreTracks{}, err
	}

	newest := make(map[string]playRelease, len(resp.Tracks))
	for _, t := range resp.Tracks {
		if rel, ok := newestRelease(t.Releases); ok {
			newest[t.Track] = rel
		}
	}

	tracks := domain.StoreTracks{
		Internal:   emptyChannel(),
		External:   externalChannel(newest),
		Production: emptyChannel(),
	}
	if rel, ok := newest[trackInternal]; ok {
		tracks.Internal = channelFrom(rel, "")
	}
	if rel, ok := newest[trackProduction]; ok {
		tracks.Production = channelFrom(rel, "")
	}
	return tracks, nil
}

func externalChannel(newest map[string]playRelease) domain.TrackRelease {
	beta, hasBeta := newest[trackBeta]
	alpha, hasAlpha := newest[trackAlpha]
	switch {
	case hasBeta && hasAlpha:
		_, betaCode := highestVersionCode(beta.VersionCodes)
		_, alphaCode := highestVersionCode(alpha.VersionCodes)
		if alphaCode > betaCode {
			return channelFrom(alpha, audienceClosedTesting)
		}
		return channelFrom(beta, audienceOpenTesting)
	case hasBeta:
		return channelFrom(beta, audienceOpenTesting)
	case hasAlpha:
		return channelFrom(alpha, audienceClosedTesting)
	}
	return emptyChannel()
}

func emptyChannel() domain.TrackRelease {
	return domain.TrackRelease{Status: domain.TrackStatusNone}
}

func channelFrom(rel playRelease, audience string) domain.TrackRelease {
	build, _ := highestVersionCode(rel.VersionCodes)
	return domain.TrackRelease{
		HasRelease:   true,
		Version:      rel.Name,
		Build:        build,
		Status:       normalizeTrackStatus(rel.Status),
		UserFraction: rel.UserFraction,
		Audience:     audience,
	}
}

func newestRelease(releases []playRelease) (playRelease, bool) {
	if len(releases) == 0 {
		return playRelease{}, false
	}
	best := releases[0]
	_, bestCode := highestVersionCode(best.VersionCodes)
	for _, rel := range releases[1:] {
		if _, code := highestVersionCode(rel.VersionCodes); code > bestCode {
			best, bestCode = rel, code
		}
	}
	return best, true
}

func highestVersionCode(codes []string) (string, int64) {
	best, bestCode := "", int64(-1)
	for _, c := range codes {
		code, err := strconv.ParseInt(c, 10, 64)
		if err != nil || code <= bestCode {
			continue
		}
		best, bestCode = c, code
	}
	return best, bestCode
}

func normalizeTrackStatus(status string) string {
	switch status {
	case "completed":
		return domain.TrackStatusLive
	case "inProgress":
		return domain.TrackStatusRollingOut
	case "halted":
		return domain.TrackStatusHalted
	case "draft":
		return domain.TrackStatusDraft
	}
	return domain.TrackStatusUnknown
}
