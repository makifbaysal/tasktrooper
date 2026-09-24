package vercel

import (
	"net/http"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// Is makes errors.Is(err, port.ErrVercelUnauthorized) answer true for a 401 or
// a 403, so the application layer can tell a refused token from an outage
// without importing this package for IsUnauthorized. Same test as
// IsUnauthorized, reachable through the standard errors machinery.
func (e *APIError) Is(target error) bool {
	if target != port.ErrVercelUnauthorized {
		return false
	}
	return e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden
}

// deploymentPageLimit bounds one Deployments call. Ten is enough for the
// details view's two questions — what shipped last, and what failed last —
// without paging a project with thousands of builds.
const deploymentPageLimit = 10

// rawDeployment is the subset of Vercel's deployment object this package
// reads. Field names and their units are from the /v7/deployments reference:
// `created`, `createdAt`, `ready` and `buildingAt` are JavaScript
// milliseconds, and `meta` is documented only as "Metadata information from
// the Git provider" — the reference enumerates none of its keys, so they are
// read defensively below rather than assumed.
type rawDeployment struct {
	UID          string         `json:"uid"`
	Name         string         `json:"name"`
	URL          string         `json:"url"`
	State        string         `json:"state"`
	ReadyState   string         `json:"readyState"`
	Target       *string        `json:"target"`
	Created      int64          `json:"created"`
	CreatedAt    int64          `json:"createdAt"`
	Ready        int64          `json:"ready"`
	BuildingAt   int64          `json:"buildingAt"`
	InspectorURL *string        `json:"inspectorUrl"`
	ErrorCode    string         `json:"errorCode"`
	ErrorMessage *string        `json:"errorMessage"`
	Meta         map[string]any `json:"meta"`
}

// metaString reads one key out of the git metadata, accepting only a string.
// The reference types `meta` as a bare object, so a non-string value is
// possible and must not become the literal text of a Go %v rendering.
func metaString(meta map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := meta[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func msTime(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}
