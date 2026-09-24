package gcloud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const defaultErrorReportingBaseURL = "https://clouderrorreporting.googleapis.com"

func (p *Provider) Errors(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef, since time.Time) ([]domain.RuntimeErrorGroup, error) {
	c, err := p.clientFor(cred)
	if err != nil {
		return nil, err
	}
	projectID := projectIDFromRef(ref, c)

	path := "/v1beta1/projects/" + url.PathEscape(projectID) + "/groupStats" +
		"?serviceFilter.service=" + url.QueryEscape(ref.Name) +
		"&timeRange.period=" + errorReportingPeriod(since) +
		"&order=COUNT_DESC&pageSize=50"

	var resp errorGroupStatsResponse
	if err := p.getErrorReporting(ctx, c, path, &resp); err != nil {
		if serviceDisabled(err) {
			return nil, port.ErrUnsupported
		}
		return nil, classifyRunError(err)
	}

	out := make([]domain.RuntimeErrorGroup, 0, len(resp.ErrorGroupStats))
	for _, g := range resp.ErrorGroupStats {
		out = append(out, mapErrorGroup(g, since, projectID))
	}
	return out, nil
}

// errorReportingPeriod picks the smallest Error Reporting period that still
// covers the requested window — a longer period than necessary would count
// occurrences the caller never asked to see.
func errorReportingPeriod(since time.Time) string {
	window := time.Since(since)
	switch {
	case window <= time.Hour:
		return "PERIOD_1_HOUR"
	case window <= 6*time.Hour:
		return "PERIOD_6_HOURS"
	case window <= 24*time.Hour:
		return "PERIOD_1_DAY"
	case window <= 7*24*time.Hour:
		return "PERIOD_1_WEEK"
	default:
		return "PERIOD_30_DAYS"
	}
}

func (p *Provider) getErrorReporting(ctx context.Context, c *Client, path string, out any) error {
	tok, err := c.bearerToken(ctx, cloudPlatformScope)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.errorReportingBaseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
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

// serviceDisabled distinguishes a project that has never turned on Error
// Reporting from a real permissions or not-found failure: the caller is
// meant to degrade to grouping log lines instead, not to report an error.
func serviceDisabled(err error) bool {
	var apiErr *apiError
	if !errors.As(err, &apiErr) {
		return false
	}
	if apiErr.Status == http.StatusNotFound {
		return true
	}
	return apiErr.Status == http.StatusForbidden && strings.Contains(apiErr.Body, "SERVICE_DISABLED")
}

type errorGroupStatsResponse struct {
	ErrorGroupStats []errorGroupStat `json:"errorGroupStats"`
}

type errorGroupStat struct {
	Group struct {
		GroupID string `json:"groupId"`
	} `json:"group"`
	Count          string `json:"count"`
	FirstSeenTime  string `json:"firstSeenTime"`
	LastSeenTime   string `json:"lastSeenTime"`
	Representative struct {
		Message string `json:"message"`
	} `json:"representative"`
}

func mapErrorGroup(g errorGroupStat, since time.Time, projectID string) domain.RuntimeErrorGroup {
	count, _ := strconv.Atoi(g.Count)
	firstSeen, _ := time.Parse(time.RFC3339, g.FirstSeenTime)
	lastSeen, _ := time.Parse(time.RFC3339, g.LastSeenTime)
	return domain.RuntimeErrorGroup{
		Fingerprint: g.Group.GroupID,
		Message:     firstLine(g.Representative.Message),
		Count:       count,
		FirstSeen:   firstSeen,
		LastSeen:    lastSeen,
		Sample:      firstNLines(g.Representative.Message, 20),
		Source:      "Error Reporting",
		New:         !firstSeen.Before(since),
		ExternalURL: fmt.Sprintf("https://console.cloud.google.com/errors/detail/%s?project=%s", g.Group.GroupID, projectID),
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func firstNLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[:n], "\n")
}
