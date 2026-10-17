// Package httpapi is the HTTP plumbing the log and trace kinds (Loki,
// Elasticsearch, Tempo) share: every request is judged by the URL policy and
// pinned to the address it vetted, a redirect may change neither host nor
// scheme, and no returned error carries a secret or the address a host
// resolved to. It also holds their common config fields and the credentials
// those fields turn into.
package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
)

const (
	maxUpstreamText = 300
	maskMinLen      = 4
)

// Endpoint is one API: Base is the URL paths hang off, Headers go on every
// request in order (credentials first, a person's own headers after, so
// theirs win), Masks are the values no returned error may carry, and API
// names it in errors ("Loki").
type Endpoint struct {
	Base    string
	Headers []domain.HeaderLine
	Masks   []string
	API     string
}

type Conn struct {
	base    *url.URL
	ips     []net.IP
	client  *http.Client
	headers []domain.HeaderLine
	masks   []string
	api     string
}

// Open checks ep.Base against policy; the caller closes the answer.
func Open(ctx context.Context, policy urlguard.Policy, ep Endpoint, timeout time.Duration) (*Conn, error) {
	raw := strings.TrimSpace(ep.Base)
	if raw == "" {
		return nil, errors.New("no URL is configured")
	}
	target, err := policy.Validate(ctx, raw)
	if err != nil {
		return nil, Refused(err, raw)
	}
	if target.URL.User != nil {
		return nil, errors.New("the URL must not carry credentials; use the authentication fields")
	}
	client := policy.ClientFor(target, timeout)
	client.CheckRedirect = policy.SameHostRedirect(target.URL)
	// Longest first: a secret inside another (a key's id inside the key file)
	// would otherwise leave the rest of the longer one showing. A header value
	// comes after them, so "Bearer " stays readable around a masked token.
	masks := append([]string(nil), ep.Masks...)
	slices.SortStableFunc(masks, func(a, b string) int { return len(b) - len(a) })
	for _, h := range ep.Headers {
		masks = append(masks, h.Value)
	}
	return &Conn{base: target.URL, ips: target.IPs, client: client, headers: ep.Headers, masks: masks, api: ep.API}, nil
}

func (c *Conn) Close() { c.client.CloseIdleConnections() }

// Request is one call. Path is appended to the base URL's path as written,
// so a segment taken from config or a caller must already be escaped (and
// checked: it must not climb out with "..").
type Request struct {
	Method string
	Path   string
	Query  url.Values
	// JSON, when set, is the body, marshalled.
	JSON  any
	Limit int64
}

type Response struct {
	Status int
	Body   []byte
	// Header is the answer's own, for an API that pages through it (Sentry's
	// Link) or says when to retry.
	Header http.Header
}

func (r Response) OK() bool { return r.Status == http.StatusOK }

// Do answers any HTTP status; only a transport failure or a body over
// r.Limit is an error.
func (c *Conn) Do(ctx context.Context, r Request) (Response, error) {
	rel, err := url.PathUnescape(r.Path)
	if err != nil || strings.ContainsAny(r.Path, "?#") || climbs(rel) {
		return Response{}, errors.New("the request path is not allowed")
	}
	u := *c.base
	rawPath := strings.TrimRight(c.base.EscapedPath(), "/") + r.Path
	path, err := url.PathUnescape(rawPath)
	if err != nil {
		return Response{}, errors.New("the request path is not allowed")
	}
	u.Path, u.RawPath = path, rawPath
	u.RawQuery = r.Query.Encode()
	var body io.Reader
	if r.JSON != nil {
		raw, err := json.Marshal(r.JSON)
		if err != nil {
			return Response{}, fmt.Errorf("%s: the request cannot be encoded", c.api)
		}
		body = bytes.NewReader(raw)
	}
	method := r.Method
	if method == "" {
		method = http.MethodGet
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return Response{}, errors.New("the URL cannot be requested")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "TaskTrooper")
	for _, h := range c.headers {
		req.Header.Set(h.Name, h.Value)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return Response{}, c.transportError(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, r.Limit+1))
	if err != nil {
		return Response{}, c.transportError(err)
	}
	if int64(len(raw)) > r.Limit {
		return Response{}, fmt.Errorf("%s answered more than %d MiB; narrow the window", c.api, r.Limit>>20)
	}
	return Response{Status: resp.StatusCode, Body: raw, Header: resp.Header}, nil
}

func climbs(path string) bool {
	for _, seg := range strings.Split(path, "/") {
		if seg == "." || seg == ".." {
			return true
		}
	}
	return false
}

// StatusError names a refusal: an authentication failure, or the HTTP
// status with whatever message the body carries.
func (c *Conn) StatusError(op string, r Response) error {
	d := c.Detail(r)
	switch {
	case r.Status == http.StatusUnauthorized || r.Status == http.StatusForbidden:
		if d != "" {
			return fmt.Errorf("%s %s: authentication failed (HTTP %d): %s", c.api, op, r.Status, d)
		}
		return fmt.Errorf("%s %s: authentication failed (HTTP %d)", c.api, op, r.Status)
	case d != "":
		return fmt.Errorf("%s %s: HTTP %d: %s", c.api, op, r.Status, d)
	}
	return fmt.Errorf("%s %s: HTTP %d", c.api, op, r.Status)
}

// Detail is the message a refusal's body carries, scrubbed: a JSON error
// (Elasticsearch's {"error": {"reason"}}, {"error": "…"}, {"message"}), or
// the first line of a plain text one (Loki and Tempo answer text).
func (c *Conn) Detail(r Response) string {
	return c.Scrub(detail(r.Body))
}

func detail(raw []byte) string {
	var nested struct {
		Error struct {
			Reason    string `json:"reason"`
			Message   string `json:"message"`
			RootCause []struct {
				Reason string `json:"reason"`
			} `json:"root_cause"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &nested) == nil {
		switch {
		case nested.Error.Reason != "":
			return nested.Error.Reason
		case nested.Error.Message != "":
			return nested.Error.Message
		case len(nested.Error.RootCause) > 0:
			return nested.Error.RootCause[0].Reason
		}
	}
	var flat struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &flat) == nil {
		if flat.Error != "" {
			return flat.Error
		}
		return flat.Message
	}
	text := strings.TrimSpace(string(raw))
	if text == "" || strings.HasPrefix(text, "<") || strings.HasPrefix(text, "{") {
		return ""
	}
	line, _, _ := strings.Cut(text, "\n")
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, line)
}

// transportError drops the request URL net/http puts in its message and
// names the host as the person typed it: the guard dials the address it
// resolved, so a raw dial error would tell them which internal IP a name
// points at.
func (c *Conn) transportError(err error) error {
	if errors.Is(err, urlguard.ErrBlocked) {
		return Refused(err, c.base.String())
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: timed out", c.api)
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		if ue.Timeout() {
			return fmt.Errorf("%s: timed out", c.api)
		}
		err = ue.Err
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "dial" && opErr.Err != nil {
		return fmt.Errorf("%s: cannot connect to %s: %s", c.api, c.base.Host, c.Scrub(opErr.Err.Error()))
	}
	return fmt.Errorf("%s: %s", c.api, c.Scrub(err.Error()))
}

// Scrub bounds upstream text, masks any secret a server echoed back, and
// puts the typed host back where a resolved address of it appears.
func (c *Conn) Scrub(s string) string {
	for _, v := range c.masks {
		if len(v) >= maskMinLen {
			s = strings.ReplaceAll(s, v, "[redacted]")
		}
	}
	if host := c.base.Hostname(); host != "" {
		for _, ip := range c.ips {
			s = strings.ReplaceAll(s, ip.String(), host)
		}
	}
	if r := []rune(s); len(r) > maxUpstreamText {
		s = string(r[:maxUpstreamText]) + "…"
	}
	return s
}

// Refused logs why and answers only that: the reason names the address a
// host resolved to, which is not the caller's to learn.
func Refused(err error, raw string) error {
	log.Warn().Err(err).Str("url", urlguard.LogRaw(raw)).Msg("data source: destination refused")
	return refusedError{}
}

type refusedError struct{}

func (refusedError) Error() string        { return "the URL is not an allowed destination" }
func (refusedError) Is(target error) bool { return target == urlguard.ErrBlocked }

// Config and secret keys the kinds of this package share.
const (
	KeyURL      = "url"
	KeyAuth     = "auth"
	KeyToken    = "token"
	KeyUsername = "username"
	KeyPassword = "password"
	KeyAPIKey   = "api_key"
	KeyHeaders  = "headers"

	AuthNone   = "none"
	AuthBearer = "bearer"
	AuthBasic  = "basic"
	AuthAPIKey = "api_key"
)

// AuthFields are the auth select, the credential fields each option shows,
// and the extra headers field, for a kind offering auths (AuthNone first).
// usernameHelp and passwordHelp say what a hosted service calls them.
func AuthFields(auths []string, usernameHelp, passwordHelp, headersPlaceholder string) []domain.DataSourceField {
	fields := []domain.DataSourceField{{
		Key: KeyAuth, Label: "Authentication", Type: domain.FieldSelect,
		Options: auths, Placeholder: AuthNone,
	}}
	for _, a := range auths {
		switch a {
		case AuthBearer:
			fields = append(fields, domain.DataSourceField{
				Key: KeyToken, Label: "Bearer token", Type: domain.FieldSecret, Required: true, Secret: true,
				ShowWhen: map[string]string{KeyAuth: AuthBearer},
			})
		case AuthBasic:
			fields = append(fields,
				domain.DataSourceField{
					Key: KeyUsername, Label: "Username", Type: domain.FieldText, Required: true,
					ShowWhen: map[string]string{KeyAuth: AuthBasic}, Help: usernameHelp,
				},
				domain.DataSourceField{
					Key: KeyPassword, Label: "Password", Type: domain.FieldSecret, Required: true, Secret: true,
					ShowWhen: map[string]string{KeyAuth: AuthBasic}, Help: passwordHelp,
				})
		case AuthAPIKey:
			fields = append(fields, domain.DataSourceField{
				Key: KeyAPIKey, Label: "API key", Type: domain.FieldSecret, Required: true, Secret: true,
				ShowWhen: map[string]string{KeyAuth: AuthAPIKey},
				Help:     "The encoded API key (base64 of id:key), as Kibana shows it when the key is created.",
			})
		}
	}
	return append(fields, domain.DataSourceField{
		Key: KeyHeaders, Label: "Extra headers", Type: domain.FieldHeaders, Secret: true,
		Placeholder: headersPlaceholder,
		Help:        "One \"Name: value\" per line, sent with every request — a multi-tenant org ID, or an Authorization header of your own. Stored encrypted.",
	})
}

// NewEndpoint turns a source's url, auth and secrets into an Endpoint. The
// encoded basic-auth value is masked beside the password: a server quoting
// the header it refused quotes that, not the password.
func NewEndpoint(api string, ds domain.DataSource, secrets map[string]string) (Endpoint, error) {
	extra, err := domain.ParseHeaderLines(secrets[KeyHeaders])
	if err != nil {
		return Endpoint{}, fmt.Errorf("extra headers: %w", err)
	}
	ep := Endpoint{Base: ds.Config[KeyURL], API: api}
	for _, v := range secrets {
		ep.Masks = append(ep.Masks, v)
	}
	switch ds.Config[KeyAuth] {
	case AuthBearer:
		ep.Headers = append(ep.Headers, domain.HeaderLine{Name: "Authorization", Value: "Bearer " + secrets[KeyToken]})
	case AuthBasic:
		basic := base64.StdEncoding.EncodeToString([]byte(ds.Config[KeyUsername] + ":" + secrets[KeyPassword]))
		ep.Headers = append(ep.Headers, domain.HeaderLine{Name: "Authorization", Value: "Basic " + basic})
		ep.Masks = append(ep.Masks, basic)
	case AuthAPIKey:
		ep.Headers = append(ep.Headers, domain.HeaderLine{Name: "Authorization", Value: "ApiKey " + secrets[KeyAPIKey]})
	}
	ep.Headers = append(ep.Headers, extra...)
	return ep, nil
}
