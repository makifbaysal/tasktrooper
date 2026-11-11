package telemetry

import (
	"net/url"
	"strings"
)

// readableRoots are the first path segments under /api/v1 a caller may GET.
// Everything that holds credentials or sessions (auth, me, orgs, api-keys,
// integrations, notification-channels, assistant, repos, internal) is absent
// on purpose, as is every route that only mutates.
var readableRoots = map[string]bool{
	"overview":            true,
	"service-map":         true,
	"services":            true,
	"environments":        true,
	"deploys":             true,
	"traces":              true,
	"errors":              true,
	"logs":                true,
	"apps":                true,
	"monitors":            true,
	"monitor-templates":   true,
	"incidents":           true,
	"kpis":                true,
	"infra":               true,
	"serverless":          true,
	"loadtests":           true,
	"maintenance-windows": true,
	"replays":             true,
	"profiles":            true,
	"ai":                  true,
	"usage":               true,
}

// ValidatePath returns the decoded path, without surrounding slashes, when
// relPath is a readable API path. A segment may carry percent-escapes (a
// process or host name with a space), but once decoded it may not be a dot
// segment, hold a slash, a backslash or a control character, or be empty, so no
// input climbs out of /api/v1.
func ValidatePath(relPath string) (string, error) {
	p := strings.Trim(relPath, "/")
	if p == "" || len(p) > 1024 {
		return "", ErrPathNotAllowed
	}
	raw := strings.Split(p, "/")
	segments := make([]string, 0, len(raw))
	for _, seg := range raw {
		if strings.ContainsAny(seg, "?#\\") {
			return "", ErrPathNotAllowed
		}
		if strings.Contains(seg, "%") {
			dec, err := url.PathUnescape(seg)
			if err != nil {
				return "", ErrPathNotAllowed
			}
			seg = dec
		}
		if seg == "" || seg == "." || seg == ".." || strings.ContainsAny(seg, "/\\") {
			return "", ErrPathNotAllowed
		}
		for _, r := range seg {
			if r < 0x20 || r == 0x7f {
				return "", ErrPathNotAllowed
			}
		}
		segments = append(segments, seg)
	}
	if !readableRoots[segments[0]] {
		return "", ErrPathNotAllowed
	}
	return strings.Join(segments, "/"), nil
}
