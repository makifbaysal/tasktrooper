// Package tempo is the data source kind for Grafana Tempo, self-hosted or
// Grafana Cloud Traces: one trace by id (OTLP JSON), TraceQL searches, and
// its tag names and values for the detector.
package tempo

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/httpapi"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	apiName = "Tempo"

	verifyTimeout = 15 * time.Second
	queryTimeout  = 30 * time.Second

	maxSmallBody  = 1 << 20
	maxTagsBody   = 8 << 20
	maxSearchBody = 16 << 20
	maxTraceBody  = 32 << 20

	// A trace answers at most maxTraceSpans spans (the earliest), each with
	// at most maxSpanAttrs attributes of its own and maxResourceAttrs of its
	// resource: the resource's are copied into every span of it.
	maxTraceSpans    = 20000
	maxSpanAttrs     = 128
	maxResourceAttrs = 32

	defaultLimit = 20
	maxLimit     = 100
	maxTags      = 5000
	maxAttrValue = 1024
	// tagWindow keeps tag listings to recent traces, which Tempo answers
	// from its ingesters without scanning blocks.
	tagWindow = time.Hour
)

var tagPattern = regexp.MustCompile(`^(\.|(resource|span|event|link|instrumentation)\.)?[A-Za-z0-9_.:-]+$`)

type Adapter struct {
	policy urlguard.Policy
	now    func() time.Time
}

var (
	_ port.DataSourceKindAdapter = (*Adapter)(nil)
	_ port.TracesQuerier         = (*Adapter)(nil)
	_ port.Discoverer            = (*Adapter)(nil)
	_ port.LabelValuesLister     = (*Adapter)(nil)
)

// New answers an adapter that refuses every destination (the zero
// urlguard.Policy allows no scheme) until SetURLPolicy.
func New() *Adapter { return &Adapter{now: time.Now} }

func (a *Adapter) SetURLPolicy(p urlguard.Policy) { a.policy = p }

func (a *Adapter) Kind() domain.DataSourceKind { return domain.DataSourceKindTempo }

func (a *Adapter) Capabilities() []domain.Capability {
	return []domain.Capability{domain.CapabilityTraces}
}

func (a *Adapter) Schema() domain.DataSourceSchema {
	fields := []domain.DataSourceField{{
		Key: httpapi.KeyURL, Label: "URL", Type: domain.FieldURL, Required: true,
		Placeholder: "http://localhost:3200",
		Help:        "The Tempo HTTP API base, without /api. For Grafana Cloud, the Traces URL ending in /tempo.",
	}}
	fields = append(fields, httpapi.AuthFields(
		[]string{httpapi.AuthNone, httpapi.AuthBearer, httpapi.AuthBasic},
		"For Grafana Cloud, the Traces user ID.",
		"For Grafana Cloud, an access policy token with traces:read.",
		"X-Scope-OrgID: tenant-1",
	)...)
	return domain.DataSourceSchema{
		Label:       "Tempo",
		Description: "Grafana Tempo or Grafana Cloud Traces: a component's traces, searched with TraceQL.",
		Fields:      fields,
	}
}

var errNotTempo = errors.New("the URL answered, but not as the Tempo API — is it the Tempo base URL?")

func (a *Adapter) open(ctx context.Context, ds domain.DataSource, secrets map[string]string, timeout time.Duration) (*httpapi.Conn, error) {
	ep, err := httpapi.NewEndpoint(apiName, ds, secrets)
	if err != nil {
		return nil, err
	}
	return httpapi.Open(ctx, a.policy, ep, timeout)
}

// Verify reads build info, falling back to /ready on anything but success
// or an auth refusal: a gateway in front of Tempo may not route it.
func (a *Adapter) Verify(ctx context.Context, ds domain.DataSource, secrets map[string]string) (domain.DataSourceInfo, error) {
	c, err := a.open(ctx, ds, secrets, verifyTimeout)
	if err != nil {
		return domain.DataSourceInfo{}, err
	}
	defer c.Close()
	r, err := c.Do(ctx, httpapi.Request{Path: "/api/status/buildinfo", Limit: maxSmallBody})
	if err != nil {
		return domain.DataSourceInfo{}, err
	}
	switch {
	case r.OK():
		var bi struct {
			Version string `json:"version"`
		}
		if json.Unmarshal(r.Body, &bi) != nil || bi.Version == "" {
			return domain.DataSourceInfo{}, errNotTempo
		}
		return domain.DataSourceInfo{Version: bi.Version, Detail: "Tempo"}, nil
	case r.Status == http.StatusUnauthorized || r.Status == http.StatusForbidden:
		return domain.DataSourceInfo{}, c.StatusError("build info", r)
	}
	r, err = c.Do(ctx, httpapi.Request{Path: "/ready", Limit: maxSmallBody})
	if err != nil {
		return domain.DataSourceInfo{}, err
	}
	if !r.OK() {
		return domain.DataSourceInfo{}, c.StatusError("readiness", r)
	}
	if strings.TrimSpace(string(r.Body)) != "ready" {
		return domain.DataSourceInfo{}, errNotTempo
	}
	return domain.DataSourceInfo{Detail: "Tempo (no build info)"}, nil
}

func refusal(c *httpapi.Conn, op string, r httpapi.Response) error {
	if r.Status == http.StatusBadRequest {
		if d := c.Detail(r); d != "" {
			return fmt.Errorf("%w: %s", port.ErrDataSourceQuery, d)
		}
		return port.ErrDataSourceQuery
	}
	return c.StatusError(op, r)
}

// Trace reads /api/traces/<id>: OTLP JSON, as "batches" (Tempo 1 and 2) or
// "resourceSpans", with "scopeSpans" or the older
// "instrumentationLibrarySpans".
func (a *Adapter) Trace(ctx context.Context, ds domain.DataSource, secrets map[string]string, traceID string) (domain.Trace, error) {
	id, ok := domain.NormalizeTraceID(traceID)
	if !ok {
		return domain.Trace{}, fmt.Errorf("%w: %q is not a trace id", port.ErrDataSourceQuery, traceID)
	}
	c, err := a.open(ctx, ds, secrets, queryTimeout)
	if err != nil {
		return domain.Trace{}, err
	}
	defer c.Close()
	r, err := c.Do(ctx, httpapi.Request{Path: "/api/traces/" + id, Limit: maxTraceBody})
	if err != nil {
		return domain.Trace{}, err
	}
	switch {
	case r.Status == http.StatusNotFound:
		return domain.Trace{}, fmt.Errorf("no trace %s in Tempo: %w", id, port.ErrNotFound)
	case !r.OK():
		return domain.Trace{}, refusal(c, "trace", r)
	}
	var doc otlpTrace
	if err := json.Unmarshal(r.Body, &doc); err != nil {
		return domain.Trace{}, errNotTempo
	}
	spans, truncated := doc.spans()
	return domain.Trace{ID: id, Spans: spans, Truncated: truncated}, nil
}

type otlpTrace struct {
	Batches       []resourceSpans `json:"batches"`
	ResourceSpans []resourceSpans `json:"resourceSpans"`
}

type resourceSpans struct {
	Resource struct {
		Attributes []keyValue `json:"attributes"`
	} `json:"resource"`
	ScopeSpans                  []scopeSpans `json:"scopeSpans"`
	InstrumentationLibrarySpans []scopeSpans `json:"instrumentationLibrarySpans"`
}

type scopeSpans struct {
	Spans []otlpSpan `json:"spans"`
}

type otlpSpan struct {
	SpanID            string          `json:"spanId"`
	ParentSpanID      string          `json:"parentSpanId"`
	Name              string          `json:"name"`
	Kind              json.RawMessage `json:"kind"`
	StartTimeUnixNano json.RawMessage `json:"startTimeUnixNano"`
	EndTimeUnixNano   json.RawMessage `json:"endTimeUnixNano"`
	Attributes        []keyValue      `json:"attributes"`
	Status            struct {
		Code    json.RawMessage `json:"code"`
		Message string          `json:"message"`
	} `json:"status"`
}

type keyValue struct {
	Key   string   `json:"key"`
	Value anyValue `json:"value"`
}

type anyValue struct {
	StringValue *string         `json:"stringValue"`
	IntValue    json.RawMessage `json:"intValue"`
	DoubleValue *float64        `json:"doubleValue"`
	BoolValue   *bool           `json:"boolValue"`
	BytesValue  *string         `json:"bytesValue"`
	ArrayValue  *struct {
		Values []anyValue `json:"values"`
	} `json:"arrayValue"`
	KvlistValue *struct {
		Values []keyValue `json:"values"`
	} `json:"kvlistValue"`
}

func (v anyValue) String() string {
	switch {
	case v.StringValue != nil:
		return *v.StringValue
	case len(v.IntValue) > 0:
		return strings.Trim(string(v.IntValue), `"`)
	case v.DoubleValue != nil:
		return strconv.FormatFloat(*v.DoubleValue, 'f', -1, 64)
	case v.BoolValue != nil:
		return strconv.FormatBool(*v.BoolValue)
	case v.BytesValue != nil:
		return *v.BytesValue
	case v.ArrayValue != nil:
		parts := make([]string, 0, len(v.ArrayValue.Values))
		for _, item := range v.ArrayValue.Values {
			parts = append(parts, item.String())
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case v.KvlistValue != nil:
		parts := make([]string, 0, len(v.KvlistValue.Values))
		for _, kv := range v.KvlistValue.Values {
			parts = append(parts, kv.Key+"="+kv.Value.String())
		}
		return "{" + strings.Join(parts, ", ") + "}"
	}
	return ""
}

// spans are the trace's earliest maxTraceSpans, by start; only those are
// turned into spans, so a huge trace costs no attribute maps for the rest.
func (t otlpTrace) spans() ([]domain.Span, bool) {
	type ref struct {
		span     *otlpSpan
		resource int
		start    int64
	}
	var resources []map[string]string
	var refs []ref
	for _, rs := range append(t.Batches, t.ResourceSpans...) {
		resources = append(resources, resourceAttributes(rs.Resource.Attributes))
		for _, ss := range append(rs.ScopeSpans, rs.InstrumentationLibrarySpans...) {
			for i := range ss.Spans {
				refs = append(refs, ref{span: &ss.Spans[i], resource: len(resources) - 1, start: unixNanos(ss.Spans[i].StartTimeUnixNano)})
			}
		}
	}
	sort.SliceStable(refs, func(i, j int) bool { return refs[i].start < refs[j].start })
	truncated := len(refs) > maxTraceSpans
	refs = refs[:min(len(refs), maxTraceSpans)]
	out := make([]domain.Span, 0, len(refs))
	for _, r := range refs {
		out = append(out, span(*r.span, resources[r.resource]))
	}
	return out, truncated
}

// resourceAttributes keeps service.name and the first others by name, up to
// maxResourceAttrs.
func resourceAttributes(kvs []keyValue) map[string]string {
	all := make(map[string]string, len(kvs))
	for _, kv := range kvs {
		all[kv.Key] = truncate(kv.Value.String())
	}
	if len(all) <= maxResourceAttrs {
		return all
	}
	out := make(map[string]string, maxResourceAttrs)
	if v, ok := all["service.name"]; ok {
		out["service.name"] = v
	}
	for _, k := range slices.Sorted(maps.Keys(all)) {
		if len(out) == maxResourceAttrs {
			break
		}
		out[k] = all[k]
	}
	return out
}

func span(s otlpSpan, resource map[string]string) domain.Span {
	start, end := unixNanos(s.StartTimeUnixNano), unixNanos(s.EndTimeUnixNano)
	out := domain.Span{
		ID:            hexID(s.SpanID),
		ParentID:      hexID(s.ParentSpanID),
		Service:       resource["service.name"],
		Name:          s.Name,
		Kind:          spanKind(s.Kind),
		Start:         time.Unix(0, start).UTC(),
		Status:        spanStatus(s.Status.Code),
		StatusMessage: s.Status.Message,
		Attributes:    make(map[string]string, len(s.Attributes)+len(resource)),
	}
	if end > start {
		out.DurationMS = float64(end-start) / float64(time.Millisecond)
	}
	for k, v := range resource {
		out.Attributes["resource."+k] = v
	}
	for _, kv := range s.Attributes[:min(len(s.Attributes), maxSpanAttrs)] {
		out.Attributes[kv.Key] = truncate(kv.Value.String())
	}
	return out
}

// unixNanos reads a uint64 the OTLP JSON mapping writes as a string, which
// some encoders write as a number instead.
func unixNanos(raw json.RawMessage) int64 {
	n, err := strconv.ParseInt(strings.Trim(string(raw), `"`), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// hexID: OTLP JSON writes ids in hex, protobuf JSON (what Tempo answers) in
// base64; 16 or 32 hex digits are never valid base64 of 8 or 16 bytes.
func hexID(raw string) string {
	if raw == "" {
		return ""
	}
	if (len(raw) == 16 || len(raw) == 32) && isHex(raw) {
		return strings.ToLower(raw)
	}
	if b, err := base64.StdEncoding.DecodeString(raw); err == nil {
		return hex.EncodeToString(b)
	}
	return raw
}

// searchID: a search answers trace ids in hex with leading zeros dropped;
// padded back, one matches the id a log line or a span carries.
func searchID(raw string) string {
	if id, ok := domain.NormalizeTraceID(raw); ok {
		return strings.Repeat("0", 32-len(id)) + id
	}
	return raw
}

func isHex(s string) bool {
	_, err := hex.DecodeString(s)
	return err == nil
}

var spanKinds = map[string]string{
	"SPAN_KIND_INTERNAL": "internal", "1": "internal",
	"SPAN_KIND_SERVER": "server", "2": "server",
	"SPAN_KIND_CLIENT": "client", "3": "client",
	"SPAN_KIND_PRODUCER": "producer", "4": "producer",
	"SPAN_KIND_CONSUMER": "consumer", "5": "consumer",
}

func spanKind(raw json.RawMessage) string { return spanKinds[strings.Trim(string(raw), `"`)] }

func spanStatus(raw json.RawMessage) domain.SpanStatus {
	switch strings.Trim(string(raw), `"`) {
	case "STATUS_CODE_OK", "1":
		return domain.SpanStatusOK
	case "STATUS_CODE_ERROR", "2":
		return domain.SpanStatusError
	}
	return domain.SpanStatusUnset
}

func truncate(s string) string {
	if r := []rune(s); len(r) > maxAttrValue {
		return string(r[:maxAttrValue]) + "…"
	}
	return s
}

type searchResponse struct {
	Traces []struct {
		TraceID           string    `json:"traceID"`
		RootServiceName   string    `json:"rootServiceName"`
		RootTraceName     string    `json:"rootTraceName"`
		StartTimeUnixNano string    `json:"startTimeUnixNano"`
		DurationMs        *float64  `json:"durationMs"`
		SpanSet           *spanSet  `json:"spanSet"`
		SpanSets          []spanSet `json:"spanSets"`
	} `json:"traces"`
}

type spanSet struct {
	Matched int `json:"matched"`
}

// SearchTraces runs a TraceQL search over the window. Tempo is given
// minDuration and the answer is held to it too, since not every version
// applies it beside a TraceQL query. Error is set on a trace that also
// matches the query with { status = error }, asked as a second search whose
// failure only leaves every Error unset.
func (a *Adapter) SearchTraces(ctx context.Context, ds domain.DataSource, secrets map[string]string, q domain.TraceQuery) ([]domain.TraceSummary, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	limit = min(limit, maxLimit)
	c, err := a.open(ctx, ds, secrets, queryTimeout)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	found, err := a.search(ctx, c, q, q.Query, limit)
	if err != nil {
		return nil, err
	}
	minMS := float64(q.MinDuration) / float64(time.Millisecond)
	out := make([]domain.TraceSummary, 0, len(found.Traces))
	for _, t := range found.Traces {
		s := domain.TraceSummary{
			ID:          searchID(t.TraceID),
			RootService: t.RootServiceName,
			RootName:    t.RootTraceName,
			Start:       time.Unix(0, unixNanos(json.RawMessage(t.StartTimeUnixNano))).UTC(),
		}
		if t.DurationMs != nil {
			s.DurationMS = *t.DurationMs
		}
		if s.DurationMS < minMS {
			continue
		}
		switch {
		case len(t.SpanSets) > 0:
			for _, set := range t.SpanSets {
				s.SpanCount += set.Matched
			}
		case t.SpanSet != nil:
			s.SpanCount = t.SpanSet.Matched
		}
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start.After(out[j].Start) })
	if len(out) > 0 {
		a.markErrors(ctx, c, q, limit, out)
	}
	return out, nil
}

func (a *Adapter) markErrors(ctx context.Context, c *httpapi.Conn, q domain.TraceQuery, limit int, out []domain.TraceSummary) {
	failed, err := a.search(ctx, c, q, "("+q.Query+") && { status = error }", limit)
	if err != nil {
		log.Debug().Err(err).Msg("tempo: the error search failed; traces are left unmarked")
		return
	}
	ids := map[string]bool{}
	for _, t := range failed.Traces {
		ids[searchID(t.TraceID)] = true
	}
	for i := range out {
		out[i].Error = ids[out[i].ID]
	}
}

func (a *Adapter) search(ctx context.Context, c *httpapi.Conn, q domain.TraceQuery, query string, limit int) (searchResponse, error) {
	params := url.Values{
		"q":     {query},
		"start": {strconv.FormatInt(q.Since.Unix(), 10)},
		"end":   {strconv.FormatInt(int64(math.Ceil(float64(q.Until.UnixNano())/float64(time.Second))), 10)},
		"limit": {strconv.Itoa(limit)},
	}
	if q.MinDuration > 0 {
		params.Set("minDuration", strconv.FormatInt(q.MinDuration.Milliseconds(), 10)+"ms")
	}
	r, err := c.Do(ctx, httpapi.Request{Path: "/api/search", Query: params, Limit: maxSearchBody})
	if err != nil {
		return searchResponse{}, err
	}
	if !r.OK() {
		return searchResponse{}, refusal(c, "search", r)
	}
	var res searchResponse
	if json.Unmarshal(r.Body, &res) != nil {
		return searchResponse{}, errNotTempo
	}
	return res, nil
}

// Discover lists the scoped tags (resource.service.name, span.http.route)
// of recent traces: a recipe's detect metric for this kind is a tag it
// compares. Tempo before 2.2 has no scopes; its tags are listed unscoped
// (.service.name).
func (a *Adapter) Discover(ctx context.Context, ds domain.DataSource, secrets map[string]string) (domain.Inventory, error) {
	c, err := a.open(ctx, ds, secrets, queryTimeout)
	if err != nil {
		return domain.Inventory{}, err
	}
	defer c.Close()
	window := a.window()
	r, err := c.Do(ctx, httpapi.Request{Path: "/api/v2/search/tags", Query: window, Limit: maxTagsBody})
	if err != nil {
		return domain.Inventory{}, err
	}
	var names []string
	switch {
	case r.OK():
		var v2 struct {
			Scopes []struct {
				Name string   `json:"name"`
				Tags []string `json:"tags"`
			} `json:"scopes"`
		}
		if json.Unmarshal(r.Body, &v2) != nil {
			return domain.Inventory{}, errNotTempo
		}
		for _, s := range v2.Scopes {
			if s.Name == "intrinsic" {
				continue
			}
			for _, t := range s.Tags {
				names = append(names, s.Name+"."+t)
			}
		}
	case r.Status == http.StatusNotFound:
		r, err = c.Do(ctx, httpapi.Request{Path: "/api/search/tags", Query: window, Limit: maxTagsBody})
		if err != nil {
			return domain.Inventory{}, err
		}
		if !r.OK() {
			return domain.Inventory{}, refusal(c, "tags", r)
		}
		var v1 struct {
			TagNames []string `json:"tagNames"`
		}
		if json.Unmarshal(r.Body, &v1) != nil {
			return domain.Inventory{}, errNotTempo
		}
		for _, t := range v1.TagNames {
			names = append(names, "."+t)
		}
	default:
		return domain.Inventory{}, refusal(c, "tags", r)
	}
	sort.Strings(names)
	names = nonNil(slices.Compact(names))
	inv := domain.Inventory{Metrics: names}
	if len(names) > maxTags {
		inv.Metrics, inv.Truncated = names[:maxTags], true
	}
	return inv, nil
}

// LabelValues lists a tag's values in recent traces, falling back to the
// unscoped v1 endpoint on a Tempo without v2.
func (a *Adapter) LabelValues(ctx context.Context, ds domain.DataSource, secrets map[string]string, tag string) ([]string, error) {
	if !tagPattern.MatchString(tag) {
		return nil, fmt.Errorf("%w: %q is not a TraceQL tag", port.ErrDataSourceQuery, tag)
	}
	c, err := a.open(ctx, ds, secrets, queryTimeout)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	window := a.window()
	r, err := c.Do(ctx, httpapi.Request{Path: "/api/v2/search/tag/" + url.PathEscape(tag) + "/values", Query: window, Limit: maxTagsBody})
	if err != nil {
		return nil, err
	}
	var values []string
	switch {
	case r.OK():
		var v2 struct {
			TagValues []struct {
				Value string `json:"value"`
			} `json:"tagValues"`
		}
		if json.Unmarshal(r.Body, &v2) != nil {
			return nil, errNotTempo
		}
		for _, v := range v2.TagValues {
			values = append(values, v.Value)
		}
	case r.Status == http.StatusNotFound:
		unscoped := tag[strings.Index(tag, ".")+1:]
		r, err = c.Do(ctx, httpapi.Request{Path: "/api/search/tag/" + url.PathEscape(unscoped) + "/values", Query: window, Limit: maxTagsBody})
		if err != nil {
			return nil, err
		}
		if !r.OK() {
			return nil, refusal(c, "tag values", r)
		}
		var v1 struct {
			TagValues []string `json:"tagValues"`
		}
		if json.Unmarshal(r.Body, &v1) != nil {
			return nil, errNotTempo
		}
		values = v1.TagValues
	default:
		return nil, refusal(c, "tag values", r)
	}
	sort.Strings(values)
	values = nonNil(slices.Compact(values))
	return values[:min(len(values), maxTags)], nil
}

func (a *Adapter) window() url.Values {
	now := a.now()
	return url.Values{
		"start": {strconv.FormatInt(now.Add(-tagWindow).Unix(), 10)},
		"end":   {strconv.FormatInt(now.Unix(), 10)},
	}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
