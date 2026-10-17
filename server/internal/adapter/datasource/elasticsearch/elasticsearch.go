// Package elasticsearch is the data source kind for log lines kept in
// Elasticsearch or OpenSearch (self-hosted, Elastic Cloud, Amazon OpenSearch
// Service behind basic auth): a bool query over one index pattern, newest
// first, paged with search_after.
package elasticsearch

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/httpapi"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	apiName = "Elasticsearch"

	keyIndex        = "index"
	keyTimeField    = "time_field"
	keyMessageField = "message_field"
	keyLevelField   = "level_field"

	defaultTimeField    = "@timestamp"
	defaultMessageField = "message"
	defaultLevelField   = "log.level"

	verifyTimeout = 15 * time.Second
	queryTimeout  = 30 * time.Second

	maxSmallBody  = 1 << 20
	maxFieldsBody = 16 << 20
	maxSearchBody = 32 << 20

	defaultLimit   = 100
	maxLimit       = 1000
	maxInventory   = 5000
	maxTermsValues = 500
	maxFields      = 64
	maxFieldValue  = 1024
	maxCursorItems = 8
	maxIndexLen    = 1024
	valuesWindow   = time.Hour
)

// indexPattern admits an index, data stream, alias or pattern list
// (logs-*,-logs-debug, remote:logs-*): never a path of its own, nor an API
// name (_search) in its place.
var indexPattern = regexp.MustCompile(`^[A-Za-z0-9.*+-][A-Za-z0-9._*,:+-]*$`)

var (
	sourceFields  = []string{"service.name", "kubernetes.container.name", "container.name", "host.name"}
	traceIDFields = []string{"trace.id", "trace_id", "traceId", "traceID"}
	messageAlts   = []string{"event.original", "log", "msg"}
)

type Adapter struct {
	policy urlguard.Policy
	now    func() time.Time
}

var (
	_ port.DataSourceKindAdapter = (*Adapter)(nil)
	_ port.LogsQuerier           = (*Adapter)(nil)
	_ port.Discoverer            = (*Adapter)(nil)
	_ port.LabelValuesLister     = (*Adapter)(nil)
)

// New answers an adapter that refuses every destination (the zero
// urlguard.Policy allows no scheme) until SetURLPolicy.
func New() *Adapter { return &Adapter{now: time.Now} }

func (a *Adapter) SetURLPolicy(p urlguard.Policy) { a.policy = p }

func (a *Adapter) Kind() domain.DataSourceKind { return domain.DataSourceKindElasticsearch }

func (a *Adapter) Capabilities() []domain.Capability {
	return []domain.Capability{domain.CapabilityLogs}
}

func (a *Adapter) Schema() domain.DataSourceSchema {
	fields := []domain.DataSourceField{
		{
			Key: httpapi.KeyURL, Label: "URL", Type: domain.FieldURL, Required: true,
			Placeholder: "http://localhost:9200",
			Help:        "The Elasticsearch or OpenSearch base URL. For Elastic Cloud, the deployment's Elasticsearch endpoint.",
		},
		{
			Key: keyIndex, Label: "Index", Type: domain.FieldText, Required: true,
			Placeholder: "logs-*",
			Help:        "The index, data stream or pattern holding the logs; several separated by commas (logs-*,app-*).",
		},
		{
			Key: keyTimeField, Label: "Time field", Type: domain.FieldText, Placeholder: defaultTimeField,
			Help: "The date field each line is sorted and filtered by. Empty means @timestamp.",
		},
		{
			Key: keyMessageField, Label: "Message field", Type: domain.FieldText, Placeholder: defaultMessageField,
			Help: "The field holding the line itself; the text filter searches it. Empty means message.",
		},
		{
			Key: keyLevelField, Label: "Level field", Type: domain.FieldText, Placeholder: defaultLevelField,
			Help: "The field holding the level (ECS log.level). Empty means log.level.",
		},
	}
	fields = append(fields, httpapi.AuthFields(
		[]string{httpapi.AuthNone, httpapi.AuthBasic, httpapi.AuthAPIKey},
		"", "", "",
	)...)
	return domain.DataSourceSchema{
		Label:       "Elasticsearch / OpenSearch",
		Description: "Log lines kept in Elasticsearch, OpenSearch or Elastic Cloud: one index pattern, filtered by field.",
		Fields:      fields,
	}
}

var errNotElasticsearch = errors.New("the URL answered, but not as Elasticsearch or OpenSearch — is it the cluster's base URL?")

type settings struct {
	index, timeField, messageField, levelField string
}

func settingsOf(ds domain.DataSource) (settings, error) {
	s := settings{
		index:        strings.TrimSpace(ds.Config[keyIndex]),
		timeField:    orDefault(ds.Config[keyTimeField], defaultTimeField),
		messageField: orDefault(ds.Config[keyMessageField], defaultMessageField),
		levelField:   orDefault(ds.Config[keyLevelField], defaultLevelField),
	}
	if len(s.index) > maxIndexLen || !indexPattern.MatchString(s.index) {
		return settings{}, fmt.Errorf("index %q is not an index name or pattern", s.index)
	}
	for _, f := range []string{s.timeField, s.messageField, s.levelField} {
		if !domain.ValidElasticsearchField(f) {
			return settings{}, fmt.Errorf("%q is not a field name", f)
		}
	}
	return s, nil
}

func orDefault(v, def string) string {
	if v = strings.TrimSpace(v); v != "" {
		return v
	}
	return def
}

func (a *Adapter) open(ctx context.Context, ds domain.DataSource, secrets map[string]string, timeout time.Duration) (*httpapi.Conn, settings, error) {
	s, err := settingsOf(ds)
	if err != nil {
		return nil, settings{}, err
	}
	ep, err := httpapi.NewEndpoint(apiName, ds, secrets)
	if err != nil {
		return nil, settings{}, err
	}
	c, err := httpapi.Open(ctx, a.policy, ep, timeout)
	return c, s, err
}

// Verify reads the cluster's root (its version and distribution), then
// checks the index pattern matches an index holding the time field.
func (a *Adapter) Verify(ctx context.Context, ds domain.DataSource, secrets map[string]string) (domain.DataSourceInfo, error) {
	c, s, err := a.open(ctx, ds, secrets, verifyTimeout)
	if err != nil {
		return domain.DataSourceInfo{}, err
	}
	defer c.Close()
	r, err := c.Do(ctx, httpapi.Request{Path: "/", Limit: maxSmallBody})
	if err != nil {
		return domain.DataSourceInfo{}, err
	}
	if !r.OK() {
		return domain.DataSourceInfo{}, c.StatusError("cluster info", r)
	}
	var root struct {
		ClusterName string `json:"cluster_name"`
		Version     struct {
			Number       string `json:"number"`
			Distribution string `json:"distribution"`
		} `json:"version"`
	}
	if json.Unmarshal(r.Body, &root) != nil || root.Version.Number == "" {
		return domain.DataSourceInfo{}, errNotElasticsearch
	}
	product := "Elasticsearch"
	if root.Version.Distribution == "opensearch" {
		product = "OpenSearch"
	}
	caps, err := a.fieldCaps(ctx, c, s, s.timeField)
	if err != nil {
		return domain.DataSourceInfo{}, err
	}
	switch {
	case len(caps.Indices) == 0:
		return domain.DataSourceInfo{}, fmt.Errorf("no index matches %s", s.index)
	case caps.Fields[s.timeField] == nil:
		return domain.DataSourceInfo{}, fmt.Errorf("no index matching %s has the time field %s", s.index, s.timeField)
	}
	detail := fmt.Sprintf("%s · %d indices match %s", product, len(caps.Indices), s.index)
	if root.ClusterName != "" {
		detail = fmt.Sprintf("%s · %s · %d indices match %s", product, root.ClusterName, len(caps.Indices), s.index)
	}
	return domain.DataSourceInfo{Version: root.Version.Number, Detail: detail}, nil
}

type fieldCaps struct {
	Indices []string                   `json:"indices"`
	Fields  map[string]json.RawMessage `json:"fields"`
}

func (a *Adapter) fieldCaps(ctx context.Context, c *httpapi.Conn, s settings, fields string) (fieldCaps, error) {
	r, err := c.Do(ctx, httpapi.Request{
		Path:  "/" + s.index + "/_field_caps",
		Query: url.Values{"fields": {fields}, "ignore_unavailable": {"true"}, "allow_no_indices": {"true"}},
		Limit: maxFieldsBody,
	})
	if err != nil {
		return fieldCaps{}, err
	}
	if !r.OK() {
		return fieldCaps{}, refusal(c, "field list", r)
	}
	var caps fieldCaps
	if json.Unmarshal(r.Body, &caps) != nil {
		return fieldCaps{}, errNotElasticsearch
	}
	return caps, nil
}

// refusal: a 400 is a query the cluster could not parse or run (a filter on
// a field it cannot search, a sort on an unmapped one).
func refusal(c *httpapi.Conn, op string, r httpapi.Response) error {
	if r.Status == http.StatusBadRequest {
		if d := c.Detail(r); d != "" {
			return fmt.Errorf("%w: %s", port.ErrDataSourceQuery, d)
		}
		return port.ErrDataSourceQuery
	}
	return c.StatusError(op, r)
}

type searchHit struct {
	Source json.RawMessage `json:"_source"`
	Sort   []any           `json:"sort"`
}

type searchResult struct {
	Hits struct {
		Hits []searchHit `json:"hits"`
	} `json:"hits"`
	Aggregations struct {
		Values struct {
			Buckets []struct {
				Key         any    `json:"key"`
				KeyAsString string `json:"key_as_string"`
			} `json:"buckets"`
		} `json:"values"`
	} `json:"aggregations"`
}

func (a *Adapter) search(ctx context.Context, c *httpapi.Conn, s settings, op string, body map[string]any) (searchResult, error) {
	r, err := c.Do(ctx, httpapi.Request{
		Method: http.MethodPost,
		Path:   "/" + s.index + "/_search",
		Query:  url.Values{"ignore_unavailable": {"true"}, "allow_no_indices": {"true"}},
		JSON:   body,
		Limit:  maxSearchBody,
	})
	if err != nil {
		return searchResult{}, err
	}
	if !r.OK() {
		return searchResult{}, refusal(c, op, r)
	}
	var res searchResult
	dec := json.NewDecoder(bytes.NewReader(r.Body))
	// Sort values stay json.Numbers: a date_nanos sort value does not fit a
	// float64, and the cursor hands it back as it came.
	dec.UseNumber()
	if dec.Decode(&res) != nil {
		return searchResult{}, errNotElasticsearch
	}
	return res, nil
}

// Logs reads the newest lines of the window, the source's filters and the
// console's text (a phrase in the message field) and severity (the lower
// levels left out by the level field) applied in the cluster. A line with
// no level field passes that and is judged here by its message.
func (a *Adapter) Logs(ctx context.Context, ds domain.DataSource, secrets map[string]string, q domain.LogQuery) (domain.RuntimeLogPage, error) {
	query, err := domain.ParseElasticsearchQuery(q.Query)
	if err != nil {
		return domain.RuntimeLogPage{}, fmt.Errorf("%w: %v", port.ErrDataSourceQuery, err)
	}
	after, err := decodeCursor(q.Cursor)
	if err != nil {
		return domain.RuntimeLogPage{}, err
	}
	limit := q.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	limit = min(limit, maxLimit)
	c, s, err := a.open(ctx, ds, secrets, queryTimeout)
	if err != nil {
		return domain.RuntimeLogPage{}, err
	}
	defer c.Close()
	res, err := a.search(ctx, c, s, "search", searchBody(s, query, q, limit, after))
	if err != nil {
		return domain.RuntimeLogPage{}, err
	}
	hits := res.Hits.Hits
	page := domain.RuntimeLogPage{Entries: make([]domain.RuntimeLogEntry, 0, len(hits))}
	if len(hits) >= limit {
		page.Truncated = true
		if last := hits[len(hits)-1]; len(last.Sort) > 0 {
			page.NextCursor = encodeCursor(last.Sort)
		}
	}
	for _, h := range hits {
		e := hitEntry(h, s)
		if q.MinSeverity != "" && e.Severity.Rank() < q.MinSeverity.Rank() {
			continue
		}
		page.Entries = append(page.Entries, e)
	}
	return page, nil
}

func searchBody(s settings, query domain.ElasticsearchQuery, q domain.LogQuery, limit int, after []any) map[string]any {
	filter := []any{map[string]any{"range": map[string]any{s.timeField: map[string]any{
		"gte": q.Since.UnixMilli(), "lte": q.Until.UnixMilli(), "format": "epoch_millis",
	}}}}
	fields := make([]string, 0, len(query.Filters))
	for f := range query.Filters {
		fields = append(fields, f)
	}
	sort.Strings(fields)
	for _, f := range fields {
		filter = append(filter, map[string]any{"match": map[string]any{f: map[string]any{"query": query.Filters[f], "operator": "and"}}})
	}
	if q.Text != "" {
		filter = append(filter, map[string]any{"match_phrase": map[string]any{s.messageField: q.Text}})
	}
	boolQuery := map[string]any{"filter": filter}
	if words := domain.SeverityWordsBelow(q.MinSeverity); len(words) > 0 {
		boolQuery["must_not"] = []any{map[string]any{"terms": map[string]any{s.levelField: caseVariants(words)}}}
	}
	// _doc breaks ties between lines of the same timestamp, so search_after
	// neither skips nor repeats those that straddle a page; it needs no point
	// in time, which _shard_doc would, and Elasticsearch and OpenSearch both
	// take it.
	body := map[string]any{
		"size":             limit,
		"track_total_hits": false,
		"sort": []any{
			map[string]any{s.timeField: map[string]any{"order": "desc", "unmapped_type": "date"}},
			map[string]any{"_doc": map[string]any{"order": "desc"}},
		},
		"query": map[string]any{"bool": boolQuery},
	}
	if len(after) > 0 {
		body["search_after"] = after
	}
	return body
}

// caseVariants: a terms filter on a keyword field is case-sensitive, and
// shippers write "error", "ERROR" and "Error" alike.
func caseVariants(words []string) []string {
	out := make([]string, 0, 3*len(words))
	for _, w := range words {
		out = append(out, w, strings.ToUpper(w), strings.ToUpper(w[:1])+w[1:])
	}
	return out
}

func encodeCursor(sortValues []any) string {
	raw, err := json.Marshal(sortValues)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeCursor(cursor string) ([]any, error) {
	if cursor == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, fmt.Errorf("%w: the cursor is not one this source handed out", port.ErrDataSourceQuery)
	}
	var values []any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if dec.Decode(&values) != nil || len(values) == 0 || len(values) > maxCursorItems {
		return nil, fmt.Errorf("%w: the cursor is not one this source handed out", port.ErrDataSourceQuery)
	}
	for _, v := range values {
		switch v.(type) {
		case json.Number, string, bool, nil:
		default:
			return nil, fmt.Errorf("%w: the cursor is not one this source handed out", port.ErrDataSourceQuery)
		}
	}
	return values, nil
}

func hitEntry(h searchHit, s settings) domain.RuntimeLogEntry {
	var src any
	dec := json.NewDecoder(bytes.NewReader(h.Source))
	dec.UseNumber()
	_ = dec.Decode(&src)
	flat := map[string]string{}
	flatten("", src, flat)

	e := domain.RuntimeLogEntry{Timestamp: hitTime(flat[s.timeField], h.Sort), Message: flat[s.messageField]}
	if e.Message == "" {
		e.Message = first(flat, messageAlts)
	}
	if e.Message == "" && len(h.Source) > 0 {
		e.Message = truncate(string(h.Source), 4*maxFieldValue)
	}
	e.Severity = domain.LogInfo
	if sev, ok := domain.SeverityFromText(flat[s.levelField]); ok {
		e.Severity = sev
	} else if sev, ok := domain.SeverityFromLine(e.Message); ok {
		e.Severity = sev
	}
	e.Source = first(flat, sourceFields)
	e.TraceID = first(flat, traceIDFields)
	e.Method = flat["http.request.method"]
	e.Path = flat["url.path"]
	if code, err := strconv.Atoi(flat["http.response.status_code"]); err == nil {
		e.StatusCode = code
	}
	keys := make([]string, 0, len(flat))
	for k := range flat {
		if k != s.timeField && k != s.messageField {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	if len(keys) > 0 {
		e.Fields = make(map[string]string, min(len(keys), maxFields))
		for _, k := range keys[:min(len(keys), maxFields)] {
			e.Fields[k] = truncate(flat[k], maxFieldValue)
		}
	}
	return e
}

// flatten writes a document's leaves under dotted keys, so log.level reads
// the same whether the shipper nested it or wrote the dotted name.
func flatten(prefix string, v any, out map[string]string) {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			key := k
			if prefix != "" {
				key = prefix + "." + k
			}
			flatten(key, child, out)
		}
	case []any:
		parts := make([]string, 0, len(t))
		for _, item := range t {
			switch item.(type) {
			case map[string]any, []any:
				raw, _ := json.Marshal(t)
				out[prefix] = string(raw)
				return
			}
			if s, ok := scalar(item); ok {
				parts = append(parts, s)
			}
		}
		if len(parts) > 0 && prefix != "" {
			out[prefix] = strings.Join(parts, ", ")
		}
	default:
		if s, ok := scalar(t); ok && prefix != "" {
			out[prefix] = s
		}
	}
}

func scalar(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	case json.Number:
		return t.String(), true
	case bool:
		return strconv.FormatBool(t), true
	}
	return "", false
}

var timeLayouts = []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05"}

// hitTime reads the time field as written, else the sort value: epoch
// milliseconds for a date field, nanoseconds for date_nanos (no date in
// milliseconds reaches 1e15 before the year 33000).
func hitTime(raw string, sortValues []any) time.Time {
	for _, layout := range timeLayouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC()
		}
	}
	if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return epoch(n)
	}
	if len(sortValues) > 0 {
		if num, ok := sortValues[0].(json.Number); ok {
			if n, err := num.Int64(); err == nil {
				return epoch(n)
			}
		}
	}
	return time.Time{}
}

func epoch(n int64) time.Time {
	if n > 1e15 {
		return time.Unix(0, n).UTC()
	}
	return time.UnixMilli(n).UTC()
}

func first(flat map[string]string, keys []string) string {
	for _, k := range keys {
		if v := flat[k]; v != "" {
			return v
		}
	}
	return ""
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// Discover lists the index pattern's searchable field names: a recipe's
// detect metric for this kind is a field it filters on (service.name).
func (a *Adapter) Discover(ctx context.Context, ds domain.DataSource, secrets map[string]string) (domain.Inventory, error) {
	c, s, err := a.open(ctx, ds, secrets, queryTimeout)
	if err != nil {
		return domain.Inventory{}, err
	}
	defer c.Close()
	caps, err := a.fieldCaps(ctx, c, s, "*")
	if err != nil {
		return domain.Inventory{}, err
	}
	names := make([]string, 0, len(caps.Fields))
	for name := range caps.Fields {
		if !strings.HasPrefix(name, "_") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	inv := domain.Inventory{Metrics: names}
	if len(names) > maxInventory {
		inv.Metrics, inv.Truncated = names[:maxInventory], true
	}
	return inv, nil
}

// LabelValues answers the most frequent values of a field over the last
// hour. A text field cannot be aggregated, so its .keyword sub-field (what
// dynamic mapping adds) is tried in its place.
func (a *Adapter) LabelValues(ctx context.Context, ds domain.DataSource, secrets map[string]string, field string) ([]string, error) {
	if !domain.ValidElasticsearchField(field) {
		return nil, fmt.Errorf("%w: %q is not a field name", port.ErrDataSourceQuery, field)
	}
	c, s, err := a.open(ctx, ds, secrets, queryTimeout)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	since := a.now().Add(-valuesWindow).UnixMilli()
	body := func(f string) map[string]any {
		return map[string]any{
			"size":             0,
			"track_total_hits": false,
			"query":            map[string]any{"range": map[string]any{s.timeField: map[string]any{"gte": since, "format": "epoch_millis"}}},
			"aggs":             map[string]any{"values": map[string]any{"terms": map[string]any{"field": f, "size": maxTermsValues}}},
		}
	}
	res, err := a.search(ctx, c, s, "field values", body(field))
	if errors.Is(err, port.ErrDataSourceQuery) && !strings.HasSuffix(field, ".keyword") {
		res, err = a.search(ctx, c, s, "field values", body(field+".keyword"))
	}
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(res.Aggregations.Values.Buckets))
	for _, b := range res.Aggregations.Values.Buckets {
		if b.KeyAsString != "" {
			out = append(out, b.KeyAsString)
		} else if v, ok := scalar(b.Key); ok {
			out = append(out, v)
		}
	}
	return out, nil
}
