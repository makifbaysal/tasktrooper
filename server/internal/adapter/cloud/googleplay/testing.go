package googleplay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

var _ port.PlayTestingClient = (*Client)(nil)

// An AAB is routinely 100+ MB and Play digests it before answering; the 30s
// budget every JSON call runs on would cut the upload off mid-stream.
const mediaUploadTimeout = 15 * time.Minute

const releaseNotesMaxRunes = 500

type playTrack struct {
	Track    string        `json:"track"`
	Releases []playRelease `json:"releases"`
}

func editPath(packageName, editID string) string {
	return "/androidpublisher/v3/applications/" + url.PathEscape(packageName) + "/edits/" + url.PathEscape(editID)
}

func (c *Client) LatestVersionCode(ctx context.Context, packageName string) (int64, error) {
	editID, err := c.insertEdit(ctx, packageName)
	if err != nil {
		return 0, err
	}
	defer c.deleteEdit(ctx, packageName, editID)

	bundles, err := c.bundleVersionCodes(ctx, packageName, editID)
	if err != nil {
		return 0, err
	}
	var apks struct {
		APKs []struct {
			VersionCode int64 `json:"versionCode"`
		} `json:"apks"`
	}
	if err := c.do(ctx, http.MethodGet, editPath(packageName, editID)+"/apks", nil, &apks); err != nil {
		return 0, err
	}
	tracks, err := c.listTracks(ctx, packageName, editID)
	if err != nil {
		return 0, err
	}

	var latest int64
	for _, code := range bundles {
		latest = max(latest, code)
	}
	for _, a := range apks.APKs {
		latest = max(latest, a.VersionCode)
	}
	for _, t := range tracks {
		for _, r := range t.Releases {
			_, code := highestVersionCode(r.VersionCodes)
			latest = max(latest, code)
		}
	}
	return latest, nil
}

func (c *Client) bundleVersionCodes(ctx context.Context, packageName, editID string) ([]int64, error) {
	var resp struct {
		Bundles []struct {
			VersionCode int64 `json:"versionCode"`
		} `json:"bundles"`
	}
	if err := c.do(ctx, http.MethodGet, editPath(packageName, editID)+"/bundles", nil, &resp); err != nil {
		return nil, err
	}
	codes := make([]int64, 0, len(resp.Bundles))
	for _, b := range resp.Bundles {
		codes = append(codes, b.VersionCode)
	}
	return codes, nil
}

func (c *Client) listTracks(ctx context.Context, packageName, editID string) ([]playTrack, error) {
	var resp struct {
		Tracks []playTrack `json:"tracks"`
	}
	if err := c.do(ctx, http.MethodGet, editPath(packageName, editID)+"/tracks", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Tracks, nil
}

func (c *Client) UploadInternalSharing(ctx context.Context, packageName, aabPath string) (port.InternalShareLink, error) {
	var resp struct {
		DownloadURL            string `json:"downloadUrl"`
		CertificateFingerprint string `json:"certificateFingerprint"`
		SHA256                 string `json:"sha256"`
	}
	path := "/upload/androidpublisher/v3/applications/" + url.PathEscape(packageName) + "/internalappsharing/bundle?uploadType=media"
	if err := c.upload(ctx, path, aabPath, &resp); err != nil {
		return port.InternalShareLink{}, err
	}
	if resp.DownloadURL == "" {
		return port.InternalShareLink{}, errors.New("googleplay: internal app sharing accepted the bundle but returned no download URL")
	}
	return port.InternalShareLink{
		DownloadURL:            resp.DownloadURL,
		CertificateFingerprint: resp.CertificateFingerprint,
		SHA256:                 resp.SHA256,
	}, nil
}

func (c *Client) ListTracks(ctx context.Context, packageName string) ([]port.PlayTrack, error) {
	editID, err := c.insertEdit(ctx, packageName)
	if err != nil {
		return nil, err
	}
	defer c.deleteEdit(ctx, packageName, editID)

	tracks, err := c.listTracks(ctx, packageName, editID)
	if err != nil {
		return nil, err
	}
	out := make([]port.PlayTrack, 0, len(tracks))
	for _, t := range tracks {
		pt := port.PlayTrack{Name: t.Track, Releases: make([]port.PlayTrackRelease, 0, len(t.Releases))}
		for _, r := range t.Releases {
			codes := make([]int64, 0, len(r.VersionCodes))
			for _, s := range r.VersionCodes {
				if code, err := strconv.ParseInt(s, 10, 64); err == nil {
					codes = append(codes, code)
				}
			}
			pt.Releases = append(pt.Releases, port.PlayTrackRelease{
				Name:         r.Name,
				Status:       r.Status,
				VersionCodes: codes,
				UserFraction: r.UserFraction,
			})
		}
		out = append(out, pt)
	}
	return out, nil
}

// ReleaseToTrack publishes a completed release, except on an app that has
// never been published: Play accepts only draft releases there, and says so
// only once asked, so that refusal is answered by publishing a draft instead.
func (c *Client) ReleaseToTrack(ctx context.Context, packageName, track, aabPath string, versionCode int64, releaseName, notes string) error {
	err := c.releaseToTrack(ctx, packageName, track, aabPath, versionCode, releaseName, notes, "completed")
	if isDraftAppRefusal(err) {
		return c.releaseToTrack(ctx, packageName, track, aabPath, versionCode, releaseName, notes, "draft")
	}
	return err
}

func (c *Client) releaseToTrack(ctx context.Context, packageName, track, aabPath string, versionCode int64, releaseName, notes, status string) error {
	return c.withEdit(ctx, packageName, func(editID string) error {
		if err := c.ensureBundle(ctx, packageName, editID, aabPath, versionCode); err != nil {
			return err
		}
		release := map[string]any{
			"name":         releaseName,
			"versionCodes": []string{strconv.FormatInt(versionCode, 10)},
			"status":       status,
		}
		if notes != "" {
			release["releaseNotes"] = []map[string]string{{"language": "en-US", "text": truncateRunes(notes, releaseNotesMaxRunes)}}
		}
		return c.putTrack(ctx, packageName, editID, track, map[string]any{"track": track, "releases": []any{release}})
	})
}

func (c *Client) ensureBundle(ctx context.Context, packageName, editID, aabPath string, versionCode int64) error {
	codes, err := c.bundleVersionCodes(ctx, packageName, editID)
	if err != nil {
		return err
	}
	if slices.Contains(codes, versionCode) {
		return nil
	}
	if aabPath == "" {
		return fmt.Errorf("googleplay: versionCode %d is not on Play and there is no local copy of the bundle to upload", versionCode)
	}
	var uploaded struct {
		VersionCode int64 `json:"versionCode"`
	}
	if err := c.upload(ctx, "/upload"+editPath(packageName, editID)+"/bundles?uploadType=media", aabPath, &uploaded); err != nil {
		return err
	}
	// Returning the error discards the edit, so the stray bundle never lands
	// on Play.
	if uploaded.VersionCode != versionCode {
		return fmt.Errorf("googleplay: %s is versionCode %d, not %d: the file is not the build it is recorded as", aabPath, uploaded.VersionCode, versionCode)
	}
	return nil
}

func isDraftAppRefusal(err error) bool {
	var apiErr *apiError
	if !errors.As(err, &apiErr) {
		return false
	}
	body := strings.ToLower(apiErr.Body)
	return strings.Contains(body, "draft app") || strings.Contains(body, "only releases with status draft")
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func (c *Client) mediaHTTPClient() *http.Client {
	hc := *c.httpClient
	hc.Timeout = mediaUploadTimeout
	return &hc
}

// upload is do for a media upload: the file at filePath is streamed as the
// body rather than read into memory.
func (c *Client) upload(ctx context.Context, path, filePath string, out any) error {
	tok, err := c.bearerToken(ctx, androidPublisherScope)
	if err != nil {
		return err
	}

	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("googleplay: opening bundle: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("googleplay: reading bundle: %w", err)
	}
	if info.Size() == 0 {
		return fmt.Errorf("googleplay: bundle %s is empty", filePath)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, f)
	if err != nil {
		return err
	}
	req.ContentLength = info.Size()
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := c.mediaHTTPClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &apiError{Status: resp.StatusCode, Body: domain.TruncateHead(string(data), 500)}
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}
