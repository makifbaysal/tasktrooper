package telemetry

import (
	"net/http"
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
	"suggestions":         true,
}

// writeRoute is one write the agents' tools may send; "*" stands for exactly
// one id segment. It is a separate list from readableRoots on purpose: the
// UI proxy reads through ValidatePath only and can never reach these.
type writeRoute struct {
	method  string
	pattern string
}

var writeRoutes = []writeRoute{
	{http.MethodPost, "monitors"},
	{http.MethodPost, "monitors/preview"},
	{http.MethodPut, "monitors/*"},
	{http.MethodPatch, "monitors/*"},
	{http.MethodDelete, "monitors/*"},
	{http.MethodPost, "kpis"},
	{http.MethodPut, "kpis/*"},
	{http.MethodDelete, "kpis/*"},
	{http.MethodPost, "loadtests/scenarios"},
	{http.MethodPost, "loadtests/scenarios/*/runs"},
	{http.MethodPost, "loadtests/runs/*/stop"},
	{http.MethodPost, "suggestions/*/accept"},
	{http.MethodPost, "suggestions/*/reject"},
	{http.MethodPost, "deploys"},
}

// ValidatePath returns the decoded path, without surrounding slashes, when
// relPath is a readable API path. A segment may carry percent-escapes (a
// process or host name with a space), but once decoded it may not be a dot
// segment, hold a slash, a backslash or a control character, or be empty, so no
// input climbs out of /api/v1.
func ValidatePath(relPath string) (string, error) {
	segments, err := cleanSegments(relPath)
	if err != nil {
		return "", err
	}
	if !readableRoots[segments[0]] {
		return "", ErrPathNotAllowed
	}
	return strings.Join(segments, "/"), nil
}

// ValidateWrite is ValidatePath for a write: the method and the path must be
// one of writeRoutes.
func ValidateWrite(method, relPath string) (string, error) {
	segments, err := cleanSegments(relPath)
	if err != nil {
		return "", err
	}
	for _, r := range writeRoutes {
		if r.method == method && matches(strings.Split(r.pattern, "/"), segments) {
			return strings.Join(segments, "/"), nil
		}
	}
	return "", ErrPathNotAllowed
}

func matches(pattern, segments []string) bool {
	if len(pattern) != len(segments) {
		return false
	}
	for i, p := range pattern {
		if p != "*" && p != segments[i] {
			return false
		}
	}
	return true
}

func cleanSegments(relPath string) ([]string, error) {
	p := strings.Trim(relPath, "/")
	if p == "" || len(p) > 1024 {
		return nil, ErrPathNotAllowed
	}
	raw := strings.Split(p, "/")
	segments := make([]string, 0, len(raw))
	for _, seg := range raw {
		if strings.ContainsAny(seg, "?#\\") {
			return nil, ErrPathNotAllowed
		}
		if strings.Contains(seg, "%") {
			dec, err := url.PathUnescape(seg)
			if err != nil {
				return nil, ErrPathNotAllowed
			}
			seg = dec
		}
		if seg == "" || seg == "." || seg == ".." || strings.ContainsAny(seg, "/\\") {
			return nil, ErrPathNotAllowed
		}
		for _, r := range seg {
			if r < 0x20 || r == 0x7f {
				return nil, ErrPathNotAllowed
			}
		}
		segments = append(segments, seg)
	}
	return segments, nil
}
