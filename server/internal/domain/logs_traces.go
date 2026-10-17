package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

// The log and trace kinds read a component's logs and traces from a store of
// their own rather than from the cloud it runs on, through its confirmed
// logs.lines or traces.spans binding.
const (
	DataSourceKindLoki          DataSourceKind = "loki"
	DataSourceKindElasticsearch DataSourceKind = "elasticsearch"
	DataSourceKindTempo         DataSourceKind = "tempo"
)

// The catalog signals the Logs explorer and the traces view read.
const (
	SignalLogsLines   = "logs.lines"
	SignalTracesSpans = "traces.spans"
)

// LogQuery is one read of a log kind. Query is in the source's own language,
// already rendered (a LogQL selector, an Elasticsearch filter document);
// MinSeverity and Text are the console's own filters, which the kind adds to
// it in its language. Until is inclusive. Cursor is the NextCursor of the
// page before, for a kind that pages.
type LogQuery struct {
	Query       string
	Since       time.Time
	Until       time.Time
	MinSeverity LogSeverity
	Text        string
	Limit       int
	Cursor      string
}

type SpanStatus string

const (
	SpanStatusUnset SpanStatus = "unset"
	SpanStatusOK    SpanStatus = "ok"
	SpanStatusError SpanStatus = "error"
)

// Span is one operation of a trace. Attributes hold the span's own
// attributes and its resource's, the latter prefixed "resource.".
type Span struct {
	ID            string            `json:"id"`
	ParentID      string            `json:"parent_id,omitempty"`
	Service       string            `json:"service"`
	Name          string            `json:"name"`
	Kind          string            `json:"kind,omitempty"`
	Start         time.Time         `json:"start"`
	DurationMS    float64           `json:"duration_ms"`
	Status        SpanStatus        `json:"status"`
	StatusMessage string            `json:"status_message,omitempty"`
	Attributes    map[string]string `json:"attributes"`
}

// Trace is every span a source holds for one trace id, by start time.
// Truncated: the trace had more spans than one answer carries.
type Trace struct {
	ID        string `json:"id"`
	Spans     []Span `json:"spans"`
	Truncated bool   `json:"truncated"`
}

// TraceSummary is one row of a trace search. SpanCount is the spans the
// search matched, not every span of the trace.
type TraceSummary struct {
	ID          string    `json:"id"`
	RootService string    `json:"root_service"`
	RootName    string    `json:"root_name"`
	Start       time.Time `json:"start"`
	DurationMS  float64   `json:"duration_ms"`
	SpanCount   int       `json:"span_count"`
	Error       bool      `json:"error"`
}

// TraceQuery is one trace search: Query in the source's language, rendered.
type TraceQuery struct {
	Query       string
	Since       time.Time
	Until       time.Time
	Limit       int
	MinDuration time.Duration
}

// ComponentTraces is GET /v1/components/:id/traces. Bound: the component has
// a confirmed traces.spans binding in the environment; Error is why that
// binding could not be read, with Traces empty.
type ComponentTraces struct {
	ComponentID  uuid.UUID         `json:"component_id"`
	Environment  DeployEnvironment `json:"environment"`
	Bound        bool              `json:"bound"`
	DataSourceID *uuid.UUID        `json:"data_source_id"`
	Traces       []TraceSummary    `json:"traces"`
	Error        *SignalReadError  `json:"error"`
}

// TraceView is one trace and the binding it was read through.
type TraceView struct {
	Trace        Trace     `json:"trace"`
	ComponentID  uuid.UUID `json:"component_id"`
	DataSourceID uuid.UUID `json:"data_source_id"`
}

var traceIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{1,32}$`)

// NormalizeTraceID answers a W3C or Jaeger trace id in lowercase hex; false
// for anything else, which is never sent anywhere.
func NormalizeTraceID(raw string) (string, bool) {
	id := strings.TrimSpace(raw)
	if !traceIDPattern.MatchString(id) {
		return "", false
	}
	return strings.ToLower(id), true
}

const (
	maxElasticsearchFilters = 32
	maxElasticsearchValue   = 1024
)

var elasticsearchFieldPattern = regexp.MustCompile(`^[A-Za-z0-9_@][A-Za-z0-9_.@-]{0,254}$`)

// ElasticsearchQuery is the query language of the elasticsearch kind:
//
//	{"filters": {"service.name": "billing", "kubernetes.namespace": "prod"}}
//
// Every filter must match (a match query with operator and: exact on a
// keyword field, every word on a text one).
type ElasticsearchQuery struct {
	Filters map[string]string `json:"filters"`
}

func ValidElasticsearchField(name string) bool { return elasticsearchFieldPattern.MatchString(name) }

func ParseElasticsearchQuery(raw string) (ElasticsearchQuery, error) {
	var q ElasticsearchQuery
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&q); err != nil {
		return ElasticsearchQuery{}, fmt.Errorf("not an Elasticsearch query: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return ElasticsearchQuery{}, errors.New("not an Elasticsearch query: more than one JSON value")
	}
	if len(q.Filters) > maxElasticsearchFilters {
		return ElasticsearchQuery{}, fmt.Errorf("more than %d filters", maxElasticsearchFilters)
	}
	for field, v := range q.Filters {
		switch {
		case !ValidElasticsearchField(field):
			return ElasticsearchQuery{}, fmt.Errorf("filter %q is not a field name", field)
		case strings.TrimSpace(v) == "":
			return ElasticsearchQuery{}, fmt.Errorf("filter %s has no value", field)
		case len(v) > maxElasticsearchValue:
			return ElasticsearchQuery{}, fmt.Errorf("filter %s is longer than %d bytes", field, maxElasticsearchValue)
		case strings.ContainsFunc(v, unicode.IsControl):
			return ElasticsearchQuery{}, fmt.Errorf("filter %s has a control character", field)
		}
	}
	if q.Filters == nil {
		q.Filters = map[string]string{}
	}
	return q, nil
}

// SeverityFromText reads a level word ("ERROR", "warn", "Fatal", "notice")
// as a LogSeverity; false for anything else.
func SeverityFromText(raw string) (LogSeverity, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "trace", "debug", "dbug", "dbg", "verbose":
		return LogDebug, true
	case "info", "information", "informational", "notice", "inf":
		return LogInfo, true
	case "warn", "warning", "wrn":
		return LogWarning, true
	case "error", "err", "eror":
		return LogError, true
	case "critical", "crit", "fatal", "panic", "alert", "emerg", "emergency", "severe":
		return LogCritical, true
	}
	return "", false
}

// SeverityWordsBelow are the level words SeverityFromText reads as less
// severe than min, lowercase: what a source-side filter for min leaves out.
func SeverityWordsBelow(min LogSeverity) []string {
	if min == "" {
		return nil
	}
	var out []string
	for _, w := range severityWords {
		if s, _ := SeverityFromText(w); s.Rank() < min.Rank() {
			out = append(out, w)
		}
	}
	return out
}

var severityWords = []string{
	"trace", "debug", "dbug", "dbg", "verbose",
	"info", "information", "informational", "notice", "inf",
	"warn", "warning", "wrn",
	"error", "err", "eror",
	"critical", "crit", "fatal", "panic", "alert", "emerg", "emergency", "severe",
}

var (
	linePrefixLevel = regexp.MustCompile(`^\W{0,3}([A-Za-z]{3,11})\b`)
	lineKeyedLevel  = regexp.MustCompile(`(?i)"?\b(?:level|lvl|severity|loglevel)"?\s*[=:]\s*"?([A-Za-z]{3,11})\b`)
)

// SeverityFromLine guesses a line's level from a leading level word
// ("ERROR ...", "[warn] ...") or a level key (level=error, "level":"warn");
// false when it says none.
func SeverityFromLine(line string) (LogSeverity, bool) {
	head := line
	if len(head) > 512 {
		head = head[:512]
	}
	if m := lineKeyedLevel.FindStringSubmatch(head); m != nil {
		if s, ok := SeverityFromText(m[1]); ok {
			return s, true
		}
	}
	if m := linePrefixLevel.FindStringSubmatch(head); m != nil {
		if s, ok := SeverityFromText(m[1]); ok {
			return s, true
		}
	}
	return "", false
}
