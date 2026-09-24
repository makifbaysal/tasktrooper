package gcloud

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const defaultLoggingBaseURL = "https://logging.googleapis.com"

// logsScope carries two scopes space-delimited in one JWT "scope" claim —
// bearerToken caches tokens keyed by this exact string, so it doubles as the
// cache key for the read-only token Logs uses.
const logsScope = "https://www.googleapis.com/auth/cloud-platform.read-only https://www.googleapis.com/auth/logging.read"

const defaultLogPageSize = 200
const maxLogPageSize = 1000

func (p *Provider) Logs(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef, q domain.RuntimeLogQuery) (domain.RuntimeLogPage, error) {
	c, err := p.clientFor(cred)
	if err != nil {
		return domain.RuntimeLogPage{}, err
	}
	projectID := projectIDFromRef(ref, c)

	filter, err := logsFilter(ref, q)
	if err != nil {
		return domain.RuntimeLogPage{}, err
	}

	limit := q.Limit
	if limit <= 0 {
		limit = defaultLogPageSize
	}
	if limit > maxLogPageSize {
		limit = maxLogPageSize
	}

	body := map[string]any{
		"resourceNames": []string{"projects/" + projectID},
		"filter":        filter,
		"orderBy":       "timestamp desc",
		"pageSize":      limit,
	}
	if q.Cursor != "" {
		body["pageToken"] = q.Cursor
	}

	var resp struct {
		Entries       []logEntry `json:"entries"`
		NextPageToken string     `json:"nextPageToken"`
	}
	if err := p.postLogging(ctx, c, "/v2/entries:list", body, &resp); err != nil {
		return domain.RuntimeLogPage{}, classifyRunError(err)
	}

	page := domain.RuntimeLogPage{NextCursor: resp.NextPageToken}
	for _, e := range resp.Entries {
		page.Entries = append(page.Entries, mapLogEntry(e))
	}
	return page, nil
}

func logsFilter(ref domain.CloudResourceRef, q domain.RuntimeLogQuery) (string, error) {
	var clauses []string
	switch ref.Kind {
	case domain.CloudResourceCloudRunService:
		clauses = append(clauses,
			`resource.type="cloud_run_revision"`,
			fmt.Sprintf(`resource.labels.service_name="%s"`, ref.Name),
			fmt.Sprintf(`resource.labels.location="%s"`, ref.Region),
		)
	case domain.CloudResourceCloudRunJob:
		clauses = append(clauses,
			`resource.type="cloud_run_job"`,
			fmt.Sprintf(`resource.labels.job_name="%s"`, ref.Name),
		)
	default:
		return "", fmt.Errorf("gcloud: unsupported resource kind %q: %w", ref.Kind, port.ErrUnsupported)
	}
	if !q.Since.IsZero() {
		clauses = append(clauses, fmt.Sprintf(`timestamp>="%s"`, q.Since.UTC().Format(time.RFC3339)))
	}
	if !q.Until.IsZero() {
		clauses = append(clauses, fmt.Sprintf(`timestamp<="%s"`, q.Until.UTC().Format(time.RFC3339)))
	}
	if q.MinSeverity != "" {
		clauses = append(clauses, fmt.Sprintf(`severity>=%s`, gcpSeverity(q.MinSeverity)))
	}
	if q.Text != "" {
		clauses = append(clauses, fmt.Sprintf(`SEARCH("%s")`, escapeLogText(q.Text)))
	}
	return strings.Join(clauses, " AND "), nil
}

func escapeLogText(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

func gcpSeverity(sev domain.LogSeverity) string {
	switch sev {
	case domain.LogDebug:
		return "DEBUG"
	case domain.LogInfo:
		return "INFO"
	case domain.LogWarning:
		return "WARNING"
	case domain.LogError:
		return "ERROR"
	case domain.LogCritical:
		return "CRITICAL"
	default:
		return "DEFAULT"
	}
}

func (p *Provider) postLogging(ctx context.Context, c *Client, path string, body, out any) error {
	tok, err := c.bearerToken(ctx, logsScope)
	if err != nil {
		return err
	}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.loggingBaseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respData, _ := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &apiError{Status: resp.StatusCode, Body: domain.TruncateHead(string(respData), 500)}
	}
	if out != nil && len(respData) > 0 {
		return json.Unmarshal(respData, out)
	}
	return nil
}

type logEntry struct {
	Timestamp   string          `json:"timestamp"`
	Severity    string          `json:"severity"`
	TextPayload string          `json:"textPayload"`
	JSONPayload json.RawMessage `json:"jsonPayload"`
	Resource    struct {
		Labels map[string]string `json:"labels"`
	} `json:"resource"`
	HTTPRequest struct {
		RequestMethod string `json:"requestMethod"`
		RequestURL    string `json:"requestUrl"`
		Status        int    `json:"status"`
	} `json:"httpRequest"`
	Trace   string            `json:"trace"`
	LogName string            `json:"logName"`
	Labels  map[string]string `json:"labels"`
}

func mapLogEntry(e logEntry) domain.RuntimeLogEntry {
	entry := domain.RuntimeLogEntry{
		Severity:   mapSeverity(e.Severity),
		Message:    logMessage(e),
		Source:     logSource(e),
		Method:     e.HTTPRequest.RequestMethod,
		StatusCode: e.HTTPRequest.Status,
		TraceID:    shortName(e.Trace),
	}
	if e.HTTPRequest.RequestURL != "" {
		if u, err := url.Parse(e.HTTPRequest.RequestURL); err == nil {
			entry.Path = u.Path
		} else {
			entry.Path = e.HTTPRequest.RequestURL
		}
	}
	if t, err := time.Parse(time.RFC3339Nano, e.Timestamp); err == nil {
		entry.Timestamp = t
	}
	if logName := logShortName(e.LogName); logName != "" {
		entry.Fields = map[string]string{"log_name": logName}
	}
	return entry
}

func logSource(e logEntry) string {
	if v := e.Resource.Labels["revision_name"]; v != "" {
		return v
	}
	return e.Labels["run.googleapis.com/execution_name"]
}

func logShortName(logName string) string {
	short := shortName(logName)
	if unescaped, err := url.QueryUnescape(short); err == nil {
		return unescaped
	}
	return short
}

func logMessage(e logEntry) string {
	if e.TextPayload != "" {
		return e.TextPayload
	}
	if len(e.JSONPayload) > 0 {
		var obj map[string]any
		if err := json.Unmarshal(e.JSONPayload, &obj); err == nil {
			for _, key := range []string{"message", "msg", "error"} {
				if v, ok := obj[key].(string); ok && v != "" {
					return v
				}
			}
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, e.JSONPayload); err == nil {
			return compact.String()
		}
		return string(e.JSONPayload)
	}
	return ""
}

func mapSeverity(s string) domain.LogSeverity {
	switch s {
	case "DEBUG", "DEFAULT":
		return domain.LogDebug
	case "INFO", "NOTICE":
		return domain.LogInfo
	case "WARNING":
		return domain.LogWarning
	case "ERROR":
		return domain.LogError
	case "CRITICAL", "ALERT", "EMERGENCY":
		return domain.LogCritical
	default:
		return domain.LogInfo
	}
}
