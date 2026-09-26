package vercel

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const (
	// defaultLogsDeadline bounds a runtime-logs read even when the caller's
	// own context has none — the endpoint is documented to hold the
	// connection open and stream, so without a cap a hung upstream would hang
	// the request forever.
	defaultLogsDeadline = 6 * time.Second
	defaultLogsLimit    = 200
	maxLogsLimit        = 1000
	buildLogsPageLimit  = 500
)

// resolveDeploymentID picks the deployment Logs reads: an explicit
// Extra["deployment_id"] wins, otherwise the most recent READY production
// deployment among the last few — the newest one is sometimes still building,
// which has no runtime logs yet.
func (p *Provider) resolveDeploymentID(ctx context.Context, token, teamID string, ref domain.CloudResourceRef) (string, error) {
	if id := ref.Extra[domain.CloudRefDeploymentID]; id != "" {
		return id, nil
	}
	deployments, err := p.listDeployments(ctx, token, teamID, ref.ID, domain.VercelTargetProduction, deploymentPageLimit)
	if err != nil {
		return "", err
	}
	for _, d := range deployments {
		if strings.EqualFold(d.ReadyState, "READY") {
			return d.UID, nil
		}
	}
	return "", nil
}

func (p *Provider) Logs(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef, q domain.RuntimeLogQuery) (domain.RuntimeLogPage, error) {
	token, teamID, err := vercelCredentials(cred)
	if err != nil {
		return domain.RuntimeLogPage{}, err
	}
	if id := ref.Extra["team_id"]; id != "" {
		teamID = id
	}

	deploymentID, err := p.resolveDeploymentID(ctx, token, teamID, ref)
	if err != nil {
		return domain.RuntimeLogPage{}, wrapVercelErr(err)
	}
	if deploymentID == "" {
		return domain.RuntimeLogPage{}, nil
	}

	limit := q.Limit
	if limit <= 0 {
		limit = defaultLogsLimit
	} else if limit > maxLogsLimit {
		limit = maxLogsLimit
	}

	entries, truncated, err := p.readRuntimeLogs(ctx, token, teamID, ref.ID, deploymentID, limit)
	if err != nil {
		return domain.RuntimeLogPage{}, wrapVercelErr(err)
	}

	entries = filterLogEntries(entries, q)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Timestamp.After(entries[j].Timestamp) })

	return domain.RuntimeLogPage{Entries: entries, Truncated: truncated}, nil
}

// readRuntimeLogs streams GET .../runtime-logs and decodes it as
// newline-delimited JSON. The endpoint may never send EOF on its own, so the
// deadline — derived from ctx, capped at defaultLogsDeadline — is what
// actually ends the read; hitting it or the line cap is reported as
// Truncated, not an error.
func (p *Provider) readRuntimeLogs(ctx context.Context, token, teamID, projectID, deploymentID string, limit int) ([]domain.RuntimeLogEntry, bool, error) {
	reqCtx, cancel := context.WithTimeout(ctx, defaultLogsDeadline)
	defer cancel()

	u := p.client.base() + "/v1/projects/" + url.PathEscape(projectID) + "/deployments/" + url.PathEscape(deploymentID) + "/runtime-logs"
	if q := withTeam(url.Values{}, teamID); len(q) > 0 {
		u += "?" + q.Encode()
	}

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, u, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := p.client.httpClient().Do(req)
	if err != nil {
		if reqCtx.Err() != nil {
			return nil, true, nil
		}
		return nil, false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, false, &APIError{Status: resp.StatusCode, Message: strings.TrimSpace(string(data))}
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)

	var entries []domain.RuntimeLogEntry
	truncated := false
	for {
		if len(entries) >= limit {
			// One more Scan tells us whether the cap actually cut something,
			// versus the stream happening to end at exactly `limit` lines.
			if scanner.Scan() || scanner.Err() != nil || reqCtx.Err() != nil {
				truncated = true
			}
			break
		}
		if !scanner.Scan() {
			if scanner.Err() != nil || reqCtx.Err() != nil {
				truncated = true
			}
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var raw rawLogLine
		if json.Unmarshal([]byte(line), &raw) == nil {
			entries = append(entries, raw.toEntry())
		}
	}
	return entries, truncated, nil
}

// rawLogLine is one object of the runtime-logs NDJSON stream.
type rawLogLine struct {
	RowID              string `json:"rowId"`
	TimestampInMs      int64  `json:"timestampInMs"`
	Level              string `json:"level"`
	Message            string `json:"message"`
	MessageTruncated   bool   `json:"messageTruncated"`
	Source             string `json:"source"`
	RequestMethod      string `json:"requestMethod"`
	RequestPath        string `json:"requestPath"`
	ResponseStatusCode int    `json:"responseStatusCode"`
	Domain             string `json:"domain"`
}

func mapLogSeverity(level string) domain.LogSeverity {
	switch strings.ToLower(level) {
	case "error":
		return domain.LogError
	case "warning", "warn":
		return domain.LogWarning
	case "fatal", "critical":
		return domain.LogCritical
	default:
		return domain.LogInfo
	}
}

func (r rawLogLine) toEntry() domain.RuntimeLogEntry {
	msg := r.Message
	if r.MessageTruncated {
		msg += "…"
	}
	var fields map[string]string
	if r.Domain != "" || r.RowID != "" {
		fields = map[string]string{}
		if r.Domain != "" {
			fields["domain"] = r.Domain
		}
		if r.RowID != "" {
			fields["rowId"] = r.RowID
		}
	}
	return domain.RuntimeLogEntry{
		Timestamp:  msTime(r.TimestampInMs),
		Severity:   mapLogSeverity(r.Level),
		Message:    msg,
		Source:     r.Source,
		Method:     r.RequestMethod,
		Path:       r.RequestPath,
		StatusCode: r.ResponseStatusCode,
		Fields:     fields,
	}
}

func filterLogEntries(entries []domain.RuntimeLogEntry, q domain.RuntimeLogQuery) []domain.RuntimeLogEntry {
	text := strings.ToLower(strings.TrimSpace(q.Text))
	var out []domain.RuntimeLogEntry
	for _, e := range entries {
		if !q.Since.IsZero() && e.Timestamp.Before(q.Since) {
			continue
		}
		if !q.Until.IsZero() && e.Timestamp.After(q.Until) {
			continue
		}
		if q.MinSeverity != "" && e.Severity.Rank() < q.MinSeverity.Rank() {
			continue
		}
		if text != "" && !strings.Contains(strings.ToLower(e.Message), text) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// rawBuildEvent is one element of GET /v3/deployments/{id}/events. The
// reference does not pin down whether `text` sits at the top level or under
// `payload`, so both are read.
type rawBuildEvent struct {
	Type    string `json:"type"`
	Created int64  `json:"created"`
	Text    string `json:"text"`
	Payload struct {
		Text string `json:"text"`
	} `json:"payload"`
}

func (e rawBuildEvent) toEntry() domain.RuntimeLogEntry {
	text := e.Text
	if text == "" {
		text = e.Payload.Text
	}
	severity := domain.LogInfo
	if e.Type == "stderr" {
		severity = domain.LogError
	}
	return domain.RuntimeLogEntry{
		Timestamp: msTime(e.Created),
		Severity:  severity,
		Message:   text,
		Source:    e.Type,
	}
}

// BuildLogs reads a deployment's build output (as opposed to Logs' runtime
// logs), for the failed-deployment view added later.
func (p *Provider) BuildLogs(ctx context.Context, cred domain.CloudCredential, deploymentID string, limit int) ([]domain.RuntimeLogEntry, error) {
	token, teamID, err := vercelCredentials(cred)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = buildLogsPageLimit
	} else if limit > maxLogsLimit {
		limit = maxLogsLimit
	}

	q := withTeam(url.Values{}, teamID)
	q.Set("limit", strconv.Itoa(limit))

	var raw []rawBuildEvent
	if err := p.client.getJSON(ctx, token, "/v3/deployments/"+url.PathEscape(deploymentID)+"/events", q, &raw); err != nil {
		return nil, wrapVercelErr(err)
	}

	entries := make([]domain.RuntimeLogEntry, 0, len(raw))
	for _, e := range raw {
		entries = append(entries, e.toEntry())
	}
	return entries, nil
}
