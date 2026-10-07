package appstore

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const defaultBaseURL = "https://api.appstoreconnect.apple.com"

const tokenRefreshMargin = 30 * time.Second

type Client struct {
	keyID    string
	issuerID string
	privKey  *ecdsa.PrivateKey

	baseURL    string
	httpClient *http.Client

	mu       sync.Mutex
	token    string
	tokenExp time.Time
}

var _ port.AppStoreClient = (*Client)(nil)

func New(cred domain.StoreCredential) (*Client, error) {
	keyID := cred.Data["key_id"]
	issuerID := cred.Data["issuer_id"]
	p8 := cred.Data["p8"]
	if keyID == "" || issuerID == "" || p8 == "" {
		return nil, fmt.Errorf("appstore: credential missing key_id, issuer_id, or p8")
	}
	priv, err := parseP8PrivateKey([]byte(p8))
	if err != nil {
		return nil, err
	}
	return &Client{
		keyID:      keyID,
		issuerID:   issuerID,
		privKey:    priv,
		baseURL:    defaultBaseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (c *Client) SetBaseURL(u string) {
	c.baseURL = u
}

func (c *Client) bearerToken() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.tokenExp.Add(-tokenRefreshMargin)) {
		return c.token, nil
	}
	tok, exp, err := mintToken(c.keyID, c.issuerID, c.privKey)
	if err != nil {
		return "", err
	}
	c.token = tok
	c.tokenExp = exp
	return c.token, nil
}

type apiError struct {
	Status int
	Body   string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("appstore api: %d %s", e.Status, e.Body)
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	tok, err := c.bearerToken()
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

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("appstore: building %s %s request: %w", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("appstore: %s %s: %w", method, path, err)
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

const ascTimeLayout = "2006-01-02T15:04:05.000-0700"

func parseASCTime(s string) (time.Time, error) {
	if t, err := time.Parse(ascTimeLayout, s); err == nil {
		return t, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("appstore: parsing timestamp %q: %w", s, err)
	}
	return t, nil
}

type jsonAPIRefList struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

type jsonAPIPage[T any] struct {
	Data  []T `json:"data"`
	Links struct {
		Next string `json:"next"`
	} `json:"links"`
}

const ascMaxCollectionPages = 40

func listAll[T any](ctx context.Context, c *Client, path string) ([]T, error) {
	var all []T
	for page := 0; path != ""; page++ {
		if page >= ascMaxCollectionPages {
			return nil, fmt.Errorf("appstore: %s: still paging after %d pages, refusing to follow the cursor further", path, ascMaxCollectionPages)
		}
		var body jsonAPIPage[T]
		if err := c.do(ctx, http.MethodGet, path, nil, &body); err != nil {
			return nil, err
		}
		all = append(all, body.Data...)
		next, err := nextPagePath(body.Links.Next)
		if err != nil {
			return nil, err
		}
		path = next
	}
	return all, nil
}

func nextPagePath(next string) (string, error) {
	if next == "" {
		return "", nil
	}
	u, err := url.Parse(next)
	if err != nil {
		return "", fmt.Errorf("appstore: parsing next page link %q: %w", next, err)
	}
	return u.RequestURI(), nil
}

func (c *Client) ValidateAuth(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/v1/apps?limit=1", nil, nil)
}

func (c *Client) AppByBundleID(ctx context.Context, bundleID string) (appID string, found bool, err error) {
	var resp jsonAPIRefList
	path := "/v1/apps?filter[bundleId]=" + url.QueryEscape(bundleID)
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return "", false, err
	}
	if len(resp.Data) == 0 {
		return "", false, nil
	}
	return resp.Data[0].ID, true, nil
}

func (c *Client) findBundleID(ctx context.Context, bundleID string) (resourceID string, found bool, err error) {
	var resp jsonAPIRefList
	path := "/v1/bundleIds?filter[identifier]=" + url.QueryEscape(bundleID)
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return "", false, err
	}
	if len(resp.Data) == 0 {
		return "", false, nil
	}
	return resp.Data[0].ID, true, nil
}

func (c *Client) EnsureBundleID(ctx context.Context, bundleID, name string) error {
	_, found, err := c.findBundleID(ctx, bundleID)
	if err != nil {
		return err
	}
	if found {
		return nil
	}
	body := map[string]any{
		"data": map[string]any{
			"type": "bundleIds",
			"attributes": map[string]any{
				"identifier": bundleID,
				"name":       name,
				"platform":   "IOS",
			},
		},
	}
	return c.do(ctx, http.MethodPost, "/v1/bundleIds", body, nil)
}

func (c *Client) CreateCertificate(ctx context.Context, csrPEM []byte) (port.StoreCert, error) {
	body := map[string]any{
		"data": map[string]any{
			"type": "certificates",
			"attributes": map[string]any{
				"certificateType": "IOS_DISTRIBUTION",
				"csrContent":      base64.StdEncoding.EncodeToString(csrPEM),
			},
		},
	}
	var resp struct {
		Data struct {
			ID         string `json:"id"`
			Attributes struct {
				SerialNumber       string `json:"serialNumber"`
				CertificateContent string `json:"certificateContent"`
				ExpirationDate     string `json:"expirationDate"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/certificates", body, &resp); err != nil {
		return port.StoreCert{}, err
	}

	der, err := base64.StdEncoding.DecodeString(resp.Data.Attributes.CertificateContent)
	if err != nil {
		return port.StoreCert{}, fmt.Errorf("appstore: decoding certificateContent: %w", err)
	}
	expiresAt, err := parseASCTime(resp.Data.Attributes.ExpirationDate)
	if err != nil {
		return port.StoreCert{}, err
	}
	return port.StoreCert{
		ID:        resp.Data.ID,
		Serial:    resp.Data.Attributes.SerialNumber,
		DER:       der,
		ExpiresAt: expiresAt,
	}, nil
}

func (c *Client) CreateProfile(ctx context.Context, bundleID, certID, name string) (port.StoreProfile, error) {
	bundleResourceID, found, err := c.findBundleID(ctx, bundleID)
	if err != nil {
		return port.StoreProfile{}, err
	}
	if !found {
		return port.StoreProfile{}, fmt.Errorf("appstore: bundle id %q is not registered with ASC", bundleID)
	}

	body := map[string]any{
		"data": map[string]any{
			"type": "profiles",
			"attributes": map[string]any{
				"name":        name,
				"profileType": "IOS_APP_STORE",
			},
			"relationships": map[string]any{
				"bundleId": map[string]any{
					"data": map[string]any{"type": "bundleIds", "id": bundleResourceID},
				},
				"certificates": map[string]any{
					"data": []map[string]any{
						{"type": "certificates", "id": certID},
					},
				},
			},
		},
	}
	var resp struct {
		Data struct {
			ID         string `json:"id"`
			Attributes struct {
				Name           string `json:"name"`
				ProfileContent string `json:"profileContent"`
				ExpirationDate string `json:"expirationDate"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/profiles", body, &resp); err != nil {
		return port.StoreProfile{}, err
	}

	content, err := base64.StdEncoding.DecodeString(resp.Data.Attributes.ProfileContent)
	if err != nil {
		return port.StoreProfile{}, fmt.Errorf("appstore: decoding profileContent: %w", err)
	}
	expiresAt, err := parseASCTime(resp.Data.Attributes.ExpirationDate)
	if err != nil {
		return port.StoreProfile{}, err
	}
	return port.StoreProfile{
		ID:        resp.Data.ID,
		Name:      resp.Data.Attributes.Name,
		Content:   content,
		ExpiresAt: expiresAt,
	}, nil
}

func (c *Client) LatestVersion(ctx context.Context, appID string) (port.AppStoreVersionInfo, error) {
	versions, err := c.listAppStoreVersions(ctx, appID)
	if err != nil {
		return port.AppStoreVersionInfo{}, err
	}
	newest, found := newestVersion(versions)
	if !found {
		return port.AppStoreVersionInfo{}, nil
	}
	return port.AppStoreVersionInfo{
		Version: newest.Attributes.VersionString,
		State:   newest.state(),
	}, nil
}

// LiveVersion is the version customers can download right now, not the newest
// one: a live app with a newer draft or a version in review has that as its
// newest row, and reading it would report the app as unpublished.
func (c *Client) LiveVersion(ctx context.Context, appID string) (port.AppStoreVersionInfo, bool, error) {
	versions, err := c.listAppStoreVersions(ctx, appID)
	if err != nil {
		return port.AppStoreVersionInfo{}, false, err
	}
	live, found := liveVersion(versions)
	if !found {
		return port.AppStoreVersionInfo{}, false, nil
	}
	return port.AppStoreVersionInfo{Version: live.Attributes.VersionString, State: live.state()}, true, nil
}

func liveVersion(versions []appStoreVersion) (appStoreVersion, bool) {
	onSale := make([]appStoreVersion, 0, len(versions))
	for _, v := range versions {
		if v.state() == stateReadyForSale {
			onSale = append(onSale, v)
		}
	}
	return newestVersion(onSale)
}

func newestVersion(versions []appStoreVersion) (appStoreVersion, bool) {
	best := -1
	for i := range versions {
		if best == -1 || versionLess(versions[best].Attributes.VersionString, versions[i].Attributes.VersionString) {
			best = i
		}
	}
	if best == -1 {
		return appStoreVersion{}, false
	}
	return versions[best], true
}

type appStoreVersion struct {
	ID         string `json:"id"`
	Attributes struct {
		VersionString   string `json:"versionString"`
		AppStoreState   string `json:"appStoreState"`
		AppVersionState string `json:"appVersionState"`
		Platform        string `json:"platform"`
		CreatedDate     string `json:"createdDate"`
	} `json:"attributes"`
}

const stateReadyForSale = "READY_FOR_SALE"

// state speaks the appStoreState vocabulary. Apple deprecated appStoreState for
// appVersionState, which says READY_FOR_DISTRIBUTION where the old field said
// READY_FOR_SALE; a version carrying only the new field must still read as live.
func (v appStoreVersion) state() string {
	if s := v.Attributes.AppStoreState; s != "" {
		return s
	}
	if v.Attributes.AppVersionState == "READY_FOR_DISTRIBUTION" {
		return stateReadyForSale
	}
	return v.Attributes.AppVersionState
}

// iOSOnly drops the macOS, tvOS and visionOS versions a universal app record
// carries next to its iOS ones; their states say nothing about the iOS app.
func iOSOnly(versions []appStoreVersion) []appStoreVersion {
	kept := make([]appStoreVersion, 0, len(versions))
	for _, v := range versions {
		if v.Attributes.Platform == "" || v.Attributes.Platform == "IOS" {
			kept = append(kept, v)
		}
	}
	return kept
}

func (c *Client) listAppStoreVersions(ctx context.Context, appID string) ([]appStoreVersion, error) {
	versions, err := listAll[appStoreVersion](ctx, c, "/v1/apps/"+url.PathEscape(appID)+"/appStoreVersions?filter[platform]=IOS")
	if err != nil {
		return nil, err
	}
	return iOSOnly(versions), nil
}

var editableAppStoreStates = map[string]bool{
	"PREPARE_FOR_SUBMISSION": true,
	"DEVELOPER_REJECTED":     true,
	"REJECTED":               true,
	"METADATA_REJECTED":      true,
}

func (c *Client) SubmitForReview(ctx context.Context, appID, version string) error {
	return c.submitVersionForReview(ctx, appID, version, "")
}

func (c *Client) submitVersionForReview(ctx context.Context, appID, version, buildID string) error {
	versions, err := c.listAppStoreVersions(ctx, appID)
	if err != nil {
		return err
	}

	versionID := ""
	for _, v := range versions {
		if v.Attributes.VersionString == version && editableAppStoreStates[v.state()] {
			versionID = v.ID
			break
		}
	}

	if versionID == "" {
		body := map[string]any{
			"data": map[string]any{
				"type": "appStoreVersions",
				"attributes": map[string]any{
					"versionString": version,
					"platform":      "IOS",
				},
				"relationships": map[string]any{
					"app": map[string]any{
						"data": map[string]any{"type": "apps", "id": appID},
					},
				},
			},
		}
		var resp struct {
			Data struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := c.do(ctx, http.MethodPost, "/v1/appStoreVersions", body, &resp); err != nil {
			return err
		}
		versionID = resp.Data.ID
	}

	if buildID != "" {
		if err := c.attachBuild(ctx, versionID, buildID); err != nil {
			return err
		}
	}

	body := map[string]any{
		"data": map[string]any{
			"type": "appStoreVersionSubmissions",
			"relationships": map[string]any{
				"appStoreVersion": map[string]any{
					"data": map[string]any{"type": "appStoreVersions", "id": versionID},
				},
			},
		},
	}
	return c.do(ctx, http.MethodPost, "/v1/appStoreVersionSubmissions", body, nil)
}

func (c *Client) attachBuild(ctx context.Context, versionID, buildID string) error {
	body := map[string]any{
		"data": map[string]any{"type": "builds", "id": buildID},
	}
	path := "/v1/appStoreVersions/" + url.PathEscape(versionID) + "/relationships/build"
	if err := c.do(ctx, http.MethodPatch, path, body, nil); err != nil {
		return fmt.Errorf("appstore: attaching build %s to version %s (ASC will not accept a submission without one): %w", buildID, versionID, err)
	}
	return nil
}

func (c *Client) ReleaseVersion(ctx context.Context, appID string) error {
	versions, err := c.listAppStoreVersions(ctx, appID)
	if err != nil {
		return err
	}

	versionID := ""
	for _, v := range versions {
		if v.state() == "PENDING_DEVELOPER_RELEASE" {
			versionID = v.ID
			break
		}
	}
	if versionID == "" {
		return fmt.Errorf("appstore: no version is pending developer release for app %s", appID)
	}

	body := map[string]any{
		"data": map[string]any{
			"type": "appStoreVersionReleaseRequests",
			"relationships": map[string]any{
				"appStoreVersion": map[string]any{
					"data": map[string]any{"type": "appStoreVersions", "id": versionID},
				},
			},
		},
	}
	return c.do(ctx, http.MethodPost, "/v1/appStoreVersionReleaseRequests", body, nil)
}

func versionLess(a, b string) bool {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var ap, bp string
		if i < len(as) {
			ap = as[i]
		}
		if i < len(bs) {
			bp = bs[i]
		}
		an, aerr := strconv.Atoi(ap)
		bn, berr := strconv.Atoi(bp)
		if aerr == nil && berr == nil {
			if an != bn {
				return an < bn
			}
			continue
		}
		if ap != bp {
			return ap < bp
		}
	}
	return false
}
